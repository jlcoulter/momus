package validate

import (
	"context"
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

const patientProfile = "http://example.org/StructureDefinition/patient"

// buildPatientRegistry returns a registry with a Patient profile exercising the
// cardinality, datatype, terminology, fixed/pattern, and slice checks.
func buildPatientRegistry() *registry.Registry {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  patientProfile,
		Type: "Patient",
		Elements: []model.ElementDefinition{
			{Path: "Patient", Min: 0, Max: "*"},
			{Path: "Patient.name", Min: 1, Max: "*", Types: []model.ElementType{{Code: "HumanName"}}},
			{Path: "Patient.birthDate", Min: 0, Max: "1", Types: []model.ElementType{{Code: "date"}}},
			{Path: "Patient.gender", Min: 0, Max: "1", Types: []model.ElementType{{Code: "code"}}, Binding: &model.Binding{Strength: "required", ValueSet: "http://example.org/ValueSet/gender"}},
			{Path: "Patient.status", Min: 0, Max: "1", Types: []model.ElementType{{Code: "code"}}, Fixed: "active"},
		},
	})
	r.AddValueSet(&model.ValueSet{
		URL: "http://example.org/ValueSet/gender",
		ComposeIncludes: []model.ValueSetInclude{{
			System:   "http://hl7.org/fhir/administrative-gender",
			Concepts: []model.ConceptReference{{Code: "male"}, {Code: "female"}},
		}},
	})
	return r
}

func TestValidateCardinalityRequiredMissing(t *testing.T) {
	r := buildPatientRegistry()
	v := New(r)
	res := map[string]any{"resourceType": "Patient"}
	issues, err := v.Validate(context.Background(), patientProfile, res)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(issues) == 0 {
		t.Fatal("expected a cardinality issue for missing Patient.name, got none")
	}
	if issues[0].Kind != "cardinality" || issues[0].Path != "Patient.name" {
		t.Fatalf("issue = %+v, want cardinality at Patient.name", issues[0])
	}
}

func TestValidateCardinalPresent(t *testing.T) {
	r := buildPatientRegistry()
	v := New(r)
	res := map[string]any{
		"name": []any{map[string]any{"family": "Smith"}},
	}
	issues, err := v.Validate(context.Background(), patientProfile, res)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	for _, iss := range issues {
		if iss.Kind == "cardinality" && iss.Path == "Patient.name" {
			t.Fatalf("unexpected cardinality issue: %+v", iss)
		}
	}
}

func TestValidateDatatypeMismatch(t *testing.T) {
	r := buildPatientRegistry()
	v := New(r)
	res := map[string]any{
		"name":      []any{map[string]any{"family": "Smith"}},
		"birthDate": "not-a-date",
	}
	issues, err := v.Validate(context.Background(), patientProfile, res)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	var found bool
	for _, iss := range issues {
		if iss.Kind == "datatype" && iss.Path == "Patient.birthDate" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected datatype issue for Patient.birthDate, got %+v", issues)
	}
}

func TestValidateTerminologyInvalid(t *testing.T) {
	r := buildPatientRegistry()
	v := New(r)
	res := map[string]any{
		"name":   []any{map[string]any{"family": "Smith"}},
		"gender": "male2", // not in the value set
	}
	issues, err := v.Validate(context.Background(), patientProfile, res)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	var found bool
	for _, iss := range issues {
		if iss.Kind == "terminology" && iss.Path == "Patient.gender" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected terminology issue for Patient.gender, got %+v", issues)
	}
}

func TestValidateTerminologyValid(t *testing.T) {
	r := buildPatientRegistry()
	v := New(r)
	res := map[string]any{
		"name":   []any{map[string]any{"family": "Smith"}},
		"gender": "male",
	}
	issues, err := v.Validate(context.Background(), patientProfile, res)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	for _, iss := range issues {
		if iss.Kind == "terminology" {
			t.Fatalf("unexpected terminology issue: %+v", iss)
		}
	}
}

func TestValidateFixedMismatch(t *testing.T) {
	r := buildPatientRegistry()
	v := New(r)
	res := map[string]any{
		"name":   []any{map[string]any{"family": "Smith"}},
		"status": "inactive",
	}
	issues, err := v.Validate(context.Background(), patientProfile, res)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	var found bool
	for _, iss := range issues {
		if iss.Kind == "fixed" && iss.Path == "Patient.status" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected fixed issue for Patient.status, got %+v", issues)
	}
}

func TestValidateFixedMatch(t *testing.T) {
	r := buildPatientRegistry()
	v := New(r)
	res := map[string]any{
		"name":   []any{map[string]any{"family": "Smith"}},
		"status": "active",
	}
	issues, err := v.Validate(context.Background(), patientProfile, res)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	for _, iss := range issues {
		if iss.Kind == "fixed" {
			t.Fatalf("unexpected fixed issue: %+v", iss)
		}
	}
}

func TestValidateMaxCardinalityExceeded(t *testing.T) {
	r := buildPatientRegistry()
	// birthDate is Max "1"; providing two values exceeds the bound.
	v := New(r)
	res := map[string]any{
		"name":      []any{map[string]any{"family": "Smith"}},
		"birthDate": []any{"2024-01-01", "2024-02-02"},
	}
	issues, err := v.Validate(context.Background(), patientProfile, res)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	var found bool
	for _, iss := range issues {
		if iss.Kind == "cardinality" && iss.Path == "Patient.birthDate" && iss.Message != "" && iss.Message != "required element Patient.birthDate is missing" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected max-cardinality issue for Patient.birthDate, got %+v", issues)
	}
}

func TestValidateMaxCardinalityWithinBound(t *testing.T) {
	r := buildPatientRegistry()
	v := New(r)
	res := map[string]any{
		"name":      []any{map[string]any{"family": "Smith"}},
		"birthDate": "2024-01-01",
	}
	issues, err := v.Validate(context.Background(), patientProfile, res)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	for _, iss := range issues {
		if iss.Kind == "cardinality" && iss.Path == "Patient.birthDate" {
			t.Fatalf("unexpected cardinality issue: %+v", iss)
		}
	}
}

func TestValidateMaxCardinalityPerArrayInstance(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/params",
		Type: "Parameters",
		Elements: []model.ElementDefinition{
			{Path: "Parameters", Min: 0, Max: "*"},
			{Path: "Parameters.parameter", Min: 0, Max: "*", Types: []model.ElementType{{Code: "BackboneElement"}}},
			{Path: "Parameters.parameter.name", Min: 1, Max: "1", Types: []model.ElementType{{Code: "code"}}},
			{Path: "Parameters.parameter.value[x]", Min: 0, Max: "1", Types: []model.ElementType{{Code: "string"}}},
		},
	})
	v := New(r)
	// Each parameter entry has exactly one name (max 1 per instance); the three
	// entries collectively have three names but none exceeds the per-instance bound.
	res := map[string]any{"resourceType": "Parameters", "parameter": []any{
		map[string]any{"name": "_outputFormat", "valueString": "ndjson"},
		map[string]any{"name": "_type", "valueString": "Patient"},
		map[string]any{"name": "_typeFilter", "valueString": "x"},
	}}
	issues, err := v.Validate(context.Background(), "http://example.org/StructureDefinition/params", res)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	for _, iss := range issues {
		if iss.Kind == "cardinality" && iss.Path == "Parameters.parameter.name" && iss.Message != "required element Parameters.parameter.name is missing" {
			t.Fatalf("expected no per-instance max-cardinality issue, got %+v", iss)
		}
	}
}

func TestParseMax(t *testing.T) {
	cases := []struct {
		max     string
		bounded bool
		want    int
	}{
		{"1", true, 1},
		{"0", true, 0},
		{"5", true, 5},
		{"*", false, 0},
		{"", false, 0},
		{"abc", false, 0},
		{"-1", false, 0},
	}
	for _, c := range cases {
		got, bounded := parseMax(c.max)
		if bounded != c.bounded || (bounded && got != c.want) {
			t.Errorf("parseMax(%q) = (%d, %v), want (%d, %v)", c.max, got, bounded, c.want, c.bounded)
		}
	}
}
