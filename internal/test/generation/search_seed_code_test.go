package generation

import (
	"testing"

	"github.com/jlcoulter/momus/internal/core/coverage"
	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
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

// TestSearchSeedKeepsRepeatableCodeableConcept verifies that a token search on a
// repeatable CodeableConcept element (e.g. Endpoint.payloadType) sets the code on
// the first element without turning the required JSON array into an object.
func TestSearchSeedKeepsRepeatableCodeableConcept(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/endpoint", Type: "Endpoint", Elements: []model.ElementDefinition{
		{Path: "Endpoint", Min: 0, Max: "*"},
		{Path: "Endpoint.status", Min: 1, Max: "1", Types: []model.ElementType{{Code: "code"}}},
		{Path: "Endpoint.connectionType", Min: 1, Max: "1", Types: []model.ElementType{{Code: "Coding"}}},
		{Path: "Endpoint.payloadType", Min: 1, Max: "*", Types: []model.ElementType{{Code: "CodeableConcept"}}},
	}})
	reg.AddSearchParameter(&model.SearchParameter{URL: "http://hl7.org/fhir/SearchParameter/Endpoint-payload-type", Name: "payload-type", Code: "payload-type", Base: []string{"Endpoint"}, Type: "token", Expression: "Endpoint.payloadType"})

	plan := &coverage.CoveragePlan{Requirements: []coverage.CoverageRequirement{
		{ID: "search|Endpoint|payload-type|search-multiple-results", ResourceType: "Endpoint", Domain: coverage.CoverageDomainSearch, Variant: coverage.CoverageVariantSearchMultipleResults, SearchCode: "payload-type"},
	}}
	opts := BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: reg}

	ds, err := BuildSetupDataset(plan, opts)
	if err != nil {
		t.Fatalf("BuildSetupDataset returned error: %v", err)
	}
	for _, inst := range ds.Resources {
		pt, ok := inst.Resource["payloadType"]
		if !ok {
			continue
		}
		arr, ok := pt.([]any)
		if !ok {
			t.Fatalf("payloadType = %T %v, want a JSON array", pt, pt)
		}
		if len(arr) == 0 {
			t.Fatal("payloadType is empty")
		}
		first, ok := arr[0].(map[string]any)
		if !ok {
			t.Fatalf("payloadType[0] = %T, want an object", arr[0])
		}
		coding, ok := first["coding"].([]any)
		if !ok || len(coding) == 0 {
			t.Fatalf("payloadType[0] has no coding: %v", first)
		}
		if c, ok := coding[0].(map[string]any); ok && c["code"] != "momus-search" {
			t.Fatalf("payloadType[0].coding[0].code = %v, want momus-search", c["code"])
		}
	}
}

// TestSearchSeedKeepsRepeatableCodePrimitive verifies that a token search on a
// repeatable primitive `code` element (e.g. Endpoint.payloadMimeType) keeps the
// value as an array of strings, never an object ("This property must be a simple
// value, not an Object").
func TestSearchSeedKeepsRepeatableCodePrimitive(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/endpoint", Type: "Endpoint", Elements: []model.ElementDefinition{
		{Path: "Endpoint", Min: 0, Max: "*"},
		{Path: "Endpoint.status", Min: 1, Max: "1", Types: []model.ElementType{{Code: "code"}}},
		{Path: "Endpoint.connectionType", Min: 1, Max: "1", Types: []model.ElementType{{Code: "Coding"}}},
		{Path: "Endpoint.payloadMimeType", Min: 0, Max: "*", Types: []model.ElementType{{Code: "code"}}},
	}})
	reg.AddSearchParameter(&model.SearchParameter{URL: "http://hl7.org/fhir/SearchParameter/Endpoint-payload-mimetype", Name: "payload-mimetype", Code: "payload-mimetype", Base: []string{"Endpoint"}, Type: "token", Expression: "Endpoint.payloadMimeType"})

	plan := &coverage.CoveragePlan{Requirements: []coverage.CoverageRequirement{
		{ID: "search|Endpoint|payload-mimetype|search-multiple-results", ResourceType: "Endpoint", Domain: coverage.CoverageDomainSearch, Variant: coverage.CoverageVariantSearchMultipleResults, SearchCode: "payload-mimetype"},
	}}
	opts := BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: reg}

	ds, err := BuildSetupDataset(plan, opts)
	if err != nil {
		t.Fatalf("BuildSetupDataset returned error: %v", err)
	}
	for _, inst := range ds.Resources {
		pt, ok := inst.Resource["payloadMimeType"]
		if !ok {
			continue
		}
		switch v := pt.(type) {
		case []any:
			if len(v) == 0 {
				t.Fatal("payloadMimeType is empty")
			}
			if s, ok := v[0].(string); !ok || s != "momus-search" {
				t.Fatalf("payloadMimeType[0] = %T %v, want string momus-search", v[0], v[0])
			}
		case string:
			if pt != "momus-search" {
				t.Fatalf("payloadMimeType = %q, want momus-search", pt)
			}
		default:
			t.Fatalf("payloadMimeType = %T %v, want a scalar string or array of strings", pt, pt)
		}
	}
}
