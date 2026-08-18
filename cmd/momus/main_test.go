package main

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"syscall"
	"testing"

	"github.com/jlcoulter/momus/internal/test/ast"
	"github.com/jlcoulter/momus/internal/test/coverage"
	"github.com/jlcoulter/momus/internal/test/runner"
)

func TestParsePerTypeCounts(t *testing.T) {
	got := parsePerTypeCounts([]string{"Organization=10", "HealthcareService=25", "bad", "=3", "Patient="})
	if got["Organization"] != 10 {
		t.Fatalf("Organization = %d, want 10", got["Organization"])
	}
	if got["HealthcareService"] != 25 {
		t.Fatalf("HealthcareService = %d, want 25", got["HealthcareService"])
	}
	if _, ok := got["bad"]; ok {
		t.Fatalf("expected malformed entry to be ignored, got %v", got)
	}
	if _, ok := got["Patient"]; ok {
		t.Fatalf("expected empty-count entry to be ignored")
	}
}

func TestMarshalCoverageRunOutputReadableSummary(t *testing.T) {
	report := &runner.Report{
		Total:  3,
		Passed: 1,
		Failed: 2,
		Cases: []runner.CaseResult{
			{RequirementID: "req-ok", Passed: true, StatusCode: http.StatusCreated, Trace: &ast.Trace{Expected: "accept", Domain: "cardinality", Variant: "valid-min"}},
			{RequirementID: "int-1", Passed: false, StatusCode: http.StatusUnprocessableEntity, Trace: &ast.Trace{Expected: "accept", Domain: "interaction", Variant: "interaction-pair"}},
			{RequirementID: "setup:Patient", Passed: false, StatusCode: http.StatusInternalServerError},
		},
		Triage: &runner.TriageSummary{
			AcceptRejected: 1,
			Groups: []runner.TriageGroup{{
				Outcome:              runner.TriageOutcomeAcceptRejected,
				Domain:               "interaction",
				Variant:              "interaction-pair",
				Expected:             "accept",
				Count:                1,
				ExampleRequirementID: "int-1",
			}},
		},
	}
	evaluation := coverage.EvaluationReport{
		TotalRequirements: 1, CoveredRequirements: 1, UncoveredRequirements: 0, CoveragePercent: 100,
	}

	out, err := marshalCoverageRunOutput(report, evaluation, false)
	if err != nil {
		t.Fatalf("marshalCoverageRunOutput returned error: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	summary, ok := payload["summary"].(map[string]any)
	if !ok {
		t.Fatalf("missing summary section: %+v", payload)
	}
	if summary["totalCases"] != float64(3) || summary["failedCases"] != float64(2) {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	// requirement/setup breakdown derived from cases (int-1 + req-ok = 2 requirement, setup:Patient = 1).
	if summary["requirementCases"] != float64(2) || summary["setupCases"] != float64(1) {
		t.Fatalf("unexpected case breakdown: %+v", summary)
	}

	// failures only contains failed cases, compact.
	failures, ok := payload["failures"].([]any)
	if !ok || len(failures) != 2 {
		t.Fatalf("failures = %v, want 2 compact entries", payload["failures"])
	}
	first := failures[0].(map[string]any)
	if first["requirementId"] != "int-1" {
		t.Fatalf("first failure = %v", first)
	}
	if first["expected"] != "accept" || first["domain"] != "interaction" {
		t.Fatalf("first failure missing trace fields: %v", first)
	}

	if _, hasTriage := payload["triage"]; !hasTriage {
		t.Fatal("missing triage section")
	}
	// Full cases must be absent by default.
	if _, hasCases := payload["cases"]; hasCases {
		t.Fatal("cases should be omitted unless --include-cases is set")
	}
}

func TestMarshalCoverageRunOutputIncludeCases(t *testing.T) {
	report := &runner.Report{Total: 1, Passed: 1, Cases: []runner.CaseResult{{RequirementID: "req-ok", Passed: true}}}
	evaluation := coverage.EvaluationReport{}
	out, err := marshalCoverageRunOutput(report, evaluation, true)
	if err != nil {
		t.Fatalf("marshalCoverageRunOutput returned error: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if _, ok := payload["cases"].([]any); !ok {
		t.Fatal("cases should be present when --include-cases is set")
	}
}

func TestIsServerUnavailable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "connection refused",
			err:  &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED},
			want: true,
		},
		{
			name: "wrapped connection refused",
			err:  &net.OpError{Op: "dial", Net: "tcp", Err: &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}},
			want: true,
		},
		{
			name: "dns failure",
			err:  &net.DNSError{Err: "no such host", Name: "localhost", IsNotFound: true},
			want: true,
		},
		{
			name: "timeout",
			err:  &net.OpError{Op: "dial", Net: "tcp", Err: timeoutError{}},
			want: true,
		},
		{
			name: "plain error",
			err:  errors.New("boom"),
			want: false,
		},
		{
			name: "nil",
			err:  nil,
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isServerUnavailable(tc.err); got != tc.want {
				t.Fatalf("isServerUnavailable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// timeoutError implements net.Error and reports itself as a timeout.
type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }
