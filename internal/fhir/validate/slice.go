package validate

import (
	"github.com/jlcoulter/momus/internal/fhir/model"
)

// checkSlicePresence (T6) verifies that every required slice of a sliced
// element is present in the resource. A slice is present when at least one
// member of the sliced element's array carries the slice's required child
// fields. Slices with unresolvable discriminators are skipped (best-effort).
func (v *ProfileValidator) checkSlicePresence(node *model.ElementNode, def *model.ElementDefinition, resource map[string]any) []Issue {
	if len(node.Slices) == 0 {
		return nil
	}
	values, present := resolvePath(resource, node.Path)
	var issues []Issue
	for _, slice := range node.Slices {
		if slice == nil || slice.Definition == nil || slice.Definition.Min <= 0 {
			continue
		}
		// If the sliced element is absent entirely, a required slice is
		// necessarily missing.
		if !present || !slicePresent(values, slice) {
			issues = append(issues, Issue{
				Path:    node.Path + ":" + slice.Name,
				Kind:    "slice",
				Message: "required slice '" + slice.Name + "' is missing",
			})
		}
	}
	return issues
}

// slicePresent reports whether any value in the sliced element's array matches
// the slice, i.e. carries all of the slice's required child fields.
func slicePresent(values []any, slice *model.SliceNode) bool {
	required := requiredChildNames(slice)
	for _, val := range values {
		m, ok := val.(map[string]any)
		if !ok {
			continue
		}
		// If the slice has no required children, any member counts as present.
		if len(required) == 0 {
			return true
		}
		all := true
		for _, child := range required {
			if resolveLeafKey(m, child) == "" {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// requiredChildNames returns the names of the slice's children with Min > 0.
func requiredChildNames(slice *model.SliceNode) []string {
	var names []string
	for name, child := range slice.Children {
		if child != nil && child.Definition != nil && child.Definition.Min > 0 {
			names = append(names, name)
		}
	}
	return names
}
