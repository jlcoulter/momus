package model

// CodeSystem is the minimal normalised representation of a FHIR CodeSystem.
type CodeSystem struct {
	URL      string
	Version  string
	Name     string
	Status   string
	Concepts []CodeSystemConcept
}

type CodeSystemConcept struct {
	Code     string
	Display  string
	Concepts []CodeSystemConcept
}
