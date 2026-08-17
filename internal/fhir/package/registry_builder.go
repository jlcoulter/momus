package fhirpackage

import (
	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

// RegistryBuilder builds a Registry from a loaded Package. It identifies
// each resource by its FHIR resource type and routes it to the correct
// registry index.
type RegistryBuilder struct{}

// NewRegistryBuilder returns a new RegistryBuilder.
func NewRegistryBuilder() *RegistryBuilder {
	return &RegistryBuilder{}
}

// Build loads every resource in p into a freshly created Registry.
// Unknown resource types are skipped.
func (b *RegistryBuilder) Build(p *Package) (*registry.Registry, error) {
	r := registry.New()
	for _, res := range p.Resources {
		switch v := res.(type) {
		case *model.StructureDefinition:
			r.AddStructureDefinition(v)
		case *model.ValueSet:
			r.AddValueSet(v)
		case *model.CodeSystem:
			r.AddCodeSystem(v)
		case *model.SearchParameter:
			r.AddSearchParameter(v)
		case *model.CapabilityStatement:
			r.AddCapabilityStatement(v)
		}
	}
	return r, nil
}
