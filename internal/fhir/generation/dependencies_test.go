package generation

import (
	"testing"

	"github.com/jlcoulter/momus/internal/core/coverage"
	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

func TestBuildDependencyPlanOrdersOptionalProfileReferences(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/location",
		Type: "Location",
		Elements: []model.ElementDefinition{
			{Path: "Location", Min: 0, Max: "*"},
			{Path: "Location.name", Min: 1, Max: "1", Types: []model.ElementType{{Code: "string"}}},
		},
	})
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/healthcareservice",
		Type: "HealthcareService",
		Elements: []model.ElementDefinition{
			{Path: "HealthcareService", Min: 0, Max: "*"},
			{Path: "HealthcareService.name", Min: 1, Max: "1", Types: []model.ElementType{{Code: "string"}}},
			// Optional reference that is not a derived coverage obligation.
			{Path: "HealthcareService.coverageArea", Min: 0, Max: "*", Types: []model.ElementType{{Code: "Reference", TargetProfile: []string{"http://example.org/StructureDefinition/location"}}}},
		},
	})

	plan := &coverage.CoveragePlan{Requirements: []coverage.CoverageRequirement{
		{ID: "l1", ResourceType: "Location", ProfileURL: "http://example.org/StructureDefinition/location"},
		{ID: "hs1", ResourceType: "HealthcareService", ProfileURL: "http://example.org/StructureDefinition/healthcareservice"},
	}}

	depPlan, err := buildDependencyPlan(plan, nil, r)
	if err != nil {
		t.Fatalf("buildDependencyPlan returned error: %v", err)
	}

	// HealthcareService must be ordered after Location because its profile
	// references Location, even though the reference obligation was not derived.
	if len(depPlan.Levels) != 2 {
		t.Fatalf("got %d levels, want 2: %+v", len(depPlan.Levels), depPlan.Levels)
	}
	if depPlan.Levels[0][0] != "Location" {
		t.Fatalf("level0 = %v, want Location first", depPlan.Levels[0])
	}
	if depPlan.Levels[1][0] != "HealthcareService" {
		t.Fatalf("level1 = %v, want HealthcareService second", depPlan.Levels[1])
	}
	if len(depPlan.Dependencies["HealthcareService"]) != 1 || depPlan.Dependencies["HealthcareService"][0] != "Location" {
		t.Fatalf("healthcareservice dependencies = %v, want [Location]", depPlan.Dependencies["HealthcareService"])
	}
}
