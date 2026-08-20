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
