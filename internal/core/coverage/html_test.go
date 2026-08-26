package coverage

import (
	"strings"
	"testing"
)

func TestRenderHTMLIncludesDrillDownItems(t *testing.T) {
	evaluation := EvaluationReport{
		TotalRequirements:     3,
		CoveredRequirements:   1,
		UncoveredRequirements: 2,
		CoveragePercent:       33.333,
		ByDomain: map[CoverageDomain]DomainCoverageSummary{
			CoverageDomainCardinality: {Total: 2, Covered: 1, Uncovered: 1, CoveragePercent: 50},
			CoverageDomainDatatype:    {Total: 1, Covered: 0, Uncovered: 1, CoveragePercent: 0},
		},
	}

	items := []HTMLItem{
		{ID: "req-1", Domain: "cardinality", Resource: "Patient", Variant: "missing-required", Expression: "status in [200]", Passed: false, StatusCode: 422, RequestMethod: "PUT", RequestURL: "http://localhost/fhir/Patient", RequestBody: `{"status":"final"}`, ResponseBody: `{"resourceType":"OperationOutcome"}`},
		{ID: "req-2", Domain: "datatype", Resource: "Patient", Variant: "datatype-valid", Expression: "status in [200]", Passed: true, StatusCode: 200, RequestMethod: "GET", RequestURL: "http://localhost/fhir/Patient/1"},
	}

	out, err := RenderHTML(evaluation, items)
	if err != nil {
		t.Fatalf("RenderHTML returned error: %v", err)
	}
	html := string(out)
	for _, want := range []string{
		"<title>Momus Coverage Report</title>",
		"33.3%",
		"By Domain",
		"By Resource Type",
		"By Variant",
		"cardinality",
		"datatype",
		"req-1",
		"req-2",
		"PASS",
		"FAIL",
		"http://localhost/fhir/Patient",
		"status in [200]",
		"OperationOutcome",
		"Request Body",
		"Response Body",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("HTML report missing %q\n%s", want, html)
		}
	}
}

// TestRenderHTMLGroupsByPolarity verifies that the HTML report splits items into
// Positive (accept) and Negative (reject) sub-groups within each drill,
// classifying variants via CoverageVariant.IsReject.
func TestRenderHTMLGroupsByPolarity(t *testing.T) {
	evaluation := EvaluationReport{
		TotalRequirements:     2,
		CoveredRequirements:   1,
		UncoveredRequirements: 1,
		CoveragePercent:       50,
		ByDomain: map[CoverageDomain]DomainCoverageSummary{
			CoverageDomainCardinality: {Total: 2, Covered: 1, Uncovered: 1, CoveragePercent: 50},
		},
	}

	items := []HTMLItem{
		{ID: "req-pos", Domain: "cardinality", Resource: "Patient", Variant: "valid-min", Expression: "status in [200,201]", Passed: true, StatusCode: 201, RequestMethod: "PUT", RequestURL: "http://localhost/fhir/Patient/pos"},
		{ID: "req-neg", Domain: "cardinality", Resource: "Patient", Variant: "missing-required", Expression: "status in [400,412,422]", Passed: false, StatusCode: 422, RequestMethod: "PUT", RequestURL: "http://localhost/fhir/Patient/neg"},
	}

	out, err := RenderHTML(evaluation, items)
	if err != nil {
		t.Fatalf("RenderHTML returned error: %v", err)
	}
	html := string(out)

	// The coverage matrix may render requirement IDs before the polarity
	// sections, so scope the polarity check to the drill-down section (which
	// starts at the "By Domain" heading) to verify grouping independently of the
	// matrix.
	if idx := strings.Index(html, "By Domain"); idx >= 0 {
		html = html[idx:]
	}

	// Both polarity headings must appear.
	if !strings.Contains(html, "Positive") {
		t.Fatalf("HTML report missing Positive sub-group\n%s", html)
	}
	if !strings.Contains(html, "Negative") {
		t.Fatalf("HTML report missing Negative sub-group\n%s", html)
	}

	// The positive item (valid-min) must appear under the Positive heading, and
	// the negative item (missing-required) under the Negative heading. Verify by
	// checking the order: Positive should precede req-pos, and Negative should
	// precede req-neg.
	posIdx := strings.Index(html, "Positive")
	negIdx := strings.Index(html, "Negative")
	posItemIdx := strings.Index(html, "req-pos")
	negItemIdx := strings.Index(html, "req-neg")
	if posIdx < 0 || posItemIdx < 0 || posItemIdx < posIdx {
		t.Fatalf("positive item req-pos not under Positive heading: pos=%d item=%d", posIdx, posItemIdx)
	}
	if negIdx < 0 || negItemIdx < 0 || negItemIdx < negIdx {
		t.Fatalf("negative item req-neg not under Negative heading: neg=%d item=%d", negIdx, negItemIdx)
	}
}

// TestRenderHTMLComputesPercentFromItemsWithoutEvaluation verifies that when no
// coverage evaluation is supplied (no --coverage-plan), the report derives the
// coverage percentage from the executed items (passed / total) instead of
// showing 0.0%.
func TestRenderHTMLComputesPercentFromItemsWithoutEvaluation(t *testing.T) {
	items := []HTMLItem{
		{ID: "req-1", Domain: "cardinality", Resource: "Patient", Variant: "valid-min", Passed: true},
		{ID: "req-2", Domain: "cardinality", Resource: "Patient", Variant: "missing-required", Passed: false},
		{ID: "req-3", Domain: "datatype", Resource: "Patient", Variant: "datatype-valid", Passed: true},
	}

	// Empty evaluation (as when --coverage-plan is not supplied).
	out, err := RenderHTML(EvaluationReport{}, items)
	if err != nil {
		t.Fatalf("RenderHTML returned error: %v", err)
	}
	html := string(out)

	// 2 of 3 passed = 66.7%%.
	if !strings.Contains(html, "66.7%") {
		t.Fatalf("HTML report missing item-derived overall percentage 66.7%%\n%s", html)
	}
	// The cardinality drill has 1 passed / 2 total = 50.0%%.
	if !strings.Contains(html, "cardinality — 50.0%") {
		t.Fatalf("HTML report missing item-derived cardinality percentage 50.0%%\n%s", html)
	}
	// The datatype drill has 1 passed / 1 total = 100.0%%.
	if !strings.Contains(html, "datatype — 100.0%") {
		t.Fatalf("HTML report missing item-derived datatype percentage 100.0%%\n%s", html)
	}
}

func TestRenderHTMLAddsPercentageFillToRows(t *testing.T) {
	items := []HTMLItem{
		{ID: "req-pass", Domain: "cardinality", Resource: "Patient", Variant: "valid-min", Passed: true},
		{ID: "req-fail", Domain: "cardinality", Resource: "Patient", Variant: "missing-required", Passed: false},
	}

	out, err := RenderHTML(EvaluationReport{}, items)
	if err != nil {
		t.Fatalf("RenderHTML returned error: %v", err)
	}
	html := string(out)

	for _, want := range []string{
		`class="coverage-row" style="--success-pct: 50.0%;"`,
		`Positive — 100.0% (1 passed / 0 failed / 1 total)`,
		`Negative — 0.0% (0 passed / 1 failed / 1 total)`,
		`style="--success-pct: 100.0%;"><span class="pass">PASS</span> — req-pass`,
		`style="--success-pct: 0.0%;"><span class="fail">FAIL</span> — req-fail`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("HTML report missing %q\n%s", html, want)
		}
	}
}

// TestRenderHTMLIncludesCoverageMatrix verifies that the HTML report renders a
// coverage matrix per resource type, with pass/fail/untested cells for each
// (element, variant) intersection.
func TestRenderHTMLIncludesCoverageMatrix(t *testing.T) {
	items := []HTMLItem{
		{ID: "cardinality|Patient|name|valid-min", HumanID: "Patient.name.cardinality.valid-min", Domain: "cardinality", Resource: "Patient", ElementPath: "Patient.name", Variant: "valid-min", Passed: true},
		{ID: "cardinality|Patient|name|missing-required", Domain: "cardinality", Resource: "Patient", ElementPath: "Patient.name", Variant: "missing-required", Passed: false},
		{ID: "search|Patient|name|search-valid", HumanID: "Patient.search.name.valid", Domain: "search", Resource: "Patient", SearchCode: "name", Variant: "search-valid", Passed: true},
	}

	out, err := RenderHTML(EvaluationReport{}, items)
	if err != nil {
		t.Fatalf("RenderHTML returned error: %v", err)
	}
	html := string(out)

	for _, want := range []string{
		"Coverage Matrix: Patient",
		"Patient.name",
		"Patient?name",
		`class="cell-pass"`,
		`class="cell-fail"`,
		`class="cell-untested"`,
		"✓",
		"✗",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("HTML report missing %q", want)
		}
	}
}

// TestRenderHTMLIncludesGlossary verifies the domain/variant glossary renders.
func TestRenderHTMLIncludesGlossary(t *testing.T) {
	out, err := RenderHTML(EvaluationReport{}, nil)
	if err != nil {
		t.Fatalf("RenderHTML returned error: %v", err)
	}
	html := string(out)

	for _, want := range []string{
		"Glossary",
		"Domains",
		"Variants",
		"cardinality",
		"Required element presence and count constraints",
		"search-valid",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("HTML report missing glossary %q", want)
		}
	}
}
