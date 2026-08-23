package generation

import (
	"reflect"
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

func TestIsFunctionName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"where", true},
		{"all", true},
		{"exists", true},
		{"foo123", true},
		{"foo_bar", true},
		{"", false},
		{"foo(", false},
		{"foo.bar", false},
		{"foo-bar", false},
	}
	for _, c := range cases {
		if got := isFunctionName(c.name); got != c.want {
			t.Errorf("isFunctionName(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestSetPathLeafBoolean(t *testing.T) {
	body := map[string]any{}
	setPathLeafBoolean(body, "active", "true")
	if body["active"] != true {
		t.Fatalf("active = %v, want true", body["active"])
	}
	setPathLeafBoolean(body, "flag", "false")
	if body["flag"] != false {
		t.Fatalf("flag = %v, want false", body["flag"])
	}
	setPathLeafBoolean(body, "other", "notabool")
	if body["other"] != "notabool" {
		t.Fatalf("other = %v, want 'notabool'", body["other"])
	}
}

func TestSetReferenceLeaf(t *testing.T) {
	body := map[string]any{}
	setReferenceLeaf(body, "subject", "Patient/p1")
	if body["subject"] == nil {
		t.Fatal("subject not set")
	}
	if m := body["subject"].(map[string]any); m["reference"] != "Patient/p1" {
		t.Fatalf("subject.reference = %v", m["reference"])
	}

	// Existing array member is updated.
	body = map[string]any{"careManager": []any{map[string]any{"reference": "old"}}}
	setReferenceLeaf(body, "careManager", "Practitioner/p2")
	first := body["careManager"].([]any)[0].(map[string]any)
	if first["reference"] != "Practitioner/p2" {
		t.Fatalf("array reference = %v", first["reference"])
	}

	// Non-map array element is replaced.
	body = map[string]any{"careManager": []any{"str"}}
	setReferenceLeaf(body, "careManager", "Practitioner/p3")
	if m := body["careManager"].([]any)[0].(map[string]any); m["reference"] != "Practitioner/p3" {
		t.Fatalf("replaced array reference = %v", m["reference"])
	}

	// Empty array.
	body = map[string]any{"careManager": []any{}}
	setReferenceLeaf(body, "careManager", "Patient/p4")
	if m := body["careManager"].([]any)[0].(map[string]any); m["reference"] != "Patient/p4" {
		t.Fatalf("empty array reference = %v", m["reference"])
	}

	// Existing map is updated.
	body = map[string]any{"subject": map[string]any{"reference": "old"}}
	setReferenceLeaf(body, "subject", "Patient/p5")
	if m := body["subject"].(map[string]any); m["reference"] != "Patient/p5" {
		t.Fatalf("map reference = %v", m["reference"])
	}

	// Non-map scalar is replaced.
	body = map[string]any{"subject": "scalar"}
	setReferenceLeaf(body, "subject", "Patient/p6")
	if m := body["subject"].(map[string]any); m["reference"] != "Patient/p6" {
		t.Fatalf("scalar replacement reference = %v", m["reference"])
	}
}

func TestSetQuantityLeaf(t *testing.T) {
	body := map[string]any{}
	setQuantityLeaf(body, "value", "123.45|http://unitsofmeasure.org|mmol")
	if v, ok := body["value"].(map[string]any); !ok {
		t.Fatalf("value = %v, want a Quantity map", body["value"])
	} else if v["value"] != 123.45 {
		t.Fatalf("quantity value = %v, want 123.45", v["value"])
	}

	// Existing Quantity keeps its value.
	body = map[string]any{"value": map[string]any{"value": 5.0}}
	setQuantityLeaf(body, "value", "999|http://example.org|in")
	if v := body["value"].(map[string]any); v["value"] != 5.0 {
		t.Fatalf("existing quantity value overwritten: %v", v["value"])
	}

	// Non-map field is replaced.
	body = map[string]any{"value": "scalar"}
	setQuantityLeaf(body, "value", "42|http://x|kg")
	if v, ok := body["value"].(map[string]any); !ok || v["value"] != 42.0 {
		t.Fatalf("scalar replacement = %v", body["value"])
	}
}

func TestFirstNumericPart(t *testing.T) {
	cases := []struct {
		in   string
		want any
	}{
		{"123.45|http://x|mmol", 123.45},
		{"42|http://x", 42.0},
		{"7", 7.0},
		{"abc", "abc"},
		{"abc|def", "abc"},
		{"", ""},
	}
	for _, c := range cases {
		if got := firstNumericPart(c.in); got != c.want {
			t.Errorf("firstNumericPart(%q) = %v (%T), want %v (%T)", c.in, got, got, c.want, c.want)
		}
	}
}

func TestApplyCompositeMatch(t *testing.T) {
	reg := buildBuilderRegistry()
	body := map[string]any{}
	// Expression with a code path and a quantity path.
	ok := applyCompositeMatch(body, "code | value", "Observation", "glucose$5.4", reg)
	if !ok {
		t.Fatal("applyCompositeMatch returned false")
	}
	if _, hasCode := body["code"]; !hasCode {
		t.Fatal("code element not set")
	}
	if _, hasValue := body["value"]; !hasValue {
		t.Fatal("value element not set")
	}

	// A malformed value (single part, no '$') must return false.
	if applyCompositeMatch(body, "code | value", "Observation", "glucose", reg) {
		t.Fatal("applyCompositeMatch with single part should return false")
	}
}

func TestApplyCompositeMatchPadsAndBranches(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/patient", Type: "Patient", Elements: []model.ElementDefinition{
		{Path: "Patient", Min: 0, Max: "*"},
		{Path: "Patient.active", Min: 0, Max: "1", Types: []model.ElementType{{Code: "boolean"}}},
	}})
	// More parts than paths -> last path is padded, so all parts land on it.
	body := map[string]any{}
	if !applyCompositeMatch(body, "active", "Patient", "true$extra$more", reg) {
		t.Fatal("applyCompositeMatch(padded) should return true")
	}
	if body["active"] == nil {
		t.Fatal("padded composite did not set the leaf")
	}
}

func TestSetSpecialLeaf(t *testing.T) {
	body := map[string]any{}
	setSpecialLeaf(body, "position.longitude", "-33.8688|151.2093")
	pos := body["position"].(map[string]any)
	if pos["latitude"] != -33.8688 {
		t.Fatalf("latitude = %v, want -33.8688", pos["latitude"])
	}
	if pos["longitude"] != 151.2093 {
		t.Fatalf("longitude = %v, want 151.2093", pos["longitude"])
	}
}

func TestSetDateLeaf(t *testing.T) {
	reg := buildBuilderRegistry()
	body := map[string]any{}
	// birthDate is typed date, so setDateLeaf places the value on the concrete
	// choice member (birthDateDate).
	setDateLeaf(body, "birthDate", "2024-01-01", reg, "Patient")
	if body["birthDateDate"] != "2024-01-01" {
		t.Fatalf("birthDateDate = %v, want 2024-01-01", body["birthDateDate"])
	}

	// An unresolvable path simply sets the leaf directly.
	body = map[string]any{}
	setDateLeaf(body, "unknown.field", "2024-01-01", reg, "Patient")
	if body["unknown"] == nil {
		t.Fatal("unknown field not set")
	}
}

func TestDescendContainer(t *testing.T) {
	// Missing key creates a map.
	body := map[string]any{}
	if got := descendContainer(body, "a"); !reflect.DeepEqual(got, map[string]any{}) {
		t.Fatalf("descend missing = %v", got)
	}
	if _, ok := body["a"].(map[string]any); !ok {
		t.Fatal("descend missing should create a map")
	}
	// Existing map is returned.
	body = map[string]any{"a": map[string]any{"x": 1}}
	if got := descendContainer(body, "a"); got["x"] != 1 {
		t.Fatalf("descend existing map = %v", got)
	}
	// Empty array creates an element.
	body = map[string]any{"a": []any{}}
	if got := descendContainer(body, "a"); !reflect.DeepEqual(got, map[string]any{}) {
		t.Fatalf("descend empty array = %v", got)
	}
	// Non-empty array descends into first element.
	body = map[string]any{"a": []any{map[string]any{"x": 1}}}
	if got := descendContainer(body, "a"); got["x"] != 1 {
		t.Fatalf("descend non-empty array = %v", got)
	}
	// Array with non-map first element is replaced.
	body = map[string]any{"a": []any{"str"}}
	if got := descendContainer(body, "a"); !reflect.DeepEqual(got, map[string]any{}) {
		t.Fatalf("descend replaced array element = %v", got)
	}
}

func TestResolveNestedLeafTypeFailures(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://hl7.org/fhir/StructureDefinition/Identifier", Type: "Identifier", Elements: []model.ElementDefinition{
		{Path: "Identifier", Min: 0, Max: "*"},
		{Path: "Identifier.value", Min: 0, Max: "1", Types: []model.ElementType{{Code: "string"}}},
	}})
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/patient", Type: "Patient", Elements: []model.ElementDefinition{
		{Path: "Patient", Min: 0, Max: "*"},
		{Path: "Patient.identifier", Min: 0, Max: "*", Types: []model.ElementType{{Code: "Identifier"}}},
	}})
	resolved, err := reg.ResolveProfile("http://example.org/StructureDefinition/patient")
	if err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}

	// Single-segment path -> not found.
	if _, _, found := resolveNestedLeafType(resolved, "Patient", "id", reg); found {
		t.Fatal("single-segment path should not resolve")
	}
	// Missing intermediate key.
	if _, _, found := resolveNestedLeafType(resolved, "Patient", "identifier.nope", reg); found {
		t.Fatal("missing key should not resolve")
	}
	// Unknown top-level container.
	reg2 := registry.New()
	reg2.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/patient", Type: "Patient", Elements: []model.ElementDefinition{
		{Path: "Patient", Min: 0, Max: "*"},
		{Path: "Patient.id", Min: 0, Max: "1", Types: []model.ElementType{{Code: "id"}}},
	}})
	resolved2, _ := reg2.ResolveProfile("http://example.org/StructureDefinition/patient")
	if _, _, found := resolveNestedLeafType(resolved2, "Patient", "id.value", reg2); found {
		t.Fatal("non-complex container should not resolve nested type")
	}
}
