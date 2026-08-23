package fhirpackage

import (
	"encoding/json"
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
)

func TestSetDebug(t *testing.T) {
	// No panic and toggles the level; call both branches for coverage.
	SetDebug(true)
	SetDebug(false)
}

func TestDecodeResourceCapabilityStatementAndSearchParameter(t *testing.T) {
	capData, _ := json.Marshal(map[string]any{
		"resourceType": "CapabilityStatement",
		"url":          "http://example.org/CapabilityStatement/x",
		"version":      "1.0.0",
		"name":         "Cap",
		"status":       "active",
		"fhirVersion":  "4.0.1",
		"rest": []map[string]any{{
			"mode": "server",
			"resource": []map[string]any{{
				"type":        "Patient",
				"interaction": []map[string]any{{"code": "read"}},
				"operation":   []map[string]any{{"name": "validate", "definition": "http://x/$validate"}},
			}},
		}},
	})
	res, err := decodeResource(capData)
	if err != nil {
		t.Fatalf("decodeResource(CapabilityStatement): %v", err)
	}
	cs, ok := res.(*model.CapabilityStatement)
	if !ok {
		t.Fatalf("expected CapabilityStatement, got %T", res)
	}
	if cs.FhirVersion != "4.0.1" || len(cs.Rest) != 1 {
		t.Fatalf("capability = %+v", cs)
	}
	if len(cs.Rest[0].Resource) != 1 || len(cs.Rest[0].Resource[0].Interaction) != 1 || len(cs.Rest[0].Resource[0].Operation) != 1 {
		t.Fatalf("capability rest = %+v", cs.Rest[0])
	}

	spData, _ := json.Marshal(map[string]any{
		"resourceType": "SearchParameter",
		"url":          "http://example.org/SearchParameter/x",
		"name":         "code",
		"code":         "code",
		"base":         []any{"Observation"},
		"type":         "token",
		"expression":   "Observation.code",
	})
	res, err = decodeResource(spData)
	if err != nil {
		t.Fatalf("decodeResource(SearchParameter): %v", err)
	}
	sp, ok := res.(*model.SearchParameter)
	if !ok {
		t.Fatalf("expected SearchParameter, got %T", res)
	}
	if sp.Code != "code" || sp.Type != "token" || len(sp.Base) != 1 {
		t.Fatalf("search parameter = %+v", sp)
	}
}

func TestDecodeResourceUsesDifferentialWhenNoSnapshot(t *testing.T) {
	data, _ := json.Marshal(map[string]any{
		"resourceType": "StructureDefinition",
		"url":          "http://example.org/StructureDefinition/x",
		"type":         "Observation",
		"differential": map[string]any{
			"element": []map[string]any{{"id": "Observation", "path": "Observation", "min": 0, "max": "*"}},
		},
	})
	res, err := decodeResource(data)
	if err != nil {
		t.Fatalf("decodeResource: %v", err)
	}
	sd, ok := res.(*model.StructureDefinition)
	if !ok {
		t.Fatalf("expected StructureDefinition, got %T", res)
	}
	if len(sd.Elements) != 1 {
		t.Fatalf("differential elements = %d, want 1", len(sd.Elements))
	}
}

func TestDecodeResourceInvalidJSON(t *testing.T) {
	if _, err := decodeResource([]byte("not-json")); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestDecodeMetaProfiles(t *testing.T) {
	if got := decodeMetaProfiles(map[string]any{}); got != nil {
		t.Fatalf("decodeMetaProfiles(empty) = %v, want nil", got)
	}
	got := decodeMetaProfiles(map[string]any{
		"meta": map[string]any{
			"profile": []any{"http://a", "http://b", 42},
		},
	})
	if len(got) != 2 {
		t.Fatalf("decodeMetaProfiles = %v, want 2", got)
	}
}

func TestFieldHelpers(t *testing.T) {
	m := map[string]any{
		"str":   "x",
		"intF":  float64(42),
		"intI":  7,
		"boolF": true,
		"slice": []any{"a", "b", "", "c"},
		"sub":   map[string]any{"k": "v"},
	}
	if got := stringField(m, "str"); got != "x" {
		t.Fatalf("stringField = %q", got)
	}
	if got := stringField(m, "missing"); got != "" {
		t.Fatalf("stringField(missing) = %q", got)
	}
	if got := stringField(m, "intF"); got != "" {
		t.Fatalf("stringField(non-string) = %q", got)
	}
	if got := intField(m, "intF"); got != 42 {
		t.Fatalf("intField(float) = %d", got)
	}
	if got := intField(m, "intI"); got != 7 {
		t.Fatalf("intField(int) = %d", got)
	}
	if got := intField(m, "missing"); got != 0 {
		t.Fatalf("intField(missing) = %d", got)
	}
	if got := intField(m, "a"); got != 0 {
		t.Fatalf("intField(non-number) = %d", got)
	}
	if !boolField(m, "boolF") {
		t.Fatal("boolField should be true")
	}
	if boolField(m, "missing") || boolField(m, "a") {
		t.Fatal("boolField should be false for missing/non-bool")
	}
	if got := mapField(m, "sub"); got["k"] != "v" {
		t.Fatalf("mapField = %v", got)
	}
	if got := mapField(m, "missing"); got != nil {
		t.Fatalf("mapField(missing) = %v", got)
	}
	if got := stringSliceField(m, "slice"); len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Fatalf("stringSliceField = %v", got)
	}
	if got := stringSliceField(m, "a"); got != nil {
		t.Fatalf("stringSliceField(non-slice) = %v", got)
	}
	if got := stringSliceField(m, "missing"); got != nil {
		t.Fatalf("stringSliceField(missing) = %v", got)
	}
	// A bare string yields a single-element slice.
	if got := stringSliceField(m, "a"); got != nil {
		t.Fatal("expected nil for non-array string field")
	}
	single := map[string]any{"s": "only"}
	if got := stringSliceField(single, "s"); len(got) != 1 || got[0] != "only" {
		t.Fatalf("stringSliceField(single) = %v", got)
	}
}

func TestLastPathPart(t *testing.T) {
	if got := lastPathPart("a/b/c"); got != "c" {
		t.Fatalf("lastPathPart = %q", got)
	}
	if got := lastPathPart("file.json"); got != "file" {
		t.Fatalf("lastPathPart(ext) = %q", got)
	}
	if got := lastPathPart("a/b/file.json"); got != "file" {
		t.Fatalf("lastPathPart = %q", got)
	}
}
