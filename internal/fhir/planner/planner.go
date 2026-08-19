// Package planner turns test data requirements into an executable TestPlan.
package planner

import (
	"context"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/test/ast"
)

// Generator turns DataRequirements into concrete Datasets.
type Generator interface {
	Generate(ctx context.Context, requirement model.DataRequirement) (*model.Dataset, error)
}

// Input describes what the planner should turn into a TestPlan.
type Input struct {
	// Requirements is the set of data requirements to plan for.
	Requirements []model.DataRequirement
	// BaseURL is the target FHIR server the plan will execute against.
	BaseURL string
	// WriteBaseURL, when set, is used for the provisioning (PUT) requests instead
	// of BaseURL, so resource creation can target a different endpoint than
	// read/search requests. When empty, provisioning uses BaseURL.
	WriteBaseURL string
}

// Planner builds a TestPlan from an Input.
type Planner interface {
	Plan(ctx context.Context, input Input) (*TestPlan, error)
}

// TestPlan contains the generated data and the executable AST to be run by the
// runner. Dataset is the generated state the plan provisions and verifies; Root
// is the execution workflow expressed with Sequence/Parallel nodes.
type TestPlan struct {
	Root ast.Node
	// Dataset is the generated data backing the plan. The same Dataset may serve
	// multiple plans.
	Dataset *model.Dataset
}
