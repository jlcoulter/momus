package generation

import (
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
	"github.com/jlcoulter/momus/internal/test/coverage"
)

func TestBuildSetupDatasetAddsSearchMatchSeed(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/patient", Type: "Patient", Elements: []model.ElementDefinition{
		{Path: "Patient", Min: 0, Max: "*"},
		{Path: "Patient.name", Min: 1, Max: "*", Types: []model.ElementType{{Code: "HumanName"}}},
	}})
	reg.AddSearchParameter(&model.SearchParameter{URL: "http://hl7.org/fhir/SearchParameter/Patient-name", Name: "name", Code: "name", Base: []string{"Patient"}, Type: "string", Expression: "Patient.name"})

	plan := &coverage.CoveragePlan{Requirements: []coverage.CoverageRequirement{
		{ID: "search|Patient|name|search-multiple-results", ResourceType: "Patient", Domain: coverage.CoverageDomainSearch, Variant: coverage.CoverageVariantSearchMultipleResults, SearchCode: "name"},
	}}
	opts := BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: reg}

	ds, err := BuildSetupDataset(plan, opts)
	if err != nil {
		t.Fatalf("BuildSetupDataset returned error: %v", err)
	}

	// Two matching resources must exist so `name=momus-search` returns >= 2.
	var matching int
	for _, inst := range ds.Resources {
		name, ok := inst.Resource["name"].([]any)
		if !ok || len(name) == 0 {
			continue
		}
		first, ok := name[0].(map[string]any)
		if !ok {
			continue
		}
		if first["family"] == "momus-search" || first["text"] == "momus-search" {
			matching++
		}
	}
	if matching != 2 {
		t.Fatalf("expected 2 search-matching seed resources, got %d", matching)
	}
}

func TestBuildSetupDatasetAddsIDSearchSeed(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/patient", Type: "Patient", Elements: []model.ElementDefinition{
		{Path: "Patient", Min: 0, Max: "*"},
		{Path: "Patient.name", Min: 1, Max: "*", Types: []model.ElementType{{Code: "HumanName"}}},
	}})
	reg.AddSearchParameter(&model.SearchParameter{URL: "http://hl7.org/fhir/SearchParameter/Resource-id", Name: "_id", Code: "_id", Base: []string{"Resource"}, Type: "token"})

	plan := &coverage.CoveragePlan{Requirements: []coverage.CoverageRequirement{
		{ID: "search|Patient|_id|valid", ResourceType: "Patient", Domain: coverage.CoverageDomainSearch, Variant: coverage.CoverageVariantSearchValid, SearchCode: "_id"},
	}}
	opts := BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: reg}

	ds, err := BuildSetupDataset(plan, opts)
	if err != nil {
		t.Fatalf("BuildSetupDataset returned error: %v", err)
	}

	// A resource with id "momus-search" must exist so `_id=momus-search` matches.
	found := false
	for _, inst := range ds.Resources {
		if inst.Resource["id"] == "momus-search" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a seed resource with id momus-search for the _id search")
	}
}

func TestSearchSeedSkipsNonMatchableSearch(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/patient", Type: "Patient", Elements: []model.ElementDefinition{
		{Path: "Patient", Min: 0, Max: "*"},
		{Path: "Patient.active", Min: 1, Max: "1", Types: []model.ElementType{{Code: "boolean"}}},
	}})
	// `active` is a boolean: the placeholder value "momus-search" cannot match,
	// so no matching seed is added (the search remains status-only).
	reg.AddSearchParameter(&model.SearchParameter{URL: "http://hl7.org/fhir/SearchParameter/Patient-active", Name: "active", Code: "active", Base: []string{"Patient"}, Type: "token", Expression: "Patient.active"})

	plan := &coverage.CoveragePlan{Requirements: []coverage.CoverageRequirement{
		{ID: "search|Patient|active|multiple", ResourceType: "Patient", Domain: coverage.CoverageDomainSearch, Variant: coverage.CoverageVariantSearchMultipleResults, SearchCode: "active"},
	}}
	opts := BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: reg}

	ds, err := BuildSetupDataset(plan, opts)
	if err != nil {
		t.Fatalf("BuildSetupDataset returned error: %v", err)
	}
	for _, inst := range ds.Resources {
		if b, ok := inst.Resource["active"].(bool); ok && b {
			t.Fatalf("expected no active:true seed for a non-matchable search; got %s", inst.LocalID)
		}
	}
}
