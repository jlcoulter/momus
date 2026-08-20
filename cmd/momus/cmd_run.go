package main

import (
	"encoding/json"
	"fmt"
	"os"

	testcoverage "github.com/jlcoulter/momus/internal/test/coverage"
	testgeneration "github.com/jlcoulter/momus/internal/test/generation"
	testrunner "github.com/jlcoulter/momus/internal/test/runner"
	"github.com/spf13/cobra"
)

// newRunCmd returns the "coverage run" command. Its single role is the
// execution stage (M) plus coverage evaluation (N) and reporting (O): it
// consumes a generated test plan (the seed dataset + test AST produced by
// "coverage ast") and executes the AST against a target server whose seed data
// has already been provisioned (via "coverage provision"). It does not take a
// package and does not provision or generate anything.
func newRunCmd(cfg *config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run <path-to-test-plan.json>",
		Short: "Execute a generated test plan and output test results",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			planPath := args[0]
			if cfg.failOnUncovered && cfg.coveragePlanPath == "" {
				return fmt.Errorf("--fail-on-uncovered requires --coverage-plan; provide the plan to evaluate contractual coverage against")
			}
			raw, err := os.ReadFile(planPath)
			if err != nil {
				return fmt.Errorf("read test plan %s: %w", planPath, err)
			}
			astPlan, setupDataset, err := decodeTestPlan(raw)
			if err != nil {
				return err
			}

			// Seed resources are provisioned ahead of execution (a separate stage).
			// The test plan carries the seed dataset; mark those resources as
			// already-created so the runner's setup-reference validation passes
			// without re-provisioning them during execution.
			preCreated := datasetResourceKeys(setupDataset)

			fmt.Printf("Testing phase: executing %d test cases\n", testgeneration.RequirementCount(astPlan))
			report, err := testrunner.Execute(cmd.Context(), astPlan.Root, testrunner.ExecuteOptions{
				BaseURL:            cfg.baseURL,
				WriteBaseURL:       cfg.writeBaseURL,
				BearerToken:        cfg.apiBearerToken,
				BasicUsername:      cfg.apiBasicUsername,
				BasicPassword:      cfg.apiBasicPassword,
				WriteBasicUsername: cfg.writeBasicUsername,
				WriteBasicPassword: cfg.writeBasicPassword,
				// Capture request/response detail whenever an HTML report is
				// requested, so failed cases can drill down into them.
				IncludeDebug: cfg.debug || cfg.htmlReport != "",
				Tracer:       newDebugTracer(cfg.debug),
				PreCreated:   preCreated,
			})
			if err != nil {
				return err
			}

			// Coverage evaluation needs the coverage plan that defined the
			// obligations. When --coverage-plan is supplied, evaluate against it;
			// otherwise report execution results only.
			var coverageEvaluation testcoverage.EvaluationReport
			if cfg.coveragePlanPath != "" {
				planRaw, err := os.ReadFile(cfg.coveragePlanPath)
				if err != nil {
					return fmt.Errorf("read coverage plan %s: %w", cfg.coveragePlanPath, err)
				}
				var coveragePlan testcoverage.CoveragePlan
				if err := json.Unmarshal(planRaw, &coveragePlan); err != nil {
					return fmt.Errorf("parse coverage plan %s: %w", cfg.coveragePlanPath, err)
				}
				executed := make([]testcoverage.ExecutedRequirementResult, 0, len(report.Cases))
				for _, c := range report.Cases {
					executed = append(executed, testcoverage.ExecutedRequirementResult{
						RequirementID: c.RequirementID,
						Passed:        c.Passed,
					})
				}
				coverageEvaluation = testcoverage.EvaluateCoverage(&coveragePlan, executed)
			}

			if cfg.htmlReport != "" {
				html, err := testcoverage.RenderHTML(coverageEvaluation, htmlItems(report.Cases))
				if err != nil {
					return fmt.Errorf("render html report: %w", err)
				}
				if err := writeOutputFile(cfg.htmlReport, html); err != nil {
					return fmt.Errorf("write html report to %s: %w", cfg.htmlReport, err)
				}
				fmt.Printf("HTML report written to %s\n", cfg.htmlReport)
			}

			out, err := marshalCoverageRunOutput(report, coverageEvaluation, cfg.includeCases)
			if err != nil {
				return fmt.Errorf("marshal test report: %w", err)
			}
			if err := writeDebugOutput(cfg.debug, "test-report.json", append(out, '\n')); err != nil {
				return err
			}

			if cfg.outputPath == "" {
				fmt.Println(string(out))
			} else {
				if err := writeOutputFile(cfg.outputPath, append(out, '\n')); err != nil {
					return fmt.Errorf("write test report to %s: %w", cfg.outputPath, err)
				}
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
					printCoverageGapSummary(coverageEvaluation)
					for idx, req := range coverageEvaluation.Uncovered {
						if idx >= 10 {
							break
						}
						fmt.Printf("  - %s\n", req.ID)
					}
				}
			} else if cfg.coveragePlanPath == "" {
				fmt.Printf("Coverage evaluation skipped: pass --coverage-plan to evaluate contractual coverage\n")
			}
			if cfg.outputPath != "" {
				fmt.Printf("Test report written to %s\n", cfg.outputPath)
			}
			if cfg.failOnUncovered && coverageEvaluation.UncoveredRequirements > 0 {
				return fmt.Errorf("coverage incomplete: %d uncovered obligations", coverageEvaluation.UncoveredRequirements)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&cfg.coveragePlanPath, "coverage-plan", "", "path to a coverage plan JSON (from 'coverage derive') used to evaluate contractual coverage")
	cmd.Flags().StringVar(&cfg.outputPath, "output", "", "write test result report JSON to a file")
	cmd.Flags().StringVar(&cfg.htmlReport, "html", "", "write an HTML coverage report with drill-down to a file")
	cmd.Flags().BoolVar(&cfg.failOnUncovered, "fail-on-uncovered", false, "return non-zero exit code when contractual coverage has uncovered obligations")
	cmd.Flags().StringVar(&cfg.baseURL, "base-url", "", "target FHIR base URL for relative request URLs (the AST usually carries absolute URLs)")
	cmd.Flags().StringVar(&cfg.writeBaseURL, "write-base-url", "", "alternate FHIR base URL for write (PUT/PATCH/POST/DELETE) request URLs; defaults to --base-url")
	cmd.Flags().StringVar(&cfg.apiBearerToken, "api-bearer-token", "", "bearer token used for API requests during execution")
	cmd.Flags().StringVar(&cfg.apiBasicUsername, "api-basic-username", "", "basic auth username used for API requests during execution")
	cmd.Flags().StringVar(&cfg.apiBasicPassword, "api-basic-password", "", "basic auth password used for API requests during execution")
	cmd.Flags().StringVar(&cfg.writeBasicUsername, "write-basic-username", "", "basic auth username used for write requests to --write-base-url; defaults to --api-basic-username")
	cmd.Flags().StringVar(&cfg.writeBasicPassword, "write-basic-password", "", "basic auth password used for write requests to --write-base-url; defaults to --api-basic-password")
	cmd.Flags().BoolVar(&cfg.includeCases, "include-cases", false, "include the full per-case result array in the JSON report (large runs produce very large output)")
	return cmd
}
