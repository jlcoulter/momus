package main

import (
	"fmt"
	"os"

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
			if cfg.FailOnUncovered && cfg.CoveragePlanPath == "" {
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

			coveragePlan, err := loadCoveragePlanFromFile(cfg.CoveragePlanPath)
			if err != nil {
				return err
			}

			report, coverageEvaluation, err := executePlan(cfg, cmd.Context(), astPlan, datasetResourceKeys(setupDataset), coveragePlan)
			if err != nil {
				return err
			}
			return writeRunReport(cfg, report, coverageEvaluation, coveragePlan != nil)
		},
	}
	cmd.Flags().StringVar(&cfg.CoveragePlanPath, "coverage-plan", "", "path to a coverage plan JSON (from 'coverage derive') used to evaluate contractual coverage")
	cmd.Flags().StringVar(&cfg.OutputPath, "output", "", "write test result report JSON to a file")
	cmd.Flags().StringVar(&cfg.OutputDir, "output-dir", ".momus/output", "write the navigable output directory to this path (use '-' to disable)")
	cmd.Flags().StringVar(&cfg.HtmlReport, "html", "", "write an HTML coverage report with drill-down to a file")
	cmd.Flags().BoolVar(&cfg.FailOnUncovered, "fail-on-uncovered", false, "return non-zero exit code when contractual coverage has uncovered obligations")
	cmd.Flags().StringVar(&cfg.BaseURL, "base-url", "", "target FHIR base URL for relative request URLs (the AST usually carries absolute URLs)")
	cmd.Flags().StringVar(&cfg.WriteBaseURL, "write-base-url", "", "alternate FHIR base URL for write (PUT/PATCH/POST/DELETE) request URLs; defaults to --base-url")
	cmd.Flags().StringVar(&cfg.ApiBearerToken, "api-bearer-token", "", "bearer token used for API requests during execution")
	cmd.Flags().StringVar(&cfg.ApiBasicUsername, "api-basic-username", "", "basic auth username used for API requests during execution")
	cmd.Flags().StringVar(&cfg.ApiBasicPassword, "api-basic-password", "", "basic auth password used for API requests during execution")
	cmd.Flags().StringVar(&cfg.WriteBasicUsername, "write-basic-username", "", "basic auth username used for write requests to --write-base-url; defaults to --api-basic-username")
	cmd.Flags().StringVar(&cfg.WriteBasicPassword, "write-basic-password", "", "basic auth password used for write requests to --write-base-url; defaults to --api-basic-password")
	cmd.Flags().BoolVar(&cfg.IncludeCases, "include-cases", false, "include the full per-case result array in the JSON report (large runs produce very large output)")
	return cmd
}
