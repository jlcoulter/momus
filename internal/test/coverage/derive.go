package coverage

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

// DeriveMVPPlan derives the initial contractual coverage obligations from
// cardinality constraints across every loaded StructureDefinition element.
func DeriveMVPPlan(r *registry.Registry) (*CoveragePlan, error) {
	if r == nil {
		return nil, errors.New("registry is required")
	}

	profiles := r.StructureDefinitions()
	if len(profiles) == 0 {
		return nil, errors.New("no structure definitions available for coverage derivation")
	}

	sort.Slice(profiles, func(i, j int) bool {
		return profiles[i].URL < profiles[j].URL
	})

	plan := &CoveragePlan{Requirements: make([]CoverageRequirement, 0)}
	seen := make(map[string]struct{})
	foundElements := false

	for _, profile := range profiles {
		if profile == nil {
			continue
		}
		for _, element := range profile.Elements {
			if !isDerivableElement(element.Path) {
				continue
			}
			foundElements = true
			appendRequirement(plan, seen, newRequirement(profile, element, CoverageVariantValidMin))
			if element.Min > 0 {
				appendRequirement(plan, seen, newRequirement(profile, element, CoverageVariantMissingRequired))
			}
			if allowsMultiple(element.Max) {
				appendRequirement(plan, seen, newRequirement(profile, element, CoverageVariantMultipleValues))
			}
		}
	}

	if !foundElements {
		return nil, errors.New("no derivable profile elements found in structure definitions")
	}

	sort.Slice(plan.Requirements, func(i, j int) bool {
		return plan.Requirements[i].ID < plan.Requirements[j].ID
	})

	return plan, nil
}

func newRequirement(profile *model.StructureDefinition, element model.ElementDefinition, variant CoverageVariant) CoverageRequirement {
	id := fmt.Sprintf("%s|%s|%s", profile.URL, element.Path, variant)
	return CoverageRequirement{
		ID:           id,
		ProfileURL:   profile.URL,
		ResourceType: profile.Type,
		ElementPath:  element.Path,
		Domain:       CoverageDomainCardinality,
		Variant:      variant,
		Min:          element.Min,
		Max:          element.Max,
	}
}

func appendRequirement(plan *CoveragePlan, seen map[string]struct{}, req CoverageRequirement) {
	if _, ok := seen[req.ID]; ok {
		return
	}
	seen[req.ID] = struct{}{}
	plan.Requirements = append(plan.Requirements, req)
}

func isDerivableElement(path string) bool {
	if path == "" {
		return false
	}
	// Skip root entries like "Patient" and derive from concrete child paths.
	return strings.Contains(path, ".")
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
