package fhircoverage

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/jlcoulter/momus/internal/core/constraint"
	"github.com/jlcoulter/momus/internal/core/coverage"
	"github.com/jlcoulter/momus/internal/fhir/constraintderive"
	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
	"github.com/jlcoulter/momus/internal/fhir/search"
)

var defaultLowValueSegments = map[string]struct{}{
	"meta":          {},
	"text":          {},
	"contained":     {},
	"implicitRules": {},
	"language":      {},
}

// DerivePlan derives contractual coverage obligations from the constraint
// model using the provided options.
//
// The constraint model is the single source of truth: every element-derived
// constraint (cardinality, datatype, terminology, invariant, reference) is
// mapped to its coverage obligations. Required slices produce structure
// obligations, which the constraint model does not yet model.
func DerivePlan(r *registry.Registry, options coverage.DeriveOptions) (*coverage.CoveragePlan, error) {
	if r == nil {
		return nil, errors.New("registry is required")
	}

	options = normalizeOptions(options)

	constraints, err := constraintderive.DeriveScoped(r)
	if err != nil {
		return nil, err
	}

	profiles := r.ScopedStructureDefinitions()
	if len(profiles) == 0 {
		return nil, errors.New("no structure definitions available for coverage derivation")
	}
	sort.Slice(profiles, func(i, j int) bool {
		return profiles[i].URL < profiles[j].URL
	})

	plan := newCoveragePlan()
	seen := make(map[string]struct{})
	foundDerivableElements := false

	includeResources := toSet(options.IncludeResourceTypes)
	includeProfiles := toSet(options.IncludeProfileURLs)
	inScopeResourceTypes := make(map[string]struct{})
	profileURLByType := make(map[string]string)

	// First pass: apply element-level scoping/pruning options and remember
	// which elements are derivable, together with their dependency targets.
	derivable := make(map[string]derivableElement)
	for _, profile := range profiles {
		if profile == nil {
			continue
		}
		if profile.Kind != "" && !strings.EqualFold(profile.Kind, "resource") {
			trackPruned(plan, coverage.PruneReasonResourceFiltered)
			continue
		}
		if len(includeResources) > 0 {
			if _, ok := includeResources[strings.ToLower(profile.Type)]; !ok {
				trackPruned(plan, coverage.PruneReasonResourceFiltered)
				continue
			}
		}
		if len(includeProfiles) > 0 {
			if _, ok := includeProfiles[strings.ToLower(profile.URL)]; !ok {
				trackPruned(plan, coverage.PruneReasonProfileFiltered)
				continue
			}
		}
		if profile.Type != "" {
			inScopeResourceTypes[profile.Type] = struct{}{}
			if profileURLByType[profile.Type] == "" {
				profileURLByType[profile.Type] = profile.URL
			}
		}

		elements, err := r.ResolveElements(profile.URL)
		if err != nil {
			continue
		}
		excludedExtensionPrefixes := extensionSlicePrefixes(elements, options.ExcludeExtensionURLs)
		for _, element := range elements {
			if isExcludedExtensionElement(element, excludedExtensionPrefixes) {
				trackPruned(plan, coverage.PruneReasonExtensionURL)
				continue
			}
			ok, reason := isDerivableElement(element, options)
			if !ok {
				trackPruned(plan, reason)
				continue
			}
			// Only elements that survive pruning count as derivable. Setting this
			// before pruning would let a plan that prunes every element (e.g.
			// MustSupportOnly with no must-support elements) pass the guard below
			// and return only operation/state/search obligations with zero element
			// coverage.
			if strings.Contains(element.Path, ".") {
				foundDerivableElements = true
			}
			key := elementKey(profile.URL, element.Path, element.SliceName)
			de := derivableElement{
				element: element,
				targets: collectDependencyTargets(r, element),
			}
			derivable[key] = de

			// Structure obligations (required slices) are derived directly from
			// element declarations until the constraint model models slicing.
			if element.SliceName != "" && element.Min > 0 {
				synthetic := constraint.Constraint{
					ID:           constraint.ID(profile.URL, element.Path, "structure"),
					ProfileURL:   profile.URL,
					ResourceType: profile.Type,
					ElementPath:  element.Path,
				}
				addRequirement(plan, seen, synthetic, de, coverage.CoverageDomainStructure, coverage.CoverageVariantStructureSlicePresent)
			}
		}
	}

	if !foundDerivableElements {
		return nil, errors.New("no derivable profile elements found in structure definitions")
	}

	// Second pass: map each element-derived constraint to its obligations.
	for _, c := range constraints {
		if !isElementConstraintKind(c.Kind) {
			continue
		}
		de, ok := derivable[elementKey(c.ProfileURL, c.ElementPath, "")]
		if !ok {
			continue
		}
		appendObligations(plan, seen, c, de)
	}

	// Search constraints produce search coverage obligations for every in-scope
	// resource type. When the server's CapabilityStatement declares search
	// parameters (CapabilitySearchCodes), only those declared codes are included.
	// Universal parameters like _content and _parameters are excluded unless the
	// server explicitly declares them; with no capability scope (nil
	// CapabilitySearchCodes) they are excluded for every type. When
	// IncludeUniversalSearchParams is set, universal parameters are included for
	// every in-scope type regardless of capability declaration.
	scopedTypes := sortedSetKeys(inScopeResourceTypes)
	searchCodesByType := make(map[string][]string)
	for _, c := range constraints {
		if c.Kind != constraint.KindSearch {
			continue
		}
		if isUniversalSearchBase(c.ResourceType) {
			// Universal parameters are only included when the server explicitly
			// declares them or when IncludeUniversalSearchParams opts into full
			// coverage of the default FHIR search parameters; with no capability
			// scope they are excluded for every type.
			if options.CapabilitySearchCodes == nil && !options.IncludeUniversalSearchParams {
				continue
			}
			for _, rt := range scopedTypes {
				if !options.IncludeUniversalSearchParams && !isSearchCodeAllowed(rt, c.SearchCode, options.CapabilitySearchCodes) {
					continue
				}
				appendSearchObligations(plan, seen, c, rt, options)
				searchCodesByType[rt] = appendUniqueString(searchCodesByType[rt], c.SearchCode)
			}
			continue
		}
		if _, ok := inScopeResourceTypes[c.ResourceType]; ok {
			if !isSearchCodeAllowed(c.ResourceType, c.SearchCode, options.CapabilitySearchCodes) {
				continue
			}
			appendSearchObligations(plan, seen, c, c.ResourceType, options)
			searchCodesByType[c.ResourceType] = appendUniqueString(searchCodesByType[c.ResourceType], c.SearchCode)
		}
	}

	// Implicit universal search parameters (_include, _revinclude, _has,
	// _sort, ...) have no SearchParameter resource in R4 core, so they never
	// become KindSearch constraints. When IncludeUniversalSearchParams opts
	// into full universal coverage, synthesize their obligations directly from
	// the known implicit list.
	if options.IncludeUniversalSearchParams {
		for _, rt := range scopedTypes {
			for _, imp := range search.ImplicitUniversal() {
				if !isSearchCodeAllowed(rt, imp.Code, options.CapabilitySearchCodes) {
					continue
				}
				appendImplicitSearchObligations(plan, seen, rt, imp)
			}
		}
	}

	// _include and _revinclude obligations are derived from the server's
	// CapabilityStatement-declared include values (SearchInclude /
	// SearchRevInclude), not from SearchParameter resources.
	if options.IncludeSearchIncludes {
		for _, rt := range scopedTypes {
			for _, inc := range r.SearchIncludesForType(rt) {
				appendIncludeObligation(plan, seen, rt, inc, coverage.CoverageVariantSearchInclude, r)
			}
			for _, rev := range r.SearchRevIncludesForType(rt) {
				appendIncludeObligation(plan, seen, rt, rev, coverage.CoverageVariantSearchRevInclude, r)
			}
		}
	}

	// Chaining obligations follow reference-type search parameters through
	// their target types. They are opt-in and only at interaction strength 2
	// (like combinations) to avoid combinatorial explosion by default.
	if options.IncludeSearchChains && options.Strength >= 2 {
		for _, rt := range scopedTypes {
			appendChainObligations(plan, seen, rt, r, options)
		}
	}

	// Pairwise search-parameter combinations are opt-in at interaction strength 2
	// (like interaction coverage) to avoid combinatorial explosion by default.
	if options.Strength >= 2 {
		for _, rt := range scopedTypes {
			appendSearchCombinationObligations(plan, seen, rt, profileURLByType[rt], searchCodesByType[rt])
		}
	}

	// Operation and state coverage obligations for every in-scope resource type.
	for _, rt := range scopedTypes {
		appendOperationObligations(plan, seen, rt, profileURLByType[rt])
		appendStateObligations(plan, seen, rt, profileURLByType[rt])
	}

	// Custom operations declared by in-scope resource types' CapabilityStatements.
	for _, c := range constraints {
		if c.Kind != constraint.KindOperation {
			continue
		}
		if _, ok := inScopeResourceTypes[c.ResourceType]; ok {
			req := coverage.CoverageRequirement{
				ID:            fmt.Sprintf("operation|%s|%s", c.ResourceType, c.OperationName),
				ProfileURL:    profileURLByType[c.ResourceType],
				ResourceType:  c.ResourceType,
				Domain:        coverage.CoverageDomainOperation,
				Variant:       coverage.CoverageVariantOperationCustom,
				OperationName: c.OperationName,
			}
			req.Description = coverage.DescribeCoverageRequirement(req)
			req.HumanID = coverage.HumanID(req)
			appendRequirement(plan, seen, req)
		}
	}

	sort.Slice(plan.Requirements, func(i, j int) bool {
		return plan.Requirements[i].ID < plan.Requirements[j].ID
	})

	// Apply the domain filter before interaction derivation so pairwise
	// interaction obligations are only derived from in-scope domains.
	filterRequirements(plan, options)

	// Interaction strength 2 adds pairwise interaction obligations: pairs of
	// accept obligations on the same profile that must be satisfiable together
	// in a single payload. These are appended as first-class requirements in
	// the interaction domain so the evaluator can measure them.
	if options.Strength >= 2 {
		plan.Strength = options.Strength
		deriveInteractionObligations(plan, seen)
		sort.Slice(plan.Requirements, func(i, j int) bool {
			return plan.Requirements[i].ID < plan.Requirements[j].ID
		})
		// Apply the variant filter to the final list (interaction obligations
		// are never excluded by domain, but variant exclusions still apply).
		filterRequirements(plan, options)
	}

	plan.Summary.TotalRequirements = len(plan.Requirements)

	return plan, nil
}

// filterRequirements removes requirements excluded by the IncludeDomains and
// ExcludeVariants options and rebuilds the plan's summary counts to match the
// filtered set. It is a no-op when neither option is set.
func filterRequirements(plan *coverage.CoveragePlan, options coverage.DeriveOptions) {
	if len(options.IncludeDomains) == 0 && len(options.ExcludeVariants) == 0 {
		return
	}
	includeDomains := make(map[coverage.CoverageDomain]struct{}, len(options.IncludeDomains))
	for _, d := range options.IncludeDomains {
		includeDomains[d] = struct{}{}
	}
	excludeVariants := make(map[coverage.CoverageVariant]struct{}, len(options.ExcludeVariants))
	for _, v := range options.ExcludeVariants {
		excludeVariants[v] = struct{}{}
	}

	filtered := make([]coverage.CoverageRequirement, 0, len(plan.Requirements))
	for _, req := range plan.Requirements {
		if len(includeDomains) > 0 {
			if _, ok := includeDomains[req.Domain]; !ok {
				continue
			}
		}
		if len(excludeVariants) > 0 {
			if _, ok := excludeVariants[req.Variant]; ok {
				continue
			}
		}
		filtered = append(filtered, req)
	}
	plan.Requirements = filtered

	// Rebuild summary counts from the filtered set.
	plan.Summary.ByDomain = make(map[coverage.CoverageDomain]int)
	plan.Summary.ByResourceType = make(map[string]int)
	plan.Summary.ByVariant = make(map[coverage.CoverageVariant]int)
	for _, req := range plan.Requirements {
		plan.Summary.ByDomain[req.Domain]++
		plan.Summary.ByResourceType[req.ResourceType]++
		plan.Summary.ByVariant[req.Variant]++
	}
}

// deriveInteractionObligations appends pairwise interaction obligations between
// accept (non-reject) base requirements that share a resource type and profile.
// Interaction obligations are derived only over accept obligations: a negative
// mutation exercises exactly one constraint, so it cannot coexist with another
// obligation in the same payload.
func deriveInteractionObligations(plan *coverage.CoveragePlan, seen map[string]struct{}) {
	type groupKey struct {
		resourceType string
		profileURL   string
	}
	groups := make(map[groupKey][]coverage.CoverageRequirement)
	for _, req := range plan.Requirements {
		if isNonElementDomain(req.Domain) || req.Variant.IsReject() {
			continue
		}
		key := groupKey{req.ResourceType, req.ProfileURL}
		groups[key] = append(groups[key], req)
	}

	keys := make([]groupKey, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].resourceType != keys[j].resourceType {
			return keys[i].resourceType < keys[j].resourceType
		}
		return keys[i].profileURL < keys[j].profileURL
	})

	for _, key := range keys {
		reqs := groups[key]
		sort.Slice(reqs, func(i, j int) bool {
			return reqs[i].ID < reqs[j].ID
		})
		for i := 0; i < len(reqs); i++ {
			for j := i + 1; j < len(reqs); j++ {
				a, b := reqs[i], reqs[j]
				id := "interaction|" + a.ID + "++" + b.ID
				interaction := coverage.CoverageRequirement{
					ID:           id,
					ProfileURL:   a.ProfileURL,
					ResourceType: a.ResourceType,
					ElementPath:  a.ElementPath + " ++ " + b.ElementPath,
					Domain:       coverage.CoverageDomainInteraction,
					Variant:      coverage.CoverageVariantInteractionPair,
					PairA:        a.ID,
					PairB:        b.ID,
				}
				interaction.Description = coverage.DescribeCoverageRequirement(interaction)
				interaction.HumanID = coverage.HumanID(interaction)
				appendRequirement(plan, seen, interaction)
				plan.Interactions = append(plan.Interactions, coverage.InteractionRequirement{
					ID:           id,
					ProfileURL:   a.ProfileURL,
					ResourceType: a.ResourceType,
					RequirementA: a.ID,
					RequirementB: b.ID,
				})
				plan.Summary.Interactions++
			}
		}
	}
}

func newCoveragePlan() *coverage.CoveragePlan {
	return &coverage.CoveragePlan{
		Requirements: make([]coverage.CoverageRequirement, 0),
		Strength:     1,
		Summary: coverage.CoverageSummary{
			ByDomain:       make(map[coverage.CoverageDomain]int),
			ByResourceType: make(map[string]int),
			ByVariant:      make(map[coverage.CoverageVariant]int),
			PrunedByReason: make(map[coverage.PruneReason]int),
		},
	}
}

func elementKey(profileURL, elementPath, sliceName string) string {
	return profileURL + "\x00" + elementPath + "\x00" + sliceName
}

type derivableElement struct {
	element model.ElementDefinition
	targets []string
}

// isNonElementDomain reports whether a coverage domain is derived independently
// of resource-element constraints and does not participate in pairwise
// interaction grouping.
func isNonElementDomain(domain coverage.CoverageDomain) bool {
	switch domain {
	case coverage.CoverageDomainInteraction, coverage.CoverageDomainSearch, coverage.CoverageDomainOperation, coverage.CoverageDomainState:
		return true
	default:
		return false
	}
}

func isElementConstraintKind(k constraint.Kind) bool {
	switch k {
	case constraint.KindCardinality,
		constraint.KindDatatype,
		constraint.KindTerminology,
		constraint.KindInvariant,
		constraint.KindReference:
		return true
	default:
		return false
	}
}

// appendSearchObligations adds the search coverage obligations for a search
// constraint applied to a single resource type. When IncludeSearchModifiers is
// set, it also derives a search-valid obligation per valid FHIR search modifier
// supported by the parameter's type.
func appendSearchObligations(plan *coverage.CoveragePlan, seen map[string]struct{}, c constraint.Constraint, resourceType string, options coverage.DeriveOptions) {
	if strings.TrimSpace(c.SearchCode) == "" {
		return
	}
	for _, variant := range []coverage.CoverageVariant{
		coverage.CoverageVariantSearchValid,
		coverage.CoverageVariantSearchNoResults,
		coverage.CoverageVariantSearchInvalidValue,
		coverage.CoverageVariantSearchMultipleResults,
		coverage.CoverageVariantSearchInvalidModifier,
	} {
		req := coverage.CoverageRequirement{
			ID:           fmt.Sprintf("search|%s|%s|%s", resourceType, c.SearchCode, variant),
			ConstraintID: c.ID,
			ResourceType: resourceType,
			Domain:       coverage.CoverageDomainSearch,
			Variant:      variant,
			SearchCode:   c.SearchCode,
		}
		req.Description = coverage.DescribeCoverageRequirement(req)
		req.HumanID = coverage.HumanID(req)
		appendRequirement(plan, seen, req)
	}
	if options.IncludeSearchModifiers {
		for _, mod := range search.ModifiersForType(c.SearchType) {
			modReq := coverage.CoverageRequirement{
				ID:             fmt.Sprintf("search|%s|%s:%s|%s", resourceType, c.SearchCode, mod, coverage.CoverageVariantSearchValid),
				ConstraintID:   c.ID,
				ResourceType:   resourceType,
				Domain:         coverage.CoverageDomainSearch,
				Variant:        coverage.CoverageVariantSearchValid,
				SearchCode:     c.SearchCode,
				SearchModifier: mod,
			}
			modReq.Description = coverage.DescribeCoverageRequirement(modReq)
			modReq.HumanID = coverage.HumanID(modReq)
			appendRequirement(plan, seen, modReq)
		}
	}
}

// appendImplicitSearchObligations adds obligations for an implicit universal
// search parameter (one with no SearchParameter resource in R4 core) applied to
// a single resource type. _include/_revinclude/_has get their dedicated
// variants; the remaining result-modifier parameters (_sort, _count, _summary,
// ...) are covered by a valid search returning no results.
func appendImplicitSearchObligations(plan *coverage.CoveragePlan, seen map[string]struct{}, resourceType string, imp model.SearchParameter) {
	var variants []coverage.CoverageVariant
	switch imp.Code {
	case "_include":
		variants = []coverage.CoverageVariant{coverage.CoverageVariantSearchInclude}
	case "_revinclude":
		variants = []coverage.CoverageVariant{coverage.CoverageVariantSearchRevInclude}
	case "_has":
		variants = []coverage.CoverageVariant{coverage.CoverageVariantSearchChaining}
	default:
		variants = []coverage.CoverageVariant{
			coverage.CoverageVariantSearchValid,
			coverage.CoverageVariantSearchNoResults,
		}
	}
	for _, variant := range variants {
		req := coverage.CoverageRequirement{
			ID:           fmt.Sprintf("search|%s|%s|%s", resourceType, imp.Code, variant),
			ResourceType: resourceType,
			Domain:       coverage.CoverageDomainSearch,
			Variant:      variant,
			SearchCode:   imp.Code,
		}
		req.Description = coverage.DescribeCoverageRequirement(req)
		req.HumanID = coverage.HumanID(req)
		appendRequirement(plan, seen, req)
	}
}

// appendIncludeObligation adds an _include or _revinclude obligation for a
// server-declared include value of the form "<ResourceType>:<searchParam>" (or
// "*"). The type that appears in the returned Bundle is the target type of the
// reference search parameter the include names (e.g. _include=Patient:organization
// includes Organization resources). SearchTargetType is set to that included
// type so seed generation and assertions know what the Bundle must contain.
func appendIncludeObligation(plan *coverage.CoveragePlan, seen map[string]struct{}, resourceType, includeValue string, variant coverage.CoverageVariant, r *registry.Registry) {
	if includeValue == "" {
		return
	}
	baseType, targetCode := parseIncludeValue(includeValue)
	includedType := baseType
	if variant == coverage.CoverageVariantSearchInclude && targetCode != "" && r != nil {
		// For _include, the referenced resources' type is the reference
		// parameter's target type, not the base type the parameter is defined
		// on (e.g. _include=Patient:organization includes Organization
		// resources). For _revinclude the included type is the base type that
		// references the subject (e.g. _revinclude=Observation:patient includes
		// Observation resources).
		if sp, ok := r.SearchParameter(baseType, targetCode); ok && len(sp.Target) > 0 {
			includedType = sp.Target[0]
		}
	}
	req := coverage.CoverageRequirement{
		ID:               fmt.Sprintf("search|%s|%s=%s|%s", resourceType, includeParamCode(variant), includeValue, variant),
		ResourceType:     resourceType,
		Domain:           coverage.CoverageDomainSearch,
		Variant:          variant,
		SearchCode:       includeParamCode(variant),
		SearchTargetType: includedType,
		SearchTargetCode: targetCode,
	}
	req.Description = coverage.DescribeCoverageRequirement(req)
	req.HumanID = coverage.HumanID(req)
	appendRequirement(plan, seen, req)
}

func includeParamCode(variant coverage.CoverageVariant) string {
	if variant == coverage.CoverageVariantSearchRevInclude {
		return "_revinclude"
	}
	return "_include"
}

// parseIncludeValue parses a CapabilityStatement include value into its
// resource type and search parameter code. FHIR uses "<ResourceType>:<param>"
// (and the equivalent dot-separated "<ResourceType>.<param>" form seen in some
// CapabilityStatements). A wildcard "*" yields empty parts.
func parseIncludeValue(value string) (string, string) {
	sep := ":"
	if !strings.Contains(value, ":") && strings.Contains(value, ".") {
		sep = "."
	}
	parts := strings.SplitN(value, sep, 2)
	targetType := strings.TrimSpace(parts[0])
	if targetType == "*" {
		targetType = ""
	}
	targetCode := ""
	if len(parts) > 1 {
		targetCode = strings.TrimSpace(parts[1])
	}
	return targetType, targetCode
}

// appendChainObligations derives chaining obligations for a resource type from
// its reference-type search parameters, following their target types up to two
// levels deep.
func appendChainObligations(plan *coverage.CoveragePlan, seen map[string]struct{}, resourceType string, r *registry.Registry, options coverage.DeriveOptions) {
	for _, ch := range search.ChainsWithDepth(r, resourceType, 2) {
		firstSeg := strings.SplitN(ch.Path, ".", 2)[0]
		if !isSearchCodeAllowed(resourceType, firstSeg, options.CapabilitySearchCodes) {
			continue
		}
		req := coverage.CoverageRequirement{
			ID:               fmt.Sprintf("search|%s|%s|%s", resourceType, ch.Path, coverage.CoverageVariantSearchChaining),
			ResourceType:     resourceType,
			Domain:           coverage.CoverageDomainSearch,
			Variant:          coverage.CoverageVariantSearchChaining,
			SearchCode:       ch.Path,
			SearchTargetType: ch.TargetType,
			SearchTargetCode: ch.TargetCode,
		}
		req.Description = coverage.DescribeCoverageRequirement(req)
		req.HumanID = coverage.HumanID(req)
		appendRequirement(plan, seen, req)
	}
}

// appendOperationObligations adds the CRUD-style operation obligations (read,
// update, delete, history) for a resource type.
func appendOperationObligations(plan *coverage.CoveragePlan, seen map[string]struct{}, resourceType, profileURL string) {
	for _, variant := range []coverage.CoverageVariant{
		coverage.CoverageVariantOperationRead,
		coverage.CoverageVariantOperationUpdate,
		coverage.CoverageVariantOperationPatch,
		coverage.CoverageVariantOperationDelete,
		coverage.CoverageVariantOperationHistory,
	} {
		req := coverage.CoverageRequirement{
			ID:           fmt.Sprintf("operation|%s|%s", resourceType, variant),
			ProfileURL:   profileURL,
			ResourceType: resourceType,
			Domain:       coverage.CoverageDomainOperation,
			Variant:      variant,
		}
		req.Description = coverage.DescribeCoverageRequirement(req)
		req.HumanID = coverage.HumanID(req)
		appendRequirement(plan, seen, req)
	}
}

// appendStateObligations adds the negative state-transition obligations for a
// resource type (reading/deleting a nonexistent resource).
func appendStateObligations(plan *coverage.CoveragePlan, seen map[string]struct{}, resourceType, profileURL string) {
	for _, variant := range []coverage.CoverageVariant{
		coverage.CoverageVariantStateCRUDSequence,
		coverage.CoverageVariantStateReadNonexistent,
		coverage.CoverageVariantStateDeleteNonexistent,
	} {
		req := coverage.CoverageRequirement{
			ID:           fmt.Sprintf("state|%s|%s", resourceType, variant),
			ProfileURL:   profileURL,
			ResourceType: resourceType,
			Domain:       coverage.CoverageDomainState,
			Variant:      variant,
		}
		req.Description = coverage.DescribeCoverageRequirement(req)
		req.HumanID = coverage.HumanID(req)
		appendRequirement(plan, seen, req)
	}
}

// appendSearchCombinationObligations adds a pairwise combination obligation for
// every pair of distinct search parameters of a resource type.
func appendSearchCombinationObligations(plan *coverage.CoveragePlan, seen map[string]struct{}, resourceType, profileURL string, codes []string) {
	codes = uniqueSortedStrings(codes)
	for i := 0; i < len(codes); i++ {
		for j := i + 1; j < len(codes); j++ {
			req := coverage.CoverageRequirement{
				ID:           fmt.Sprintf("search|%s|%s++%s|%s", resourceType, codes[i], codes[j], coverage.CoverageVariantSearchCombination),
				ProfileURL:   profileURL,
				ResourceType: resourceType,
				Domain:       coverage.CoverageDomainSearch,
				Variant:      coverage.CoverageVariantSearchCombination,
				SearchCode:   codes[i],
				SearchCodeB:  codes[j],
			}
			req.Description = coverage.DescribeCoverageRequirement(req)
			req.HumanID = coverage.HumanID(req)
			appendRequirement(plan, seen, req)
		}
	}
}

func uniqueSortedStrings(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, v := range values {
		if v != "" {
			set[v] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func appendObligations(plan *coverage.CoveragePlan, seen map[string]struct{}, c constraint.Constraint, de derivableElement) {
	switch c.Kind {
	case constraint.KindCardinality:
		addRequirement(plan, seen, c, de, coverage.CoverageDomainCardinality, coverage.CoverageVariantValidMin)
		if de.element.Min > 0 {
			addRequirement(plan, seen, c, de, coverage.CoverageDomainCardinality, coverage.CoverageVariantMissingRequired)
		}
		if allowsMultiple(de.element.Max) {
			addRequirement(plan, seen, c, de, coverage.CoverageDomainCardinality, coverage.CoverageVariantMultipleValues)
		}
	case constraint.KindDatatype:
		for _, variant := range []coverage.CoverageVariant{
			coverage.CoverageVariantDatatypeValid,
			coverage.CoverageVariantDatatypeInvalidLexical,
			coverage.CoverageVariantDatatypeWrongJSONType,
			coverage.CoverageVariantDatatypeNull,
		} {
			addRequirement(plan, seen, c, de, coverage.CoverageDomainDatatype, variant)
		}
	case constraint.KindTerminology:
		for _, variant := range []coverage.CoverageVariant{
			coverage.CoverageVariantTerminologyValid,
			coverage.CoverageVariantTerminologyInvalid,
			coverage.CoverageVariantTerminologyAbsent,
		} {
			addRequirement(plan, seen, c, de, coverage.CoverageDomainTerminology, variant)
		}
	case constraint.KindInvariant:
		// Only the positive satisfies obligation is derived. A concrete
		// invariant-violates payload cannot be constructed reliably (that would
		// require evaluating the FHIRPath invariant expression, which is not yet
		// implemented), and nulling the subject element does not break most
		// invariants, so deriving a violates obligation would generate a test that
		// a conformant server accepts and the reject assertion would falsely fail.
		addRequirement(plan, seen, c, de, coverage.CoverageDomainInvariant, coverage.CoverageVariantInvariantSatisfies)
	case constraint.KindReference:
		for _, variant := range []coverage.CoverageVariant{
			coverage.CoverageVariantReferenceValid,
			coverage.CoverageVariantReferenceWrongTarget,
			coverage.CoverageVariantReferenceDangling,
		} {
			addRequirement(plan, seen, c, de, coverage.CoverageDomainReference, variant)
		}
	}
}

func addRequirement(plan *coverage.CoveragePlan, seen map[string]struct{}, c constraint.Constraint, de derivableElement, domain coverage.CoverageDomain, variant coverage.CoverageVariant) {
	// Include the datatype in the obligation ID when present so that choice
	// elements (e.g. value[x] declared string|integer|dateTime) yield distinct
	// obligations per datatype instead of collapsing to the first one via the
	// seen-dedup map.
	id := fmt.Sprintf("%s|%s|%s", c.ProfileURL, c.ElementPath, variant)
	if c.Datatype != "" {
		id = fmt.Sprintf("%s|%s|%s|%s", c.ProfileURL, c.ElementPath, c.Datatype, variant)
	}
	req := coverage.CoverageRequirement{
		ID:                id,
		ConstraintID:      c.ID,
		ProfileURL:        c.ProfileURL,
		ResourceType:      c.ResourceType,
		ElementPath:       c.ElementPath,
		DependencyTargets: de.targets,
		Domain:            domain,
		Variant:           variant,
		Min:               de.element.Min,
		Max:               de.element.Max,
		Datatype:          c.Datatype,
	}
	req.Description = coverage.DescribeCoverageRequirement(req)
	req.HumanID = coverage.HumanID(req)
	appendRequirement(plan, seen, req)
}

func collectDependencyTargets(r *registry.Registry, element model.ElementDefinition) []string {
	targets := make([]string, 0)
	for _, canonical := range element.TargetProfile {
		if resourceType := canonicalToResourceType(r, canonical); resourceType != "" {
			targets = appendUniqueString(targets, resourceType)
		}
	}
	for _, et := range element.Types {
		for _, canonical := range et.TargetProfile {
			if resourceType := canonicalToResourceType(r, canonical); resourceType != "" {
				targets = appendUniqueString(targets, resourceType)
			}
		}
	}
	return targets
}

func canonicalToResourceType(r *registry.Registry, canonical string) string {
	v := strings.TrimSpace(canonical)
	if v == "" {
		return ""
	}
	if i := strings.Index(v, "|"); i >= 0 {
		v = v[:i]
	}
	if i := strings.Index(v, "#"); i >= 0 {
		v = v[:i]
	}
	if r != nil {
		if sd, ok := r.StructureDefinition(v); ok && sd != nil && strings.TrimSpace(sd.Type) != "" {
			return sd.Type
		}
	}
	v = strings.TrimRight(v, "/")
	if v == "" {
		return ""
	}
	name := path.Base(v)
	if name == "StructureDefinition" {
		return ""
	}
	return name
}

func appendUniqueString(values []string, candidate string) []string {
	for _, v := range values {
		if v == candidate {
			return values
		}
	}
	return append(values, candidate)
}

func appendRequirement(plan *coverage.CoveragePlan, seen map[string]struct{}, req coverage.CoverageRequirement) {
	if _, ok := seen[req.ID]; ok {
		return
	}
	seen[req.ID] = struct{}{}
	plan.Requirements = append(plan.Requirements, req)
	plan.Summary.ByDomain[req.Domain]++
	plan.Summary.ByResourceType[req.ResourceType]++
	plan.Summary.ByVariant[req.Variant]++
}

// extensionSlicePrefixes returns the set of element IDs (and their descendant
// prefixes) identifying the slice entry elements for every extension whose
// profile URL matches one of the excluded extension URLs. Each returned entry
// is the slice element's ID; a trailing "." is appended to match descendant
// elements (e.g. "Organization.extension:suppressed.url"). A slice element's
// profile URL lives on its type profile (Code "Extension"). The returned set is
// empty when no extensions are excluded.
func extensionSlicePrefixes(elements []model.ElementDefinition, excludedURLs []string) []string {
	if len(excludedURLs) == 0 {
		return nil
	}
	excluded := make(map[string]struct{}, len(excludedURLs))
	for _, u := range excludedURLs {
		excluded[normalizeCanonicalURL(u)] = struct{}{}
	}
	var prefixes []string
	for _, el := range elements {
		if !elementIsExcludedExtension(el, excluded) {
			continue
		}
		if el.ID == "" {
			continue
		}
		prefixes = append(prefixes, el.ID, el.ID+".")
	}
	return prefixes
}

// isExcludedExtensionElement reports whether element belongs to an extension
// whose profile URL is in the excluded set, either because the element is the
// extension's slice entry itself (exact ID match) or because it is a descendant
// of such a slice (matched by the slice ID prefix).
func isExcludedExtensionElement(element model.ElementDefinition, excludedIDs []string) bool {
	if len(excludedIDs) == 0 {
		return false
	}
	for _, id := range excludedIDs {
		if element.ID == id || strings.HasPrefix(element.ID, id) {
			return true
		}
	}
	return false
}

// elementIsExcludedExtension reports whether element is an Extension-typed slice
// (or the root Extension element) whose declared type profile matches one of the
// excluded URLs.
func elementIsExcludedExtension(element model.ElementDefinition, excluded map[string]struct{}) bool {
	for _, et := range element.Types {
		if !strings.EqualFold(et.Code, "Extension") {
			continue
		}
		for _, p := range et.Profile {
			if _, ok := excluded[normalizeCanonicalURL(p)]; ok {
				return true
			}
		}
	}
	return false
}

// normalizeCanonicalURL lowercases a canonical URL and strips any trailing
// version ("|v") or fragment ("#v") qualifier so an excluded URL matches a
// profile URL that carries a version suffix (e.g. ".../suppressed|26.0.0").
func normalizeCanonicalURL(v string) string {
	v = strings.TrimSpace(v)
	if i := strings.IndexAny(v, "|#"); i >= 0 {
		v = v[:i]
	}
	return strings.ToLower(v)
}

func isDerivableElement(element model.ElementDefinition, options coverage.DeriveOptions) (bool, coverage.PruneReason) {
	if element.Path == "" {
		return false, coverage.PruneReasonRootPath
	}
	// Skip root entries like "Patient" and derive from concrete child paths.
	if !strings.Contains(element.Path, ".") {
		return false, coverage.PruneReasonRootPath
	}

	if hasExcludedPrefix(element.Path, options.ExcludePathPrefixes) {
		return false, coverage.PruneReasonExcludedPathPrefix
	}

	if !options.IncludeLowValuePaths && isLowValuePath(element.Path) {
		return false, coverage.PruneReasonLowValuePath
	}

	if options.MustSupportOnly && !element.MustSupport {
		return false, coverage.PruneReasonMustSupportFiltered
	}

	if !options.IncludeOptional && element.Min == 0 && !element.MustSupport {
		return false, coverage.PruneReasonOptionalFiltered
	}

	return true, ""
}

func allowsMultiple(max string) bool {
	if max == "*" {
		return true
	}
	n, err := strconv.Atoi(max)
	if err != nil {
		return false
	}
	return n > 1
}

func normalizeOptions(options coverage.DeriveOptions) coverage.DeriveOptions {
	// Reserved for future option normalization.
	return options
}

func hasExcludedPrefix(path string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if prefix == "" {
			continue
		}
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func isLowValuePath(path string) bool {
	parts := strings.Split(path, ".")
	if len(parts) < 2 {
		return false
	}
	_, ok := defaultLowValueSegments[parts[1]]
	return ok
}

// isUniversalSearchBase reports whether a search parameter base denotes the
// universal parameters that apply to every resource type. In FHIR these are
// defined against the abstract Resource base rather than "*".
// isSearchCodeAllowed reports whether a search parameter code should be included
// for a resource type when the server's CapabilityStatement declares search
// parameters. When CapabilitySearchCodes is nil (no capability scope), all
// codes are allowed. When the resource type is absent from the map, all codes
// are allowed for that type (the server did not restrict search for it).
// When the resource type is present, only codes in its declared set are allowed;
// a present-but-empty set (declared searchParam: []) allows no codes.
func isSearchCodeAllowed(resourceType, code string, capabilityCodes map[string][]string) bool {
	if capabilityCodes == nil {
		return true
	}
	codes, ok := capabilityCodes[resourceType]
	if !ok {
		// Resource type not in the capability search map: no restriction.
		return true
	}
	for _, c := range codes {
		if strings.EqualFold(c, code) {
			return true
		}
	}
	return false
}

func isUniversalSearchBase(resourceType string) bool {
	switch resourceType {
	case "*", "Resource", "DomainResource":
		return true
	default:
		return false
	}
}

func sortedSetKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func toSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(values))
	for _, v := range values {
		if v == "" {
			continue
		}
		set[strings.ToLower(strings.TrimSpace(v))] = struct{}{}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

func trackPruned(plan *coverage.CoveragePlan, reason coverage.PruneReason) {
	if reason == "" {
		return
	}
	plan.Summary.PrunedByReason[reason]++
}
