package validate

import (
	"context"

	"github.com/jlcoulter/momus/internal/fhir/fhirpath"
	"github.com/jlcoulter/momus/internal/fhir/model"
)

// checkProfile runs every structural check against the resource and collects
// the resulting issues, iterating over the resolved profile's element path
// index. Element-order is deterministic: sorted by canonical path.
func (v *ProfileValidator) checkProfile(ctx context.Context, profile *model.ResolvedProfile, resource map[string]any) []Issue {
	var issues []Issue
	paths := sortedPaths(profile.Elements)
	for _, p := range paths {
		node := profile.Elements[p]
		if node == nil || node.Definition == nil {
			continue
		}
		def := node.Definition
		if def.SliceName != "" {
			// Sliced members are validated as part of slice presence (T6);
			// individual slice children are handled by the slice walker.
			continue
		}
		issues = append(issues, v.checkElement(ctx, node, def, resource)...)
	}
	return issues
}

// checkElement dispatches an element definition to the concrete checks.
func (v *ProfileValidator) checkElement(ctx context.Context, node *model.ElementNode, def *model.ElementDefinition, resource map[string]any) []Issue {
	var issues []Issue
	issues = append(issues, v.checkCardinality(node, def, resource)...)
	issues = append(issues, v.checkDatatype(node, def, resource)...)
	issues = append(issues, v.checkTerminology(node, def, resource)...)
	issues = append(issues, v.checkFixedPattern(node, def, resource)...)
	issues = append(issues, v.checkSlicePresence(node, def, resource)...)
	issues = append(issues, v.checkInvariants(ctx, node, def, resource)...)
	return issues
}

// checkInvariants (T11) evaluates each error-severity FHIRPath constraint on an
// element against its value as %context. A constraint that evaluates false (and
// is known) is a violation; an out-of-scope (unknown) result is not.
func (v *ProfileValidator) checkInvariants(ctx context.Context, node *model.ElementNode, def *model.ElementDefinition, resource map[string]any) []Issue {
	var issues []Issue
	for _, c := range def.Constraints {
		if c.Severity != "error" || c.Expression == "" {
			continue
		}
		values, present := resolvePath(resource, node.Path)
		if !present {
			// No element value; evaluate against the missing (empty) context.
			ok, known, err := fhirpath.EvalBool(ctx, c.Expression, nil)
			if err != nil {
				continue // a parse/unknown error on the constraint is not a violation
			}
			if known && !ok {
				issues = append(issues, Issue{
					Path:    node.Path,
					Kind:    "invariant",
					Message: invariantMessage(c),
				})
			}
			continue
		}
		for _, val := range values {
			if !isPresent(val) {
				continue
			}
			ok, known, err := fhirpath.EvalBool(ctx, c.Expression, val)
			if err != nil {
				continue
			}
			if known && !ok {
				issues = append(issues, Issue{
					Path:    node.Path,
					Kind:    "invariant",
					Message: invariantMessage(c),
					Value:   val,
				})
			}
		}
	}
	return issues
}

func invariantMessage(c model.ElementConstraint) string {
	if c.Human != "" {
		return "invariant " + c.Key + ": " + c.Human
	}
	return "invariant " + c.Key + " is not satisfied"
}

// checkCardinality (T2) enforces required-presence: an element with Min > 0
// must be present in the resource.
func (v *ProfileValidator) checkCardinality(node *model.ElementNode, def *model.ElementDefinition, resource map[string]any) []Issue {
	if def.Min <= 0 {
		return nil
	}
	values, present := resolvePath(resource, node.Path)
	if !present || len(values) == 0 {
		return []Issue{{
			Path:    node.Path,
			Kind:    "cardinality",
			Message: "required element " + node.Path + " is missing",
		}}
	}
	return nil
}

// checkDatatype (T3) verifies an element's value JSON kind matches its declared
// FHIR datatype. Unresolvable or unknown types are skipped (no issue).
func (v *ProfileValidator) checkDatatype(node *model.ElementNode, def *model.ElementDefinition, resource map[string]any) []Issue {
	if len(def.Types) == 0 {
		return nil
	}
	values, present := resolvePath(resource, node.Path)
	if !present {
		return nil
	}
	var issues []Issue
	for _, val := range values {
		if !isPresent(val) {
			continue
		}
		if !matchesDatatype(val, def.Types) {
			issues = append(issues, Issue{
				Path:    node.Path,
				Kind:    "datatype",
				Message: "value does not match declared datatype",
				Value:   val,
			})
		}
	}
	return issues
}

// checkTerminology (T4) verifies a code or CodeableConcept/Coding value is a
// member of the element's bound value set.
func (v *ProfileValidator) checkTerminology(node *model.ElementNode, def *model.ElementDefinition, resource map[string]any) []Issue {
	if def.Binding == nil || def.Binding.ValueSet == "" {
		return nil
	}
	values, present := resolvePath(resource, node.Path)
	if !present {
		return nil
	}
	var issues []Issue
	for _, val := range values {
		if !isPresent(val) {
			continue
		}
		if !v.isMemberOf(def.Binding.ValueSet, val) {
			issues = append(issues, Issue{
				Path:    node.Path,
				Kind:    "terminology",
				Message: "value is not a member of bound value set " + def.Binding.ValueSet,
				Value:   val,
			})
		}
	}
	return issues
}

// checkFixedPattern (T5) verifies fixed/pattern value conformance.
func (v *ProfileValidator) checkFixedPattern(node *model.ElementNode, def *model.ElementDefinition, resource map[string]any) []Issue {
	if def.Fixed == nil && def.Pattern == nil {
		return nil
	}
	values, present := resolvePath(resource, node.Path)
	if !present {
		return nil
	}
	var issues []Issue
	for _, val := range values {
		if !isPresent(val) {
			continue
		}
		if def.Fixed != nil && !equalJSON(def.Fixed, val) {
			issues = append(issues, Issue{
				Path:    node.Path,
				Kind:    "fixed",
				Message: "value does not equal fixed value",
				Value:   val,
			})
		}
		if def.Pattern != nil && !containsPattern(def.Pattern, val) {
			issues = append(issues, Issue{
				Path:    node.Path,
				Kind:    "pattern",
				Message: "value does not match pattern",
				Value:   val,
			})
		}
	}
	return issues
}
