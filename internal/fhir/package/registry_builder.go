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
	if p == nil {
		debug("registry build requested with nil package")
		return registry.New(), nil
	}
	return b.BuildFromPackages([]*Package{p})
}

// BuildFromPackages loads every resource from pkgs into a freshly created
// Registry. Unknown resource types are skipped.
func (b *RegistryBuilder) BuildFromPackages(pkgs []*Package) (*registry.Registry, error) {
	return b.buildFromPackages(pkgs, nil)
}

// BuildFromPackagesScoped loads every resource from pkgs into a freshly
// created Registry, then restricts the scope of test-generation subjects to
// the StructureDefinitions declared by root. Every package's resources remain
// indexed so dependencies (referenced profiles, base definitions, value sets)
// resolve, but only root's StructureDefinitions are returned by
// ScopedStructureDefinitions. When root is nil, no scope is applied and every
// indexed StructureDefinition is a subject.
func (b *RegistryBuilder) BuildFromPackagesScoped(pkgs []*Package, root *Package) (*registry.Registry, error) {
	return b.buildFromPackages(pkgs, root)
}

func (b *RegistryBuilder) buildFromPackages(pkgs []*Package, root *Package) (*registry.Registry, error) {
	r := registry.New()
	if len(pkgs) == 0 {
		debug("registry build requested with no packages")
		return r, nil
	}

	debug("building registry from package set", "packages", len(pkgs))

	var sdCount, vsCount, csCount, spCount, capCount, skipped int
	for _, p := range pkgs {
		if p == nil {
			debug("registry build skipping nil package entry")
			continue
		}

		debug("building registry package", "packageName", p.Name, "packageVersion", p.Version, "resources", len(p.Resources))
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
	}

	if root != nil {
		scope := make([]string, 0)
		for _, res := range root.Resources {
			if sd, ok := res.(*model.StructureDefinition); ok && sd != nil && sd.URL != "" {
				scope = append(scope, sd.URL)
			}
		}
		r.SetScope(scope)
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
