package coverage

import "testing"

func TestEvaluateCoverageAllCovered(t *testing.T) {
	plan := &CoveragePlan{Requirements: []CoverageRequirement{
		{ID: "r1", Domain: CoverageDomainCardinality, ResourceType: "Patient", ElementPath: "Patient.name", Variant: CoverageVariantValidMin},
		{ID: "r2", Domain: CoverageDomainCardinality, ResourceType: "Patient", ElementPath: "Patient.name", Variant: CoverageVariantMissingRequired},
	}}

	executed := []ExecutedRequirementResult{{RequirementID: "r1", Passed: true}, {RequirementID: "r2", Passed: true}}
	report := EvaluateCoverage(plan, executed)

	if report.TotalRequirements != 2 {
		t.Fatalf("got total %d, want 2", report.TotalRequirements)
	}
	if report.CoveredRequirements != 2 || report.UncoveredRequirements != 0 {
		t.Fatalf("unexpected covered/uncovered counts: %+v", report)
	}
	if len(report.Covered) != 2 || len(report.Uncovered) != 0 {
		t.Fatalf("got %d covered / %d uncovered requirements, want 2/0", len(report.Covered), len(report.Uncovered))
	}
	if report.CoveragePercent != 100 {
		t.Fatalf("got coverage percent %.2f, want 100", report.CoveragePercent)
	}
	if domain := report.ByDomain[CoverageDomainCardinality]; domain.Total != 2 || domain.Covered != 2 || domain.Uncovered != 0 {
		t.Fatalf("unexpected cardinality summary: %+v", domain)
	}
	if resource := report.ByResourceType["Patient"]; resource.Total != 2 || resource.Covered != 2 || resource.Uncovered != 0 {
		t.Fatalf("unexpected resource summary: %+v", resource)
	}
	if variant := report.ByVariant[CoverageVariantValidMin]; variant.Total != 1 || variant.Covered != 1 || variant.Uncovered != 0 {
		t.Fatalf("unexpected valid-min summary: %+v", variant)
	}
}

func TestEvaluateCoverageRequiresAllCasesPass(t *testing.T) {
	plan := &CoveragePlan{Requirements: []CoverageRequirement{
		{ID: "r1", Domain: CoverageDomainState, ResourceType: "Patient", Variant: CoverageVariantStateCRUDSequence},
		{ID: "r2", Domain: CoverageDomainCardinality, ResourceType: "Patient", Variant: CoverageVariantValidMin},
	}}

	// r1 is a multi-step obligation (e.g. a CRUD sequence): one case passed but
	// another failed, so the obligation must NOT count as covered.
	executed := []ExecutedRequirementResult{
		{RequirementID: "r1", Passed: true},
		{RequirementID: "r1", Passed: false},
		{RequirementID: "r1", Passed: true},
		{RequirementID: "r2", Passed: true},
	}
	report := EvaluateCoverage(plan, executed)

	if report.CoveredRequirements != 1 || report.UncoveredRequirements != 1 {
		t.Fatalf("unexpected covered/uncovered counts: %+v", report)
	}
	if len(report.Covered) != 1 || report.Covered[0].ID != "r2" {
		t.Fatalf("covered should be r2 only (r1 had a failing step), got %v", report.Covered)
	}
	if len(report.Uncovered) != 1 || report.Uncovered[0].ID != "r1" {
		t.Fatalf("r1 should be uncovered (not all steps passed), got %v", report.Uncovered)
	}
	if report.CoveragePercent != 50 {
		t.Fatalf("got coverage percent %.2f, want 50", report.CoveragePercent)
	}
	if variant := report.ByVariant[CoverageVariantStateCRUDSequence]; variant.Total != 1 || variant.Covered != 0 || variant.Uncovered != 1 {
		t.Fatalf("unexpected crud-sequence summary: %+v", variant)
	}
}

func TestEvaluateCoverageRequirementWithNoResultIsUncovered(t *testing.T) {
	plan := &CoveragePlan{Requirements: []CoverageRequirement{
		{ID: "r1", Domain: CoverageDomainCardinality, ResourceType: "Patient", Variant: CoverageVariantValidMin},
		{ID: "r2", Domain: CoverageDomainCardinality, ResourceType: "Patient", Variant: CoverageVariantMissingRequired},
	}}

	// r1 has a passing result; r2 has none and must remain uncovered.
	executed := []ExecutedRequirementResult{
		{RequirementID: "r1", Passed: true},
	}
	report := EvaluateCoverage(plan, executed)

	if report.CoveredRequirements != 1 || report.UncoveredRequirements != 1 {
		t.Fatalf("unexpected covered/uncovered counts: %+v", report)
	}
	if len(report.Uncovered) != 1 || report.Uncovered[0].ID != "r2" {
		t.Fatalf("unexpected uncovered requirements: %+v", report.Uncovered)
	}
}

func TestEvaluateCoverageEmptyPlan(t *testing.T) {
	report := EvaluateCoverage(nil, nil)
	if report.TotalRequirements != 0 {
		t.Fatalf("got total %d, want 0", report.TotalRequirements)
	}
	if report.CoveredRequirements != 0 || report.UncoveredRequirements != 0 {
		t.Fatalf("unexpected covered/uncovered counts: %+v", report)
	}
	// An empty plan must not report 100% coverage.
	if report.CoveragePercent != 0 {
		t.Fatalf("got coverage percent %.2f, want 0", report.CoveragePercent)
	}
}
