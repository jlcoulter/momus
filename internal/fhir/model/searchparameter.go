package model

// SearchParameter is the minimal normalised representation of a FHIR
// SearchParameter resource.
type SearchParameter struct {
	URL        string
	Name       string
	Code       string
	Base       []string // Resource types the parameter applies to.
	Type       string
	Expression string
}
