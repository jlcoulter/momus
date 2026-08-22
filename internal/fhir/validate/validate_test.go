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
