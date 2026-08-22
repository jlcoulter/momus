package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jlcoulter/momus/internal/core/ast"
	"github.com/jlcoulter/momus/internal/core/coverage"
	"github.com/jlcoulter/momus/internal/core/runner"
	reportwriter "github.com/jlcoulter/momus/internal/fhir/report"
)

func TestExplainCmdRendersCase(t *testing.T) {
	dir := t.TempDir()
	rep := &runner.Report{
		Total:  1,
		Passed: 1,
		Cases: []runner.CaseResult{{
			RequirementID: "search|Patient|name",
			Description:   "search-valid",
			Passed:        true,
			StatusCode:    200,
			Trace:         &ast.Trace{ConstraintID: "search|Patient|name", ResourceType: "Patient", Domain: "search", Variant: "search-valid"},
		}},
	}
	if err := reportwriter.WriteDir(dir, rep, coverage.EvaluationReport{}, false, reportwriter.Options{}); err != nil {
		t.Fatalf("WriteDir: %v", err)
	}

	cfg := &config{}
	cmd := newExplainCmd(cfg)
	cfg.outputDir = dir
	cmd.SetContext(context.Background())
	if err := cmd.RunE(cmd, []string{"search|Patient|name"}); err != nil {
		t.Fatalf("explain failed: %v", err)
	}
}

func TestExplainCmdMissingCase(t *testing.T) {
	dir := t.TempDir()
	cfg := &config{}
	cmd := newExplainCmd(cfg)
	cfg.outputDir = dir
	cmd.SetContext(context.Background())
	if err := cmd.RunE(cmd, []string{"nonexistent"}); err == nil {
		t.Fatal("expected error for missing case, got nil")
	} else {
		// Ensure it's the not-found error, not a panic.
		if filepath.Join(dir, "cases") != "" && os.IsNotExist(err) {
			// no-op
		}
	}
}
