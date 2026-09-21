package generation

import (
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

func TestNormalizeGeneratedResource(t *testing.T) {
	reg := registry.New()
	reg.AddCodeSystem(&model.CodeSystem{
		URL:      "http://cs",
		Concepts: []model.CodeSystemConcept{{Code: "C", Display: "Canonical"}},
	})

	// nil body is a no-op.
	NormalizeGeneratedResource(nil, "Patient", "p1", reg)

	body := map[string]any{
		"resourceType": "Patient",
		"id":           "p1",
		// A self-reference should be dropped.
		"managingOrganization": map[string]any{"reference": "Patient/p1"},
		// A coding with an echoing display gets the canonical display.
		"gender": map[string]any{"coding": []any{map[string]any{"system": "http://cs", "code": "C", "display": "C"}}},
		// An empty extension should be dropped entirely.
		"extension": []any{map[string]any{"url": "http://ext/empty"}},
		// A fixed-coding marker is stripped.
		"telecom": []any{map[string]any{"system": "phone", "value": "x", fixedCodingKey: true}},
	}
	NormalizeGeneratedResource(body, "Patient", "p1", reg)

	if _, ok := body["managingOrganization"]; ok {
		t.Fatal("self-reference should have been stripped")
	}
	gender := body["gender"].(map[string]any)
	if gender["coding"].([]any)[0].(map[string]any)["display"] != "Canonical" {
		t.Fatalf("coding display not normalised: %v", gender)
	}
	if _, ok := body["extension"]; ok {
		t.Fatal("empty extension should have been removed")
	}
	telecom := body["telecom"].([]any)[0].(map[string]any)
	if _, ok := telecom[fixedCodingKey]; ok {
		t.Fatal("fixed coding marker should have been stripped")
	}
}

func TestSetPathLeafRepeatable(t *testing.T) {
	body := map[string]any{}

	// Repeatable -> array wrapper.
	setPathLeafRepeatable(body, "name.given", "momus-search", true)
	given := body["name"].(map[string]any)["given"]
	if arr, ok := given.([]any); !ok || len(arr) != 1 || arr[0] != "momus-search" {
		t.Fatalf("repeatable leaf = %#v, want [momus-search]", given)
	}

	// Non-repeatable -> scalar via setPathLeaf.
	body = map[string]any{}
	setPathLeafRepeatable(body, "active", "true", false)
	if body["active"] != "true" {
		t.Fatalf("non-repeatable leaf = %#v, want true", body["active"])
	}
}

func TestClearSiblingChoiceMembers(t *testing.T) {
	body := map[string]any{
		"occurredDateTime": "2020-01-01T00:00:00Z",
		"occurredPeriod":   map[string]any{"start": "x"},
		"status":           "final",
	}
	// Clear siblings sharing the base "occurred", keep the written member and
	// unrelated keys.
	clearSiblingChoiceMembers(body, []string{"occurredDateTime"}, "occurred", "occurredDateTime")
	if _, ok := body["occurredPeriod"]; ok {
		t.Fatal("sibling choice member should have been removed")
	}
	if _, ok := body["occurredDateTime"]; !ok {
		t.Fatal("written member should be retained")
	}
	if _, ok := body["status"]; !ok {
		t.Fatal("unrelated key should be retained")
	}
}

func TestClearSiblingChoiceMembersNested(t *testing.T) {
	body := map[string]any{
		"value": map[string]any{
			"valueString":  "a",
			"valueInteger": 1,
		},
	}
	clearSiblingChoiceMembers(body, []string{"value", "valueString"}, "value", "valueString")
	inner := body["value"].(map[string]any)
	if _, ok := inner["valueInteger"]; ok {
		t.Fatal("nested sibling choice member should have been removed")
	}
}

func TestSetContactPointValue(t *testing.T) {
	// Absent field -> creates a contact point with system phone.
	body := map[string]any{}
	setContactPointValue(body, "telecom", "555")
	telecom := body["telecom"].([]any)[0].(map[string]any)
	if telecom["system"] != "phone" || telecom["value"] != "555" {
		t.Fatalf("new contact point = %v", telecom)
	}

	// Existing contact point without system -> adds phone.
	body = map[string]any{"telecom": []any{map[string]any{"value": "old"}}}
	setContactPointValue(body, "telecom", "new")
	tel := body["telecom"].([]any)[0].(map[string]any)
	if tel["value"] != "new" || tel["system"] != "phone" {
		t.Fatalf("existing contact point = %v", tel)
	}

	// Existing contact point with system -> keeps it.
	body = map[string]any{"telecom": []any{map[string]any{"system": "email", "value": "a@b"}}}
	setContactPointValue(body, "telecom", "c@d")
	tel = body["telecom"].([]any)[0].(map[string]any)
	if tel["system"] != "email" || tel["value"] != "c@d" {
		t.Fatalf("system-preserving contact point = %v", tel)
	}

	// Single map (non-array) field.
	body = map[string]any{"telecom": map[string]any{"value": "x"}}
	setContactPointValue(body, "telecom", "y")
	if body["telecom"].(map[string]any)["system"] != "phone" {
		t.Fatalf("map contact point = %v", body["telecom"])
	}
}

func TestParsePeriodInstant(t *testing.T) {
	if got, err := parsePeriodInstant("2024-01-15T10:30:00Z"); err != nil || got.Year() != 2024 {
		t.Fatalf("parsePeriodInstant(RFC3339) = %v, %v", got, err)
	}
	if got, err := parsePeriodInstant("2024-01-15"); err != nil || got.Year() != 2024 || got.Month() != 1 {
		t.Fatalf("parsePeriodInstant(date) = %v, %v", got, err)
	}
	if _, err := parsePeriodInstant("not-a-date"); err == nil {
		t.Fatal("expected error for unparseable value")
	}
}

func TestNormalizeInstantValue(t *testing.T) {
	if got := normalizeInstantValue(""); got != "" {
		t.Fatalf("normalizeInstantValue(empty) = %q", got)
	}
	if got := normalizeInstantValue("2024-01-15"); got != "2024-01-15T00:00:00Z" {
		t.Fatalf("normalizeInstantValue(bare date) = %q", got)
	}
	if got := normalizeInstantValue("2024-01-15T10:30:00Z"); got != "2024-01-15T10:30:00Z" {
		t.Fatalf("normalizeInstantValue(qualified) = %q", got)
	}
	if got := normalizeInstantValue("2024-01-15 10:30:00"); got != "2024-01-15 10:30:00" {
		t.Fatalf("normalizeInstantValue(space) = %q", got)
	}
}
