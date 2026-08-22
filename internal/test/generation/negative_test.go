package generation

import (
	"testing"

	"github.com/jlcoulter/momus/internal/core/coverage"
	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

func TestDeletePathNested(t *testing.T) {
	body := map[string]any{
		"name": []any{map[string]any{"family": "Momus", "given": []any{"Test"}}},
	}
	deletePath(body, "Patient.name.family")
	name := body["name"].([]any)[0].(map[string]any)
	if _, ok := name["family"]; ok {
		t.Fatal("expected nested family field to be deleted")
	}
	if name["given"] == nil {
		t.Fatal("expected sibling given field to remain")
	}
}

func TestSetPathTopLevel(t *testing.T) {
	body := map[string]any{"status": "final"}
	setPath(body, "Observation.status", nil)
	if body["status"] != nil {
		t.Fatalf("got %v, want nil", body["status"])
	}
}

func TestWrongDatatypeValueByVariant(t *testing.T) {
	req := coverage.CoverageRequirement{
		Variant:     coverage.CoverageVariantDatatypeInvalidLexical,
		ElementPath: "Observation.birthDate",
	}
	// No registry provided: element type is unknown, so the generic invalid
	// lexical value is used.
	if got := wrongDatatypeValue(req, nil); got != "not-a-valid-value" {
		t.Fatalf("expected generic invalid lexical for unknown type, got %v", got)
	}

	req.Variant = coverage.CoverageVariantDatatypeWrongJSONType
	// Unknown element type defaults to a boolean, which is not a date.
	if got := wrongDatatypeValue(req, nil); got != true {
		t.Fatalf("got %v, want true (wrong JSON type fallback)", got)
	}
}

func TestApplyNegativeMutationMissingRequired(t *testing.T) {
	body := map[string]any{"status": "final", "value": 3.0}
	applyNegativeMutation(body, coverage.CoverageRequirement{
		Variant:     coverage.CoverageVariantMissingRequired,
		ElementPath: "Observation.status",
	}, nil)
	if _, ok := body["status"]; ok {
		t.Fatal("expected status to be deleted for missing-required")
	}
	if body["value"] == nil {
		t.Fatal("expected unrelated field to remain")
	}
}

func TestApplyNegativeMutationNull(t *testing.T) {
	body := map[string]any{"value": "x"}
	applyNegativeMutation(body, coverage.CoverageRequirement{
		Variant:     coverage.CoverageVariantDatatypeNull,
		ElementPath: "Observation.value",
	}, nil)
	if body["value"] != nil {
		t.Fatalf("got %v, want nil", body["value"])
	}
}

func TestApplyNegativeMutationReferenceDangling(t *testing.T) {
	body := map[string]any{"subject": map[string]any{"reference": "Patient/momus-setup-patient", "type": "Patient"}}
	applyNegativeMutation(body, coverage.CoverageRequirement{
		Variant:     coverage.CoverageVariantReferenceDangling,
		ElementPath: "Observation.subject",
	}, nil)
	subject := body["subject"].(map[string]any)
	if subject["reference"] != "Patient/momus-does-not-exist" {
		t.Fatalf("got reference %v, want dangling reference", subject["reference"])
	}
}

func TestApplyNegativeMutationTerminologyInvalidCodeableConcept(t *testing.T) {
	body := map[string]any{
		"code": map[string]any{
			"coding": []any{map[string]any{"system": "http://example.org", "code": "final"}},
		},
	}
	applyNegativeMutation(body, coverage.CoverageRequirement{
		Variant:     coverage.CoverageVariantTerminologyInvalid,
		ElementPath: "Observation.code",
	}, nil)
	code := body["code"].(map[string]any)["coding"].([]any)[0].(map[string]any)
	if code["code"] != "not-a-real-code" {
		t.Fatalf("got code %v, want bogus code", code["code"])
	}
}

// TestSetPathChoiceElementResolvesConcreteKey verifies that mutating a choice
// [x] element targets the concrete suffixed key present in the payload (e.g.
// valueString for ElementPath Observation.value) instead of writing an illegal
// bare "value" property that a conformant server would reject or ignore.
func TestSetPathChoiceElementResolvesConcreteKey(t *testing.T) {
	body := map[string]any{"valueString": "abc", "status": "final"}
	setPath(body, "Observation.value", nil)
	if body["valueString"] != nil {
		t.Fatalf("valueString = %v, want nil", body["valueString"])
	}
	if _, ok := body["value"]; ok {
		t.Fatal("must not create an illegal bare choice key 'value'")
	}
	if body["status"] != "final" {
		t.Fatal("unrelated field must remain")
	}
}

// TestDeletePathChoiceElementRemovesConcreteKey verifies that deleting a choice
// element removes the concrete suffixed key, not a bare choice key.
func TestDeletePathChoiceElementRemovesConcreteKey(t *testing.T) {
	body := map[string]any{
		"valueQuantity": map[string]any{"value": 5, "unit": "mg"},
	}
	deletePath(body, "Observation.value")
	if _, ok := body["valueQuantity"]; ok {
		t.Fatal("expected valueQuantity to be deleted")
	}
	if _, ok := body["value"]; ok {
		t.Fatal("must not leave a bare 'value' key")
	}
}

// TestSetBogusCodeChoiceElement verifies that bogus-code mutation of a choice
// element resolves the concrete suffixed key (e.g. valueCodeableConcept) and
// mutates its code in place.
func TestSetBogusCodeChoiceElement(t *testing.T) {
	body := map[string]any{
		"valueCodeableConcept": map[string]any{
			"coding": []any{map[string]any{"system": "http://example.org", "code": "final"}},
		},
	}
	setBogusCode(body, "Observation.value")
	if _, ok := body["value"]; ok {
		t.Fatal("must not create a bare 'value' key")
	}
	v := body["valueCodeableConcept"].(map[string]any)
	code := v["coding"].([]any)[0].(map[string]any)
	if code["code"] != "not-a-real-code" {
		t.Fatalf("got code %v, want bogus code", code["code"])
	}
}

// TestReferenceMapsAtChoiceElement verifies reference mutations resolve a choice
// element's concrete suffixed key.
func TestReferenceMapsAtChoiceElement(t *testing.T) {
	body := map[string]any{
		"subjectReference": map[string]any{"reference": "Patient/momus-setup-patient"},
	}
	refs := referenceMapsAt(body, "Observation.subject")
	if len(refs) != 1 {
		t.Fatalf("got %d reference maps, want 1", len(refs))
	}
	if refs[0]["reference"] != "Patient/momus-setup-patient" {
		t.Fatalf("reference = %v", refs[0]["reference"])
	}
}

// TestNegativeMutationSkippedWhenElementAbsent verifies that a negative
// requirement whose target element is not present in the synthesized payload is
// skipped (no reject test is emitted), because the mutation could not construct
// a concrete violation and a conformant server would accept the payload.
func TestNegativeMutationSkippedWhenElementAbsent(t *testing.T) {
	// No registry: the body cannot be populated, so Observation.value is absent
	// and the negative datatype case must be skipped entirely.
	plan, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{
		Requirements: []coverage.CoverageRequirement{
			{ID: "d-invalid", ProfileURL: "http://example.org/StructureDefinition/observation", ResourceType: "Observation", ElementPath: "Observation.value", Variant: coverage.CoverageVariantDatatypeInvalidLexical},
		},
	}, BuildOptions{BaseURL: "http://localhost:8080/fhir"})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}
	if got := RequirementCount(plan); got != 0 {
		t.Fatalf("got %d generated cases, want 0 (negative case skipped when element absent)", got)
	}
	expressions := map[string]bool{}
	collectAssertExpressions(plan.Root, expressions)
	if expressions["status in [400,412,422]"] {
		t.Fatal("must not emit a reject assertion when no violation could be constructed")
	}

	// With a registry declaring a required value element, the case is generated.
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/observation",
		Type: "Observation",
		Elements: []model.ElementDefinition{
			{Path: "Observation", Min: 0, Max: "*"},
			{Path: "Observation.value", Min: 1, Max: "1", Types: []model.ElementType{{Code: "string"}}},
		},
	})
	plan2, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{
		Requirements: []coverage.CoverageRequirement{
			{ID: "d-invalid", ProfileURL: "http://example.org/StructureDefinition/observation", ResourceType: "Observation", ElementPath: "Observation.value", Variant: coverage.CoverageVariantDatatypeInvalidLexical},
		},
	}, BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: reg})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan (with registry) returned error: %v", err)
	}
	if got := RequirementCount(plan2); got != 1 {
		t.Fatalf("got %d generated cases, want 1 (negative case generated when element present)", got)
	}
	expressions2 := map[string]bool{}
	collectAssertExpressions(plan2.Root, expressions2)
	if !expressions2["status in [400,412,422]"] {
		t.Fatal("expected a negative (reject) assertion when the element is present")
	}
}
