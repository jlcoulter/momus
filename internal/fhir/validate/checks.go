package validate

import (
	"context"
	"strconv"

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
	issues = append(issues, v.checkMaxCardinality(node, def, resource)...)
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
			// The element is absent: an invariant only constrains element
			// instances, so an absent element is vacuously satisfied. Evaluating
			// against a nil context would turn "extension.exists() != value.
			// exists()" (ext-1) into "false != false" and falsely reject every
			// element without an extension field.
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
// must be present in the resource. A required child is only enforced when its
// ancestor path chain is present (an absent optional parent cannot obligate its
// required children), mirroring FHIR's conditional cardinality semantics.
func (v *ProfileValidator) checkCardinality(node *model.ElementNode, def *model.ElementDefinition, resource map[string]any) []Issue {
	if def.Min <= 0 {
		return nil
	}
	// If any ancestor element along the path is absent, the child is not
	// required (its optional parent chain does not exist).
	if !ancestorsPresent(resource, node.Path) {
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

// checkMaxCardinality (T2b) enforces upper-bound cardinality: an element with a
// numeric Max > 1 (or a bounded value such as "1") must not contain more members
// than allowed. "0" means prohibited, "*" is unbounded. Like required-presence,
// the bound is only enforced when the element is actually present. The bound
// applies per element instance: when the element's parent repeats (an array
// parent like Parameters.parameter), the count is taken per parent instance, not
// across the whole collection.
func (v *ProfileValidator) checkMaxCardinality(node *model.ElementNode, def *model.ElementDefinition, resource map[string]any) []Issue {
	max, bounded := parseMax(def.Max)
	if !bounded {
		return nil
	}
	count := maxLeafCount(resource, node.Path)
	if count > max {
		return []Issue{{
			Path:    node.Path,
			Kind:    "cardinality",
			Message: "element " + node.Path + " exceeds maximum cardinality of " + def.Max,
		}}
	}
	return nil
}

// maxLeafCount returns the maximum number of leaf values carried by a single
// parent instance at the given path. For a non-repeatable parent there is one
// instance (e.g. a top-level scalar counts its own members). For a repeatable
// parent (an array such as Parameters.parameter) the bound is evaluated per
// array element, so a max-1 leaf that appears once in each of several parent
// instances does not exceed its bound. It returns 0 when the element is absent.
func maxLeafCount(resource map[string]any, path string) int {
	segments := elementSegments(path)
	if len(segments) == 0 {
		return 0
	}
	// Walk to the parent segment, keeping each parent instance as a distinct
	// group rather than flattening arrays together.
	parents := []map[string]any{resource}
	for i := 0; i < len(segments)-1; i++ {
		var next []map[string]any
		for _, p := range parents {
			key := segments[i]
			if _, ok := p[key]; !ok {
				if rk := resolveLeafKey(p, segments[i]); rk != "" {
					key = rk
				} else {
					continue
				}
			}
			switch val := p[key].(type) {
			case []any:
				for _, el := range val {
					if em, ok := el.(map[string]any); ok {
						next = append(next, em)
					}
				}
			case map[string]any:
				next = append(next, val)
			}
		}
		if len(next) == 0 {
			return 0
		}
		parents = next
	}
	leaf := segments[len(segments)-1]
	maxCount := 0
	for _, p := range parents {
		key := leaf
		if _, ok := p[leaf]; !ok {
			if rk := resolveLeafKey(p, leaf); rk != "" {
				key = rk
			} else {
				continue
			}
		}
		count := 1
		if arr, isArr := p[key].([]any); isArr {
			count = len(arr)
		}
		if count > maxCount {
			maxCount = count
		}
	}
	return maxCount
}

// parseMax interprets an ElementDefinition.Max cardinality string. It returns
// the integer upper bound and whether that bound is finite. "*" is unbounded;
// a non-numeric value is treated as unbounded (best-effort, never over-reject).
func parseMax(max string) (int, bool) {
	if max == "" || max == "*" {
		return 0, false
	}
	n, err := strconv.Atoi(max)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// excluding) the leaf is present in the resource. For "Observation.component.
// code", it checks that "Observation.component" resolves.
func ancestorsPresent(resource map[string]any, path string) bool {
	segments := elementSegments(path)
	// Walk the parent segments: check each prefix except the leaf.
	cur := []any{resource}
	for i := 0; i < len(segments)-1; i++ {
		var next []any
		for _, c := range cur {
			m, ok := c.(map[string]any)
			if !ok {
				continue
			}
			key := segments[i]
			if _, ok := m[key]; ok {
				key = segments[i]
			} else if rk := resolveLeafKey(m, segments[i]); rk != "" {
				key = rk
			} else {
				continue
			}
			switch val := m[key].(type) {
			case []any:
				next = append(next, val...)
			default:
				next = append(next, val)
			}
		}
		if len(next) == 0 {
			return false
		}
		cur = next
	}
	return true
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
