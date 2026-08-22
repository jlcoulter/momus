package main

import (
	"fmt"

	"github.com/jlcoulter/momus/internal/openapi"
	"github.com/spf13/cobra"
)

// newTestOpenapiCmd returns the "test openapi" command, which runs the full
// end-to-end conformance pipeline for an OpenAPI document: generate a test plan
// (one request+assert case per operation) and execute it against the target
// server. It reuses the same generic back-end (execute, evaluate, report) as
// "test fhir"; OpenAPI has no seed dataset to provision and no coverage plan
// yet, so those stages are skipped.
func newTestOpenapiCmd(cfg *config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "openapi <path-to-openapi.json>",
		Short: "Run the full end-to-end conformance test pipeline against an OpenAPI document",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cfg.baseURL == "" {
				return fmt.Errorf("base URL is required; provide --base-url")
			}
			doc, err := loadOpenAPIDocument(args[0])
			if err != nil {
				return err
			}
			astPlan, err := openapi.GeneratePlan(doc, cfg.baseURL, cfg.writeBaseURL)
			if err != nil {
				return err
			}
			fmt.Printf("Generated test plan with %d operation cases from %s\n", len(doc.Paths), args[0])

			// OpenAPI has no seed dataset to provision and no coverage plan yet,
			// so provisioning and coverage evaluation are skipped.
			report, coverageEvaluation, err := executePlan(cfg, cmd.Context(), astPlan, nil, nil)
			if err != nil {
				return err
			}
			return writeRunReport(cfg, report, coverageEvaluation, false)
		},
	}
	cmd.Flags().StringVar(&cfg.outputPath, "output", "", "write test result report JSON to a file")
	cmd.Flags().StringVar(&cfg.htmlReport, "html", "", "write an HTML coverage report with drill-down to a file")
	cmd.Flags().StringVar(&cfg.baseURL, "base-url", "", "target API base URL for the server under test")
	cmd.Flags().StringVar(&cfg.writeBaseURL, "write-base-url", "", "alternate API base URL for write (POST/PUT/PATCH) requests; defaults to --base-url")
	cmd.Flags().StringVar(&cfg.apiBearerToken, "api-bearer-token", "", "bearer token used for API requests during execution")
	cmd.Flags().StringVar(&cfg.apiBasicUsername, "api-basic-username", "", "basic auth username used for API requests during execution")
	cmd.Flags().StringVar(&cfg.apiBasicPassword, "api-basic-password", "", "basic auth password used for API requests during execution")
	cmd.Flags().StringVar(&cfg.writeBasicUsername, "write-basic-username", "", "basic auth username used for write requests to --write-base-url; defaults to --api-basic-username")
	cmd.Flags().StringVar(&cfg.writeBasicPassword, "write-basic-password", "", "basic auth password used for write requests to --write-base-url; defaults to --api-basic-password")
	cmd.Flags().BoolVar(&cfg.includeCases, "include-cases", false, "include the full per-case result array in the JSON report (large runs produce very large output)")
	return cmd
}
