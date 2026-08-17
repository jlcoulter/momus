package model

// ValueSet is the minimal normalised representation of a FHIR ValueSet.
type ValueSet struct {
	URL     string
	Version string
	Name    string
	Status  string
	// Expansion and composition are intentionally omitted for now.
}
