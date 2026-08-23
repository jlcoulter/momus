package coverage

import "testing"

func TestReportTypeConstruction(t *testing.T) {
	// ExecutedRequirementResult.
	res := ExecutedRequirementResult{RequirementID: "r1", Passed: true}
	if res.RequirementID != "r1" || !res.Passed {
		t.Fatalf("ExecutedRequirementResult = %+v", res)
	}

	// DomainCoverageSummary.
	ds := DomainCoverageSummary{Total: 10, Covered: 7, Uncovered: 3, CoveragePercent: 70.0}
	if ds.Total != 10 || ds.Covered != 7 || ds.Uncovered != 3 || ds.CoveragePercent != 70.0 {
		t.Fatalf("DomainCoverageSummary = %+v", ds)
	}

	// EvaluationReport with all fields populated.
	er := EvaluationReport{
		TotalRequirements:     10,
		CoveredRequirements:   7,
		UncoveredRequirements: 3,
		CoveragePercent:       70.0,
		ByDomain: map[CoverageDomain]DomainCoverageSummary{
			CoverageDomainSearch: {Total: 4, Covered: 3},
		},
		ByResourceType: map[string]DomainCoverageSummary{
			"Patient": {Total: 5, Covered: 4},
		},
		ByVariant: map[CoverageVariant]DomainCoverageSummary{
			CoverageVariantSearchValid: {Total: 2, Covered: 2},
		},
		Covered:   []CoverageRequirement{{ID: "c1"}},
		Uncovered: []CoverageRequirement{{ID: "u1"}},
	}
	if er.TotalRequirements != 10 || er.ByDomain[CoverageDomainSearch].Covered != 3 ||
		er.ByResourceType["Patient"].Covered != 4 || er.ByVariant[CoverageVariantSearchValid].Covered != 2 {
		t.Fatalf("EvaluationReport = %+v", er)
	}
	if len(er.Covered) != 1 || len(er.Uncovered) != 1 {
		t.Fatalf("covered/uncovered lengths = %d/%d", len(er.Covered), len(er.Uncovered))
	}
}
