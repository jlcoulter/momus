package validate

import (
	"context"
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

const invProfile = "http://example.org/StructureDefinition/patient-inv"

func TestValidateInvariantViolated(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  invProfile,
		Type: "Patient",
		Elements: []model.ElementDefinition{
			{Path: "Patient", Min: 0, Max: "*"},
			{Path: "Patient.name", Min: 0, Max: "*", Types: []model.ElementType{{Code: "HumanName"}},
				Constraints: []model.ElementConstraint{{Key: "pt-1", Severity: "error", Expression: "family.exists()", Human: "name must have a family"}}},
		},
	})
	v := New(r)

	// name present but without a family -> family.exists() false -> invariant violation
	res := map[string]any{"resourceType": "Patient", "name": []any{map[string]any{"given": []any{"Alice"}}}}
	issues, err := v.Validate(context.Background(), invProfile, res)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	var found bool
	for _, iss := range issues {
		if iss.Kind == "invariant" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an invariant issue, got %+v", issues)
	}
}

func TestValidateInvariantAbsentElementDoesNotFire(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/ext-sd",
		Type: "Organization",
		Elements: []model.ElementDefinition{
			{Path: "Organization", Min: 0, Max: "*"},
			{Path: "Organization.contact", Min: 0, Max: "*", Types: []model.ElementType{{Code: "BackboneElement"}}},
			{Path: "Organization.contact.extension", Min: 0, Max: "*", Types: []model.ElementType{{Code: "Extension"}},
				Constraints: []model.ElementConstraint{{Key: "ext-1", Severity: "error", Human: "Must have either extensions or value[x]", Expression: "extension.exists() != value.exists()"}}},
		},
	})
	v := New(r)
	// contact present but with no extension field: the ext-1 invariant must not fire
	// on an absent element (it is vacuously satisfied).
	res := map[string]any{"resourceType": "Organization", "contact": []any{map[string]any{"name": "Acme"}}}
	issues, err := v.Validate(context.Background(), "http://example.org/StructureDefinition/ext-sd", res)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	for _, iss := range issues {
		if iss.Kind == "invariant" {
			t.Fatalf("absent element must not fire its invariant: %+v", iss)
		}
	}
}

func TestValidateInvariantSatisfied(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  invProfile,
		Type: "Patient",
		Elements: []model.ElementDefinition{
			{Path: "Patient", Min: 0, Max: "*"},
			{Path: "Patient.name", Min: 0, Max: "*", Types: []model.ElementType{{Code: "HumanName"}},
				Constraints: []model.ElementConstraint{{Key: "pat-1", Severity: "error", Expression: "family.exists()", Human: "name must have a family"}}},
		},
	})
	v := New(r)
	// name present with a family -> family.exists() true -> satisfied
	res := map[string]any{"name": []any{map[string]any{"family": "Smith"}}}
	issues, err := v.Validate(context.Background(), invProfile, res)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	for _, iss := range issues {
		if iss.Kind == "invariant" {
			t.Fatalf("unexpected invariant issue: %+v", iss)
		}
	}
}
