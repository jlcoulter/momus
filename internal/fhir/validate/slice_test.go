package validate

import (
	"context"
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

const obsProfile = "http://example.org/StructureDefinition/observation"

// buildObsRegistry returns a registry whose Observation profile has a sliced
// "component" element with a required slice "component:min".
func buildObsRegistry() *registry.Registry {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  obsProfile,
		Type: "Observation",
		Elements: []model.ElementDefinition{
			{Path: "Observation", Min: 0, Max: "*"},
			{Path: "Observation.status", Min: 1, Max: "1", Types: []model.ElementType{{Code: "code"}}},
			{Path: "Observation.component", Min: 0, Max: "*", Types: []model.ElementType{{Code: "BackboneElement"}}, ID: "Observation.component"},
			{Path: "Observation.component", Min: 1, Max: "*", Types: []model.ElementType{{Code: "BackboneElement"}}, ID: "Observation.component:min", SliceName: "min"},
			{Path: "Observation.component.code", Min: 1, Max: "1", Types: []model.ElementType{{Code: "CodeableConcept"}}},
		},
	})
	return r
}

func TestValidateSliceRequiredMissing(t *testing.T) {
	r := buildObsRegistry()
	v := New(r)
	res := map[string]any{
		"status": "final",
		// No component:min slice present.
	}
	issues, err := v.Validate(context.Background(), obsProfile, res)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	var found bool
	for _, iss := range issues {
		if iss.Kind == "slice" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a slice issue for missing Observation.component:min, got %+v", issues)
	}
}

func TestValidateSlicePresent(t *testing.T) {
	r := buildObsRegistry()
	v := New(r)
	res := map[string]any{
		"status": "final",
		"component": []any{
			map[string]any{"code": map[string]any{"coding": []any{}}},
		},
	}
	issues, err := v.Validate(context.Background(), obsProfile, res)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	for _, iss := range issues {
		if iss.Kind == "slice" {
			t.Fatalf("unexpected slice issue: %+v", iss)
		}
	}
}
