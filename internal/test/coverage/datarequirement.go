package coverage

import (
	"errors"
	"fmt"
	"sort"

	"github.com/jlcoulter/momus/internal/fhir/model"
)

// PlanToDataRequirements converts a coverage plan into one DataRequirement per
// (resource type, profile) group, so the resource Generator can produce a batch
// of valid instances. Relationship references are derived from reference-domain
// obligations: each distinct reference element becomes a relationship whose
// target type is the element's first dependency target. Interaction obligations
// are ignored (they do not change the generated resource).
func PlanToDataRequirements(plan *CoveragePlan) ([]model.DataRequirement, error) {
	if plan == nil {
		return nil, errors.New("coverage plan is required")
	}

	type groupKey struct {
		resourceType string
		profileURL   string
	}
	type group struct {
		resourceType string
		profileURL   string
		refs         map[string]string // elementPath -> target resource type
	}

	groups := make(map[groupKey]*group)
	order := make([]groupKey, 0)
	for _, req := range plan.Requirements {
		if req.Domain == CoverageDomainInteraction || req.ResourceType == "" {
			continue
		}
		key := groupKey{req.ResourceType, req.ProfileURL}
		g := groups[key]
		if g == nil {
			g = &group{resourceType: req.ResourceType, profileURL: req.ProfileURL, refs: make(map[string]string)}
			groups[key] = g
			order = append(order, key)
		}
		if req.Domain == CoverageDomainReference && len(req.DependencyTargets) > 0 {
			if _, ok := g.refs[req.ElementPath]; !ok {
				g.refs[req.ElementPath] = req.DependencyTargets[0]
			}
		}
	}

	sort.Slice(order, func(i, j int) bool {
		if order[i].resourceType != order[j].resourceType {
			return order[i].resourceType < order[j].resourceType
		}
		return order[i].profileURL < order[j].profileURL
	})

	requirements := make([]model.DataRequirement, 0, len(order))
	for idx, key := range order {
		g := groups[key]
		dr := model.DataRequirement{
			ID:          fmt.Sprintf("req-%d", idx),
			Purpose:     model.PurposeFixture,
			Resource:    model.ResourceRequirement{Type: g.resourceType, Profile: []string{g.profileURL}},
			Cardinality: model.Exactly(1),
		}
		paths := make([]string, 0, len(g.refs))
		for path := range g.refs {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for _, path := range paths {
			dr.Relationships = append(dr.Relationships, model.RelationshipRequirement{
				Path:   path,
				Target: model.ResourceRequirement{Type: g.refs[path]},
			})
		}
		requirements = append(requirements, dr)
	}
	return requirements, nil
}
