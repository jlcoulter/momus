package main

import (
	"context"
	"fmt"

	reportwriter "github.com/jlcoulter/momus/internal/fhir/report"
	"github.com/jlcoulter/momus/internal/home"

	testast "github.com/jlcoulter/momus/internal/core/ast"
	testcoverage "github.com/jlcoulter/momus/internal/core/coverage"
	coregeneration "github.com/jlcoulter/momus/internal/core/generation"
	testrunner "github.com/jlcoulter/momus/internal/core/runner"
)

// This file holds the generic stage functions of the test pipeline that are
// shared across every server type. They operate only on the test AST and the
// execution report — no server-type-specific types — so "test fhir", "test
// openapi", and any future server type all reuse them unchanged. The
// server-type-specific front-ends (package resolution, coverage derivation,
// provisioning) live in pipeline_fhir.go and the per-command files.

// executePlan executes the test AST against the target server and evaluates
// contractual coverage against coveragePlan when non-nil. preCreated lists
// resource keys ("Type/id") that already exist on the server before execution
// (e.g. FHIR seed resources provisioned ahead of the run); it is nil for server
// types without a provisioning stage.
func executePlan(cfg *config, ctx context.Context, astPlan *testast.Plan, preCreated map[string]struct{}, coveragePlan *testcoverage.CoveragePlan) (*testrunner.Report, testcoverage.EvaluationReport, error) {
	// Resolve the write base URL up front so write-specific basic auth
	// credentials are applied even when the user relies on the documented
	// "defaults to --base-url" behavior.
	writeBase := cfg.WriteBaseURL
	if writeBase == "" {
		writeBase = cfg.BaseURL
	}

	fmt.Printf("Testing phase: executing %d test cases\n", coregeneration.RequirementCount(astPlan))

	// Render a live progress bar to stderr during execution (only when stderr
	// is a terminal). It is cleared before the report is printed.
	bar := newProgressBar(40)
	report, err := testrunner.Execute(ctx, astPlan.Root, testrunner.ExecuteOptions{
		BaseURL:            cfg.BaseURL,
		WriteBaseURL:       writeBase,
		BearerToken:        cfg.ApiBearerToken,
		BasicUsername:      cfg.ApiBasicUsername,
		BasicPassword:      cfg.ApiBasicPassword,
		WriteBasicUsername: cfg.WriteBasicUsername,
		WriteBasicPassword: cfg.WriteBasicPassword,
		IncludeDebug:       cfg.Debug || cfg.HtmlReport != "",
		Tracer:             newDebugTracer(cfg.Debug),
		PreCreated:         preCreated,
		Progress:           bar.render,
	})
	bar.finish()
	if err != nil {
		return nil, testcoverage.EvaluationReport{}, err
	}

	var coverageEvaluation testcoverage.EvaluationReport
	if coveragePlan != nil {
		executed := make([]testcoverage.ExecutedRequirementResult, 0, len(report.Cases))
		for _, c := range report.Cases {
			executed = append(executed, testcoverage.ExecutedRequirementResult{
				RequirementID: c.RequirementID,
				Passed:        c.Passed,
			})
		}
		coverageEvaluation = testcoverage.EvaluateCoverage(coveragePlan, executed)
	}
	return report, coverageEvaluation, nil
}

// writeRunReport renders, writes, and prints the execution report, honoring
// --html, --output, --include-cases, and --fail-on-uncovered. coverageEvaluated
// reports whether contractual coverage was evaluated (so the "skipped" notice is
// only shown when it was genuinely skipped).
func writeRunReport(cfg *config, report *testrunner.Report, coverageEvaluation testcoverage.EvaluationReport, coverageEvaluated bool) error {
	if cfg.HtmlReport != "" {
		html, err := testcoverage.RenderHTML(coverageEvaluation, htmlItems(report.Cases))
		if err != nil {
			return fmt.Errorf("render html report: %w", err)
		}
		if err := writeOutputFile(cfg.HtmlReport, html); err != nil {
			return fmt.Errorf("write html report to %s: %w", cfg.HtmlReport, err)
		}
		fmt.Printf("HTML report written to %s\n", cfg.HtmlReport)
	}

	out, err := marshalCoverageRunOutput(report, coverageEvaluation, cfg.IncludeCases)
	if err != nil {
		return fmt.Errorf("marshal test report: %w", err)
	}
	if err := writeDebugOutput(cfg.Debug, "test-report.json", append(out, '\n')); err != nil {
		return err
	}

	// The full JSON report is only written to a file via --output; it is never
	// dumped to stdout (the concise summary below is the user-facing output).
	if cfg.OutputPath != "" {
		if err := writeOutputFile(cfg.OutputPath, append(out, '\n')); err != nil {
			return fmt.Errorf("write test report to %s: %w", cfg.OutputPath, err)
		}
	}

	// The navigable output directory (default $MOMUS_HOME/output) is always
	// written unless the user opted out with "-". It slices the run into small,
	// navigable files (index, per-case, by-resource, by-parameter) instead of
	// one monolith. Existing output is rotated to a timestamped backup.
	outputDir := cfg.OutputDir
	if outputDir == "" {
		outputDir = home.OutputDir()
	}
	if outputDir != "-" {
		if err := writeVersionedDir(outputDir, func() error {
			return reportwriter.WriteDir(outputDir, report, coverageEvaluation, coverageEvaluated, reportwriter.Options{WriteFull: cfg.IncludeCases})
		}); err != nil {
			return fmt.Errorf("write output directory %s: %w", outputDir, err)
		}
		fmt.Printf("Output directory written to %s\n", outputDir)
	}

	requirementCases, setupCases := countRequirementAndSetupCases(report.Cases)
	fmt.Printf("Executed %d cases: %d passed, %d failed (%d requirement cases + %d setup cases)\n",
		report.Total, report.Passed, report.Failed, requirementCases, setupCases)
	if coverageEvaluation.TotalRequirements > 0 {
		fmt.Printf("Contractual coverage: %.1f%% (%d/%d)\n", coverageEvaluation.CoveragePercent, coverageEvaluation.CoveredRequirements, coverageEvaluation.TotalRequirements)
		if requirementCases != coverageEvaluation.TotalRequirements {
			fmt.Printf("WARNING: generated %d requirement cases but the plan defines %d obligations; check for duplicate or missing requirements\n", requirementCases, coverageEvaluation.TotalRequirements)
		}
		if coverageEvaluation.UncoveredRequirements > 0 {
			fmt.Printf("Uncovered contractual obligations: %d\n", coverageEvaluation.UncoveredRequirements)
			// The detailed gap breakdown (by domain/resource/variant and the
			// uncovered requirement IDs) is verbose; it is only printed with
			// --debug. The full detail is always in the JSON report.
			if cfg.Debug {
				printCoverageGapSummary(coverageEvaluation)
				for idx, req := range coverageEvaluation.Uncovered {
					if idx >= 10 {
						break
					}
					label := req.HumanID
					if label == "" {
						label = req.ID
					}
					fmt.Printf("  - %s\n", label)
				}
			}
		}
	} else if !coverageEvaluated {
		fmt.Printf("Coverage evaluation skipped: pass --coverage-plan to evaluate contractual coverage\n")
	}
	if cfg.OutputPath != "" {
		fmt.Printf("Test report written to %s\n", cfg.OutputPath)
	}
	if cfg.FailOnUncovered && coverageEvaluation.UncoveredRequirements > 0 {
		return fmt.Errorf("coverage incomplete: %d uncovered obligations", coverageEvaluation.UncoveredRequirements)
	}
	return nil
}
