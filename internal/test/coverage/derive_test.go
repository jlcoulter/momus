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
	if len(plan.Requirements) != 0 {
		t.Fatalf("got %d requirements, want 0", len(plan.Requirements))
	}
	if plan.Summary.PrunedByReason[PruneReasonOptionalFiltered] == 0 {
		t.Fatal("expected optional-filtered prune reason")
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

func TestDerivePlanIncludeOptional(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/patient-profile",
		Type: "Patient",
		Elements: []model.ElementDefinition{
			{Path: "Patient", Min: 0, Max: "*"},
			{Path: "Patient.name", Min: 0, Max: "1"},
		},
	})

	plan, err := DerivePlan(r, DeriveOptions{IncludeOptional: true})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}
	if len(plan.Requirements) != 1 {
		t.Fatalf("got %d requirements, want 1", len(plan.Requirements))
	}
	if !hasVariant(plan, CoverageVariantValidMin) {
		t.Fatal("expected valid-min requirement")
	}
}

func TestDerivePlanScopeAndPruningOptions(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/patient-profile",
		Type: "Patient",
		Elements: []model.ElementDefinition{
			{Path: "Patient", Min: 0, Max: "*"},
			{Path: "Patient.identifier", Min: 0, Max: "*", MustSupport: true},
			{Path: "Patient.meta", Min: 0, Max: "1", MustSupport: true},
			{Path: "Patient.name", Min: 1, Max: "*"},
		},
	})
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/observation-profile",
		Type: "Observation",
		Elements: []model.ElementDefinition{
			{Path: "Observation", Min: 0, Max: "*"},
			{Path: "Observation.status", Min: 1, Max: "1", MustSupport: true},
		},
	})

	plan, err := DerivePlan(r, DeriveOptions{
		IncludeResourceTypes: []string{"Patient"},
		MustSupportOnly:      true,
		ExcludePathPrefixes:  []string{"Patient.meta"},
	})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}

	if len(plan.Requirements) != 2 {
		t.Fatalf("got %d requirements, want 2", len(plan.Requirements))
	}
	if plan.Summary.ByResourceType["Patient"] != 2 {
		t.Fatalf("got patient summary count %d, want 2", plan.Summary.ByResourceType["Patient"])
	}
	if plan.Summary.PrunedByReason[PruneReasonResourceFiltered] == 0 {
		t.Fatal("expected resource-filtered prune reason")
	}
	if plan.Summary.PrunedByReason[PruneReasonExcludedPathPrefix] == 0 {
		t.Fatal("expected excluded-path-prefix prune reason")
	}
}

func TestDerivePlanIncludesDependencyTargetsFromElementMetadata(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/observation-profile",
		Type: "Observation",
		Elements: []model.ElementDefinition{
			{Path: "Observation", Min: 0, Max: "*"},
			{
				Path: "Observation.subject",
				Min:  1,
				Max:  "1",
				Types: []model.ElementType{
					{Code: "Reference", TargetProfile: []string{"http://hl7.org/fhir/StructureDefinition/Patient|4.0.1"}},
				},
			},
		},
	})

	plan, err := DerivePlan(r, DeriveOptions{IncludeOptional: true})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}

	var found bool
	for _, req := range plan.Requirements {
		if req.ElementPath != "Observation.subject" {
			continue
		}
		found = true
		if len(req.DependencyTargets) != 1 || req.DependencyTargets[0] != "Patient" {
			t.Fatalf("unexpected dependency targets for %s: %+v", req.ID, req.DependencyTargets)
		}
	}
	if !found {
		t.Fatal("expected requirement for Observation.subject")
	}
}

func TestDerivePlanPrunesOptionalReferenceDependencies(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/location-profile",
		Type: "Location",
		Elements: []model.ElementDefinition{
			{Path: "Location", Min: 0, Max: "*"},
			{Path: "Location.name", Min: 1, Max: "1", Types: []model.ElementType{{Code: "string"}}},
			{Path: "Location.managingOrganization", Min: 0, Max: "1", Types: []model.ElementType{{Code: "Reference", TargetProfile: []string{"http://hl7.org/fhir/StructureDefinition/Organization|4.0.1"}}}},
		},
	})
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:      "http://hl7.org/fhir/StructureDefinition/Organization",
		Type:     "Organization",
		Elements: []model.ElementDefinition{{Path: "Organization", Min: 0, Max: "*"}},
	})

	plan, err := DerivePlan(r, DeriveOptions{IncludeOptional: false})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}

	var found bool
	for _, req := range plan.Requirements {
		if req.ResourceType != "Location" || req.ElementPath != "Location.name" || req.Variant != CoverageVariantValidMin {
			continue
		}
		found = true
		if len(req.DependencyTargets) != 0 {
			t.Fatalf("unexpected dependency targets for %s: %+v", req.ID, req.DependencyTargets)
		}
	}
	if !found {
		t.Fatal("expected Location.name requirement")
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
