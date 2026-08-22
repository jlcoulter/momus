package generation

import (
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

// BuildOptions controls FHIR test-plan construction behavior. It is the
// FHIR-side counterpart to core/generation.BuildOptions, carrying the registry
// and capability scope used to synthesize FHIR payloads and seed data.
type BuildOptions struct {
	BaseURL string
	// WriteBaseURL, when set, is used for write requests (PUT/PATCH/POST/DELETE)
	// instead of BaseURL, so resource creation can target a different endpoint
	// than read/search requests. When empty, write requests use BaseURL.
	WriteBaseURL string
	// Registry is the source of truth for FHIR payload and seed synthesis.
	Registry *registry.Registry
	// PreferredProfileURLsByResource, when non-empty, orders the profile URLs
	// used to synthesize a resource type's payloads.
	PreferredProfileURLsByResource map[string][]string
	// Strength is the interaction strength used when generating.
	Strength int
	// Exhaustive populates optional (Min == 0) elements in addition to required
	// ones, with randomised presence.
	Exhaustive bool
	// CapabilityResourceTypes, when non-nil, restricts the seed dataset (and the
	// transitive reference closure) to resource types the target server's
	// CapabilityStatement declares.
	CapabilityResourceTypes map[string]struct{}
	// CapabilityProfiles, when non-nil, restricts the seed dataset to resource
	// profiles the target server's CapabilityStatement declares.
	CapabilityProfiles map[string]struct{}
	// Progress, when non-nil, is invoked after each requirement is processed
	// during generation, with the number of requirements completed so far and
	// the total number of requirements. It is used to render a live progress
	// bar in the CLI.
	Progress func(done, total int)
}
