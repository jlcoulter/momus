package coverage

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/jlcoulter/momus/internal/fhir/constraint"
	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
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
func DerivePlan(r *registry.Registry, options DeriveOptions) (*CoveragePlan, error) {
	if r == nil {
		return nil, errors.New("registry is required")
	}

	options = normalizeOptions(options)

	constraints, err := constraint.DeriveScoped(r)
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
			trackPruned(plan, PruneReasonResourceFiltered)
			continue
		}
		if len(includeResources) > 0 {
			if _, ok := includeResources[strings.ToLower(profile.Type)]; !ok {
				trackPruned(plan, PruneReasonResourceFiltered)
				continue
			}
		}
		if len(includeProfiles) > 0 {
			if _, ok := includeProfiles[strings.ToLower(profile.URL)]; !ok {
				trackPruned(plan, PruneReasonProfileFiltered)
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
		for _, element := range elements {
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
				addRequirement(plan, seen, synthetic, de, CoverageDomainStructure, CoverageVariantStructureSlicePresent)
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
				appendSearchObligations(plan, seen, c, rt)
				searchCodesByType[rt] = appendUniqueString(searchCodesByType[rt], c.SearchCode)
			}
			continue
		}
		if _, ok := inScopeResourceTypes[c.ResourceType]; ok {
			if !isSearchCodeAllowed(c.ResourceType, c.SearchCode, options.CapabilitySearchCodes) {
				continue
			}
			appendSearchObligations(plan, seen, c, c.ResourceType)
			searchCodesByType[c.ResourceType] = appendUniqueString(searchCodesByType[c.ResourceType], c.SearchCode)
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
			appendRequirement(plan, seen, CoverageRequirement{
				ID:            fmt.Sprintf("operation|%s|%s", c.ResourceType, c.OperationName),
				ProfileURL:    profileURLByType[c.ResourceType],
				ResourceType:  c.ResourceType,
				Domain:        CoverageDomainOperation,
				Variant:       CoverageVariantOperationCustom,
				OperationName: c.OperationName,
			})
		}
	}

	sort.Slice(plan.Requirements, func(i, j int) bool {
		return plan.Requirements[i].ID < plan.Requirements[j].ID
	})

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
	}

	plan.Summary.TotalRequirements = len(plan.Requirements)

	return plan, nil
}

// deriveInteractionObligations appends pairwise interaction obligations between
// accept (non-reject) base requirements that share a resource type and profile.
// Interaction obligations are derived only over accept obligations: a negative
// mutation exercises exactly one constraint, so it cannot coexist with another
// obligation in the same payload.
func deriveInteractionObligations(plan *CoveragePlan, seen map[string]struct{}) {
	type groupKey struct {
		resourceType string
		profileURL   string
	}
	groups := make(map[groupKey][]CoverageRequirement)
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
				interaction := CoverageRequirement{
					ID:           id,
					ProfileURL:   a.ProfileURL,
					ResourceType: a.ResourceType,
					ElementPath:  a.ElementPath + " ++ " + b.ElementPath,
					Domain:       CoverageDomainInteraction,
					Variant:      CoverageVariantInteractionPair,
					PairA:        a.ID,
					PairB:        b.ID,
				}
				appendRequirement(plan, seen, interaction)
				plan.Interactions = append(plan.Interactions, InteractionRequirement{
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

func newCoveragePlan() *CoveragePlan {
	return &CoveragePlan{
		Requirements: make([]CoverageRequirement, 0),
		Strength:     1,
		Summary: CoverageSummary{
			ByDomain:       make(map[CoverageDomain]int),
			ByResourceType: make(map[string]int),
			ByVariant:      make(map[CoverageVariant]int),
			PrunedByReason: make(map[PruneReason]int),
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
func isNonElementDomain(domain CoverageDomain) bool {
	switch domain {
	case CoverageDomainInteraction, CoverageDomainSearch, CoverageDomainOperation, CoverageDomainState:
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
// constraint applied to a single resource type.
func appendSearchObligations(plan *CoveragePlan, seen map[string]struct{}, c constraint.Constraint, resourceType string) {
	if strings.TrimSpace(c.SearchCode) == "" {
		return
	}
	for _, variant := range []CoverageVariant{
		CoverageVariantSearchValid,
		CoverageVariantSearchNoResults,
		CoverageVariantSearchInvalidValue,
		CoverageVariantSearchMultipleResults,
		CoverageVariantSearchInvalidModifier,
	} {
		appendRequirement(plan, seen, CoverageRequirement{
			ID:           fmt.Sprintf("search|%s|%s|%s", resourceType, c.SearchCode, variant),
			ConstraintID: c.ID,
			ResourceType: resourceType,
			Domain:       CoverageDomainSearch,
			Variant:      variant,
			SearchCode:   c.SearchCode,
		})
	}
}

// appendOperationObligations adds the CRUD-style operation obligations (read,
// update, delete, history) for a resource type.
func appendOperationObligations(plan *CoveragePlan, seen map[string]struct{}, resourceType, profileURL string) {
	for _, variant := range []CoverageVariant{
		CoverageVariantOperationRead,
		CoverageVariantOperationUpdate,
		CoverageVariantOperationPatch,
		CoverageVariantOperationDelete,
		CoverageVariantOperationHistory,
	} {
		appendRequirement(plan, seen, CoverageRequirement{
			ID:           fmt.Sprintf("operation|%s|%s", resourceType, variant),
			ProfileURL:   profileURL,
			ResourceType: resourceType,
			Domain:       CoverageDomainOperation,
			Variant:      variant,
		})
	}
}

// appendStateObligations adds the negative state-transition obligations for a
// resource type (reading/deleting a nonexistent resource).
func appendStateObligations(plan *CoveragePlan, seen map[string]struct{}, resourceType, profileURL string) {
	for _, variant := range []CoverageVariant{
		CoverageVariantStateCRUDSequence,
		CoverageVariantStateReadNonexistent,
		CoverageVariantStateDeleteNonexistent,
	} {
		appendRequirement(plan, seen, CoverageRequirement{
			ID:           fmt.Sprintf("state|%s|%s", resourceType, variant),
			ProfileURL:   profileURL,
			ResourceType: resourceType,
			Domain:       CoverageDomainState,
			Variant:      variant,
		})
	}
}

// appendSearchCombinationObligations adds a pairwise combination obligation for
// every pair of distinct search parameters of a resource type.
func appendSearchCombinationObligations(plan *CoveragePlan, seen map[string]struct{}, resourceType, profileURL string, codes []string) {
	codes = uniqueSortedStrings(codes)
	for i := 0; i < len(codes); i++ {
		for j := i + 1; j < len(codes); j++ {
			appendRequirement(plan, seen, CoverageRequirement{
				ID:           fmt.Sprintf("search|%s|%s++%s|%s", resourceType, codes[i], codes[j], CoverageVariantSearchCombination),
				ProfileURL:   profileURL,
				ResourceType: resourceType,
				Domain:       CoverageDomainSearch,
				Variant:      CoverageVariantSearchCombination,
				SearchCode:   codes[i],
				SearchCodeB:  codes[j],
			})
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

func appendObligations(plan *CoveragePlan, seen map[string]struct{}, c constraint.Constraint, de derivableElement) {
	switch c.Kind {
	case constraint.KindCardinality:
		addRequirement(plan, seen, c, de, CoverageDomainCardinality, CoverageVariantValidMin)
		if de.element.Min > 0 {
			addRequirement(plan, seen, c, de, CoverageDomainCardinality, CoverageVariantMissingRequired)
		}
		if allowsMultiple(de.element.Max) {
			addRequirement(plan, seen, c, de, CoverageDomainCardinality, CoverageVariantMultipleValues)
		}
	case constraint.KindDatatype:
		for _, variant := range []CoverageVariant{
			CoverageVariantDatatypeValid,
			CoverageVariantDatatypeInvalidLexical,
			CoverageVariantDatatypeWrongJSONType,
			CoverageVariantDatatypeNull,
		} {
			addRequirement(plan, seen, c, de, CoverageDomainDatatype, variant)
		}
	case constraint.KindTerminology:
		for _, variant := range []CoverageVariant{
			CoverageVariantTerminologyValid,
			CoverageVariantTerminologyInvalid,
			CoverageVariantTerminologyAbsent,
		} {
			addRequirement(plan, seen, c, de, CoverageDomainTerminology, variant)
		}
	case constraint.KindInvariant:
		// Only the positive satisfies obligation is derived. A concrete
		// invariant-violates payload cannot be constructed reliably (that would
		// require evaluating the FHIRPath invariant expression, which is not yet
		// implemented), and nulling the subject element does not break most
		// invariants, so deriving a violates obligation would generate a test that
		// a conformant server accepts and the reject assertion would falsely fail.
		addRequirement(plan, seen, c, de, CoverageDomainInvariant, CoverageVariantInvariantSatisfies)
	case constraint.KindReference:
		for _, variant := range []CoverageVariant{
			CoverageVariantReferenceValid,
			CoverageVariantReferenceWrongTarget,
			CoverageVariantReferenceDangling,
		} {
			addRequirement(plan, seen, c, de, CoverageDomainReference, variant)
		}
	}
}

func addRequirement(plan *CoveragePlan, seen map[string]struct{}, c constraint.Constraint, de derivableElement, domain CoverageDomain, variant CoverageVariant) {
	// Include the datatype in the obligation ID when present so that choice
	// elements (e.g. value[x] declared string|integer|dateTime) yield distinct
	// obligations per datatype instead of collapsing to the first one via the
	// seen-dedup map.
	id := fmt.Sprintf("%s|%s|%s", c.ProfileURL, c.ElementPath, variant)
	if c.Datatype != "" {
		id = fmt.Sprintf("%s|%s|%s|%s", c.ProfileURL, c.ElementPath, c.Datatype, variant)
	}
	appendRequirement(plan, seen, CoverageRequirement{
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
	})
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

func appendRequirement(plan *CoveragePlan, seen map[string]struct{}, req CoverageRequirement) {
	if _, ok := seen[req.ID]; ok {
		return
	}
	seen[req.ID] = struct{}{}
	plan.Requirements = append(plan.Requirements, req)
	plan.Summary.ByDomain[req.Domain]++
	plan.Summary.ByResourceType[req.ResourceType]++
	plan.Summary.ByVariant[req.Variant]++
}

func isDerivableElement(element model.ElementDefinition, options DeriveOptions) (bool, PruneReason) {
	if element.Path == "" {
		return false, PruneReasonRootPath
	}
	// Skip root entries like "Patient" and derive from concrete child paths.
	if !strings.Contains(element.Path, ".") {
		return false, PruneReasonRootPath
	}

	if hasExcludedPrefix(element.Path, options.ExcludePathPrefixes) {
		return false, PruneReasonExcludedPathPrefix
	}

	if !options.IncludeLowValuePaths && isLowValuePath(element.Path) {
		return false, PruneReasonLowValuePath
	}

	if options.MustSupportOnly && !element.MustSupport {
		return false, PruneReasonMustSupportFiltered
	}

	if !options.IncludeOptional && element.Min == 0 && !element.MustSupport {
		return false, PruneReasonOptionalFiltered
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

func normalizeOptions(options DeriveOptions) DeriveOptions {
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

func trackPruned(plan *CoveragePlan, reason PruneReason) {
	if reason == "" {
		return
	}
	plan.Summary.PrunedByReason[reason]++
}
