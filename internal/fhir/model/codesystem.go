package model

// CodeSystem is the minimal normalised representation of a FHIR CodeSystem.
type CodeSystem struct {
	URL     string
	Version string
	Name    string
	Status  string
	// Concept content is intentionally omitted for now.
}
