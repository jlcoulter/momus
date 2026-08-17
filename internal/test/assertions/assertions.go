// Package assertions defines the assertion interface and result type.
package assertions

import "context"

// Result is the outcome of executing a step, against which assertions are
// evaluated.
type Result struct {
	StatusCode int
	Body       []byte
	Variables  map[string]any
}

// Assertion evaluates a result and returns an error if the assertion fails.
//
// No evaluation engines (JSONPath/FHIRPath) are implemented yet.
type Assertion interface {
	Evaluate(ctx context.Context, result Result) error
}
