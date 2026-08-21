// Package assertions defines the assertion interface and result type.
package assertions

import (
	"context"
	"net/http"
)

// Result is the outcome of executing a step, against which assertions are
// evaluated.
type Result struct {
	StatusCode int
	Body       []byte
	Headers    http.Header
	Variables  map[string]any
}

// Assertion evaluates a result and returns an error if the assertion fails.
type Assertion interface {
	Evaluate(ctx context.Context, result Result) error
}
