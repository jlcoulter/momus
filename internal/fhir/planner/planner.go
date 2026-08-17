// Package planner turns a test input into a TestPlan.
package planner

import (
	"context"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/test/ast"
)

// Input describes what the planner should turn into a TestPlan.
type Input struct {
	// Requirements is the set of data requirements to plan for.
	Requirements []model.DataRequirement
	// BaseURL is the target FHIR server, if known at plan time.
	BaseURL string
}

// Planner builds a TestPlan from an Input.
type Planner interface {
	Plan(ctx context.Context, input Input) (*TestPlan, error)
}

// TestPlan contains the AST to be executed by the runner.
type TestPlan struct {
	Root ast.Node
}
