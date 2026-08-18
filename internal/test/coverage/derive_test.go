package coverage

import (
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

func TestDeriveMVPPlanPatientNameOneToMany(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/patient-profile",
		Type: "Patient",
		Elements: []model.ElementDefinition{
			{Path: "Patient", Min: 0, Max: "*"},
			{Path: "Patient.name", Min: 1, Max: "*"},
		},
	})
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/observation-profile",
		Type: "Observation",
		Elements: []model.ElementDefinition{
			{Path: "Observation", Min: 0, Max: "*"},
			{Path: "Observation.status", Min: 1, Max: "1"},
		},
	})

	plan, err := DeriveMVPPlan(r)
	if err != nil {
		t.Fatalf("DeriveMVPPlan returned error: %v", err)
	}
	if len(plan.Requirements) != 5 {
		t.Fatalf("got %d requirements, want 5", len(plan.Requirements))
	}

	if !hasVariant(plan, CoverageVariantValidMin) {
		t.Fatal("expected valid-min requirement")
	}
	if !hasVariant(plan, CoverageVariantMissingRequired) {
		t.Fatal("expected missing-required requirement")
	}
	if !hasVariant(plan, CoverageVariantMultipleValues) {
		t.Fatal("expected multiple-values requirement")
	}
}

func TestDeriveMVPPlanPatientNameOptionalSingle(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/patient-profile",
		Type: "Patient",
		Elements: []model.ElementDefinition{
			{Path: "Patient", Min: 0, Max: "*"},
			{Path: "Patient.name", Min: 0, Max: "1"},
		},
	})

	plan, err := DeriveMVPPlan(r)
	if err != nil {
		t.Fatalf("DeriveMVPPlan returned error: %v", err)
	}
	if len(plan.Requirements) != 1 {
		t.Fatalf("got %d requirements, want 1", len(plan.Requirements))
	}
	if !hasVariant(plan, CoverageVariantValidMin) {
		t.Fatal("expected valid-min requirement")
	}
	if hasVariant(plan, CoverageVariantMissingRequired) {
		t.Fatal("did not expect missing-required requirement")
	}
	if hasVariant(plan, CoverageVariantMultipleValues) {
		t.Fatal("did not expect multiple-values requirement")
	}
}

func TestDeriveMVPPlanDerivesWithoutPatientProfiles(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/observation-profile",
		Type: "Observation",
		Elements: []model.ElementDefinition{
			{Path: "Observation", Min: 0, Max: "*"},
			{Path: "Observation.status", Min: 1, Max: "1"},
		},
	})

	plan, err := DeriveMVPPlan(r)
	if err != nil {
		t.Fatalf("DeriveMVPPlan returned error: %v", err)
	}
	if len(plan.Requirements) != 2 {
		t.Fatalf("got %d requirements, want 2", len(plan.Requirements))
	}
}

func TestDeriveMVPPlanFailsWithoutStructureDefinitions(t *testing.T) {
	r := registry.New()
	if _, err := DeriveMVPPlan(r); err == nil {
		t.Fatal("expected error when no structure definitions exist")
	}
}

func hasVariant(plan *CoveragePlan, variant CoverageVariant) bool {
	for _, req := range plan.Requirements {
		if req.Variant == variant {
			return true
		}
	}
	return false
}
