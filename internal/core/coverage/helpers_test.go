package coverage

import (
	"html/template"
	"testing"
)

func TestCoveragePercent(t *testing.T) {
	if got := coveragePercent(0, 0); got != 100 {
		t.Fatalf("coveragePercent(0,0) = %v, want 100", got)
	}
	if got := coveragePercent(50, 100); got != 50 {
		t.Fatalf("coveragePercent(50,100) = %v, want 50", got)
	}
	if got := coveragePercent(-1, 0); got != 100 {
		t.Fatalf("coveragePercent(neg,0) = %v, want 100", got)
	}
}

func TestPercentHelpers(t *testing.T) {
	m := map[CoverageDomain]DomainCoverageSummary{CoverageDomainSearch: {CoveragePercent: 75}}
	if got := percentOf(m, string(CoverageDomainSearch)); got != 75 {
		t.Fatalf("percentOf = %v, want 75", got)
	}
	if got := percentOf(m, string(CoverageDomainDatatype)); got != 0 {
		t.Fatalf("percentOf(missing) = %v, want 0", got)
	}
	rm := map[string]DomainCoverageSummary{"Patient": {CoveragePercent: 60}}
	if got := percentOfResource(rm, "Patient"); got != 60 {
		t.Fatalf("percentOfResource = %v, want 60", got)
	}
	if got := percentOfResource(rm, "Observation"); got != 0 {
		t.Fatalf("percentOfResource(missing) = %v, want 0", got)
	}
	vm := map[CoverageVariant]DomainCoverageSummary{CoverageVariantSearchValid: {CoveragePercent: 80}}
	if got := percentOfVariant(vm, string(CoverageVariantSearchValid)); got != 80 {
		t.Fatalf("percentOfVariant = %v, want 80", got)
	}
	if got := percentOfVariant(vm, "other"); got != 0 {
		t.Fatalf("percentOfVariant(missing) = %v, want 0", got)
	}
}

func TestRowFillStyle(t *testing.T) {
	if got := rowFillStyle(50); got != template.CSS("--success-pct: 50.0%;") {
		t.Fatalf("rowFillStyle(50) = %q", got)
	}
	if got := rowFillStyle(-5); got != template.CSS("--success-pct: 0.0%;") {
		t.Fatalf("rowFillStyle(neg) = %q", got)
	}
	if got := rowFillStyle(150); got != template.CSS("--success-pct: 100.0%;") {
		t.Fatalf("rowFillStyle(over) = %q", got)
	}
}

func TestAppendUniqueAndNormalizeDependencyTarget(t *testing.T) {
	if got := appendUnique([]string{"a", "b"}, "a"); len(got) != 2 {
		t.Fatalf("appendUnique(dup) = %v", got)
	}
	if got := appendUnique([]string{"a"}, "b"); len(got) != 2 || got[1] != "b" {
		t.Fatalf("appendUnique(new) = %v", got)
	}
	resourceSet := map[string]struct{}{"Patient": {}}
	resourceTypes := []string{"Patient", "Observation"}
	if got := normalizeDependencyTarget("Patient", resourceSet, resourceTypes); got != "Patient" {
		t.Fatalf("normalizeDependencyTarget(exact) = %q", got)
	}
	if got := normalizeDependencyTarget("", resourceSet, resourceTypes); got != "" {
		t.Fatalf("normalizeDependencyTarget(empty) = %q", got)
	}
	if got := normalizeDependencyTarget("patient", resourceSet, resourceTypes); got != "Patient" {
		t.Fatalf("normalizeDependencyTarget(case-insensitive) = %q", got)
	}
}
