package generation

import (
	"sort"
	"strings"

	"github.com/jlcoulter/momus/internal/core/ast"
	"github.com/jlcoulter/momus/internal/core/coverage"
	coregen "github.com/jlcoulter/momus/internal/core/generation"
)

// buildResourceCases turns a resource type's obligations into a list of test
// case nodes. At strength 1 it emits one test per requirement. At strength >= 2
// it groups compatible accept obligations into a single shared-payload test
// (selected by greedy set-cover) so pairwise interaction obligations are
// exercised together, while each reject obligation keeps its own test.
// progress, when non-nil, is invoked after each requirement is processed.
func buildResourceCases(reqs []coverage.CoverageRequirement, plan *coverage.CoveragePlan, options coregen.BuildOptions, deps []string, progress func()) []ast.Node {
	// Search/operation/state obligations are separate requests (GET/DELETE/etc.)
	// and do not participate in interaction candidate grouping.
	searchReqs := make([]coverage.CoverageRequirement, 0)
	opReqs := make([]coverage.CoverageRequirement, 0)
	rest := make([]coverage.CoverageRequirement, 0)
	for _, req := range reqs {
		switch req.Domain {
		case coverage.CoverageDomainSearch:
			searchReqs = append(searchReqs, req)
		case coverage.CoverageDomainOperation, coverage.CoverageDomainState:
			opReqs = append(opReqs, req)
		default:
			rest = append(rest, req)
		}
	}

	cases := make([]ast.Node, 0, len(reqs))
	for _, req := range searchReqs {
		cases = append(cases, buildSearchCase(req, options))
		if progress != nil {
			progress()
		}
	}
	for _, req := range opReqs {
		if req.Variant == coverage.CoverageVariantStateCRUDSequence {
			cases = append(cases, buildCRUDCase(req, options))
			continue
		}
		cases = append(cases, buildOperationCase(req, options))
		if progress != nil {
			progress()
		}
	}

	if effectiveStrength(plan, options) < 2 {
		for _, req := range rest {
			if node := buildSingleRequirementCase(req, options, deps); node != nil {
				cases = append(cases, node)
			}
			if progress != nil {
				progress()
			}
		}
		return cases
	}

	selected := selectInteractionCandidates(rest)
	for _, cand := range selected {
		if node := buildCandidateCase(cand, options, deps); node != nil {
			cases = append(cases, node)
		}
		if progress != nil {
			progress()
		}
	}
	return cases
}

// effectiveStrength returns the interaction strength to use: the explicit
// build option if set, otherwise the coverage plan's own strength, defaulting
// to 1.
func effectiveStrength(plan *coverage.CoveragePlan, options coregen.BuildOptions) int {
	if options.Strength >= 1 {
		return options.Strength
	}
	if plan != nil && plan.Strength >= 1 {
		return plan.Strength
	}
	return 1
}

// candidateTest is a single generated test. A group candidate shares one valid
// payload across several accept obligations plus their pairwise interactions;
// a reject candidate tests exactly one negative obligation.
type candidateTest struct {
	resourceType string
	profileURL   string
	positives    []coverage.CoverageRequirement
	interactions []coverage.CoverageRequirement
	negative     *coverage.CoverageRequirement
}

// requiresDistinctContent reports whether an accept obligation can only be
// exercised by content that differs from a single minimal valid payload (e.g. an
// element holding multiple values). When a group candidate contains such an
// obligation, the shared payload must be built from it so the group does not
// claim coverage for content its payload does not exercise.
func requiresDistinctContent(variant coverage.CoverageVariant) bool {
	return variant == coverage.CoverageVariantMultipleValues
}

// ensureDistinctContent mutates body so an accept obligation that needs distinct
// content (e.g. multiple-values) is actually exercised: the element it targets is
// made to hold multiple values. This prevents a shared group payload from claiming
// coverage for content it does not contain.
func ensureDistinctContent(body map[string]any, req coverage.CoverageRequirement) {
	if !requiresDistinctContent(req.Variant) {
		return
	}
	prop := lastPathSegment(req.ElementPath)
	if prop == "" {
		return
	}
	raw, ok := body[prop]
	if !ok {
		return
	}
	switch v := raw.(type) {
	case []any:
		if len(v) >= 2 {
			return
		}
		if len(v) == 1 {
			body[prop] = []any{v[0], cloneValue(v[0])}
		}
	default:
		body[prop] = []any{raw, cloneValue(raw)}
	}
}

// cloneValue deep-copies a JSON-like value (map, slice, or scalar) so a repeated
// element can be duplicated without aliasing the original.
func cloneValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = cloneValue(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = cloneValue(val)
		}
		return out
	default:
		return v
	}
}

// lastPathSegment returns the final dot-separated segment of a FHIR element path
// (e.g. "Patient.name" -> "name"), or "" when there is no dot.
func lastPathSegment(path string) string {
	idx := strings.LastIndex(path, ".")
	if idx < 0 {
		return ""
	}
	return path[idx+1:]
}

// coversBase returns the base (non-interaction) requirement IDs a candidate
// satisfies in a single payload.
func (c candidateTest) coversBase() map[string]struct{} {
	set := make(map[string]struct{}, len(c.positives)+1)
	if c.negative != nil {
		set[c.negative.ID] = struct{}{}
	}
	for _, p := range c.positives {
		set[p.ID] = struct{}{}
	}
	return set
}

// selectInteractionCandidates partitions a resource type's obligations and uses
// greedy set-cover to select a near-minimal set of tests covering every base
// obligation. Each profile with accept obligations yields one group candidate
// (covering all its accepts plus the interactions within them); each reject
// obligation yields its own candidate.
func selectInteractionCandidates(reqs []coverage.CoverageRequirement) []candidateTest {
	// Partition obligations per profile.
	type profileGroup struct {
		positives    []coverage.CoverageRequirement
		negatives    []coverage.CoverageRequirement
		interactions []coverage.CoverageRequirement
	}
	byProfile := make(map[string]*profileGroup)
	profileOrder := make([]string, 0)
	seenBaseProfile := make(map[string]struct{})
	baseIDs := make(map[string]struct{})
	for _, req := range reqs {
		if req.Domain == coverage.CoverageDomainInteraction {
			g := byProfile[req.ProfileURL]
			if g == nil {
				g = &profileGroup{}
				byProfile[req.ProfileURL] = g
			}
			g.interactions = append(g.interactions, req)
			continue
		}
		baseIDs[req.ID] = struct{}{}
		g := byProfile[req.ProfileURL]
		if g == nil {
			g = &profileGroup{}
			byProfile[req.ProfileURL] = g
		}
		if _, seen := seenBaseProfile[req.ProfileURL]; !seen {
			seenBaseProfile[req.ProfileURL] = struct{}{}
			profileOrder = append(profileOrder, req.ProfileURL)
		}
		if req.Variant.IsReject() {
			g.negatives = append(g.negatives, req)
		} else {
			g.positives = append(g.positives, req)
		}
	}

	// Build candidate tests (deterministic order for a stable set-cover result).
	candidates := make([]candidateTest, 0)
	for _, profileURL := range profileOrder {
		g := byProfile[profileURL]
		positiveSet := make(map[string]struct{}, len(g.positives))
		for _, p := range g.positives {
			positiveSet[p.ID] = struct{}{}
		}
		interactions := make([]coverage.CoverageRequirement, 0)
		for _, in := range g.interactions {
			_, aOK := positiveSet[in.PairA]
			_, bOK := positiveSet[in.PairB]
			if aOK && bOK {
				interactions = append(interactions, in)
			}
		}
		sort.Slice(g.positives, func(i, j int) bool { return g.positives[i].ID < g.positives[j].ID })
		sort.Slice(g.negatives, func(i, j int) bool { return g.negatives[i].ID < g.negatives[j].ID })
		sort.Slice(interactions, func(i, j int) bool { return interactions[i].ID < interactions[j].ID })

		if len(g.positives) > 0 {
			resourceType := g.positives[0].ResourceType
			candidates = append(candidates, candidateTest{
				resourceType: resourceType,
				profileURL:   profileURL,
				positives:    g.positives,
				interactions: interactions,
			})
		}
		for i := range g.negatives {
			neg := g.negatives[i]
			candidates = append(candidates, candidateTest{
				resourceType: neg.ResourceType,
				profileURL:   neg.ProfileURL,
				negative:     &neg,
			})
		}
	}

	return greedySetCover(baseIDs, candidates)
}

// greedySetCover selects a near-minimal subset of candidates whose covers span
// the universe, repeatedly choosing the candidate that covers the most
// uncovered obligations. The cover is exact for this structure (each base
// obligation appears in exactly one candidate).
func greedySetCover(universe map[string]struct{}, candidates []candidateTest) []candidateTest {
	covered := make(map[string]struct{})
	selected := make([]candidateTest, 0)
	for len(covered) < len(universe) {
		bestIdx := -1
		bestNew := 0
		for i, cand := range candidates {
			newCount := 0
			for id := range cand.coversBase() {
				if _, ok := covered[id]; !ok {
					newCount++
				}
			}
			if newCount > bestNew {
				bestNew = newCount
				bestIdx = i
			}
		}
		if bestIdx < 0 || bestNew == 0 {
			break
		}
		selected = append(selected, candidates[bestIdx])
		for id := range candidates[bestIdx].coversBase() {
			covered[id] = struct{}{}
		}
	}
	return selected
}

// buildCandidateCase turns a selected candidate into a test node: one request
// plus an assert per obligation it satisfies (accepts and interactions share
// one valid payload; a reject asserts rejection).
func buildCandidateCase(cand candidateTest, options coregen.BuildOptions, deps []string) ast.Node {
	var seed coverage.CoverageRequirement
	if cand.negative != nil {
		seed = *cand.negative
	} else {
		seed = cand.positives[0]
	}
	requestID := coregen.RequirementResourceID(seed)
	profiles := coregen.OrderedProfilesForResource(seed.ResourceType, seed.ProfileURL, options.PreferredProfileURLsByResource)
	primaryProfile := coregen.FirstProfileURL(profiles)
	body, applied := options.Builder.BuildBody(seed, requestID, profiles, primaryProfile, deps, options.Exhaustive)

	// The shared payload must actually exercise every accept obligation the group
	// claims to cover. Obligations that need distinct content (e.g. multiple-values)
	// are ensured to hold multiple values so coverage is not claimed for content the
	// payload does not exercise.
	if cand.negative == nil {
		for _, p := range cand.positives {
			ensureDistinctContent(body, p)
		}
	}

	request := &ast.Request{
		Method: "PUT",
		URL:    coregen.JoinInstanceURL(coregen.BaseURLForMethod(options, "PUT"), seed.ResourceType, requestID),
		Headers: map[string]string{
			"Content-Type":           "application/fhir+json",
			"X-Momus-Requirement-ID": seed.ID,
		},
		Body: body,
	}

	seq := &ast.Sequence{Steps: []ast.Node{request}}
	if cand.negative != nil {
		// A negative candidate whose target element is absent from the payload has
		// no concrete violation to assert; skip it rather than emit a reject test
		// a conformant server would accept.
		if !applied {
			return nil
		}
		seq.Steps = append(seq.Steps, buildRequirementAssert(*cand.negative))
		return seq
	}

	for _, p := range cand.positives {
		seq.Steps = append(seq.Steps, buildRequirementAssert(p))
	}
	for _, in := range cand.interactions {
		seq.Steps = append(seq.Steps, buildInteractionAssert(in))
	}
	return seq
}

// buildInteractionAssert builds the assertion for a pairwise interaction
// obligation: the shared payload is valid for both source obligations, so it is
// expected to be accepted. The trace exposes the interaction domain so the
// evaluator can measure it independently.
func buildInteractionAssert(req coverage.CoverageRequirement) *ast.Assert {
	return &ast.Assert{
		Description:   "server accepts pairwise interaction payload",
		RequirementID: req.ID,
		Expression:    "status in [200,201]",
		Trace: &ast.Trace{
			ProfileURL:   req.ProfileURL,
			ResourceType: req.ResourceType,
			ElementPath:  req.ElementPath,
			Domain:       string(req.Domain),
			Variant:      string(req.Variant),
			Expected:     "accept",
		},
	}
}
