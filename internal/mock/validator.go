package mock

import "context"

// Validator is implemented by a FHIR profile validator (internal/fhir/validate)
// and consumed by the mock server to enforce conformance on writes. It is a
// local interface so internal/mock stays decoupled from the concrete validator.
type Validator interface {
	// Validate returns the conformance issues for a resource against a profile
	// URL. An empty slice means the resource conforms.
	Validate(ctx context.Context, profileURL string, resource map[string]any) ([]Issue, error)
}

// Issue is a single validation violation. It mirrors validate.Issue but is
// declared locally to keep internal/mock free of a FHIR-side dependency.
type Issue struct {
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Value   any    `json:"value,omitempty"`
}

// WithValidator installs a profile validator so the mock rejects
// non-conformant PUT/POST payloads with a 422 + OperationOutcome. Without it,
// the mock stores any payload (the existing echo behaviour).
func WithValidator(v Validator) Option {
	return func(s *Server) { s.validator = v }
}
