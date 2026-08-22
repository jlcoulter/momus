package fhirpath

// isUnknown reports whether a Result carries the unknown sentinel.
func isUnknown(r Result) bool {
	_, ok := r.value.(unknownMark)
	return ok
}

// unknownResult returns an unknown-marked Result.
func unknownResult() Result { return Result{value: unknownSentinel} }

// asResult converts a bare Go value into a Result.
func asResult(v any) Result {
	if _, ok := v.(unknownMark); ok {
		return unknownResult()
	}
	return Result{value: v}
}
