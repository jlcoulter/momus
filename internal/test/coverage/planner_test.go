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

func TestPlanDependenciesCycleReturnsDeterministicFallbackLevel(t *testing.T) {
	reqs := []CoverageRequirement{
		{ID: "a1", ResourceType: "A", ElementPath: "A.ref", DependencyTargets: []string{"B"}},
		{ID: "b1", ResourceType: "B", ElementPath: "B.ref", DependencyTargets: []string{"A"}},
	}

	plan, err := PlanDependencies(reqs)
	if err != nil {
		t.Fatalf("PlanDependencies returned error: %v", err)
	}
	if len(plan.Levels) != 1 {
		t.Fatalf("got %d levels, want 1", len(plan.Levels))
	}
	if len(plan.Levels[0]) != 2 {
		t.Fatalf("got level entries %+v, want two entries", plan.Levels[0])
	}
	if plan.Levels[0][0] != "A" || plan.Levels[0][1] != "B" {
		t.Fatalf("expected deterministic alphabetical cycle fallback [A B], got %+v", plan.Levels[0])
	}
}
