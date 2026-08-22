// Package runner will execute test plans.
package runner

import (
	"context"

	"github.com/jlcoulter/momus/internal/core/ast"
)

// Runner executes a test plan's AST.
//
// The runner only knows about the AST, never about raw FHIR package files.
type Runner interface {
	Run(ctx context.Context, plan ast.Node) error
}
