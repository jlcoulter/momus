package coverage

import "testing"

func TestPlanDependenciesReturnsTopologicalLevels(t *testing.T) {
	reqs := []CoverageRequirement{
		{ID: "p1", ResourceType: "Patient", ElementPath: "Patient.name"},
		{ID: "o1", ResourceType: "Observation", ElementPath: "Observation.subject", DependencyTargets: []string{"Patient"}},
		{ID: "d1", ResourceType: "DiagnosticReport", ElementPath: "DiagnosticReport.result", DependencyTargets: []string{"Observation", "Patient"}},
	}

	plan, err := PlanDependencies(reqs)
	if err != nil {
		t.Fatalf("PlanDependencies returned error: %v", err)
	}
	if len(plan.Levels) != 3 {
		t.Fatalf("got %d levels, want 3: %+v", len(plan.Levels), plan.Levels)
	}
	if got := plan.Levels[0][0]; got != "Patient" {
		t.Fatalf("level0 got %q, want Patient", got)
	}
	if got := plan.Levels[1][0]; got != "Observation" {
		t.Fatalf("level1 got %q, want Observation", got)
	}
	if got := plan.Levels[2][0]; got != "DiagnosticReport" {
		t.Fatalf("level2 got %q, want DiagnosticReport", got)
	}
}

func TestPlanDependenciesParallelLevel(t *testing.T) {
	reqs := []CoverageRequirement{
		{ID: "p1", ResourceType: "Patient", ElementPath: "Patient.name"},
		{ID: "c1", ResourceType: "Condition", ElementPath: "Condition.subject", DependencyTargets: []string{"Patient"}},
		{ID: "m1", ResourceType: "MedicationRequest", ElementPath: "MedicationRequest.subject", DependencyTargets: []string{"Patient"}},
	}

	plan, err := PlanDependencies(reqs)
	if err != nil {
		t.Fatalf("PlanDependencies returned error: %v", err)
	}
	if len(plan.Levels) != 2 {
		t.Fatalf("got %d levels, want 2", len(plan.Levels))
	}
	if plan.Levels[0][0] != "Patient" {
		t.Fatalf("unexpected level0: %+v", plan.Levels[0])
	}
	if len(plan.Levels[1]) != 2 {
		t.Fatalf("expected two parallel items in level1, got %+v", plan.Levels[1])
	}
}

func TestPlanDependenciesWithoutReferencePathsReturnsSingleLevel(t *testing.T) {
	reqs := []CoverageRequirement{
		{ID: "p1", ResourceType: "Patient", ElementPath: "Patient.gender"},
		{ID: "o1", ResourceType: "Observation", ElementPath: "Observation.status"},
	}

	plan, err := PlanDependencies(reqs)
	if err != nil {
		t.Fatalf("PlanDependencies returned error: %v", err)
	}
	if len(plan.Levels) != 1 {
		t.Fatalf("got %d levels, want 1", len(plan.Levels))
	}
	if len(plan.Levels[0]) != 2 {
		t.Fatalf("got level entries %+v, want 2 entries", plan.Levels[0])
	}
}

func TestPlanDependenciesCycleBreaksDeterministically(t *testing.T) {
	reqs := []CoverageRequirement{
		{ID: "a1", ResourceType: "A", ElementPath: "A.ref", DependencyTargets: []string{"B"}},
		{ID: "b1", ResourceType: "B", ElementPath: "B.ref", DependencyTargets: []string{"A"}},
	}

	plan, err := PlanDependencies(reqs)
	if err != nil {
		t.Fatalf("PlanDependencies returned error: %v", err)
	}
	if len(plan.Levels) != 2 {
		t.Fatalf("got %d levels, want 2", len(plan.Levels))
	}
	if len(plan.Levels[0]) != 1 || len(plan.Levels[1]) != 1 {
		t.Fatalf("expected single-entry cycle levels, got %+v", plan.Levels)
	}
	if plan.Levels[0][0] != "A" || plan.Levels[1][0] != "B" {
		t.Fatalf("expected deterministic cycle break order [A] then [B], got %+v", plan.Levels)
	}
}

func TestPlanDependenciesCycleBreakerPreservesDownstreamOrder(t *testing.T) {
	reqs := []CoverageRequirement{
		{ID: "a1", ResourceType: "A", ElementPath: "A.ref", DependencyTargets: []string{"B"}},
		{ID: "b1", ResourceType: "B", ElementPath: "B.ref", DependencyTargets: []string{"A"}},
		{ID: "c1", ResourceType: "C", ElementPath: "C.ref", DependencyTargets: []string{"A"}},
	}

	plan, err := PlanDependencies(reqs)
	if err != nil {
		t.Fatalf("PlanDependencies returned error: %v", err)
	}
	if len(plan.Levels) < 2 {
		t.Fatalf("expected at least 2 levels, got %+v", plan.Levels)
	}
	if plan.Levels[0][0] != "A" {
		t.Fatalf("expected cycle breaker A first, got %+v", plan.Levels[0])
	}
	seenC := false
	for _, level := range plan.Levels[1:] {
		for _, rt := range level {
			if rt == "C" {
				seenC = true
			}
		}
	}
	if !seenC {
		t.Fatalf("expected downstream resource C to be scheduled after A, got %+v", plan.Levels)
	}
}

func TestPlanDependenciesNormalizesProfileLikeDependencyTargets(t *testing.T) {
	reqs := []CoverageRequirement{
		{ID: "org-1", ResourceType: "Organization", ElementPath: "Organization.name"},
		{ID: "prov-1", ResourceType: "Provenance", ElementPath: "Provenance.target", DependencyTargets: []string{"hcpd-organization"}},
	}

	plan, err := PlanDependencies(reqs)
	if err != nil {
		t.Fatalf("PlanDependencies returned error: %v", err)
	}
	if len(plan.Dependencies["Provenance"]) != 1 || plan.Dependencies["Provenance"][0] != "Organization" {
		t.Fatalf("got provenance dependencies %+v, want [Organization]", plan.Dependencies["Provenance"])
	}
	if len(plan.Levels) != 2 || plan.Levels[0][0] != "Organization" || plan.Levels[1][0] != "Provenance" {
		t.Fatalf("got levels %+v, want [[Organization] [Provenance]]", plan.Levels)
	}
}
