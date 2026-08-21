package coverage

import (
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

// uniqueRequirementCount returns the number of distinct requirement IDs.
func uniqueRequirementCount(plan *CoveragePlan) int {
	seen := make(map[string]struct{}, len(plan.Requirements))
	for _, req := range plan.Requirements {
		seen[req.ID] = struct{}{}
	}
	return len(seen)
}

func TestDerivePlanNeverProducesDuplicateIDs(t *testing.T) {
	r := registry.New()
	// A realistic profile: sliced repeatable element with required slices, a
	// choice element with multiple types, required simple fields, and a
	// reference with a target profile.
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/observation",
		Type: "Observation",
		Elements: []model.ElementDefinition{
			{Path: "Observation", Min: 0, Max: "*"},
			{Path: "Observation.status", Min: 1, Max: "1", Types: []model.ElementType{{Code: "code"}}},
			// choice element: multiple types share the canonical path
			{Path: "Observation.value", Min: 1, Max: "1", Types: []model.ElementType{{Code: "string"}, {Code: "integer"}, {Code: "dateTime"}}},
			// sliced element: base plus two required slices on the same path
			{Path: "Observation.component", Min: 1, Max: "*"},
			{Path: "Observation.component", Min: 1, Max: "1", SliceName: "min", Types: []model.ElementType{{Code: "CodeableConcept"}}},
			{Path: "Observation.component", Min: 1, Max: "*", SliceName: "max", Types: []model.ElementType{{Code: "Quantity"}}},
			{Path: "Observation.subject", Min: 1, Max: "1", Types: []model.ElementType{{Code: "Reference", TargetProfile: []string{"http://hl7.org/fhir/StructureDefinition/Patient"}}}},
		},
	})
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/patient",
		Type: "Patient",
		Elements: []model.ElementDefinition{
			{Path: "Patient", Min: 0, Max: "*"},
			{Path: "Patient.name", Min: 1, Max: "*", Types: []model.ElementType{{Code: "HumanName"}}},
			{Path: "Patient.birthDate", Min: 1, Max: "1", Types: []model.ElementType{{Code: "date"}}},
		},
	})

	plan, err := DerivePlan(r, DeriveOptions{})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}
	if got, unique := len(plan.Requirements), uniqueRequirementCount(plan); got != unique {
		t.Fatalf("plan has %d requirements but only %d unique IDs (%d duplicates); duplicates must never be emitted", got, unique, got-unique)
	}
}
