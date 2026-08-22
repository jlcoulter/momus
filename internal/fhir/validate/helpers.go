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

// isPresent reports whether a value has members to validate: it is false for
// nil, an explicit JSON null, and an empty array (no elements to check), and
// true otherwise. An empty array is therefore treated as "no values present"
// by the per-element checks, matching FHIR's rule that an absent-or-empty
// element has nothing to validate. Note this is distinct from cardinality,
// where presence of the element itself is handled separately via resolvePath.
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
