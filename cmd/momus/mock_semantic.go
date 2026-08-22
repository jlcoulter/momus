package main

import (
	"context"

	"github.com/jlcoulter/momus/internal/fhir/registry"
	"github.com/jlcoulter/momus/internal/fhir/validate"
	"github.com/jlcoulter/momus/internal/mock"
)

// validatorAdapter adapts a validate.ProfileValidator to the mock.Validator
// interface, whose Issue type is declared locally in the mock package.
type validatorAdapter struct {
	inner *validate.ProfileValidator
}

func (a validatorAdapter) Validate(ctx context.Context, profileURL string, resource map[string]any) ([]mock.Issue, error) {
	issues, err := a.inner.Validate(ctx, profileURL, resource)
	if err != nil {
		return nil, err
	}
	out := make([]mock.Issue, 0, len(issues))
	for _, iss := range issues {
		out = append(out, mock.Issue{Path: iss.Path, Kind: iss.Kind, Message: iss.Message, Value: iss.Value})
	}
	return out, nil
}

// mockValidatorAdapterFrom builds a mock.Validator-backed semantic validator
// from a registry (used by "mock --semantic").
func mockValidatorAdapterFrom(reg *registry.Registry) mock.Validator {
	return validatorAdapter{inner: validate.New(reg)}
}
