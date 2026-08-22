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
