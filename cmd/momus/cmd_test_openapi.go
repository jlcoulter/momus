package main

import (
	"fmt"

	"github.com/jlcoulter/momus/internal/mock"
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
			// When --mock is set, start a plan-aware mock server and target it.
			// The mock's base URL is used unless the caller supplied --base-url.
			var mockServer *mock.Server
			if cfg.Mock {
				s, baseURL, err := startMock(cfg, "")
				if err != nil {
					return err
				}
				mockServer = s
				defer s.Close()
				if cfg.BaseURL == "" {
					cfg.BaseURL = baseURL
				}
			}
			if cfg.BaseURL == "" {
				return fmt.Errorf("base URL is required; provide --base-url or --mock")
			}
			doc, err := loadOpenAPIDocument(args[0])
			if err != nil {
				return err
			}
			astPlan, err := openapi.GeneratePlan(doc, cfg.BaseURL, cfg.WriteBaseURL)
			if err != nil {
				return err
			}
			fmt.Printf("Generated test plan with %d operation cases from %s\n", len(doc.Paths), args[0])

			// Feed the generated plan's reject routes into the mock so it rejects
			// the requests the plan expects to be rejected.
			if mockServer != nil {
				setMockPlan(mockServer, astPlan)
			}

			// OpenAPI has no seed dataset to provision and no coverage plan yet,
			// so provisioning and coverage evaluation are skipped.
			report, coverageEvaluation, err := executePlan(cfg, cmd.Context(), astPlan, nil, nil)
			if err != nil {
				return err
			}
			return writeRunReport(cfg, report, coverageEvaluation, false)
		},
	}
	cmd.Flags().StringVar(&cfg.OutputPath, "output", "", "write test result report JSON to a file")
	cmd.Flags().StringVar(&cfg.HtmlReport, "html", "", "write an HTML coverage report with drill-down to a file")
	cmd.Flags().StringVar(&cfg.BaseURL, "base-url", "", "target API base URL for the server under test (defaults to the mock server when --mock is set)")
	cmd.Flags().StringVar(&cfg.WriteBaseURL, "write-base-url", "", "alternate API base URL for write (POST/PUT/PATCH) requests; defaults to --base-url")
	cmd.Flags().BoolVar(&cfg.Mock, "mock", false, "start an in-process plan-aware mock server and test against it")
	cmd.Flags().IntVar(&cfg.MockPort, "mock-port", 0, "port for the mock server (default: ephemeral)")
	cmd.Flags().StringVar(&cfg.ApiBearerToken, "api-bearer-token", "", "bearer token used for API requests during execution")
	cmd.Flags().StringVar(&cfg.ApiBasicUsername, "api-basic-username", "", "basic auth username used for API requests during execution")
	cmd.Flags().StringVar(&cfg.ApiBasicPassword, "api-basic-password", "", "basic auth password used for API requests during execution")
	cmd.Flags().StringVar(&cfg.WriteBasicUsername, "write-basic-username", "", "basic auth username used for write requests to --write-base-url; defaults to --api-basic-username")
	cmd.Flags().StringVar(&cfg.WriteBasicPassword, "write-basic-password", "", "basic auth password used for write requests to --write-base-url; defaults to --api-basic-password")
	cmd.Flags().BoolVar(&cfg.IncludeCases, "include-cases", false, "include the full per-case result array in the JSON report (large runs produce very large output)")
	return cmd
}
