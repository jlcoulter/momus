package main

import (
	"github.com/jlcoulter/momus/internal/fhir/registry"
	"github.com/jlcoulter/momus/internal/fhir/validate"
	"github.com/jlcoulter/momus/internal/mock"
)

// mockValidatorAdapterFrom builds a mock.Validator-backed semantic validator
// from a registry (used by "mock --semantic").
func mockValidatorAdapterFrom(reg *registry.Registry) mock.Validator {
	return validate.NewMockAdapter(reg)
}
