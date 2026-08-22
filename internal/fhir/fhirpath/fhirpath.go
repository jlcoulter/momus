// Package fhirpath implements a pragmatic subset of the FHIRPath expression
// language, sufficient to evaluate the invariant expressions that appear in
// real FHIR StructureDefinition constraints. It is written in-house to keep the
// module's dependency set minimal.
//
// The evaluator returns a three-valued result: true, false, or unknown. Any
// construct outside the supported subset evaluates to unknown (never a
// hard error), so a validator that calls EvalBool treats an out-of-scope
// expression as "not a violation" rather than falsely rejecting a conformant
// resource.
package fhirpath

import "context"

// unknown is a sentinel value marking an out-of-scope result.
type unknownMark struct{}

var unknownSentinel = unknownMark{}

// Result is the result of evaluating an expression: a FHIRPath value, which
// may be a scalar or a collection. Collections are represented as []any.
type Result struct {
	value any
}

// Value returns the underlying Go value (a scalar, []any, nil, or the unknown
// sentinel).
func (r Result) Value() any { return r.value }

// Eval parses and evaluates a FHIRPath expression against a context value.
func Eval(ctx context.Context, expression string, contextValue any) (Result, error) {
	expr, err := Parse(expression)
	if err != nil {
		return Result{}, err
	}
	return expr.eval(ctx, contextValue)
}

// EvalBool evaluates an expression as a boolean. The second return value is
// false when the result is unknown (an out-of-scope construct), in which case
// the first value is meaningless and callers must treat it as "not a
// violation".
func EvalBool(ctx context.Context, expression string, contextValue any) (bool, bool, error) {
	res, err := Eval(ctx, expression, contextValue)
	if err != nil {
		return false, false, err
	}
	b, known := resTruthy(res)
	return b, known, nil
}

// resTruthy converts a Result to FHIRPath truthiness. Returns (value, known).
func resTruthy(r Result) (bool, bool) {
	if isUnknown(r) {
		return false, false
	}
	switch v := r.value.(type) {
	case bool:
		return v, true
	case nil:
		return false, true
	case []any:
		if len(v) == 0 {
			return false, true
		}
		if len(v) == 1 {
			return resTruthy(Result{value: v[0]})
		}
		return true, true
	default:
		return true, true
	}
}
