package generation

import (
	"testing"

	"github.com/jlcoulter/momus/internal/core/coverage"
	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

func TestElementSegmentsNegative(t *testing.T) {
	if got := elementSegments("Patient.name.given"); len(got) != 2 || got[0] != "name" {
		t.Fatalf("elementSegments = %v", got)
	}
	if got := elementSegments("Patient"); got != nil {
		t.Fatalf("elementSegments(root) = %v", got)
	}
}

func TestLookupChild(t *testing.T) {
	m := map[string]any{"valueString": "x", "name": "n"}
	if v, key, ok := lookupChild(m, "value"); !ok || key != "valueString" || v != "x" {
		t.Fatalf("lookupChild(choice) = %v, %q, %v", v, key, ok)
	}
	if _, key, ok := lookupChild(m, "name"); !ok || key != "name" {
		t.Fatalf("lookupChild(exact) = %q, %v", key, ok)
	}
	if _, _, ok := lookupChild(m, "missing"); ok {
		t.Fatal("lookupChild(missing) should be false")
	}
}

func TestResolveLeafKeyNegative(t *testing.T) {
	m := map[string]any{"valueString": 1, "valueQuantity": 2}
	if got := resolveLeafKey(m, "value"); got != "valueQuantity" && got != "valueString" {
		t.Fatalf("resolveLeafKey = %q", got)
	}
	if got := resolveLeafKey(map[string]any{"a": 1}, "b"); got != "" {
		t.Fatalf("resolveLeafKey(missing) = %q", got)
	}
}

func TestDeletePathAndSetPath(t *testing.T) {
	body := map[string]any{"name": []any{map[string]any{"family": "Smith"}}, "active": true}
	if !deletePath(body, "Patient.name.family") {
		t.Fatal("deletePath should return true when present")
	}
	if !deletePath(body, "Patient.active") {
		t.Fatal("deletePath(active) should return true")
	}
	if deletePath(body, "Patient.missing") {
		t.Fatal("deletePath(missing) should return false")
	}
	// setPath on absent element returns false.
	if setPath(body, "Patient.nope", nil) {
		t.Fatal("setPath(missing) should return false")
	}
	// setPath on a choice key.
	body2 := map[string]any{"deceasedBoolean": false}
	if !setPath(body2, "Patient.deceased", nil) {
		t.Fatal("setPath(choice) should return true")
	}
}

func TestWrongDatatypeValue(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{
		URL: "http://example.org/StructureDefinition/patient", Type: "Patient",
		Elements: []model.ElementDefinition{
			{Path: "Patient", Min: 0, Max: "*"},
			{Path: "Patient.birthDate", Min: 0, Max: "1", Types: []model.ElementType{{Code: "date"}}},
			{Path: "Patient.deceased", Min: 0, Max: "1", Types: []model.ElementType{{Code: "boolean"}}},
			{Path: "Patient.name", Min: 0, Max: "*", Types: []model.ElementType{{Code: "HumanName"}}},
		},
	})

	// Invalid lexical form for a date.
	v := wrongDatatypeValue(coverage.CoverageRequirement{Variant: coverage.CoverageVariantDatatypeInvalidLexical, ProfileURL: "http://example.org/StructureDefinition/patient", ElementPath: "Patient.birthDate"}, reg)
	if v != "not-a-date" {
		t.Fatalf("invalid lexical date = %v", v)
	}
	// Wrong JSON type for a boolean.
	v = wrongDatatypeValue(coverage.CoverageRequirement{Variant: coverage.CoverageVariantDatatypeWrongJSONType, ProfileURL: "http://example.org/StructureDefinition/patient", ElementPath: "Patient.deceased"}, reg)
	if v != "not-a-boolean" {
		t.Fatalf("wrong json boolean = %v", v)
	}
	// Unknown element type with invalid lexical.
	v = wrongDatatypeValue(coverage.CoverageRequirement{Variant: coverage.CoverageVariantDatatypeInvalidLexical, ProfileURL: "http://example.org/StructureDefinition/patient", ElementPath: "Patient.unknown"}, reg)
	if v != "not-a-valid-value" {
		t.Fatalf("unknown invalid = %v", v)
	}
}

func TestSetBogusCode(t *testing.T) {
	// CodeableConcept with coding.
	body := map[string]any{"gender": map[string]any{"coding": []any{map[string]any{"code": "male"}}}}
	if !setBogusCode(body, "Patient.gender") {
		t.Fatal("setBogusCode should return true")
	}
	// Array of codeable concepts.
	body = map[string]any{"gender": []any{map[string]any{"code": "x"}}}
	if !setBogusCode(body, "Patient.gender") {
		t.Fatal("setBogusCode(array) should return true")
	}
	// Scalar.
	body = map[string]any{"gender": "male"}
	if !setBogusCode(body, "Patient.gender") {
		t.Fatal("setBogusCode(scalar) should return true")
	}
	// Absent.
	if setBogusCode(map[string]any{}, "Patient.missing") {
		t.Fatal("setBogusCode(missing) should return false")
	}
}

func TestBogusCodedValue(t *testing.T) {
	m := map[string]any{"coding": []any{map[string]any{"code": "x"}}}
	bogusCodedValue(m)
	if m["coding"].([]any)[0].(map[string]any)["code"] != "not-a-real-code" {
		t.Fatalf("bogusCodedValue(coding) = %v", m)
	}
	m2 := map[string]any{}
	bogusCodedValue(m2)
	if m2["code"] != "not-a-real-code" {
		t.Fatalf("bogusCodedValue(bare) = %v", m2)
	}
}

func TestReferenceMapsAtAndMutations(t *testing.T) {
	// Single reference.
	body := map[string]any{"subject": map[string]any{"reference": "Patient/p-1"}}
	refs := referenceMapsAt(body, "Observation.subject")
	if len(refs) != 1 {
		t.Fatalf("referenceMapsAt = %v", refs)
	}
	if !mutateReferenceType(body, "Observation.subject") {
		t.Fatal("mutateReferenceType should return true")
	}
	if body["subject"].(map[string]any)["reference"] != "Organization/p-1" {
		t.Fatalf("mutated reference = %v", body["subject"])
	}
	// Array of references.
	body = map[string]any{"author": []any{map[string]any{"reference": "Practitioner/p-1"}, map[string]any{"reference": "Practitioner/p-2"}}}
	if !mutateReferenceDangling(body, "Composition.author") {
		t.Fatal("mutateReferenceDangling should return true")
	}
	auth := body["author"].([]any)
	if auth[0].(map[string]any)["reference"] != "Patient/momus-does-not-exist" {
		t.Fatalf("dangling reference = %v", auth[0])
	}
	// Absent -> false.
	if mutateReferenceType(map[string]any{}, "Observation.missing") {
		t.Fatal("mutateReferenceType(missing) should be false")
	}
}

func TestReferenceID(t *testing.T) {
	if got := referenceID("Patient/p-1"); got != "p-1" {
		t.Fatalf("referenceID = %q", got)
	}
	if got := referenceID("no-slash"); got != "momus-wrong" {
		t.Fatalf("referenceID(no-slash) = %q", got)
	}
}

func TestElementDefinitionOfAndTypeOf(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{
		URL: "http://example.org/StructureDefinition/patient", Type: "Patient",
		Elements: []model.ElementDefinition{
			{Path: "Patient", Min: 0, Max: "*"},
			{Path: "Patient.active", Min: 0, Max: "1", Types: []model.ElementType{{Code: "boolean"}}},
		},
	})
	if got := elementTypeOf(reg, "http://example.org/StructureDefinition/patient", "Patient.active"); got != "boolean" {
		t.Fatalf("elementTypeOf = %q", got)
	}
	if got := elementTypeOf(nil, "http://x", "Patient.active"); got != "" {
		t.Fatalf("elementTypeOf(nil reg) = %q", got)
	}
	if got := elementTypeOf(reg, "http://missing", "Patient.active"); got != "" {
		t.Fatalf("elementTypeOf(unknown) = %q", got)
	}
	if got := elementTypeOf(reg, "http://example.org/StructureDefinition/patient", "Patient.nope"); got != "" {
		t.Fatalf("elementTypeOf(unknown element) = %q", got)
	}
}

func TestApplyNegativeMutation(t *testing.T) {
	body := map[string]any{"active": true}
	// Positive variant leaves unchanged.
	if !applyNegativeMutation(body, coverage.CoverageRequirement{Variant: coverage.CoverageVariantValidMin}, nil) {
		t.Fatal("positive variant should require no mutation")
	}
	// Missing required -> delete.
	if !applyNegativeMutation(body, coverage.CoverageRequirement{Variant: coverage.CoverageVariantMissingRequired, ElementPath: "Patient.active"}, nil) {
		t.Fatal("missing-required should delete")
	}
	if _, ok := body["active"]; ok {
		t.Fatal("active should be deleted")
	}
	// Null datatype on an absent element returns false (nothing to null).
	if applyNegativeMutation(body, coverage.CoverageRequirement{Variant: coverage.CoverageVariantDatatypeNull, ElementPath: "Patient.missing"}, nil) {
		t.Fatal("null on missing should be false")
	}
}
