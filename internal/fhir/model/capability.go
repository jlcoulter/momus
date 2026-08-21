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
	// Operation lists custom operations ($name) the server supports for this
	// resource type.
	Operation []CapabilityStatementOperation
	// SearchParam lists search parameters the server supports for this resource
	// type. When non-empty, only these search parameters should be included in
	// the test plan; when empty, no search-parameter restriction is applied.
	SearchParam []CapabilityStatementSearchParam
}

// CapabilityStatementSearchParam represents a search parameter declared in a
// CapabilityStatement resource entry.
type CapabilityStatementSearchParam struct {
	Name       string
	Definition string
	Type       string
}

// CapabilityStatementOperation represents a custom operation ($name) supported
// for a resource type, referencing its OperationDefinition.
type CapabilityStatementOperation struct {
	Name       string
	Definition string
}

// CapabilityStatementInteraction represents a supported REST interaction code.
type CapabilityStatementInteraction struct {
	Code string
}
