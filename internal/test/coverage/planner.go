package coverage

import (
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

	ready := make([]string, 0)
	for rt, d := range inDegree {
		if d == 0 {
			ready = append(ready, rt)
		}
	}
	sort.Strings(ready)

	levels := make([][]string, 0)
	processed := 0
	for processed < len(resourceSet) {
		if len(ready) == 0 {
			breaker := pickCycleBreaker(inDegree, reverse)
			if breaker == "" {
				break
			}
			ready = []string{breaker}
		}

		level := append([]string(nil), ready...)
		levels = append(levels, level)
		processed += len(level)

		nextSet := make(map[string]struct{})
		for _, rt := range level {
			for _, child := range reverse[rt] {
				inDegree[child]--
				if inDegree[child] == 0 {
					nextSet[child] = struct{}{}
				}
			}
		}

		next := make([]string, 0, len(nextSet))
		for rt := range nextSet {
			next = append(next, rt)
		}
		sort.Strings(next)
		ready = next
	}
	return levels, nil
}

func pickCycleBreaker(inDegree map[string]int, reverse map[string][]string) string {
	best := ""
	bestOutDegree := -1
	bestInDegree := 0
	for rt, degree := range inDegree {
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
