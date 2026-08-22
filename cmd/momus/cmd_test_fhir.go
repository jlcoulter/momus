package main

import (
	"fmt"

	fhirpackage "github.com/jlcoulter/momus/internal/fhir/package"
	"github.com/jlcoulter/momus/internal/mock"
	testgeneration "github.com/jlcoulter/momus/internal/test/generation"
	"github.com/spf13/cobra"
)

// newTestFhirCmd returns the "test fhir" command, which runs the full
// end-to-end FHIR conformance pipeline from a single package archive: resolve
// the dependency graph, derive coverage obligations, generate the test plan
// (seed dataset + test AST), provision the seed data to the target server,
// execute the tests, and report contractual coverage. It is the one-shot path
// over the same stage functions the granular commands use, so the two cannot
// drift.
func newTestFhirCmd(cfg *config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fhir <path-to-package.tgz>",
		Short: "Run the full end-to-end FHIR conformance test pipeline against a package",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rootPath := args[0]

			// When --mock is set, start a plan-aware mock server and target it.
			// The mock's base URL is used unless the caller supplied --base-url.
			var mockServer *mock.Server
			if cfg.mock {
				s, baseURL, err := startMock(cfg, "/fhir")
				if err != nil {
					return err
				}
				mockServer = s
				defer s.Close()
				if cfg.baseURL == "" {
					cfg.baseURL = baseURL
				}
			}
			if cfg.baseURL == "" {
				return fmt.Errorf("base URL is required; provide --base-url or --mock")
			}

			// Stage 1: resolve the package graph and build the scoped registry.
			graph, reg, err := resolvePackageGraph(cfg, rootPath)
			if err != nil {
				return err
			}
			// Overlay the package's own CapabilityStatement as the reduced scope
			// over the full registry (mirrors "coverage ast").
			reg.OverlayCapabilityScope()

			// Stage 2: derive coverage obligations, scoped to the server's
			// CapabilityStatement when reachable.
			coverageResourceTypes, coverageProfileURLs, preferredProfilesByResource, coverageSearchCodes, err := resourceScopeForRun(cmd, cfg, newDebugTracer(cfg.debug))
			if err != nil {
				return err
			}
			coveragePlan, err := deriveCoveragePlan(cfg, reg, coverageResourceTypes, coverageProfileURLs, coverageSearchCodes)
			if err != nil {
				return err
			}
			fmt.Printf("Derived %d coverage requirements from %d resolved packages\n", len(coveragePlan.Requirements), len(graph.Packages))

			// Stage 3: generate the test plan (seed dataset + test AST).
			astPlan, setupDataset, err := buildTestPlan(cfg, reg, coveragePlan, preferredProfilesByResource, coverageResourceTypes, coverageProfileURLs)
			if err != nil {
				return err
			}
			fmt.Printf("Generated test plan with %d requirement cases and %d seed resources\n", testgeneration.RequirementCount(astPlan), len(setupDataset.Resources))

			// Feed the generated plan's reject routes into the mock so it rejects
			// the requests the plan expects to be rejected.
			if mockServer != nil {
				setMockPlan(mockServer, astPlan)
			}

			// Stage 4: provision the seed dataset to the target server.
			if err := provisionDataset(cfg, cmd.Context(), setupDataset); err != nil {
				return err
			}

			// Stage 5: execute the tests and evaluate contractual coverage.
			report, coverageEvaluation, err := executePlan(cfg, cmd.Context(), astPlan, datasetResourceKeys(setupDataset), coveragePlan)
			if err != nil {
				return err
			}

			// Stage 6: write and print the report, honoring --fail-on-uncovered.
			return writeRunReport(cfg, report, coverageEvaluation, true)
		},
	}
	cmd.Flags().StringVar(&cfg.depsDir, "deps-dir", "", "directory to search for dependency package archives (.tgz/.tar.gz)")
	cmd.Flags().StringVar(&cfg.downloadDir, "download-dir", "", "directory to store downloaded dependency package archives")
	cmd.Flags().StringVar(&cfg.conflictPolicy, "conflict-policy", string(fhirpackage.ConflictPolicyRootWins), "dependency conflict policy: root-wins or strict")
	cmd.Flags().StringVar(&cfg.outputPath, "output", "", "write test result report JSON to a file")
	cmd.Flags().StringVar(&cfg.htmlReport, "html", "", "write an HTML coverage report with drill-down to a file")
	cmd.Flags().BoolVar(&cfg.failOnUncovered, "fail-on-uncovered", false, "return non-zero exit code when contractual coverage has uncovered obligations")
	cmd.Flags().StringVar(&cfg.baseURL, "base-url", "", "target FHIR base URL for the server under test (defaults to the mock server when --mock is set)")
	cmd.Flags().StringVar(&cfg.writeBaseURL, "write-base-url", "", "alternate FHIR base URL for write (PUT/PATCH/POST/DELETE) requests; defaults to --base-url")
	cmd.Flags().BoolVar(&cfg.mock, "mock", false, "start an in-process plan-aware mock FHIR server and test against it")
	cmd.Flags().IntVar(&cfg.mockPort, "mock-port", 0, "port for the mock server (default: ephemeral)")
	cmd.Flags().StringVar(&cfg.capabilityBaseURL, "capability-base-url", "", "optional alternate FHIR base URL to fetch CapabilityStatement metadata for scope/profile selection")
	cmd.Flags().StringVar(&cfg.metadataFile, "metadata", "", "path to a local CapabilityStatement JSON file to use for scope/profile selection instead of fetching /metadata")
	cmd.Flags().StringVar(&cfg.apiBearerToken, "api-bearer-token", "", "bearer token used for API requests during provisioning and execution")
	cmd.Flags().StringVar(&cfg.apiBasicUsername, "api-basic-username", "", "basic auth username used for API requests during provisioning and execution")
	cmd.Flags().StringVar(&cfg.apiBasicPassword, "api-basic-password", "", "basic auth password used for API requests during provisioning and execution")
	cmd.Flags().StringVar(&cfg.writeBasicUsername, "write-basic-username", "", "basic auth username used for write requests to --write-base-url; defaults to --api-basic-username")
	cmd.Flags().StringVar(&cfg.writeBasicPassword, "write-basic-password", "", "basic auth password used for write requests to --write-base-url; defaults to --api-basic-password")
	cmd.Flags().StringSliceVar(&cfg.includeResourceTypes, "include-resource", nil, "include only these resource types (repeatable)")
	cmd.Flags().StringSliceVar(&cfg.includeProfileURLs, "include-profile-url", nil, "include only these profile canonical URLs (repeatable)")
	cmd.Flags().StringSliceVar(&cfg.excludePathPrefixes, "exclude-path-prefix", nil, "exclude element paths by prefix (repeatable)")
	cmd.Flags().BoolVar(&cfg.mustSupportOnly, "must-support-only", false, "derive only elements marked mustSupport")
	cmd.Flags().BoolVar(&cfg.includeOptional, "include-optional", false, "include optional non-mustSupport elements")
	cmd.Flags().BoolVar(&cfg.includeLowValuePaths, "include-low-value-paths", false, "include low-value infrastructure paths like meta/text/language")
	cmd.Flags().BoolVar(&cfg.includeUniversalSearch, "include-universal-search", false, "include universal FHIR search parameters for every resource type even when the server's CapabilityStatement does not declare them")
	cmd.Flags().IntVar(&cfg.interactionStrength, "strength", 1, "interaction strength: 1 = individual requirements, 2 = pairwise interactions")
	cmd.Flags().BoolVar(&cfg.exhaustive, "exhaustive", true, "populate optional elements to produce fuller, more realistic payloads")
	cmd.Flags().BoolVar(&cfg.includeCases, "include-cases", false, "include the full per-case result array in the JSON report (large runs produce very large output)")
	return cmd
}
