package coverage

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// DependencyPlan describes a topological execution order for resource types.
type DependencyPlan struct {
	Levels       [][]string          `json:"levels"`
	Dependencies map[string][]string `json:"dependencies"`
}

// PlanDependencies builds a DAG from coverage requirements and returns
// topological levels where each level can be executed in parallel.
func PlanDependencies(requirements []CoverageRequirement) (*DependencyPlan, error) {
	resourceSet := make(map[string]struct{})
	for _, req := range requirements {
		if req.ResourceType == "" {
			continue
		}
		resourceSet[req.ResourceType] = struct{}{}
	}
	resourceTypes := make([]string, 0, len(resourceSet))
	for rt := range resourceSet {
		resourceTypes = append(resourceTypes, rt)
	}
	sort.Strings(resourceTypes)
	if len(resourceSet) == 0 {
		return &DependencyPlan{Levels: nil, Dependencies: map[string][]string{}}, nil
	}

	dependencies := make(map[string][]string, len(resourceSet))
	for _, req := range requirements {
		if req.ResourceType == "" {
			continue
		}
		for _, dep := range req.DependencyTargets {
			normalizedDep := normalizeDependencyTarget(dep, resourceSet, resourceTypes)
			if normalizedDep == "" || normalizedDep == req.ResourceType {
				continue
			}
			if _, ok := resourceSet[normalizedDep]; !ok {
				continue
			}
			dependencies[req.ResourceType] = appendUnique(dependencies[req.ResourceType], normalizedDep)
		}
	}
	for resourceType := range resourceSet {
		sort.Strings(dependencies[resourceType])
	}

	levels, err := topologicalLevels(resourceSet, dependencies)
	if err != nil {
		return nil, err
	}

	return &DependencyPlan{Levels: levels, Dependencies: dependencies}, nil
}

// PlanLevels builds a DependencyPlan from an explicit set of resource types and
// a dependency map, ignoring dependencies that reference types outside the set.
func PlanLevels(resourceTypes []string, dependencies map[string][]string) (*DependencyPlan, error) {
	resourceSet := make(map[string]struct{}, len(resourceTypes))
	for _, rt := range resourceTypes {
		if strings.TrimSpace(rt) != "" {
			resourceSet[rt] = struct{}{}
		}
	}
	if len(resourceSet) == 0 {
		return &DependencyPlan{Levels: nil, Dependencies: map[string][]string{}}, nil
	}

	filtered := make(map[string][]string, len(dependencies))
	for child, deps := range dependencies {
		for _, dep := range deps {
			if dep == child {
				continue
			}
			if _, ok := resourceSet[dep]; !ok {
				continue
			}
			filtered[child] = appendUnique(filtered[child], dep)
		}
	}
	for rt := range resourceSet {
		sort.Strings(filtered[rt])
	}

	levels, err := topologicalLevels(resourceSet, filtered)
	if err != nil {
		return nil, err
	}
	return &DependencyPlan{Levels: levels, Dependencies: filtered}, nil
}

func topologicalLevels(resourceSet map[string]struct{}, dependencies map[string][]string) ([][]string, error) {
	inDegree := make(map[string]int, len(resourceSet))
	reverse := make(map[string][]string, len(resourceSet))
	for rt := range resourceSet {
		inDegree[rt] = 0
	}

	for child, deps := range dependencies {
		inDegree[child] = len(deps)
		for _, dep := range deps {
			reverse[dep] = append(reverse[dep], child)
		}
	}
	for dep := range reverse {
		sort.Strings(reverse[dep])
	}

	remaining := make(map[string]struct{}, len(resourceSet))
	for rt := range resourceSet {
		remaining[rt] = struct{}{}
	}

	levels := make([][]string, 0)
	for len(remaining) > 0 {
		ready := make([]string, 0)
		for rt := range remaining {
			if inDegree[rt] == 0 {
				ready = append(ready, rt)
			}
		}
		if len(ready) == 0 {
			// Cycle: break it by removing the node that releases the most
			// dependents. Each node is still emitted exactly once.
			breaker := pickCycleBreaker(inDegree, reverse, remaining)
			if breaker == "" {
				return nil, fmt.Errorf("dependency graph has an unbreakable cycle")
			}
			inDegree[breaker] = 0
			ready = []string{breaker}
		}
		sort.Strings(ready)

		level := make([]string, 0, len(ready))
		for _, rt := range ready {
			if _, ok := remaining[rt]; !ok {
				continue
			}
			delete(remaining, rt)
			level = append(level, rt)
			for _, child := range reverse[rt] {
				if _, ok := remaining[child]; !ok {
					continue
				}
				inDegree[child]--
			}
		}
		if len(level) == 0 {
			return nil, fmt.Errorf("dependency graph has an unbreakable cycle")
		}
		levels = append(levels, level)
	}
	return levels, nil
}

func pickCycleBreaker(inDegree map[string]int, reverse map[string][]string, remaining map[string]struct{}) string {
	best := ""
	bestOutDegree := -1
	bestInDegree := 0
	for rt := range remaining {
		degree := inDegree[rt]
		if degree <= 0 {
			continue
		}
		outDegree := len(reverse[rt])
		if best == "" || outDegree > bestOutDegree || (outDegree == bestOutDegree && degree < bestInDegree) || (outDegree == bestOutDegree && degree == bestInDegree && rt < best) {
			best = rt
			bestOutDegree = outDegree
			bestInDegree = degree
		}
	}
	return best
}

func appendUnique(values []string, candidate string) []string {
	for _, v := range values {
		if v == candidate {
			return values
		}
	}
	return append(values, candidate)
}

func normalizeDependencyTarget(dep string, resourceSet map[string]struct{}, resourceTypes []string) string {
	dep = strings.TrimSpace(dep)
	if dep == "" {
		return ""
	}
	if _, ok := resourceSet[dep]; ok {
		return dep
	}
	for _, resourceType := range resourceTypes {
		if strings.EqualFold(resourceType, dep) {
			return resourceType
		}
	}
	tokens := strings.FieldsFunc(strings.ToLower(dep), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, token := range tokens {
		for _, resourceType := range resourceTypes {
			if token == strings.ToLower(resourceType) {
				return resourceType
			}
		}
	}
	return ""
}
