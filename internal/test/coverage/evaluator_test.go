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

func TestEvaluateCoveragePartiallyCovered(t *testing.T) {
	plan := &CoveragePlan{Requirements: []CoverageRequirement{
		{ID: "r1", Domain: CoverageDomainCardinality, ResourceType: "Patient", ElementPath: "Patient.name", Variant: CoverageVariantValidMin},
		{ID: "r2", Domain: CoverageDomainCardinality, ResourceType: "Patient", ElementPath: "Patient.name", Variant: CoverageVariantMissingRequired},
	}}

	executed := []ExecutedRequirementResult{
		{RequirementID: "r1", Passed: false},
		{RequirementID: "r1", Passed: true},
		{RequirementID: "external-id", Passed: true},
	}
	report := EvaluateCoverage(plan, executed)

	if report.CoveredRequirements != 1 || report.UncoveredRequirements != 1 {
		t.Fatalf("unexpected covered/uncovered counts: %+v", report)
	}
	if len(report.Uncovered) != 1 || report.Uncovered[0].ID != "r2" {
		t.Fatalf("unexpected uncovered requirements: %+v", report.Uncovered)
	}
	if report.CoveragePercent != 50 {
		t.Fatalf("got coverage percent %.2f, want 50", report.CoveragePercent)
	}
	if resource := report.ByResourceType["Patient"]; resource.Total != 2 || resource.Covered != 1 || resource.Uncovered != 1 {
		t.Fatalf("unexpected resource summary: %+v", resource)
	}
	if variant := report.ByVariant[CoverageVariantMissingRequired]; variant.Total != 1 || variant.Covered != 0 || variant.Uncovered != 1 {
		t.Fatalf("unexpected missing-required summary: %+v", variant)
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
	if report.CoveragePercent != 100 {
		t.Fatalf("got coverage percent %.2f, want 100", report.CoveragePercent)
	}
}
