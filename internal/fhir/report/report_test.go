package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jlcoulter/momus/internal/core/ast"
	"github.com/jlcoulter/momus/internal/core/coverage"
	"github.com/jlcoulter/momus/internal/core/runner"
)

func sampleReport() *runner.Report {
	return &runner.Report{
		Total:  2,
		Passed: 1,
		Failed: 1,
		Cases: []runner.CaseResult{
			{
				RequirementID: "search|Patient|name",
				Description:   "search-valid",
				Expression:    "status in [200]",
				Passed:        true,
				StatusCode:    200,
				Trace:         runnerTrace("Patient", "search", "search-valid", "search|Patient|name"),
			},
			{
				RequirementID: "search|Patient|gender|search-invalid",
				Description:   "server rejects",
				Expression:    "status in [400]",
				Passed:        false,
				StatusCode:    422,
				Error:         "assertion failed",
				Trace:         runnerTrace("Patient", "gender", "search-invalid", "search|Patient|gender"),
			},
		},
	}
}

func runnerTrace(rt, param, variant, cid string) *ast.Trace {
	return &ast.Trace{
		ConstraintID: cid,
		ResourceType: rt,
		Domain:       "search",
		Variant:      variant,
	}
}

func TestWriteDirCreatesTree(t *testing.T) {
	dir := t.TempDir()
	if err := WriteDir(dir, sampleReport(), coverage.EvaluationReport{}, false, Options{WriteFull: true}); err != nil {
		t.Fatalf("WriteDir: %v", err)
	}

	// index.json must exist and list the failed case pointer.
	var idx map[string]any
	readJSON(t, filepath.Join(dir, "index.json"), &idx)
	if idx["total"].(float64) != 2 {
		t.Fatalf("index total = %v, want 2", idx["total"])
	}
	failed, ok := idx["failed"].([]any)
	if !ok || len(failed) != 1 {
		t.Fatalf("index failed = %v, want 1 entry", idx["failed"])
	}

	// summary.json
	var sum map[string]any
	readJSON(t, filepath.Join(dir, "summary.json"), &sum)
	if sum["passed"].(float64) != 1 {
		t.Fatalf("summary passed = %v, want 1", sum["passed"])
	}

	// full.json present because WriteFull.
	if _, err := os.Stat(filepath.Join(dir, "full.json")); err != nil {
		t.Fatalf("full.json missing: %v", err)
	}

	// cases/ has two files.
	entries, err := os.ReadDir(filepath.Join(dir, "cases"))
	if err != nil {
		t.Fatalf("cases dir: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("cases dir has %d files, want 2", len(entries))
	}

	// by-resource/Patient.json and by-parameter files present.
	if _, err := os.Stat(filepath.Join(dir, "by-resource", "Patient.json")); err != nil {
		t.Fatalf("by-resource/Patient.json missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "by-parameter", "name.json")); err != nil {
		t.Fatalf("by-parameter/name.json missing: %v", err)
	}

	// failed-index.json
	if _, err := os.Stat(filepath.Join(dir, "failed-index.json")); err != nil {
		t.Fatalf("failed-index.json missing: %v", err)
	}
}

func TestWriteDirWithoutFull(t *testing.T) {
	dir := t.TempDir()
	if err := WriteDir(dir, sampleReport(), coverage.EvaluationReport{}, false, Options{}); err != nil {
		t.Fatalf("WriteDir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "full.json")); err == nil {
		t.Fatal("full.json should be absent when WriteFull is false")
	}
	if _, err := os.Stat(filepath.Join(dir, "index.html")); err != nil {
		t.Fatalf("index.html missing: %v", err)
	}
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

func TestCoverageString(t *testing.T) {
	if got := coverageString(coverage.EvaluationReport{}); got != "n/a" {
		t.Fatalf("coverageString(empty) = %q, want n/a", got)
	}
	got := coverageString(coverage.EvaluationReport{TotalRequirements: 10, CoveredRequirements: 7, CoveragePercent: 70.0})
	if got != "70.0% (7/10)" {
		t.Fatalf("coverageString = %q, want 70.0%% (7/10)", got)
	}
}

func TestParamOf(t *testing.T) {
	// Search constraint ID -> param.
	c := runner.CaseResult{Trace: &ast.Trace{ConstraintID: "search|Patient|name", ResourceType: "Patient"}}
	if got := paramOf(c); got != "name" {
		t.Fatalf("paramOf = %q, want name", got)
	}
	// Non-search -> resource type.
	c = runner.CaseResult{Trace: &ast.Trace{ConstraintID: "cardinality|x", ResourceType: "Patient"}}
	if got := paramOf(c); got != "Patient" {
		t.Fatalf("paramOf(non-search) = %q, want Patient", got)
	}
	// Nil trace -> empty.
	if got := paramOf(runner.CaseResult{}); got != "" {
		t.Fatalf("paramOf(no trace) = %q, want empty", got)
	}
}

func TestKindOf(t *testing.T) {
	// With trace variant.
	c := runner.CaseResult{Trace: &ast.Trace{Domain: "search", Variant: "search-valid"}}
	if got := kindOf(c); got != "search|search-valid" {
		t.Fatalf("kindOf = %q", got)
	}
	// With error.
	c = runner.CaseResult{Error: "boom"}
	if got := kindOf(c); got != "error" {
		t.Fatalf("kindOf(error) = %q", got)
	}
	// Plain failure.
	c = runner.CaseResult{}
	if got := kindOf(c); got != "failure" {
		t.Fatalf("kindOf(failure) = %q", got)
	}
}

func TestSummaryWithEvaluation(t *testing.T) {
	s := summary(sampleReport(), coverage.EvaluationReport{TotalRequirements: 2, CoveredRequirements: 1, UncoveredRequirements: 1, CoveragePercent: 50.0})
	if s["total"].(int) != 2 || s["passed"].(int) != 1 || s["failed"].(int) != 1 {
		t.Fatalf("summary = %v", s)
	}
	if s["coveragePercent"].(float64) != 50.0 || s["totalRequirements"].(int) != 2 {
		t.Fatalf("summary coverage = %v", s)
	}
	// No evaluation.
	s2 := summary(sampleReport(), coverage.EvaluationReport{})
	if _, ok := s2["coveragePercent"]; ok {
		t.Fatal("summary should omit coverage when unevaluated")
	}
}

func TestCaseFileName(t *testing.T) {
	if got := caseFileName(runner.CaseResult{RequirementID: "search|Patient|name"}); got != "search_Patient_name.json" {
		t.Fatalf("caseFileName = %q", got)
	}
	if got := caseFileName(runner.CaseResult{}); got != "case.json" {
		t.Fatalf("caseFileName(empty) = %q", got)
	}
}

func TestSafeName(t *testing.T) {
	if got := safeName("a|b"); got != "a_b" {
		t.Fatalf("safeName = %q", got)
	}
}

func TestMatrix(t *testing.T) {
	m := matrix(sampleReport().Cases)
	if m["total"].(int) != 2 || m["passed"].(int) != 1 || m["failed"].(int) != 1 {
		t.Fatalf("matrix = %v", m)
	}
}

func TestWriteDirInvalidPathError(t *testing.T) {
	// A path that cannot be created surfaces an error.
	if err := WriteDir("/proc/definitely-not-writable/x", sampleReport(), coverage.EvaluationReport{}, false, Options{}); err == nil {
		t.Fatal("expected error for unwritable output directory")
	}
}

func TestReportSubWriters(t *testing.T) {
	dir := t.TempDir()
	cases := sampleReport().Cases

	// writeCases creates case files.
	if err := writeCases(dir, cases); err != nil {
		t.Fatalf("writeCases: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "cases", "search_Patient_name.json")); err != nil {
		t.Fatalf("case file missing: %v", err)
	}
	// writeFailedIndex.
	if err := writeFailedIndex(dir, cases); err != nil {
		t.Fatalf("writeFailedIndex: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "failed-index.json")); err != nil {
		t.Fatalf("failed-index missing: %v", err)
	}
	// writeMatrices.
	if err := writeMatrices(dir, cases); err != nil {
		t.Fatalf("writeMatrices: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "by-resource", "Patient.json")); err != nil {
		t.Fatalf("by-resource missing: %v", err)
	}
	// writeJSON on an unwritable path.
	if err := writeJSON("/proc/definitely-not-writable/x.json", "x"); err == nil {
		t.Fatal("expected writeJSON error for unwritable path")
	}
}
