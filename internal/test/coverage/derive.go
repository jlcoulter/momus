package coverage

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

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

// DeriveMVPPlan derives the initial contractual coverage obligations from
// cardinality constraints across every loaded StructureDefinition element.
func DeriveMVPPlan(r *registry.Registry) (*CoveragePlan, error) {
	return DerivePlan(r, DefaultDeriveOptions())
}

// DerivePlan derives contractual coverage obligations from loaded
// StructureDefinition elements using the provided options.
func DerivePlan(r *registry.Registry, options DeriveOptions) (*CoveragePlan, error) {
	if r == nil {
		return nil, errors.New("registry is required")
	}

	options = normalizeOptions(options)

	profiles := r.StructureDefinitions()
	if len(profiles) == 0 {
		return nil, errors.New("no structure definitions available for coverage derivation")
	}

	sort.Slice(profiles, func(i, j int) bool {
		return profiles[i].URL < profiles[j].URL
	})

	plan := &CoveragePlan{
		Requirements: make([]CoverageRequirement, 0),
		Summary: CoverageSummary{
			ByDomain:       make(map[CoverageDomain]int),
			ByResourceType: make(map[string]int),
			ByVariant:      make(map[CoverageVariant]int),
			PrunedByReason: make(map[PruneReason]int),
		},
	}
	seen := make(map[string]struct{})
	foundDerivableElements := false

	includeResources := toSet(options.IncludeResourceTypes)
	includeProfiles := toSet(options.IncludeProfileURLs)

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

		for _, element := range profile.Elements {
			if strings.Contains(element.Path, ".") {
				foundDerivableElements = true
			}
			derivable, reason := isDerivableElement(element, options)
			if !derivable {
				trackPruned(plan, reason)
				continue
			}
			appendRequirement(plan, seen, newRequirement(r, profile, element, CoverageVariantValidMin))
			if element.Min > 0 {
				appendRequirement(plan, seen, newRequirement(r, profile, element, CoverageVariantMissingRequired))
			}
			if allowsMultiple(element.Max) {
				appendRequirement(plan, seen, newRequirement(r, profile, element, CoverageVariantMultipleValues))
			}
		}
	}

	if !foundDerivableElements {
		return nil, errors.New("no derivable profile elements found in structure definitions")
	}

	sort.Slice(plan.Requirements, func(i, j int) bool {
		return plan.Requirements[i].ID < plan.Requirements[j].ID
	})
	plan.Summary.TotalRequirements = len(plan.Requirements)

	return plan, nil
}

// DefaultDeriveOptions returns the practical default derivation policy.
func DefaultDeriveOptions() DeriveOptions {
	return DeriveOptions{
		IncludeOptional: false,
	}
}

func newRequirement(r *registry.Registry, profile *model.StructureDefinition, element model.ElementDefinition, variant CoverageVariant) CoverageRequirement {
	id := fmt.Sprintf("%s|%s|%s", profile.URL, element.Path, variant)
	return CoverageRequirement{
		ID:                id,
		ProfileURL:        profile.URL,
		ResourceType:      profile.Type,
		ElementPath:       element.Path,
		DependencyTargets: collectDependencyTargets(r, element),
		Domain:            CoverageDomainCardinality,
		Variant:           variant,
		Min:               element.Min,
		Max:               element.Max,
	}
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
