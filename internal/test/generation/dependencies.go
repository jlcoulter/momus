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
// profile's Reference elements target. The expansion is transitive: a type is
// added only if it is reachable from a coverage obligation via reference edges,
// and each added type's own references are followed too. This ensures all
// referenced setup resources are created before their dependents, even when the
// referencing element is optional and therefore not itself a derived coverage
// obligation, and even when a referenced type is not itself a coverage
// obligation (e.g. Patient referenced by Observation).
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

	dependencies := depPlan.Dependencies
	if dependencies == nil {
		dependencies = make(map[string][]string)
	}

	// Transitive closure over profile reference targets. Worklist-driven so each
	// newly discovered type's own references are followed, guaranteeing every
	// transitively-referenced type is seeded and ordered before its dependents.
	// When a capability scope is set, only reference targets the server declares
	// are added, so the plan never provisions a resource type the server does not
	// support.
	allowed := options.CapabilityResourceTypes
	queue := make([]string, 0, len(resourceSet))
	for rt := range resourceSet {
		queue = append(queue, rt)
	}
	visited := make(map[string]struct{})
	for len(queue) > 0 {
		rt := queue[0]
		queue = queue[1:]
		if _, seen := visited[rt]; seen {
			continue
		}
		visited[rt] = struct{}{}
		profileURL := primaryProfileURL(plan.Requirements, rt, options.Registry)
		for _, target := range profileReferenceTargets(options.Registry, profileURL) {
			if target == rt {
				continue
			}
			if allowed != nil {
				if _, ok := allowed[target]; !ok {
					continue
				}
			}
			dependencies[rt] = appendUniqueString(dependencies[rt], target)
			if _, ok := resourceSet[target]; !ok {
				resourceSet[target] = struct{}{}
				queue = append(queue, target)
			}
		}
	}

	resourceTypes := make([]string, 0, len(resourceSet))
	for rt := range resourceSet {
		resourceTypes = append(resourceTypes, rt)
	}
	sort.Strings(resourceTypes)
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
		if rt := resolveTargetResourceType(canonical, reg); rt != "" && !isAbstractResourceType(rt) {
			out = appendUniqueString(out, rt)
		}
	}
	for _, et := range def.Types {
		for _, canonical := range et.TargetProfile {
			if rt := resolveTargetResourceType(canonical, reg); rt != "" && !isAbstractResourceType(rt) {
				out = appendUniqueString(out, rt)
			}
		}
	}
	return out
}

// isAbstractResourceType reports whether a resource type is an abstract FHIR
// base type (Resource, DomainResource, ...) that must not be instantiated as a
// provisioned resource or reference target.
func isAbstractResourceType(resourceType string) bool {
	switch strings.TrimSpace(resourceType) {
	case "Resource", "DomainResource", "CanonicalResource", "MetadataResource":
		return true
	}
	return false
}

func appendUniqueString(values []string, candidate string) []string {
	for _, v := range values {
		if v == candidate {
			return values
		}
	}
	return append(values, candidate)
}
