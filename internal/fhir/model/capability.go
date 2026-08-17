package model

// CapabilityStatement is the minimal normalised representation of a FHIR
// CapabilityStatement resource.
type CapabilityStatement struct {
	URL         string
	Version     string
	Name        string
	Status      string
	FhirVersion string
	// REST interactions are intentionally omitted for now.
}
