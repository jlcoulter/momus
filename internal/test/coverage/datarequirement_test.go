package coverage

import (
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
)

func TestPlanToDataRequirementsGroupsByResourceTypeAndProfile(t *testing.T) {
	plan := &CoveragePlan{
		Requirements: []CoverageRequirement{
			{ID: "c1", ResourceType: "Observation", ProfileURL: "http://example.org/p/obs"},
			{ID: "c2", ResourceType: "Observation", ProfileURL: "http://example.org/p/obs"},
			{ID: "c3", ResourceType: "Patient", ProfileURL: "http://example.org/p/pat"},
		},
	}

	reqs, err := PlanToDataRequirements(plan)
	if err != nil {
		t.Fatalf("PlanToDataRequirements returned error: %v", err)
	}
	if len(reqs) != 2 {
		t.Fatalf("got %d requirements, want 2", len(reqs))
	}

	// Sorted by resource type then profile: Observation before Patient.
	if reqs[0].Resource.Type != "Observation" || reqs[1].Resource.Type != "Patient" {
		t.Fatalf("unexpected ordering: %+v", reqs)
	}
	if reqs[0].Resource.Profile[0] != "http://example.org/p/obs" {
		t.Fatalf("unexpected profile: %v", reqs[0].Resource.Profile)
	}
}

func TestPlanToDataRequirementsDerivesRelationships(t *testing.T) {
	plan := &CoveragePlan{
		Requirements: []CoverageRequirement{
			{
				ID:                "r1",
				ResourceType:      "Observation",
				ProfileURL:        "http://example.org/p/obs",
				Domain:            CoverageDomainReference,
				ElementPath:       "Observation.subject",
				DependencyTargets: []string{"Patient"},
			},
			{
				ID:           "r2",
				ResourceType: "Observation",
				ProfileURL:   "http://example.org/p/obs",
				Domain:       CoverageDomainCardinality,
				ElementPath:  "Observation.status",
			},
		},
	}

	reqs, err := PlanToDataRequirements(plan)
	if err != nil {
		t.Fatalf("PlanToDataRequirements returned error: %v", err)
	}
	if len(reqs) != 1 {
		t.Fatalf("got %d requirements, want 1", len(reqs))
	}
	if len(reqs[0].Relationships) != 1 {
		t.Fatalf("got %d relationships, want 1", len(reqs[0].Relationships))
	}
	rel := reqs[0].Relationships[0]
	if rel.Path != "Observation.subject" || rel.Target.Type != "Patient" {
		t.Fatalf("unexpected relationship: %+v", rel)
	}
	if reqs[0].Cardinality != (model.CardinalityRequirement{Min: 1, Max: 1}) {
		t.Fatalf("unexpected cardinality: %+v", reqs[0].Cardinality)
	}
}

func TestPlanToDataRequirementsIgnoresInteractionsAndEmptyTypes(t *testing.T) {
	plan := &CoveragePlan{
		Requirements: []CoverageRequirement{
			{ID: "i1", Domain: CoverageDomainInteraction, ResourceType: "Observation"},
			{ID: "i2", ResourceType: ""},
		},
	}
	reqs, err := PlanToDataRequirements(plan)
	if err != nil {
		t.Fatalf("PlanToDataRequirements returned error: %v", err)
	}
	if len(reqs) != 0 {
		t.Fatalf("got %d requirements, want 0", len(reqs))
	}
}

func TestPlanToDataRequirementsNilPlan(t *testing.T) {
	if _, err := PlanToDataRequirements(nil); err == nil {
		t.Fatal("expected error for nil plan")
	}
}
