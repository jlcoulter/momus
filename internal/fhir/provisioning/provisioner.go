// Package provisioning defines how generated datasets are written to a
// FHIR server.
package provisioning

import (
	"context"

	"github.com/jlcoulter/momus/internal/fhir/model"
)

// Provisioner writes a Dataset to a target FHIR server.
type Provisioner interface {
	Provision(ctx context.Context, dataset *model.Dataset) error
}
