package model

// Resource is the generic representation of a FHIR resource instance, e.g. an
// example Patient or PractitionerRole resource shipped in a package. It
// preserves the resource's type, its canonical profile URLs (from
// meta.profile), and its raw JSON content so downstream layers can consult
// package example data as a source of conformant values.
//
// It is distinct from the conformance types (StructureDefinition, ValueSet,
// CodeSystem, CapabilityStatement, SearchParameter), which are normalised into
// typed models. Instance resources are kept opaque because Momus does not model
// every FHIR resource type; it needs only to index and traverse them.
type Resource struct {
	// ResourceType is the FHIR resource type (e.g. "Patient", "PractitionerRole").
	ResourceType string
	// ProfileURLs are the canonical URLs declared in the resource's meta.profile.
	ProfileURLs []string
	// Raw is the decoded JSON content of the resource.
	Raw map[string]any
}
