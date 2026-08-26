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
	// Target lists the resource types a reference-type parameter can resolve
	// to. Empty for non-reference search parameters. Chaining is only possible
	// through reference parameters, so Target determines which chains are valid.
	Target []string
}
