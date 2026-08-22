// Package validate implements a FHIR profile validator. It decides whether a
// JSON resource conforms to a StructureDefinition profile and reports each
// violation as a structured Issue with the offending element path, the check
// kind (cardinality, datatype, terminology, slice, fixed, pattern, invariant),
// and a one-line human-readable message.
//
// It is the inverse of internal/fhir/generation's synthesizeBody: generation
// produces conformant payloads, validation checks them.
package validate

import (
	"context"
	"fmt"

	"github.com/jlcoulter/momus/internal/fhir/registry"
)

// Issue is a single validation violation: a path, a check kind, a one-line
// message, and the offending value (when applicable).
type Issue struct {
	Path    string `json:"path"`            // canonical FHIR path, e.g. "Patient.name"
	Kind    string `json:"kind"`            // cardinality | datatype | terminology | slice | fixed | pattern | invariant
	Message string `json:"message"`         // human-readable, one line
	Value   any    `json:"value,omitempty"` // the offending value, when applicable
}

// Validator is the minimal interface the semantic mock depends on. It decouples
// internal/mock from the concrete ProfileValidator implementation.
type Validator interface {
	// Validate returns the conformance issues for a single resource against the
	// given profile URL. An empty, non-nil slice means the resource conforms.
	Validate(ctx context.Context, profileURL string, resource map[string]any) ([]Issue, error)
}

// ProfileValidator validates resources against profiles using a registry as the
// source of StructureDefinition, ValueSet, and CodeSystem knowledge.
type ProfileValidator struct {
	reg *registry.Registry
}

// New returns a ProfileValidator backed by the given registry.
func New(reg *registry.Registry) *ProfileValidator {
	return &ProfileValidator{reg: reg}
}

// Validate implements Validator. It resolves profileURL to a ResolvedProfile
// and runs every structural check against the resource. A nil or empty result
// means the resource conforms.
func (v *ProfileValidator) Validate(ctx context.Context, profileURL string, resource map[string]any) ([]Issue, error) {
	if resource == nil {
		return nil, fmt.Errorf("validate: resource is nil")
	}
	profile, err := v.reg.ResolveProfile(profileURL)
	if err != nil {
		return nil, fmt.Errorf("validate: resolve profile %s: %w", profileURL, err)
	}
	if profile == nil || profile.Root == nil {
		return nil, fmt.Errorf("validate: profile %s is not resolvable", profileURL)
	}
	return v.checkProfile(ctx, profile, resource), nil
}
