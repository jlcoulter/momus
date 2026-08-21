package runner

import (
	"net/http"
	"testing"

	"github.com/jlcoulter/momus/internal/test/ast"
)

func TestClassifyTriage(t *testing.T) {
	cases := []struct {
		name       string
		expected   string
		passed     bool
		statusCode int
		want       TriageOutcome
	}{
		{"accept passed", "accept", true, 201, ""},
		{"accept rejected validation", "accept", false, 422, TriageOutcomeAcceptRejected},
		{"accept rejected bad request", "accept", false, 400, TriageOutcomeAcceptRejected},
		{"accept server error", "accept", false, 500, TriageOutcomeServerError},
		{"accept redirect ambiguous", "accept", false, 302, TriageOutcomeAmbiguous},
		{"reject passed", "reject", true, 400, ""},
		{"reject accepted", "reject", false, 200, TriageOutcomeRejectAccepted},
		{"reject accepted 201", "reject", false, 201, TriageOutcomeRejectAccepted},
		{"reject server error", "reject", false, 500, TriageOutcomeServerError},
		{"reject other 4xx ambiguous", "reject", false, 404, TriageOutcomeAmbiguous},
		{"unknown expected", "weird", false, 400, TriageOutcomeAmbiguous},
		{"case insensitive", "ACCEPT", false, 400, TriageOutcomeAcceptRejected},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyTriage(tc.expected, tc.passed, tc.statusCode); got != tc.want {
				t.Fatalf("classifyTriage(%q, %v, %d) = %q, want %q", tc.expected, tc.passed, tc.statusCode, got, tc.want)
			}
		})
	}
}

func trace(expected, domain, variant string) *ast.Trace {
	return &ast.Trace{Expected: expected, Domain: domain, Variant: variant, ElementPath: "Patient.name"}
}

func TestBuildTriageSummaryRollsUpByVariant(t *testing.T) {
	cases := []CaseResult{
		// Three interaction accept failures (systematic test-bug signal).
		{RequirementID: "int-1", Passed: false, StatusCode: http.StatusUnprocessableEntity, Trace: trace("accept", "interaction", "interaction-pair")},
		{RequirementID: "int-2", Passed: false, StatusCode: http.StatusUnprocessableEntity, Trace: trace("accept", "interaction", "interaction-pair")},
		{RequirementID: "int-3", Passed: false, StatusCode: http.StatusUnprocessableEntity, Trace: trace("accept", "interaction", "interaction-pair")},
		// One reject accepted (server accepted a bad payload).
		{RequirementID: "neg-1", Passed: false, StatusCode: http.StatusOK, Trace: trace("reject", "terminology", "terminology-invalid")},
		// One server error.
		{RequirementID: "req-x", Passed: false, StatusCode: http.StatusInternalServerError, Trace: trace("accept", "datatype", "datatype-valid")},
		// Passed cases are ignored.
		{RequirementID: "ok-1", Passed: true, StatusCode: http.StatusCreated, Trace: trace("accept", "cardinality", "valid-min")},
		// Setup case (no trace) is ignored.
		{RequirementID: "setup:Patient", Passed: false, StatusCode: http.StatusInternalServerError},
	}

	summary := buildTriageSummary(cases)
	if summary == nil {
		t.Fatal("expected a non-nil triage summary")
	}
	if summary.AcceptRejected != 3 || summary.RejectAccepted != 1 || summary.ServerError != 1 || summary.Ambiguous != 0 {
		t.Fatalf("unexpected counts: accept-rejected=%d reject-accepted=%d server-error=%d ambiguous=%d",
			summary.AcceptRejected, summary.RejectAccepted, summary.ServerError, summary.Ambiguous)
	}
	if len(summary.Groups) != 3 {
		t.Fatalf("got %d groups, want 3", len(summary.Groups))
	}
	// Largest group first: the interaction accepts.
	lead := summary.Groups[0]
	if lead.Outcome != TriageOutcomeAcceptRejected || lead.Variant != "interaction-pair" || lead.Count != 3 {
		t.Fatalf("lead group = %+v, want accept-rejected interaction-pair count 3", lead)
	}
	if lead.ExampleRequirementID != "int-1" {
		t.Fatalf("lead example = %s, want int-1", lead.ExampleRequirementID)
	}
	// Summary hint should call out the likely broken shared payload.
	if summary.Hint == "" || summary.Hint != "Most failures are interaction accepts sharing one payload: the shared valid payload is likely invalid, so this is probably a test-generation bug to fix, not a server issue." {
		t.Fatalf("unexpected hint: %q", summary.Hint)
	}
}

func TestBuildTriageSummaryNilWhenNoFailures(t *testing.T) {
	cases := []CaseResult{
		{RequirementID: "ok-1", Passed: true, StatusCode: http.StatusCreated, Trace: trace("accept", "cardinality", "valid-min")},
	}
	if got := buildTriageSummary(cases); got != nil {
		t.Fatalf("expected nil triage for all-pass run, got %+v", got)
	}
}

func TestBuildTriageSummaryRejectAcceptedHint(t *testing.T) {
	cases := []CaseResult{
		{RequirementID: "neg-1", Passed: false, StatusCode: http.StatusOK, Trace: trace("reject", "terminology", "terminology-invalid")},
		{RequirementID: "neg-2", Passed: false, StatusCode: http.StatusOK, Trace: trace("reject", "terminology", "terminology-invalid")},
	}
	summary := buildTriageSummary(cases)
	if summary == nil {
		t.Fatal("expected non-nil summary")
	}
	if summary.RejectAccepted != 2 {
		t.Fatalf("reject-accepted = %d, want 2", summary.RejectAccepted)
	}
	if summary.Hint == "" {
		t.Fatal("expected a reject-accepted hint")
	}
}

func TestBuildTriageSummaryDerivesExpectationWhenTraceNil(t *testing.T) {
	// OpenAPI-generated cases carry no coverage trace; the expectation must be
	// derived from the expression so they still participate in triage.
	cases := []CaseResult{
		{RequirementID: "op-1", Passed: false, StatusCode: http.StatusUnprocessableEntity, Expression: "status in [200,201,202,203,204]"},
		{RequirementID: "op-2", Passed: false, StatusCode: http.StatusOK, Expression: "status in [400,412,422]"},
		// No expression -> no derivable expectation -> skipped.
		{RequirementID: "setup:Patient", Passed: false, StatusCode: http.StatusInternalServerError},
	}
	summary := buildTriageSummary(cases)
	if summary == nil {
		t.Fatal("expected non-nil summary")
	}
	if summary.AcceptRejected != 1 || summary.RejectAccepted != 1 || summary.ServerError != 0 || summary.Ambiguous != 0 {
		t.Fatalf("unexpected counts: accept-rejected=%d reject-accepted=%d server-error=%d ambiguous=%d",
			summary.AcceptRejected, summary.RejectAccepted, summary.ServerError, summary.Ambiguous)
	}
}

func TestDeriveExpected(t *testing.T) {
	cases := []struct {
		expression string
		want       string
	}{
		{"status in [200,201]", "accept"},
		{"status in [200,201,202,203,204]", "accept"},
		{"status in [200,204]", "accept"},
		{"status in [400,412,422]", "reject"},
		{"status in [200,400]", ""},
		{"status in [300]", ""},
		{"body.total >= 2", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := deriveExpected(tc.expression); got != tc.want {
			t.Fatalf("deriveExpected(%q) = %q, want %q", tc.expression, got, tc.want)
		}
	}
}
