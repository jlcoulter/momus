// Package resource defines the interface for the FHIR resource generator.
package resource

import (
	"context"

	"github.com/jlcoulter/momus/internal/fhir/model"
)

// Generator produces a Dataset satisfying a DataRequirement.
//
// Implementations depend on the Registry rather than parsing
// StructureDefinitions themselves.
type Generator interface {
	Generate(ctx context.Context, requirement model.DataRequirement) (*model.Dataset, error)
}
