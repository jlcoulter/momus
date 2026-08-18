package generation

import "strings"

// This file provides boundary-value generation: concrete values that sit at
// the edges of a range so tests exercise meaningful constraint boundaries
// rather than only mid-range values.
//
// Length-bounded string obligations are not yet modelled by the constraint
// model, so StringBoundaryValues is applied to whatever explicit length range
// a caller knows; cardinality boundaries are exercised via the existing
// valid-min / missing-required / multiple-values variants, whose value counts
// BoundaryCapacity makes explicit.

// StringBoundaryValues returns example strings bracketing the length range
// [min, max]: min-1, min, min+1, max-1, max, max+1, de-duplicated and clamped
// to positive lengths. This mirrors the architecture's 3..10 example
// (2/3/4/9/10/11).
func StringBoundaryValues(min, max int) []string {
	if min < 1 {
		min = 1
	}
	candidates := []int{min - 1, min, min + 1, max - 1, max, max + 1}
	seen := make(map[int]bool)
	var out []string
	for _, n := range candidates {
		if n < 1 || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, strings.Repeat("a", n))
	}
	return out
}

// NumericBoundaryValues returns edge values around zero and at a large
// magnitude, suitable for unbounded or wide numeric ranges.
func NumericBoundaryValues() []any {
	return []any{0, -1, 1, 1000000000}
}

// BoundaryCapacities returns the value counts that exercise a cardinality
// bound [min, max] (max < 0 means unbounded): min-1, min, min+1 and, when
// bounded, max and max+1, de-duplicated and clamped to non-negative counts.
func BoundaryCapacities(min, max int) []int {
	candidates := []int{min - 1, min, min + 1}
	if max >= 0 {
		candidates = append(candidates, max, max+1)
	}
	seen := make(map[int]bool)
	var out []int
	for _, n := range candidates {
		if n < 0 || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}
