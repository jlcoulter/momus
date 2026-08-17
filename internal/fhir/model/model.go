// Package model contains Momus's internal, normalised FHIR domain models.
//
// These types are deliberately minimal: they represent only the subset of
// the FHIR specification that Momus needs to reason about, not a complete
// FHIR SDK. Keep this package free of any I/O, parsing, or execution logic.
package model

// StructureDefinition represents the normalised subset of a FHIR
// StructureDefinition resource that Momus cares about.
type StructureDefinition struct {
	URL            string
	Version        string
	Name           string
	Title          string
	Type           string
	BaseDefinition string
	Kind           string
	Derivation     string
	Elements       []ElementDefinition
}

// ElementDefinition describes a single element in a structure, including
// its cardinality, types, and constraints.
type ElementDefinition struct {
	ID            string
	Path          string
	Name          string
	Min           int
	Max           string
	Types         []ElementType
	MustSupport   bool
	Fixed         any
	Pattern       any
	Binding       *Binding
	Profile       []string
	TargetProfile []string
	SliceName     string
}

// ElementType is a possible type for an element.
type ElementType struct {
	Code          string
	Profile       []string
	TargetProfile []string
}

// Binding links an element to a terminology value set.
type Binding struct {
	Strength string
	ValueSet string
}
