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
	if p == nil {
		debug("registry build requested with nil package")
		return r, nil
	}

	debug("building registry", "packageName", p.Name, "packageVersion", p.Version, "resources", len(p.Resources))

	var sdCount, vsCount, csCount, spCount, capCount, skipped int
	for _, res := range p.Resources {
		switch v := res.(type) {
		case *model.StructureDefinition:
			r.AddStructureDefinition(v)
			sdCount++
		case *model.ValueSet:
			r.AddValueSet(v)
			vsCount++
		case *model.CodeSystem:
			r.AddCodeSystem(v)
			csCount++
		case *model.SearchParameter:
			r.AddSearchParameter(v)
			spCount++
		case *model.CapabilityStatement:
			r.AddCapabilityStatement(v)
			capCount++
		default:
			skipped++
			debug("registry skipping unsupported resource type", "type", res)
		}
	}

	debug("registry build complete",
		"structureDefinitions", sdCount,
		"valueSets", vsCount,
		"codeSystems", csCount,
		"searchParameters", spCount,
		"capabilityStatements", capCount,
		"skipped", skipped,
	)
	return r, nil
}
