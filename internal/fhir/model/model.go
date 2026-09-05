// Package model contains Momus's internal, normalised FHIR domain models.
//
// The FHIR conformance types (StructureDefinition, ElementDefinition,
// ValueSet, CodeSystem, CapabilityStatement, SearchParameter, Resource) are
// re-exported from the fhir-registry library, which is the canonical
// representation. Momus-specific types (Dataset, ResourceInstance, Reference)
// and the resolved element tree (ElementNode, SliceNode, ResolvedProfile)
// remain defined here.
package model

import (
	"encoding/json"
	"strconv"

	fhir "github.com/jlcoulter/fhir-registry"
)

// StructureDefinition is the normalised subset of a FHIR StructureDefinition
// resource that Momus cares about. It carries a flat element list; the
// fhir-registry representation (Snapshot/Differential) is produced at the
// registry boundary via fhir.NewStructureDefinition.
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
	// HasSnapshot reports whether Elements came from a complete snapshot
	// (rather than a differential). When true, ToFhir emits a Snapshot and the
	// registry does not attempt to merge a base definition.
	HasSnapshot bool
}

// ToFhir converts this definition into the fhir-registry representation with
// the element list as its snapshot. When the definition has a base definition
// and the elements are a differential (not a complete snapshot), they are
// stored as a differential so the registry's ensureSnapshot merges the base
// definition's elements.
func (sd *StructureDefinition) ToFhir() *fhir.StructureDefinition {
	if sd.BaseDefinition != "" && !sd.HasSnapshot {
		return fhir.NewStructureDefinitionDiff(sd.URL, sd.Name, sd.Type, sd.Kind, sd.BaseDefinition, sd.Derivation, sd.Elements)
	}
	return fhir.NewStructureDefinition(sd.URL, sd.Name, sd.Type, sd.Kind, sd.BaseDefinition, sd.Derivation, sd.Elements)
}

// UnmarshalJSON decodes a StructureDefinition from its JSON form. It accepts
// both the flat "elements" array (Momus's legacy shape, with string "max"
// values) and the standard FHIR snapshot/differential shape.
func (sd *StructureDefinition) UnmarshalJSON(data []byte) error {
	type alias struct {
		URL            string
		Version        string
		Name           string
		Title          string
		Type           string
		BaseDefinition string
		Kind           string
		Derivation     string
		Elements       []json.RawMessage `json:"elements"`
	}
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	var head struct {
		Snapshot     *json.RawMessage `json:"snapshot"`
		Differential *json.RawMessage `json:"differential"`
	}
	_ = json.Unmarshal(data, &head)
	sd.HasSnapshot = head.Snapshot != nil
	sd.URL = a.URL
	sd.Version = a.Version
	sd.Name = a.Name
	sd.Title = a.Title
	sd.Type = a.Type
	sd.BaseDefinition = a.BaseDefinition
	sd.Kind = a.Kind
	sd.Derivation = a.Derivation
	for _, raw := range a.Elements {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			return err
		}
		// The legacy "elements" shape carries "max" as a string ("*", "1").
		if maxStr, ok := m["max"].(string); ok {
			if maxStr == "*" {
				m["max"] = fhir.MaxUnbounded
			} else if n, err := strconv.Atoi(maxStr); err == nil {
				m["max"] = fhir.Max(n)
			}
		}
		re, err := json.Marshal(m)
		if err != nil {
			return err
		}
		var e ElementDefinition
		if err := json.Unmarshal(re, &e); err != nil {
			return err
		}
		sd.Elements = append(sd.Elements, e)
	}
	return nil
}

// ElementDefinition describes a single element in a structure, including its
// cardinality, types, and constraints.
type ElementDefinition = fhir.ElementDefinition

// ElementConstraint is a FHIR invariant (constraint) attached to an element.
type ElementConstraint = fhir.ElementConstraint

// ElementType is a possible type for an element.
type ElementType = fhir.ElementType

// Binding links an element to a terminology value set.
type Binding = fhir.Binding

// ValueSet is the minimal normalised representation of a FHIR ValueSet.
type ValueSet = fhir.ValueSet

// ValueSetCompose is the "compose" block of a ValueSet.
type ValueSetCompose = fhir.ValueSetCompose

// ValueSetExpansion is the "expansion" block of a ValueSet.
type ValueSetExpansion = fhir.ValueSetExpansion

// ValueSetInclude references a code system contributing codes to a ValueSet.
type ValueSetInclude = fhir.ValueSetInclude

// ValueSetExpansionContains is a single code in a ValueSet expansion.
type ValueSetExpansionContains = fhir.ValueSetExpansionContains

// ConceptReference is a single code reference within a ValueSet include.
type ConceptReference = fhir.ConceptReference

// CodeSystem is the minimal normalised representation of a FHIR CodeSystem.
type CodeSystem = fhir.CodeSystem

// CodeSystemConcept is a single code in a CodeSystem.
type CodeSystemConcept = fhir.CodeSystemConcept

// CapabilityStatement is the minimal normalised representation of a FHIR
// CapabilityStatement resource.
type CapabilityStatement = fhir.CapabilityStatement

// CapabilityStatementRest describes a REST endpoint block in a CapabilityStatement.
type CapabilityStatementRest = fhir.CapabilityStatementRest

// CapabilityStatementRestResource describes supported interactions for a resource type.
type CapabilityStatementRestResource = fhir.CapabilityStatementRestResource

// CapabilityStatementSearchParam represents a search parameter declared in a
// CapabilityStatement resource entry.
type CapabilityStatementSearchParam = fhir.CapabilityStatementSearchParam

// CapabilityStatementOperation represents a custom operation ($name) supported
// for a resource type, referencing its OperationDefinition.
type CapabilityStatementOperation = fhir.CapabilityStatementOperation

// CapabilityStatementInteraction represents a supported REST interaction code.
type CapabilityStatementInteraction = fhir.CapabilityStatementInteraction

// SearchParameter is the minimal normalised representation of a FHIR
// SearchParameter resource.
type SearchParameter = fhir.SearchParameter

// Resource is the generic representation of a FHIR resource instance, e.g. an
// example Patient or PractitionerRole resource shipped in a package.
type Resource = fhir.Resource
