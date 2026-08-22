package validate

import (
	"sort"

	"github.com/jlcoulter/momus/internal/fhir/model"
)

// sortedPaths returns the canonical element paths in the profile's path index,
// sorted lexically for deterministic validation ordering.
func sortedPaths(elements map[string]*model.ElementNode) []string {
	paths := make([]string, 0, len(elements))
	for p := range elements {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

// isPresent reports whether a value is present in the FHIR sense: not nil and
// not an explicit null. An empty array counts as present (the element exists
// but has no members); a JSON null does not.
func isPresent(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case []any:
		return len(t) > 0
	default:
		return true
	}
}
