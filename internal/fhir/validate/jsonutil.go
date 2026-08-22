package validate

// equalJSON reports whether two JSON values are deep-equal.
func equalJSON(a, b any) bool {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, avv := range av {
			bvv, ok := bv[k]
			if !ok || !equalJSON(avv, bvv) {
				return false
			}
		}
		return true
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !equalJSON(av[i], bv[i]) {
				return false
			}
		}
		return true
	case float64:
		bv, ok := b.(float64)
		return ok && av == bv
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	case nil:
		return b == nil
	default:
		return a == b
	}
}

// containsPattern reports whether value satisfies the FHIR pattern match rule:
// the value must contain at least the pattern's fields with equal values (a
// subset match on maps; an element-wise match on arrays). Scalars must equal.
func containsPattern(pattern, value any) bool {
	switch pv := pattern.(type) {
	case map[string]any:
		vv, ok := value.(map[string]any)
		if !ok {
			return false
		}
		for k, pp := range pv {
			vp, ok := vv[k]
			if !ok || !containsPattern(pp, vp) {
				return false
			}
		}
		return true
	case []any:
		vv, ok := value.([]any)
		if !ok {
			return false
		}
		// Every pattern element must be satisfied by some value element.
		for _, pe := range pv {
			found := false
			for _, ve := range vv {
				if containsPattern(pe, ve) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	default:
		return equalJSON(pattern, value)
	}
}
