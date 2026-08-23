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

func TestSlicePresent(t *testing.T) {
	slice := &model.SliceNode{
		Name: "min",
		Children: map[string]*model.ElementNode{
			"code":     {Definition: &model.ElementDefinition{Path: "Observation.component.code", Min: 1}},
			"optional": {Definition: &model.ElementDefinition{Path: "Observation.component.value", Min: 0}},
		},
	}
	// A member carrying the required child counts as present.
	values := []any{map[string]any{"code": map[string]any{}}}
	if !slicePresent(values, slice) {
		t.Fatal("slicePresent with required child = false, want true")
	}
	// A member missing the required child is not present.
	values = []any{map[string]any{"value": float64(1)}}
	if slicePresent(values, slice) {
		t.Fatal("slicePresent without required child = true, want false")
	}
	// A non-map member is skipped.
	values = []any{"not-a-map"}
	if slicePresent(values, slice) {
		t.Fatal("slicePresent with non-map member = true, want false")
	}
	// A slice with no required children: any member counts.
	empty := &model.SliceNode{Name: "min", Children: map[string]*model.ElementNode{}}
	if !slicePresent([]any{map[string]any{}}, empty) {
		t.Fatal("slicePresent with no required children = false, want true")
	}
}

func TestRequiredChildNames(t *testing.T) {
	slice := &model.SliceNode{
		Children: map[string]*model.ElementNode{
			"req":   {Definition: &model.ElementDefinition{Path: "a.req", Min: 1}},
			"opt":   {Definition: &model.ElementDefinition{Path: "a.opt", Min: 0}},
			"nulld": nil,
		},
	}
	names := requiredChildNames(slice)
	if len(names) != 1 || names[0] != "req" {
		t.Fatalf("requiredChildNames = %v, want [req]", names)
	}
}

func TestIsPresent(t *testing.T) {
	cases := []struct {
		name string
		val  any
		want bool
	}{
		{"nil", nil, false},
		{"empty array", []any{}, false},
		{"non-empty array", []any{float64(1)}, true},
		{"string", "x", true},
		{"map", map[string]any{}, true},
		{"zero", float64(0), true},
		{"false", false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isPresent(c.val); got != c.want {
				t.Fatalf("isPresent(%v) = %v, want %v", c.val, got, c.want)
			}
		})
	}
}
