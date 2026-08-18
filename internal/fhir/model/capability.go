package model

// CapabilityStatement is the minimal normalised representation of a FHIR
// CapabilityStatement resource.
type CapabilityStatement struct {
	URL         string
	Version     string
	Name        string
	Status      string
	FhirVersion string
	Rest        []CapabilityStatementRest
}

// CapabilityStatementRest describes a REST endpoint block in a CapabilityStatement.
type CapabilityStatementRest struct {
	Mode     string
	Resource []CapabilityStatementRestResource
}

// CapabilityStatementRestResource describes supported interactions for a resource type.
type CapabilityStatementRestResource struct {
	Type             string
	Profile          string
	SupportedProfile []string
	Interaction      []CapabilityStatementInteraction
}

// CapabilityStatementInteraction represents a supported REST interaction code.
type CapabilityStatementInteraction struct {
	Code string
}
