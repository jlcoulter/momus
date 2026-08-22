package generation

import (
	"testing"

	"github.com/jlcoulter/momus/internal/test/coverage"
)

func TestGenerateFromCoveragePlanProgress(t *testing.T) {
	reqs := []coverage.CoverageRequirement{
		{ID: "req-org", ResourceType: "Organization", Domain: coverage.CoverageDomainSearch, Variant: coverage.CoverageVariantSearchValid, SearchCode: "_id"},
		{ID: "req-prac", ResourceType: "Practitioner", Domain: coverage.CoverageDomainSearch, Variant: coverage.CoverageVariantSearchValid, SearchCode: "_id"},
		{ID: "req-loc", ResourceType: "Location", Domain: coverage.CoverageDomainSearch, Variant: coverage.CoverageVariantSearchValid, SearchCode: "_id"},
	}

	var calls []int
	var lastTotal int
	_, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{Requirements: reqs}, BuildOptions{
		BaseURL: "http://localhost:8080/fhir",
		Progress: func(done, total int) {
			calls = append(calls, done)
			lastTotal = total
		},
	})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan: %v", err)
	}
	if lastTotal != 3 {
		t.Fatalf("progress total = %d, want 3", lastTotal)
	}
	if len(calls) != 3 {
		t.Fatalf("progress calls = %d, want 3 (got %v)", len(calls), calls)
	}
	if calls[0] != 1 || calls[1] != 2 || calls[2] != 3 {
		t.Fatalf("progress sequence = %v, want [1 2 3]", calls)
	}
}
