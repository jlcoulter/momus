package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/test/ast"
	"github.com/jlcoulter/momus/internal/test/coverage"
)

// writeTestPlan encodes a test plan (seed dataset + test AST) to a temp file and
// returns its path, so "coverage run"/"coverage provision" can consume it.
func writeTestPlan(t *testing.T, plan *ast.Plan, ds *model.Dataset) string {
	t.Helper()
	out, err := encodeTestPlan(plan, ds)
	if err != nil {
		t.Fatalf("encodeTestPlan: %v", err)
	}
	path := filepath.Join(t.TempDir(), "test-plan.json")
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		t.Fatalf("write test plan: %v", err)
	}
	return path
}

// TestRunCmdConsumesTestPlanAndEvaluatesCoverage exercises the rewritten
// "coverage run" command end to end: it loads a generated test plan from disk
// (no package), executes the AST against a stub server whose seed data is
// already provisioned, and evaluates contractual coverage against a supplied
// coverage plan. This validates that execution (M), evaluation (N), and
// reporting (O) are wired together without any provisioning interleaved.
func TestRunCmdConsumesTestPlanAndEvaluatesCoverage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected only GET test requests, got %s %s", r.Method, r.URL)
		}
		w.Header().Set("Content-Type", "application/fhir+json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"resourceType":"Bundle","total":1,"entry":[{"resource":{"resourceType":"Patient","id":"momus-setup-patient"}}]}`))
	}))
	defer server.Close()

	testPlan := &ast.Plan{Version: "v1", Root: &ast.Sequence{Steps: []ast.Node{
		&ast.Request{
			Method:  "GET",
			URL:     server.URL + "/Patient?name=momus-search",
			Headers: map[string]string{"X-Momus-Requirement-ID": "req-search"},
		},
		&ast.Assert{
			Description:   "server returns search results",
			RequirementID: "req-search",
			Expression:    "status in [200]",
			Trace:         &ast.Trace{ResourceType: "Patient", Domain: string(coverage.CoverageDomainSearch), Variant: string(coverage.CoverageVariantSearchValid), Expected: "accept"},
		},
	}}}
	// The plan carries the seed dataset (provisioned separately); the search
	// itself does not depend on it, but the dataset flows through the plan.
	dataset := &model.Dataset{Resources: map[string]*model.ResourceInstance{
		"momus-setup-patient": {LocalID: "momus-setup-patient", ResourceType: "Patient", Resource: map[string]any{"resourceType": "Patient", "id": "momus-setup-patient"}},
	}}
	planPath := writeTestPlan(t, testPlan, dataset)

	coveragePlan := &coverage.CoveragePlan{Requirements: []coverage.CoverageRequirement{
		{ID: "req-search", ResourceType: "Patient", Domain: coverage.CoverageDomainSearch, Variant: coverage.CoverageVariantSearchValid},
	}}
	planBytes, err := json.MarshalIndent(coveragePlan, "", "  ")
	if err != nil {
		t.Fatalf("marshal coverage plan: %v", err)
	}
	planPath2 := filepath.Join(t.TempDir(), "coverage-plan.json")
	if err := os.WriteFile(planPath2, append(planBytes, '\n'), 0o644); err != nil {
		t.Fatalf("write coverage plan: %v", err)
	}

	outPath := filepath.Join(t.TempDir(), "results.json")
	cfg := &config{}
	cmd := newRunCmd(cfg)
	cfg.baseURL = server.URL
	cfg.coveragePlanPath = planPath2
	cfg.outputPath = outPath
	cmd.SetContext(context.Background())
	if err := cmd.RunE(cmd, []string{planPath}); err != nil {
		t.Fatalf("run command failed: %v", err)
	}

	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read results: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("results not valid JSON: %v", err)
	}
	summary := payload["summary"].(map[string]any)
	if summary["passedCases"] != float64(1) {
		t.Fatalf("passedCases = %v, want 1", summary["passedCases"])
	}
	if summary["totalRequirements"] != float64(1) || summary["coveredRequirements"] != float64(1) {
		t.Fatalf("coverage summary = %+v, want 1/1 covered", summary)
	}
	if summary["coveragePercent"] != float64(100) {
		t.Fatalf("coveragePercent = %v, want 100", summary["coveragePercent"])
	}
}

// TestRunCmdUsesDatasetForPreCreated verifies that the run command treats the
// seed resources carried by the test plan as already provisioned, so a test
// case referencing a seed resource executes without the runner rejecting the
// reference as unresolved.
func TestRunCmdUsesDatasetForPreCreated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/fhir+json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"resourceType":"Observation","id":"momus-req-obs"}`))
	}))
	defer server.Close()

	testPlan := &ast.Plan{Version: "v1", Root: &ast.Sequence{Steps: []ast.Node{
		&ast.Request{
			Method:  "PUT",
			URL:     server.URL + "/Observation/momus-req-obs",
			Headers: map[string]string{"X-Momus-Requirement-ID": "req-obs", "Content-Type": "application/fhir+json"},
			Body: map[string]any{
				"resourceType": "Observation",
				"id":           "momus-req-obs",
				"status":       "final",
				"subject":      map[string]any{"reference": "Patient/momus-setup-patient"},
			},
		},
		&ast.Assert{Description: "server accepts observation", RequirementID: "req-obs", Expression: "status in [200,201]"},
	}}}
	// The seed Patient is carried by the plan; provisioning uploads it
	// separately, so the runner must treat it as pre-created.
	dataset := &model.Dataset{Resources: map[string]*model.ResourceInstance{
		"momus-setup-patient": {LocalID: "momus-setup-patient", ResourceType: "Patient", Resource: map[string]any{"resourceType": "Patient", "id": "momus-setup-patient"}},
	}}
	planPath := writeTestPlan(t, testPlan, dataset)

	outPath := filepath.Join(t.TempDir(), "results.json")
	cfg := &config{}
	cmd := newRunCmd(cfg)
	cfg.baseURL = server.URL
	cfg.outputPath = outPath
	cmd.SetContext(context.Background())
	if err := cmd.RunE(cmd, []string{planPath}); err != nil {
		t.Fatalf("run command failed (seed reference should be treated as pre-provisioned): %v", err)
	}

	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read results: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("results not valid JSON: %v", err)
	}
	summary := payload["summary"].(map[string]any)
	if summary["passedCases"] != float64(1) {
		t.Fatalf("passedCases = %v, want 1 (seed reference should resolve)", summary["passedCases"])
	}
}

// TestRunCmdFailOnUncoveredRequiresCoveragePlan verifies that --fail-on-uncovered
// without --coverage-plan is rejected up front. Without this guard the coverage
// evaluation stays at its zero value (no uncovered obligations), so the gate
// would silently never fail and CI would get a green run with no enforcement.
func TestRunCmdFailOnUncoveredRequiresCoveragePlan(t *testing.T) {
	cfg := &config{}
	cmd := newRunCmd(cfg)
	cfg.failOnUncovered = true
	cmd.SetContext(context.Background())
	// The plan path is never read because the guard runs before plan loading;
	// any path exercises the error branch.
	err := cmd.RunE(cmd, []string{"does-not-exist.json"})
	if err == nil {
		t.Fatal("expected an error when --fail-on-uncovered is set without --coverage-plan, got nil")
	}
	if !strings.Contains(err.Error(), "--fail-on-uncovered requires --coverage-plan") {
		t.Fatalf("unexpected error: %v", err)
	}
}
