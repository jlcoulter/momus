package generation

import (
	"reflect"
	"testing"

	"github.com/jlcoulter/momus/internal/test/coverage"
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

func TestStringBoundaryValues(t *testing.T) {
	got := StringBoundaryValues(3, 10)
	want := []string{"aa", "aaa", "aaaa", "aaaaaaaaa", "aaaaaaaaaa", "aaaaaaaaaaa"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBoundaryCapacities(t *testing.T) {
	got := BoundaryCapacities(1, 1)
	want := []int{0, 1, 2}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	// Unbounded (max < 0) drops the upper-edge capacities.
	if got := BoundaryCapacities(0, -1); !reflect.DeepEqual(got, []int{0, 1}) {
		t.Fatalf("got %v, want [0 1]", got)
	}
}

func TestNumericBoundaryValues(t *testing.T) {
	got := NumericBoundaryValues()
	if len(got) != 4 {
		t.Fatalf("got %d values, want 4", len(got))
	}
	if got[0] != 0 {
		t.Fatalf("expected 0 as first boundary value, got %v", got[0])
	}
}
