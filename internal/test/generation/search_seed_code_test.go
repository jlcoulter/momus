package generation

import (
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
	"github.com/jlcoulter/momus/internal/test/coverage"
)

// TestSearchSeedKeepsCodingPrimitive verifies that a token search on a `Coding`
// element (e.g. Endpoint.connectionType) sets the Coding's `code` member and
// never adds an illegal `coding` array, which servers reject with
// "Unrecognized property 'coding'" (HAPI-0521-style).
func TestSearchSeedKeepsCodingPrimitive(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/endpoint", Type: "Endpoint", Elements: []model.ElementDefinition{
		{Path: "Endpoint", Min: 0, Max: "*"},
		{Path: "Endpoint.status", Min: 1, Max: "1", Types: []model.ElementType{{Code: "code"}}},
		{Path: "Endpoint.connectionType", Min: 1, Max: "1", Types: []model.ElementType{{Code: "Coding"}}},
	}})
	reg.AddSearchParameter(&model.SearchParameter{URL: "http://hl7.org/fhir/SearchParameter/Endpoint-connection-type", Name: "connection-type", Code: "connection-type", Base: []string{"Endpoint"}, Type: "token", Expression: "Endpoint.connectionType"})

	plan := &coverage.CoveragePlan{Requirements: []coverage.CoverageRequirement{
		{ID: "search|Endpoint|connection-type|search-multiple-results", ResourceType: "Endpoint", Domain: coverage.CoverageDomainSearch, Variant: coverage.CoverageVariantSearchMultipleResults, SearchCode: "connection-type"},
	}}
	opts := BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: reg}

	ds, err := BuildSetupDataset(plan, opts)
	if err != nil {
		t.Fatalf("BuildSetupDataset returned error: %v", err)
	}
	for _, inst := range ds.Resources {
		ct, ok := inst.Resource["connectionType"]
		if !ok {
			continue
		}
		obj, ok := ct.(map[string]any)
		if !ok {
			t.Fatalf("connectionType = %T %v, want a Coding object", ct, ct)
		}
		if _, hasCoding := obj["coding"]; hasCoding {
			t.Fatalf("connectionType %v has an illegal 'coding' property on a Coding", obj)
		}
		if obj["code"] != "momus-search" {
			t.Fatalf("connectionType.code = %v, want momus-search", obj["code"])
		}
	}
}
