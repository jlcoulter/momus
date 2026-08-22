package golden

import (
	"context"
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
	"github.com/jlcoulter/momus/internal/fhir/validate"
)

func TestValidateSamples(t *testing.T) {
	const profileURL = "http://example.org/StructureDefinition/patient"
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  profileURL,
		Type: "Patient",
		Elements: []model.ElementDefinition{
			{Path: "Patient", Min: 0, Max: "*"},
			{Path: "Patient.name", Min: 1, Max: "*", Types: []model.ElementType{{Code: "HumanName"}}},
		},
	})
	v := validate.New(r)

	// Conformant sample: has the required name.
	if err := validateSamples(context.Background(), v, &Fixture{Resources: []SampleResource{
		{ProfileURL: profileURL, Resource: map[string]any{"resourceType": "Patient", "id": "p1", "name": []any{map[string]any{"family": "Doe"}}}},
	}}); err != nil {
		t.Fatalf("conformant sample rejected: %v", err)
	}

	// Non-conformant sample: missing required name.
	if err := validateSamples(context.Background(), v, &Fixture{Resources: []SampleResource{
		{ProfileURL: profileURL, Resource: map[string]any{"resourceType": "Patient", "id": "p1"}},
	}}); err == nil {
		t.Fatal("expected non-conformant sample to fail validation")
	}

	// Missing profile URL.
	if err := validateSamples(context.Background(), v, &Fixture{Resources: []SampleResource{
		{Resource: map[string]any{"resourceType": "Patient"}},
	}}); err == nil {
		t.Fatal("expected sample without profileUrl to fail")
	}
}
