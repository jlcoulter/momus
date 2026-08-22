// Package golden contains the golden-matrix self-conformance runner. For each
// reference fixture in testdata/golden it builds a registry in-memory, derives
// a coverage plan, generates the test AST, snapshots it byte-identically, and
// runs it against the semantic mock asserting 100% pass. It is the developer's
// daily oracle: add a feature -> add a fixture -> every parameter's positive,
// negative, and edge cases are exercised and proven.
package golden

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

// Fixture is a reference IG expressed as data (no .tgz required).
type Fixture struct {
	StructureDefinitions []*model.StructureDefinition `json:"structureDefinitions"`
	SearchParameters     []*model.SearchParameter     `json:"searchParameters"`
	ValueSets            []*model.ValueSet            `json:"valueSets"`
	CodeSystems          []*model.CodeSystem          `json:"codeSystems"`
	CapabilityStatements []*model.CapabilityStatement `json:"capabilityStatements"`
	// Resources are sample conformant resources, each tagged with a profile URL
	// they are expected to conform to.
	Resources []SampleResource `json:"resources"`
}

// SampleResource is a conformant example resource and the profile it claims.
type SampleResource struct {
	ProfileURL string         `json:"profileUrl"`
	Resource   map[string]any `json:"resource"`
}

// LoadFixture reads a fixture JSON file and returns it.
func LoadFixture(path string) (*Fixture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var fx Fixture
	if err := json.Unmarshal(data, &fx); err != nil {
		return nil, fmt.Errorf("golden: parse fixture %s: %w", path, err)
	}
	return &fx, nil
}

// BuildRegistry builds an in-memory registry from a fixture using the same
// Add* calls the package loader uses.
func BuildRegistry(fx *Fixture) (*registry.Registry, error) {
	r := registry.New()
	for _, sd := range fx.StructureDefinitions {
		r.AddStructureDefinition(sd)
	}
	for _, sp := range fx.SearchParameters {
		r.AddSearchParameter(sp)
	}
	for _, vs := range fx.ValueSets {
		r.AddValueSet(vs)
	}
	for _, cs := range fx.CodeSystems {
		r.AddCodeSystem(cs)
	}
	for _, cap := range fx.CapabilityStatements {
		r.AddCapabilityStatement(cap)
	}
	return r, nil
}
