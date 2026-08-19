package generation

import (
	"sort"
	"strings"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
	"github.com/jlcoulter/momus/internal/test/coverage"
)

// buildDependencyPlan computes the resource dependency order for execution,
// expanding the coverage-derived dependencies with every resource type that a
// profile's Reference elements target. This ensures all referenced setup
// resources are created before their dependents, even when the referencing
// element is optional and therefore not itself a derived coverage obligation.
func buildDependencyPlan(plan *coverage.CoveragePlan, options BuildOptions) (*coverage.DependencyPlan, error) {
	depPlan, err := coverage.PlanDependencies(plan.Requirements)
	if err != nil {
		return nil, err
	}

	resourceSet := make(map[string]struct{})
	for _, req := range plan.Requirements {
		if req.ResourceType != "" {
			resourceSet[req.ResourceType] = struct{}{}
		}
	}
	resourceTypes := make([]string, 0, len(resourceSet))
	for rt := range resourceSet {
		resourceTypes = append(resourceTypes, rt)
	}
	sort.Strings(resourceTypes)

	dependencies := depPlan.Dependencies
	if dependencies == nil {
		dependencies = make(map[string][]string)
	}
	for resourceType := range resourceSet {
		profileURL := primaryProfileURL(plan.Requirements, resourceType, options.Registry)
		for _, target := range profileReferenceTargets(options.Registry, profileURL) {
			if target == resourceType {
				continue
			}
			dependencies[resourceType] = appendUniqueString(dependencies[resourceType], target)
		}
	}
	return coverage.PlanLevels(resourceTypes, dependencies)
}

// primaryProfileURL returns the first declared profile URL for a resource type,
// falling back to the registry's first profile for the type.
func primaryProfileURL(requirements []coverage.CoverageRequirement, resourceType string, reg *registry.Registry) string {
	for _, req := range requirements {
		if req.ResourceType == resourceType && strings.TrimSpace(req.ProfileURL) != "" {
			return req.ProfileURL
		}
	}
	if reg != nil {
		if profiles := reg.ProfilesForResource(resourceType); len(profiles) > 0 {
			return profiles[0].URL
		}
	}
	return ""
}

// profileReferenceTargets returns the resource types a profile's Reference
// elements reference, deterministically sorted.
func profileReferenceTargets(reg *registry.Registry, profileURL string) []string {
	if profileURL == "" || reg == nil {
		return nil
	}
	resolved, err := reg.ResolveProfile(profileURL)
	if err != nil || resolved == nil || resolved.Root == nil {
		return nil
	}
	set := make(map[string]struct{})
	collectReferenceTargets(resolved.Root, reg, set)
	out := make([]string, 0, len(set))
	for rt := range set {
		out = append(out, rt)
	}
	sort.Strings(out)
	return out
}

func collectReferenceTargets(node *model.ElementNode, reg *registry.Registry, set map[string]struct{}) {
	if node == nil {
		return
	}
	if node.Definition != nil && primaryTypeCode(node.Definition) == "Reference" {
		for _, target := range allTargetResourceTypes(node.Definition, reg) {
			set[target] = struct{}{}
		}
	}
	for _, child := range node.Children {
		collectReferenceTargets(child, reg, set)
	}
	for _, slice := range node.Slices {
		for _, child := range slice.Children {
			collectReferenceTargets(child, reg, set)
		}
	}
}

func allTargetResourceTypes(def *model.ElementDefinition, reg *registry.Registry) []string {
	var out []string
	for _, canonical := range def.TargetProfile {
		if rt := resolveTargetResourceType(canonical, reg); rt != "" {
			out = appendUniqueString(out, rt)
		}
	}
	for _, et := range def.Types {
		for _, canonical := range et.TargetProfile {
			if rt := resolveTargetResourceType(canonical, reg); rt != "" {
				out = appendUniqueString(out, rt)
			}
		}
	}
	return out
}

func appendUniqueString(values []string, candidate string) []string {
	for _, v := range values {
		if v == candidate {
			return values
		}
	}
	return append(values, candidate)
}
