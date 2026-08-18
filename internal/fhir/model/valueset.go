package model

// ValueSet is the minimal normalised representation of a FHIR ValueSet.
type ValueSet struct {
	URL               string
	Version           string
	Name              string
	Status            string
	ComposeIncludes   []ValueSetInclude
	ExpansionContains []ValueSetExpansionContains
}

type ValueSetInclude struct {
	System   string
	Concepts []ConceptReference
}

type ValueSetExpansionContains struct {
	System   string
	Code     string
	Display  string
	Contains []ValueSetExpansionContains
}

type ConceptReference struct {
	Code    string
	Display string
}
