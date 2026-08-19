package main

import (
	"fmt"
	"path/filepath"

	fhirpackage "github.com/jlcoulter/momus/internal/fhir/package"
	provisioning "github.com/jlcoulter/momus/internal/fhir/provisioning"
	testcoverage "github.com/jlcoulter/momus/internal/test/coverage"
	testgeneration "github.com/jlcoulter/momus/internal/test/generation"
	testrunner "github.com/jlcoulter/momus/internal/test/runner"
	"github.com/spf13/cobra"
)

// newRunCmd returns the "coverage run" command.
func newRunCmd(cfg *config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run <path-to-package.tgz>",
		Short: "Execute generated AST and output test results",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cfg.baseURL == "" {
				return fmt.Errorf("base URL is required; provide --base-url")
			}
			// Resolve the write base URL up front: the provisioner below needs a
			// concrete URL (it does not default internally), while the library
			// calls (planner, generation, runner) also default empty to baseURL as
			// a safety net.
			writeBase := cfg.writeBaseURL
			if writeBase == "" {
				writeBase = cfg.baseURL
			}
			// Resolve write-specific basic auth credentials, falling back to the
			// general API credentials when the write-specific ones are not set.
			writeBasicUser := cfg.writeBasicUsername
			if writeBasicUser == "" {
				writeBasicUser = cfg.apiBasicUsername
			}
			writeBasicPass := cfg.writeBasicPassword
			if writeBasicPass == "" {
				writeBasicPass = cfg.apiBasicPassword
			}

			rootPath := args[0]
			searchDir := cfg.depsDir
			if searchDir == "" {
				searchDir = filepath.Dir(rootPath)
			}
			cacheDir := cfg.downloadDir
			if cacheDir == "" {
				cacheDir = filepath.Join(searchDir, ".momus", "packages")
			}

			graph, err := fhirpackage.ResolveLocalPackageGraphWithOptions(rootPath, fhirpackage.ResolveOptions{
				DepsDir:        searchDir,
				DownloadDir:    cacheDir,
				ConflictPolicy: fhirpackage.ConflictPolicy(cfg.conflictPolicy),
			})
			if err != nil {
				return err
			}
			if err := writeDebugGraph(cfg.debug, graph); err != nil {
				return err
			}

			builder := fhirpackage.NewRegistryBuilder()
			reg, err := builder.BuildFromPackagesScoped(graph.Packages, graph.Root)
			if err != nil {
				return err
			}

			capabilityResourceTypes, capabilityProfileURLs, preferredProfilesByResource, err := resourceScopeForRun(cmd, cfg, newDebugTracer(cfg.debug))
			if err != nil {
				return err
			}

			coveragePlan, err := testcoverage.DerivePlan(reg, testcoverage.DeriveOptions{
				IncludeResourceTypes: capabilityResourceTypes,
				IncludeProfileURLs:   capabilityProfileURLs,
				ExcludePathPrefixes:  cfg.excludePathPrefixes,
				MustSupportOnly:      cfg.mustSupportOnly,
				IncludeOptional:      cfg.includeOptional,
				IncludeLowValuePaths: cfg.includeLowValuePaths,
				Strength:             cfg.interactionStrength,
			})
			if err != nil {
				return err
			}

			// Build the seed dataset using the same generation logic as the AST so
			// the provisioned data conforms to the same profiles and is exactly what
			// the generated tests reference, then provision it ahead of execution so
			// tests run against real provisioned state.
			setupDataset, err := testgeneration.BuildSetupDataset(coveragePlan, testgeneration.BuildOptions{
				BaseURL:                        cfg.baseURL,
				WriteBaseURL:                   writeBase,
				Registry:                       reg,
				PreferredProfileURLsByResource: preferredProfilesByResource,
				Strength:                       cfg.interactionStrength,
				Exhaustive:                     cfg.exhaustive,
			})
			if err != nil {
				return err
			}
			provisioner := provisioning.New(writeBase, &provisioning.Options{
				BearerToken:   cfg.apiBearerToken,
				BasicUsername: writeBasicUser,
				BasicPassword: writeBasicPass,
				Tracer:        newDebugTracer(cfg.debug),
			})
			// Data seeding is essential to achieve full coverage success, so the
			// user must be told when the dataset was not fully uploaded.
			seed := provisioner.ProvisionAll(cmd.Context(), setupDataset)
			if !seed.Complete() {
				fmt.Printf("WARNING: dataset seeding incomplete — %d of %d resources uploaded. Data seeding is essential to achieve full coverage success. Fix the failing resources and re-run.\n", seed.Provisioned, seed.Provisioned+seed.Failed)
				for _, failure := range seed.Failures {
					fmt.Printf("  - %s\n", failure.Describe())
				}
				if !cfg.debug {
					fmt.Printf("Run with --debug to write the rejected payloads and full server responses to %s for inspection.\n", debugOutputDir)
				}
				if err := writeDebugProvisionFailures(cfg.debug, seed.Failures); err != nil {
					return err
				}
			} else {
				fmt.Printf("Dataset seeded: %d resources uploaded ahead of execution\n", seed.Provisioned)
			}

			astPlan, err := testgeneration.GenerateFromCoveragePlan(coveragePlan, testgeneration.BuildOptions{BaseURL: cfg.baseURL, WriteBaseURL: writeBase, Registry: reg, PreferredProfileURLsByResource: preferredProfilesByResource, Strength: cfg.interactionStrength, Exhaustive: cfg.exhaustive})
			if err != nil {
				return err
			}

			report, err := testrunner.Execute(cmd.Context(), astPlan.Root, testrunner.ExecuteOptions{
				BaseURL:            cfg.baseURL,
				WriteBaseURL:       writeBase,
				BearerToken:        cfg.apiBearerToken,
				BasicUsername:      cfg.apiBasicUsername,
				BasicPassword:      cfg.apiBasicPassword,
				WriteBasicUsername: cfg.writeBasicUsername,
				WriteBasicPassword: cfg.writeBasicPassword,
				// Capture request/response detail whenever an HTML report is
				// requested, so failed cases can drill down into them.
				IncludeDebug: cfg.debug || cfg.htmlReport != "",
				Tracer:       newDebugTracer(cfg.debug),
			})
			if err != nil {
				return err
			}

			executed := make([]testcoverage.ExecutedRequirementResult, 0, len(report.Cases))
			for _, c := range report.Cases {
				executed = append(executed, testcoverage.ExecutedRequirementResult{
					RequirementID: c.RequirementID,
					Passed:        c.Passed,
				})
			}
			coverageEvaluation := testcoverage.EvaluateCoverage(coveragePlan, executed)

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
			if cfg.outputPath != "" {
				fmt.Printf("Test report written to %s\n", cfg.outputPath)
			}
			if cfg.failOnUncovered && coverageEvaluation.UncoveredRequirements > 0 {
				return fmt.Errorf("coverage incomplete: %d uncovered obligations", coverageEvaluation.UncoveredRequirements)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&cfg.depsDir, "deps-dir", "", "directory to search for dependency package archives (.tgz/.tar.gz)")
	cmd.Flags().StringVar(&cfg.downloadDir, "download-dir", "", "directory to store downloaded dependency package archives")
	cmd.Flags().StringVar(&cfg.conflictPolicy, "conflict-policy", string(fhirpackage.ConflictPolicyRootWins), "dependency conflict policy: root-wins or strict")
	cmd.Flags().StringVar(&cfg.outputPath, "output", "", "write test result report JSON to a file")
	cmd.Flags().StringVar(&cfg.htmlReport, "html", "", "write an HTML coverage report with drill-down to a file")
	cmd.Flags().StringSliceVar(&cfg.includeResourceTypes, "include-resource", nil, "include only these resource types (repeatable)")
	cmd.Flags().StringSliceVar(&cfg.includeProfileURLs, "include-profile-url", nil, "include only these profile canonical URLs (repeatable)")
	cmd.Flags().StringSliceVar(&cfg.excludePathPrefixes, "exclude-path-prefix", nil, "exclude element paths by prefix (repeatable)")
	cmd.Flags().BoolVar(&cfg.mustSupportOnly, "must-support-only", false, "derive only elements marked mustSupport")
	cmd.Flags().BoolVar(&cfg.includeOptional, "include-optional", false, "include optional non-mustSupport elements")
	cmd.Flags().BoolVar(&cfg.includeLowValuePaths, "include-low-value-paths", false, "include low-value infrastructure paths like meta/text/language")
	cmd.Flags().BoolVar(&cfg.scopeToCapability, "scope-to-capability", true, "limit derivation to CapabilityStatement server resources that support create")
	cmd.Flags().BoolVar(&cfg.failOnUncovered, "fail-on-uncovered", false, "return non-zero exit code when contractual coverage has uncovered obligations")
	cmd.Flags().StringVar(&cfg.baseURL, "base-url", "", "target FHIR base URL for request execution")
	cmd.Flags().StringVar(&cfg.writeBaseURL, "write-base-url", "", "alternate FHIR base URL for resource creation (write) requests; defaults to --base-url")
	cmd.Flags().StringVar(&cfg.capabilityBaseURL, "capability-base-url", "", "optional alternate FHIR base URL to fetch CapabilityStatement metadata for scope/profile selection")
	cmd.Flags().StringVar(&cfg.apiBearerToken, "api-bearer-token", "", "bearer token used for API requests during coverage run")
	cmd.Flags().StringVar(&cfg.apiBasicUsername, "api-basic-username", "", "basic auth username used for API requests during coverage run")
	cmd.Flags().StringVar(&cfg.apiBasicPassword, "api-basic-password", "", "basic auth password used for API requests during coverage run")
	cmd.Flags().StringVar(&cfg.writeBasicUsername, "write-basic-username", "", "basic auth username used for write requests to --write-base-url; defaults to --api-basic-username")
	cmd.Flags().StringVar(&cfg.writeBasicPassword, "write-basic-password", "", "basic auth password used for write requests to --write-base-url; defaults to --api-basic-password")
	cmd.Flags().IntVar(&cfg.interactionStrength, "strength", 1, "interaction strength: 1 = individual requirements, 2 = pairwise interactions")
	cmd.Flags().BoolVar(&cfg.includeCases, "include-cases", false, "include the full per-case result array in the JSON report (large runs produce very large output)")
	cmd.Flags().BoolVar(&cfg.exhaustive, "exhaustive", true, "populate optional elements to produce fuller, more realistic payloads")
	return cmd
}
