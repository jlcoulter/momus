package main

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/jlcoulter/momus/internal/core/ast"
	"github.com/jlcoulter/momus/internal/core/coverage"
	"github.com/jlcoulter/momus/internal/core/runner"
	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/provisioning"
)

func TestParsePerTypeCounts(t *testing.T) {
	got := parsePerTypeCounts([]string{"Organization=10", "HealthcareService=25", "bad", "=3", "Patient=", "Encounter=-5"})
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
	if _, ok := got["Encounter"]; ok {
		t.Fatalf("expected negative-count entry to be ignored, got %v", got)
	}
}

func TestIntersectCaseInsensitive(t *testing.T) {
	// Empty available: no restriction, returns requested unchanged.
	got, err := intersectCaseInsensitive([]string{"Patient", "Observation"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %v, want 2 entries", got)
	}

	// Empty requested: returns available unchanged.
	got, err = intersectCaseInsensitive(nil, []string{"Patient"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != "Patient" {
		t.Fatalf("got %v, want [Patient]", got)
	}

	// Case-insensitive intersection preserves the requested spelling.
	got, err = intersectCaseInsensitive([]string{"patient", "Observation"}, []string{"Patient", "Encounter"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != "patient" {
		t.Fatalf("got %v, want [patient]", got)
	}

	// Non-empty request with zero intersection: error, not silent widening.
	_, err = intersectCaseInsensitive([]string{"Patient"}, []string{"Encounter"})
	if err == nil {
		t.Fatal("expected error for zero intersection")
	}
	if !strings.Contains(err.Error(), "none of the requested resource types are supported") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestLoadOpenAPIDocumentErrorWrapping(t *testing.T) {
	_, err := loadOpenAPIDocument(filepath.Join(t.TempDir(), "missing.json"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "read openapi document") {
		t.Fatalf("error lacks context: %v", err)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("error does not wrap os.ErrNotExist: %v", err)
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

func TestWriteDebugOutput(t *testing.T) {
	dir := t.TempDir()
	orig := debugOutputDir
	debugOutputDir = dir
	t.Cleanup(func() { debugOutputDir = orig })

	// Disabled: no file written.
	if err := writeDebugOutput(false, "coverage-plan.json", []byte("{}")); err != nil {
		t.Fatalf("writeDebugOutput(false) returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "coverage-plan.json")); !os.IsNotExist(err) {
		t.Fatalf("expected no file when debug disabled")
	}

	// Enabled: file written with content.
	if err := writeDebugOutput(true, "coverage-plan.json", []byte("{}\n")); err != nil {
		t.Fatalf("writeDebugOutput(true) returned error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "coverage-plan.json"))
	if err != nil {
		t.Fatalf("read debug output: %v", err)
	}
	if string(data) != "{}\n" {
		t.Fatalf("debug output = %q, want %q", data, "{}\n")
	}
}

func TestWriteDebugBulk(t *testing.T) {
	dir := t.TempDir()
	orig := debugOutputDir
	debugOutputDir = dir
	t.Cleanup(func() { debugOutputDir = orig })

	instances := []*model.ResourceInstance{
		{ResourceType: "Patient", LocalID: "p1", Resource: map[string]any{"resourceType": "Patient", "id": "p1"}},
	}

	// Disabled: no file written.
	if err := writeDebugBulk(false, instances); err != nil {
		t.Fatalf("writeDebugBulk(false) returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "bulk.ndjson")); !os.IsNotExist(err) {
		t.Fatalf("expected no bulk file when debug disabled")
	}

	// Enabled: NDJSON written.
	if err := writeDebugBulk(true, instances); err != nil {
		t.Fatalf("writeDebugBulk(true) returned error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "bulk.ndjson"))
	if err != nil {
		t.Fatalf("read bulk debug output: %v", err)
	}
	if string(data) != "{\"id\":\"p1\",\"resourceType\":\"Patient\"}\n" {
		t.Fatalf("bulk debug output = %q", data)
	}
}

func TestResolveBulkOutputPath(t *testing.T) {
	dir := t.TempDir()

	got, err := resolveBulkOutputPath(dir)
	if err != nil {
		t.Fatalf("resolveBulkOutputPath(existing dir): %v", err)
	}
	if got != filepath.Join(dir, "bulk.ndjson") {
		t.Fatalf("existing dir output = %q, want %q", got, filepath.Join(dir, "bulk.ndjson"))
	}

	got, err = resolveBulkOutputPath(filepath.Join(dir, "nested") + string(os.PathSeparator))
	if err != nil {
		t.Fatalf("resolveBulkOutputPath(trailing separator): %v", err)
	}
	if got != filepath.Join(dir, "nested", "bulk.ndjson") {
		t.Fatalf("trailing separator output = %q, want %q", got, filepath.Join(dir, "nested", "bulk.ndjson"))
	}

	filePath := filepath.Join(dir, "data.ndjson")
	got, err = resolveBulkOutputPath(filePath)
	if err != nil {
		t.Fatalf("resolveBulkOutputPath(file): %v", err)
	}
	if got != filePath {
		t.Fatalf("file output = %q, want %q", got, filePath)
	}
}

func TestStreamBulkDatasetWritesTargetsBeforeDependents(t *testing.T) {
	var mu sync.Mutex
	created := make(map[string]bool)
	var order []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, r.URL.Path)
		if r.URL.Path == "/Observation/obs" && !created["/Patient/pat"] {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"resourceType":"OperationOutcome","issue":[{"severity":"error","diagnostics":"missing patient"}]}`))
			return
		}
		created[r.URL.Path] = true
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	dataset := &model.Dataset{
		Resources: map[string]*model.ResourceInstance{
			"obs": {LocalID: "obs", ResourceType: "Observation", Resource: map[string]any{
				"resourceType": "Observation", "id": "obs", "subject": map[string]any{"reference": "Patient/pat"},
			}},
			"pat": {LocalID: "pat", ResourceType: "Patient", Resource: map[string]any{"resourceType": "Patient", "id": "pat"}},
		},
		Relationships: []model.Reference{{SourceID: "obs", Path: "Observation.subject", TargetID: "pat"}},
	}

	res := provisioning.New(server.URL, nil).ProvisionAll(t.Context(), dataset)
	if !res.Complete() {
		t.Fatalf("ProvisionAll incomplete: %d failed", res.Failed)
	}
	if len(order) != 2 || order[0] != "/Patient/pat" || order[1] != "/Observation/obs" {
		t.Fatalf("write order = %v, want target before dependent", order)
	}
}
