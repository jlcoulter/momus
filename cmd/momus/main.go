package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	testbulk "github.com/jlcoulter/momus/internal/fhir/bulk"
	testconstraint "github.com/jlcoulter/momus/internal/fhir/constraint"
	"github.com/jlcoulter/momus/internal/fhir/model"
	fhirpackage "github.com/jlcoulter/momus/internal/fhir/package"
	fhirresource "github.com/jlcoulter/momus/internal/fhir/resource"
	testast "github.com/jlcoulter/momus/internal/test/ast"
	testcoverage "github.com/jlcoulter/momus/internal/test/coverage"
	testgeneration "github.com/jlcoulter/momus/internal/test/generation"
	testrunner "github.com/jlcoulter/momus/internal/test/runner"
	"github.com/spf13/cobra"
)

// version is the Momus version. Bumped as part of releases.
const version = "0.0.0"

// abstractResourceTypes are FHIR types with kind "resource" that are abstract
// base types and cannot be instantiated as concrete data.
var abstractResourceTypes = map[string]bool{
	"Resource":          true,
	"DomainResource":    true,
	"CanonicalResource": true,
	"MetadataResource":  true,
}

func main() {
	var debug bool
	var apiBearerToken string
	var apiBasicUsername string
	var apiBasicPassword string

	rootCmd := &cobra.Command{
		Use:   "momus",
		Short: "API and FHIR conformance testing framework",
	}

	rootCmd.Version = version
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "enable verbose debug logging")
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		fhirpackage.SetDebug(debug)
	}

	packageCmd := &cobra.Command{
		Use:   "package",
		Short: "FHIR package operations",
	}
	coverageCmd := &cobra.Command{
		Use:   "coverage",
		Short: "Coverage planning operations",
	}
	var depsDir string
	var downloadDir string
	var conflictPolicy string
	var outputPath string
	var includeResourceTypes []string
	var includeProfileURLs []string
	var excludePathPrefixes []string
	var mustSupportOnly bool
	var includeOptional bool
	var includeLowValuePaths bool
	var baseURL string
	var capabilityBaseURL string
	var scopeToCapability bool
	var failOnUncovered bool
	var interactionStrength int
	var includeCases bool
	var exhaustiveGen bool
	var bulkCount int
	var bulkPerTypeCounts []string

	loadCmd := &cobra.Command{
		Use:   "load <path-to-package.tgz>",
		Short: "Load and decode a FHIR package archive",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pkg, err := fhirpackage.ReadPackage(args[0])
			if err != nil {
				return err
			}

			fmt.Printf("Loaded package %s@%s with %d dependencies and %d resources\n",
				pkg.Name,
				pkg.Version,
				len(pkg.Dependencies),
				len(pkg.Resources),
			)
			return nil
		},
	}

	resolveCmd := &cobra.Command{
		Use:   "resolve <path-to-package.tgz>",
		Short: "Resolve a package and its transitive local dependencies",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rootPath := args[0]
			searchDir := depsDir
			if searchDir == "" {
				searchDir = filepath.Dir(rootPath)
			}
			cacheDir := downloadDir
			if cacheDir == "" {
				cacheDir = filepath.Join(searchDir, ".momus", "packages")
			}

			graph, err := fhirpackage.ResolveLocalPackageGraphWithOptions(rootPath, fhirpackage.ResolveOptions{
				DepsDir:        searchDir,
				DownloadDir:    cacheDir,
				ConflictPolicy: fhirpackage.ConflictPolicy(conflictPolicy),
			})
			if err != nil {
				return err
			}
			if err := writeDebugGraph(debug, graph); err != nil {
				return err
			}

			totalResources := 0
			for _, p := range graph.Packages {
				totalResources += len(p.Resources)
			}

			fmt.Printf("Resolved %d packages from %s using download dir %s with %d total resources\n", len(graph.Packages), searchDir, cacheDir, totalResources)
			for _, p := range graph.Packages {
				fmt.Printf("- %s@%s (deps=%d, resources=%d)\n", p.Name, p.Version, len(p.Dependencies), len(p.Resources))
			}
			return nil
		},
	}
	resolveCmd.Flags().StringVar(&depsDir, "deps-dir", "", "directory to search for dependency package archives (.tgz/.tar.gz)")
	resolveCmd.Flags().StringVar(&downloadDir, "download-dir", "", "directory to store downloaded dependency package archives")
	resolveCmd.Flags().StringVar(&conflictPolicy, "conflict-policy", string(fhirpackage.ConflictPolicyRootWins), "dependency conflict policy: root-wins or strict")

	deriveCmd := &cobra.Command{
		Use:   "derive <path-to-package.tgz>",
		Short: "Derive MVP coverage obligations from resolved profile cardinality constraints",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rootPath := args[0]
			searchDir := depsDir
			if searchDir == "" {
				searchDir = filepath.Dir(rootPath)
			}
			cacheDir := downloadDir
			if cacheDir == "" {
				cacheDir = filepath.Join(searchDir, ".momus", "packages")
			}

			graph, err := fhirpackage.ResolveLocalPackageGraphWithOptions(rootPath, fhirpackage.ResolveOptions{
				DepsDir:        searchDir,
				DownloadDir:    cacheDir,
				ConflictPolicy: fhirpackage.ConflictPolicy(conflictPolicy),
			})
			if err != nil {
				return err
			}
			if err := writeDebugGraph(debug, graph); err != nil {
				return err
			}

			builder := fhirpackage.NewRegistryBuilder()
			reg, err := builder.BuildFromPackagesScoped(graph.Packages, graph.Root)
			if err != nil {
				return err
			}

			plan, err := testcoverage.DerivePlan(reg, testcoverage.DeriveOptions{
				IncludeResourceTypes: includeResourceTypes,
				IncludeProfileURLs:   includeProfileURLs,
				ExcludePathPrefixes:  excludePathPrefixes,
				MustSupportOnly:      mustSupportOnly,
				IncludeOptional:      includeOptional,
				IncludeLowValuePaths: includeLowValuePaths,
				Strength:             interactionStrength,
			})
			if err != nil {
				return err
			}

			out, err := json.MarshalIndent(plan, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal coverage plan: %w", err)
			}
			if err := writeDebugOutput(debug, "coverage-plan.json", append(out, '\n')); err != nil {
				return err
			}

			if outputPath == "" {
				fmt.Println(string(out))
			} else {
				if err := writeOutputFile(outputPath, append(out, '\n')); err != nil {
					return fmt.Errorf("write coverage plan to %s: %w", outputPath, err)
				}
			}

			fmt.Printf("Derived %d coverage requirements from %d resolved packages\n", len(plan.Requirements), len(graph.Packages))
			for domain, count := range plan.Summary.ByDomain {
				fmt.Printf("- %s: %d\n", domain, count)
			}
			fmt.Printf("Resource types: %d, variants: %d\n", len(plan.Summary.ByResourceType), len(plan.Summary.ByVariant))
			if plan.Summary.Interactions > 0 {
				fmt.Printf("Interactions (strength %d): %d\n", plan.Strength, plan.Summary.Interactions)
			}
			if len(plan.Summary.PrunedByReason) > 0 {
				fmt.Println("Pruned elements:")
				for reason, count := range plan.Summary.PrunedByReason {
					fmt.Printf("- %s: %d\n", reason, count)
				}
			}
			if outputPath != "" {
				fmt.Printf("Coverage plan written to %s\n", outputPath)
			}
			return nil
		},
	}
	deriveCmd.Flags().StringVar(&depsDir, "deps-dir", "", "directory to search for dependency package archives (.tgz/.tar.gz)")
	deriveCmd.Flags().StringVar(&downloadDir, "download-dir", "", "directory to store downloaded dependency package archives")
	deriveCmd.Flags().StringVar(&conflictPolicy, "conflict-policy", string(fhirpackage.ConflictPolicyRootWins), "dependency conflict policy: root-wins or strict")
	deriveCmd.Flags().StringVar(&outputPath, "output", "", "write derived coverage plan JSON to a file")
	deriveCmd.Flags().StringSliceVar(&includeResourceTypes, "include-resource", nil, "include only these resource types (repeatable)")
	deriveCmd.Flags().StringSliceVar(&includeProfileURLs, "include-profile-url", nil, "include only these profile canonical URLs (repeatable)")
	deriveCmd.Flags().StringSliceVar(&excludePathPrefixes, "exclude-path-prefix", nil, "exclude element paths by prefix (repeatable)")
	deriveCmd.Flags().BoolVar(&mustSupportOnly, "must-support-only", false, "derive only elements marked mustSupport")
	deriveCmd.Flags().BoolVar(&includeOptional, "include-optional", false, "include optional non-mustSupport elements")
	deriveCmd.Flags().BoolVar(&includeLowValuePaths, "include-low-value-paths", false, "include low-value infrastructure paths like meta/text/language")
	deriveCmd.Flags().IntVar(&interactionStrength, "strength", 1, "interaction strength: 1 = individual requirements, 2 = pairwise interactions")

	astCmd := &cobra.Command{
		Use:   "ast <path-to-package.tgz>",
		Short: "Generate a test AST from derived coverage requirements",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rootPath := args[0]
			searchDir := depsDir
			if searchDir == "" {
				searchDir = filepath.Dir(rootPath)
			}
			cacheDir := downloadDir
			if cacheDir == "" {
				cacheDir = filepath.Join(searchDir, ".momus", "packages")
			}

			graph, err := fhirpackage.ResolveLocalPackageGraphWithOptions(rootPath, fhirpackage.ResolveOptions{
				DepsDir:        searchDir,
				DownloadDir:    cacheDir,
				ConflictPolicy: fhirpackage.ConflictPolicy(conflictPolicy),
			})
			if err != nil {
				return err
			}
			if err := writeDebugGraph(debug, graph); err != nil {
				return err
			}

			builder := fhirpackage.NewRegistryBuilder()
			reg, err := builder.BuildFromPackagesScoped(graph.Packages, graph.Root)
			if err != nil {
				return err
			}

			coverageResourceTypes, coverageProfileURLs, preferredProfilesByResource, err := resourceScopeForRun(cmd, includeResourceTypes, includeProfileURLs, baseURL, capabilityBaseURL, scopeToCapability, apiBearerToken, apiBasicUsername, apiBasicPassword)
			if err != nil {
				return err
			}

			coveragePlan, err := testcoverage.DerivePlan(reg, testcoverage.DeriveOptions{
				IncludeResourceTypes: coverageResourceTypes,
				IncludeProfileURLs:   coverageProfileURLs,
				ExcludePathPrefixes:  excludePathPrefixes,
				MustSupportOnly:      mustSupportOnly,
				IncludeOptional:      includeOptional,
				IncludeLowValuePaths: includeLowValuePaths,
				Strength:             interactionStrength,
			})
			if err != nil {
				return err
			}

			astPlan, err := testgeneration.GenerateFromCoveragePlan(coveragePlan, testgeneration.BuildOptions{BaseURL: baseURL, Registry: reg, PreferredProfileURLsByResource: preferredProfilesByResource, Strength: interactionStrength, Exhaustive: exhaustiveGen})
			if err != nil {
				return err
			}
			encoded, err := testast.EncodePlan(astPlan)
			if err != nil {
				return err
			}

			out, err := json.MarshalIndent(encoded, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal AST plan: %w", err)
			}
			if err := writeDebugOutput(debug, "ast-plan.json", append(out, '\n')); err != nil {
				return err
			}

			if outputPath == "" {
				fmt.Println(string(out))
			} else {
				if err := writeOutputFile(outputPath, append(out, '\n')); err != nil {
					return fmt.Errorf("write AST plan to %s: %w", outputPath, err)
				}
			}

			fmt.Printf("Generated AST with %d requirement cases from %d resolved packages\n", testgeneration.RequirementCount(astPlan), len(graph.Packages))
			if outputPath != "" {
				fmt.Printf("AST plan written to %s\n", outputPath)
			}
			return nil
		},
	}
	astCmd.Flags().StringVar(&depsDir, "deps-dir", "", "directory to search for dependency package archives (.tgz/.tar.gz)")
	astCmd.Flags().StringVar(&downloadDir, "download-dir", "", "directory to store downloaded dependency package archives")
	astCmd.Flags().StringVar(&conflictPolicy, "conflict-policy", string(fhirpackage.ConflictPolicyRootWins), "dependency conflict policy: root-wins or strict")
	astCmd.Flags().StringVar(&outputPath, "output", "", "write generated AST plan JSON to a file")
	astCmd.Flags().StringSliceVar(&includeResourceTypes, "include-resource", nil, "include only these resource types (repeatable)")
	astCmd.Flags().StringSliceVar(&includeProfileURLs, "include-profile-url", nil, "include only these profile canonical URLs (repeatable)")
	astCmd.Flags().StringSliceVar(&excludePathPrefixes, "exclude-path-prefix", nil, "exclude element paths by prefix (repeatable)")
	astCmd.Flags().BoolVar(&mustSupportOnly, "must-support-only", false, "derive only elements marked mustSupport")
	astCmd.Flags().BoolVar(&includeOptional, "include-optional", false, "include optional non-mustSupport elements")
	astCmd.Flags().BoolVar(&includeLowValuePaths, "include-low-value-paths", false, "include low-value infrastructure paths like meta/text/language")
	astCmd.Flags().StringVar(&baseURL, "base-url", "", "target FHIR base URL for request nodes")
	astCmd.Flags().StringVar(&capabilityBaseURL, "capability-base-url", "", "optional alternate FHIR base URL to fetch CapabilityStatement metadata for scope/profile selection")
	astCmd.Flags().IntVar(&interactionStrength, "strength", 1, "interaction strength: 1 = individual requirements, 2 = pairwise interactions")
	astCmd.Flags().BoolVar(&exhaustiveGen, "exhaustive", true, "populate optional elements to produce fuller, more realistic payloads")

	runCmd := &cobra.Command{
		Use:   "run <path-to-package.tgz>",
		Short: "Execute generated AST and output test results",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if baseURL == "" {
				return fmt.Errorf("base URL is required; provide --base-url")
			}

			rootPath := args[0]
			searchDir := depsDir
			if searchDir == "" {
				searchDir = filepath.Dir(rootPath)
			}
			cacheDir := downloadDir
			if cacheDir == "" {
				cacheDir = filepath.Join(searchDir, ".momus", "packages")
			}

			graph, err := fhirpackage.ResolveLocalPackageGraphWithOptions(rootPath, fhirpackage.ResolveOptions{
				DepsDir:        searchDir,
				DownloadDir:    cacheDir,
				ConflictPolicy: fhirpackage.ConflictPolicy(conflictPolicy),
			})
			if err != nil {
				return err
			}
			if err := writeDebugGraph(debug, graph); err != nil {
				return err
			}

			builder := fhirpackage.NewRegistryBuilder()
			reg, err := builder.BuildFromPackagesScoped(graph.Packages, graph.Root)
			if err != nil {
				return err
			}

			capabilityResourceTypes, capabilityProfileURLs, preferredProfilesByResource, err := resourceScopeForRun(cmd, includeResourceTypes, includeProfileURLs, baseURL, capabilityBaseURL, scopeToCapability, apiBearerToken, apiBasicUsername, apiBasicPassword)
			if err != nil {
				return err
			}

			coveragePlan, err := testcoverage.DerivePlan(reg, testcoverage.DeriveOptions{
				IncludeResourceTypes: capabilityResourceTypes,
				IncludeProfileURLs:   capabilityProfileURLs,
				ExcludePathPrefixes:  excludePathPrefixes,
				MustSupportOnly:      mustSupportOnly,
				IncludeOptional:      includeOptional,
				IncludeLowValuePaths: includeLowValuePaths,
				Strength:             interactionStrength,
			})
			if err != nil {
				return err
			}

			astPlan, err := testgeneration.GenerateFromCoveragePlan(coveragePlan, testgeneration.BuildOptions{BaseURL: baseURL, Registry: reg, PreferredProfileURLsByResource: preferredProfilesByResource, Strength: interactionStrength, Exhaustive: exhaustiveGen})
			if err != nil {
				return err
			}

			report, err := testrunner.Execute(cmd.Context(), astPlan.Root, testrunner.ExecuteOptions{
				BaseURL:       baseURL,
				BearerToken:   apiBearerToken,
				BasicUsername: apiBasicUsername,
				BasicPassword: apiBasicPassword,
				IncludeDebug:  debug,
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

			out, err := marshalCoverageRunOutput(report, coverageEvaluation, includeCases)
			if err != nil {
				return fmt.Errorf("marshal test report: %w", err)
			}
			if err := writeDebugOutput(debug, "test-report.json", append(out, '\n')); err != nil {
				return err
			}

			if outputPath == "" {
				fmt.Println(string(out))
			} else {
				if err := writeOutputFile(outputPath, append(out, '\n')); err != nil {
					return fmt.Errorf("write test report to %s: %w", outputPath, err)
				}
			}

			requirementCases, setupCases := countRequirementAndSetupCases(report.Cases)
			fmt.Printf("Executed %d cases: %d passed, %d failed (%d requirement cases + %d setup cases)\n",
				report.Total, report.Passed, report.Failed, requirementCases, setupCases)
			if report.Diagnostics != nil && len(report.Diagnostics.TopSignatures) > 0 {
				fmt.Printf("Top failure signatures (%d OperationOutcome failures):\n", report.Diagnostics.OperationOutcomeFailures)
				for i, sig := range report.Diagnostics.TopSignatures {
					fmt.Printf("  %d. %s (count=%d, req=%s)\n", i+1, sig.Signature, sig.Count, sig.ExampleRequirementID)
				}
				if report.Diagnostics.LikelyAuthFailure && report.Diagnostics.Hint != "" {
					fmt.Printf("Hint: %s\n", report.Diagnostics.Hint)
				}
			}
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
			if outputPath != "" {
				fmt.Printf("Test report written to %s\n", outputPath)
			}
			if failOnUncovered && coverageEvaluation.UncoveredRequirements > 0 {
				return fmt.Errorf("coverage incomplete: %d uncovered obligations", coverageEvaluation.UncoveredRequirements)
			}
			return nil
		},
	}
	runCmd.Flags().StringVar(&depsDir, "deps-dir", "", "directory to search for dependency package archives (.tgz/.tar.gz)")
	runCmd.Flags().StringVar(&downloadDir, "download-dir", "", "directory to store downloaded dependency package archives")
	runCmd.Flags().StringVar(&conflictPolicy, "conflict-policy", string(fhirpackage.ConflictPolicyRootWins), "dependency conflict policy: root-wins or strict")
	runCmd.Flags().StringVar(&outputPath, "output", "", "write test result report JSON to a file")
	runCmd.Flags().StringSliceVar(&includeResourceTypes, "include-resource", nil, "include only these resource types (repeatable)")
	runCmd.Flags().StringSliceVar(&includeProfileURLs, "include-profile-url", nil, "include only these profile canonical URLs (repeatable)")
	runCmd.Flags().StringSliceVar(&excludePathPrefixes, "exclude-path-prefix", nil, "exclude element paths by prefix (repeatable)")
	runCmd.Flags().BoolVar(&mustSupportOnly, "must-support-only", false, "derive only elements marked mustSupport")
	runCmd.Flags().BoolVar(&includeOptional, "include-optional", false, "include optional non-mustSupport elements")
	runCmd.Flags().BoolVar(&includeLowValuePaths, "include-low-value-paths", false, "include low-value infrastructure paths like meta/text/language")
	runCmd.Flags().BoolVar(&scopeToCapability, "scope-to-capability", true, "limit derivation to CapabilityStatement server resources that support create")
	runCmd.Flags().BoolVar(&failOnUncovered, "fail-on-uncovered", false, "return non-zero exit code when contractual coverage has uncovered obligations")
	runCmd.Flags().StringVar(&baseURL, "base-url", "", "target FHIR base URL for request execution")
	runCmd.Flags().StringVar(&capabilityBaseURL, "capability-base-url", "", "optional alternate FHIR base URL to fetch CapabilityStatement metadata for scope/profile selection")
	runCmd.Flags().StringVar(&apiBearerToken, "api-bearer-token", "", "bearer token used for API requests during coverage run")
	runCmd.Flags().StringVar(&apiBasicUsername, "api-basic-username", "", "basic auth username used for API requests during coverage run")
	runCmd.Flags().StringVar(&apiBasicPassword, "api-basic-password", "", "basic auth password used for API requests during coverage run")
	runCmd.Flags().IntVar(&interactionStrength, "strength", 1, "interaction strength: 1 = individual requirements, 2 = pairwise interactions")
	runCmd.Flags().BoolVar(&includeCases, "include-cases", false, "include the full per-case result array in the JSON report (large runs produce very large output)")
	runCmd.Flags().BoolVar(&exhaustiveGen, "exhaustive", true, "populate optional elements to produce fuller, more realistic payloads")

	bulkCmd := &cobra.Command{
		Use:   "bulk <path-to-package.tgz>",
		Short: "Generate NDJSON bulk data from derived coverage requirements",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rootPath := args[0]
			searchDir := depsDir
			if searchDir == "" {
				searchDir = filepath.Dir(rootPath)
			}
			cacheDir := downloadDir
			if cacheDir == "" {
				cacheDir = filepath.Join(searchDir, ".momus", "packages")
			}

			graph, err := fhirpackage.ResolveLocalPackageGraphWithOptions(rootPath, fhirpackage.ResolveOptions{
				DepsDir:        searchDir,
				DownloadDir:    cacheDir,
				ConflictPolicy: fhirpackage.ConflictPolicy(conflictPolicy),
			})
			if err != nil {
				return err
			}
			if err := writeDebugGraph(debug, graph); err != nil {
				return err
			}

			builder := fhirpackage.NewRegistryBuilder()
			reg, err := builder.BuildFromPackagesScoped(graph.Packages, graph.Root)
			if err != nil {
				return err
			}

			resourceTypes := includeResourceTypes
			if len(resourceTypes) == 0 {
				seen := make(map[string]bool)
				for _, sd := range reg.ScopedStructureDefinitions() {
					if sd.Type == "" || sd.Kind != "resource" || abstractResourceTypes[sd.Type] {
						continue
					}
					if !seen[sd.Type] {
						seen[sd.Type] = true
						resourceTypes = append(resourceTypes, sd.Type)
					}
				}
				sort.Strings(resourceTypes)
			}

			generator := fhirresource.NewGeneratorWithOptions(reg, fhirresource.Options{Exhaustive: exhaustiveGen})
			corpus, err := generator.GenerateCorpus(cmd.Context(), resourceTypes, bulkCount, parsePerTypeCounts(bulkPerTypeCounts))
			if err != nil {
				return err
			}

			var out io.Writer = os.Stdout
			var f *os.File
			if outputPath != "" {
				if dir := filepath.Dir(outputPath); dir != "" && dir != "." {
					if err := os.MkdirAll(dir, 0o755); err != nil {
						return fmt.Errorf("create bulk output dir %s: %w", dir, err)
					}
				}
				f, err = os.Create(outputPath)
				if err != nil {
					return fmt.Errorf("create bulk file %s: %w", outputPath, err)
				}
				defer f.Close()
				out = f
			}

			w := testbulk.NewWriter(out)
			instances := testbulk.Link([]*model.Dataset{corpus})
			if err := writeDebugBulk(debug, instances); err != nil {
				return err
			}
			if err := w.WriteInstances(instances); err != nil {
				return err
			}
			if err := w.Close(); err != nil {
				return err
			}

			fmt.Printf("Generated NDJSON bulk data: %d resources across %d resource types\n", len(instances), len(resourceTypes))
			if outputPath != "" {
				fmt.Printf("Bulk data written to %s\n", outputPath)
			}
			return nil
		},
	}
	bulkCmd.Flags().StringVar(&depsDir, "deps-dir", "", "directory to search for dependency package archives (.tgz/.tar.gz)")
	bulkCmd.Flags().StringVar(&downloadDir, "download-dir", "", "directory to store downloaded dependency package archives")
	bulkCmd.Flags().StringVar(&conflictPolicy, "conflict-policy", string(fhirpackage.ConflictPolicyRootWins), "dependency conflict policy: root-wins or strict")
	bulkCmd.Flags().StringVar(&outputPath, "output", "", "write NDJSON bulk data to a file")
	bulkCmd.Flags().BoolVar(&exhaustiveGen, "exhaustive", true, "populate optional elements to produce fuller, more complete resources")
	bulkCmd.Flags().IntVar(&bulkCount, "count", 25, "number of resources to generate per resource type")
	bulkCmd.Flags().StringSliceVar(&bulkPerTypeCounts, "per-type", nil, "per-type resource counts as Type=Count (repeatable); overrides --count")
	bulkCmd.Flags().StringSliceVar(&includeResourceTypes, "include-resource", nil, "include only these resource types (repeatable)")
	bulkCmd.Flags().StringSliceVar(&includeProfileURLs, "include-profile-url", nil, "include only these profile canonical URLs (repeatable)")
	bulkCmd.Flags().StringSliceVar(&excludePathPrefixes, "exclude-path-prefix", nil, "exclude element paths by prefix (repeatable)")
	bulkCmd.Flags().BoolVar(&mustSupportOnly, "must-support-only", false, "derive only elements marked mustSupport")
	bulkCmd.Flags().BoolVar(&includeOptional, "include-optional", false, "include optional non-mustSupport elements")
	bulkCmd.Flags().BoolVar(&includeLowValuePaths, "include-low-value-paths", false, "include low-value infrastructure paths like meta/text/language")
	bulkCmd.Flags().IntVar(&interactionStrength, "strength", 1, "interaction strength: 1 = individual requirements, 2 = pairwise interactions")

	constraintsCmd := &cobra.Command{
		Use:   "constraints <path-to-package.tgz>",
		Short: "Derive the constraint model from resolved package definitions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rootPath := args[0]
			searchDir := depsDir
			if searchDir == "" {
				searchDir = filepath.Dir(rootPath)
			}
			cacheDir := downloadDir
			if cacheDir == "" {
				cacheDir = filepath.Join(searchDir, ".momus", "packages")
			}

			graph, err := fhirpackage.ResolveLocalPackageGraphWithOptions(rootPath, fhirpackage.ResolveOptions{
				DepsDir:        searchDir,
				DownloadDir:    cacheDir,
				ConflictPolicy: fhirpackage.ConflictPolicy(conflictPolicy),
			})
			if err != nil {
				return err
			}
			if err := writeDebugGraph(debug, graph); err != nil {
				return err
			}

			builder := fhirpackage.NewRegistryBuilder()
			reg, err := builder.BuildFromPackages(graph.Packages)
			if err != nil {
				return err
			}

			constraints, err := testconstraint.Derive(reg)
			if err != nil {
				return err
			}

			out, err := json.MarshalIndent(constraints, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal constraints: %w", err)
			}
			if err := writeDebugOutput(debug, "constraints.json", append(out, '\n')); err != nil {
				return err
			}

			if outputPath == "" {
				fmt.Println(string(out))
			} else {
				if err := writeOutputFile(outputPath, append(out, '\n')); err != nil {
					return fmt.Errorf("write constraints to %s: %w", outputPath, err)
				}
			}

			fmt.Printf("Derived %d constraints from %d resolved packages\n", len(constraints), len(graph.Packages))
			if outputPath != "" {
				fmt.Printf("Constraints written to %s\n", outputPath)
			}
			return nil
		},
	}
	constraintsCmd.Flags().StringVar(&depsDir, "deps-dir", "", "directory to search for dependency package archives (.tgz/.tar.gz)")
	constraintsCmd.Flags().StringVar(&downloadDir, "download-dir", "", "directory to store downloaded dependency package archives")
	constraintsCmd.Flags().StringVar(&conflictPolicy, "conflict-policy", string(fhirpackage.ConflictPolicyRootWins), "dependency conflict policy: root-wins or strict")
	constraintsCmd.Flags().StringVar(&outputPath, "output", "", "write derived constraints JSON to a file")

	packageCmd.AddCommand(loadCmd)
	packageCmd.AddCommand(resolveCmd)
	coverageCmd.AddCommand(deriveCmd)
	coverageCmd.AddCommand(constraintsCmd)
	coverageCmd.AddCommand(astCmd)
	coverageCmd.AddCommand(runCmd)
	rootCmd.AddCommand(packageCmd)
	rootCmd.AddCommand(coverageCmd)
	coverageCmd.AddCommand(bulkCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func resourceScopeForRun(cmd *cobra.Command, includeResourceTypes, includeProfileURLs []string, baseURL, capabilityBaseURL string, scopeToCapability bool, bearerToken, basicUsername, basicPassword string) ([]string, []string, map[string][]string, error) {
	if !scopeToCapability {
		return includeResourceTypes, includeProfileURLs, nil, nil
	}
	metadataBaseURL := strings.TrimSpace(capabilityBaseURL)
	if metadataBaseURL == "" {
		metadataBaseURL = baseURL
	}
	capabilityStatement, err := testcoverage.FetchCapabilityStatement(cmd.Context(), metadataBaseURL, testcoverage.CapabilityFetchOptions{
		BearerToken:   bearerToken,
		BasicUsername: basicUsername,
		BasicPassword: basicPassword,
	})
	if err != nil {
		// When the target server is not reachable, fall back to the loaded
		// package as the source of truth rather than failing the run. Other
		// errors (a reachable server returning an auth or protocol error) are
		// surfaced so misconfiguration is not silently ignored.
		if isServerUnavailable(err) {
			fmt.Fprintf(os.Stderr, "WARNING: target server unreachable (%v); falling back to package definitions as source of truth\n", err)
			return includeResourceTypes, includeProfileURLs, nil, nil
		}
		return nil, nil, nil, err
	}
	capabilityTypes := testcoverage.ResourceTypesFromCapabilityStatement(capabilityStatement, true)
	capabilityProfiles := testcoverage.SupportedProfileURLsFromCapabilityStatement(capabilityStatement, true)
	capabilityProfilesByResource := testcoverage.SupportedProfileURLsByResourceFromCapabilityStatement(capabilityStatement, true)
	if len(capabilityTypes) == 0 {
		// Some CapabilityStatements omit per-resource create interactions.
		// Fall back to server-declared resource/profile scope instead of unscoped derivation.
		capabilityTypes = testcoverage.ResourceTypesFromCapabilityStatement(capabilityStatement, false)
		capabilityProfiles = testcoverage.SupportedProfileURLsFromCapabilityStatement(capabilityStatement, false)
		capabilityProfilesByResource = testcoverage.SupportedProfileURLsByResourceFromCapabilityStatement(capabilityStatement, false)
	}
	if len(capabilityProfiles) > 0 {
		return intersectCaseInsensitive(includeResourceTypes, capabilityTypes), intersectCaseInsensitive(includeProfileURLs, capabilityProfiles), capabilityProfilesByResource, nil
	}
	if len(capabilityTypes) == 0 {
		return includeResourceTypes, includeProfileURLs, capabilityProfilesByResource, nil
	}
	return intersectCaseInsensitive(includeResourceTypes, capabilityTypes), includeProfileURLs, capabilityProfilesByResource, nil
}

// isServerUnavailable reports whether a fetch error means the target server
// could not be reached, as opposed to a reachable server returning an
// application/protocol error.
func isServerUnavailable(err error) bool {
	if err == nil {
		return false
	}
	for _, target := range []error{
		syscall.ECONNREFUSED,
		syscall.ECONNRESET,
		syscall.ENETUNREACH,
		syscall.EHOSTUNREACH,
	} {
		if errors.Is(err, target) {
			return true
		}
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return false
}

func intersectCaseInsensitive(requested, available []string) []string {
	if len(available) == 0 {
		return requested
	}
	if len(requested) == 0 {
		return available
	}
	requestedSet := make(map[string]string, len(requested))
	for _, value := range requested {
		key := strings.ToLower(strings.TrimSpace(value))
		if key == "" {
			continue
		}
		requestedSet[key] = value
	}
	intersected := make([]string, 0)
	for _, value := range available {
		key := strings.ToLower(strings.TrimSpace(value))
		if original, ok := requestedSet[key]; ok {
			intersected = append(intersected, original)
		}
	}
	return intersected
}

func marshalCoverageRunOutput(report *testrunner.Report, evaluation testcoverage.EvaluationReport, includeCases bool) ([]byte, error) {
	requirementCases, setupCases := countRequirementAndSetupCases(report.Cases)

	// Readable summary first so the report can be scanned without wading through
	// the (potentially very large) per-case data.
	payload := map[string]any{
		"summary": map[string]any{
			"totalCases":            report.Total,
			"passedCases":           report.Passed,
			"failedCases":           report.Failed,
			"requirementCases":      requirementCases,
			"setupCases":            setupCases,
			"totalRequirements":     evaluation.TotalRequirements,
			"coveredRequirements":   evaluation.CoveredRequirements,
			"uncoveredRequirements": evaluation.UncoveredRequirements,
			"coveragePercent":       evaluation.CoveragePercent,
		},
		"coverage":    evaluation,
		"triage":      report.Triage,
		"diagnostics": report.Diagnostics,
	}

	// Failures, compact, so every failed test is addressable by requirement id
	// without dumping all passing cases (which coverage.covered already records).
	failures := make([]map[string]any, 0)
	for _, c := range report.Cases {
		if c.Passed {
			continue
		}
		failures = append(failures, compactFailure(c))
	}
	payload["failures"] = failures

	if includeCases {
		payload["cases"] = report.Cases
	}
	return json.MarshalIndent(payload, "", "  ")
}

// compactFailure renders a failed case with the fields needed to look it up and
// decide whether it is a broken test or a server issue, omitting debug bloat.
func compactFailure(c testrunner.CaseResult) map[string]any {
	m := map[string]any{
		"requirementId": c.RequirementID,
		"description":   c.Description,
		"expression":    c.Expression,
		"statusCode":    c.StatusCode,
	}
	if c.Error != "" {
		m["error"] = c.Error
	}
	if c.FailureFingerprint != "" {
		m["failureFingerprint"] = c.FailureFingerprint
	}
	if c.Trace != nil {
		m["expected"] = c.Trace.Expected
		m["domain"] = c.Trace.Domain
		m["variant"] = c.Trace.Variant
		m["resourceType"] = c.Trace.ResourceType
		m["elementPath"] = c.Trace.ElementPath
	}
	if c.Debug != nil {
		m["requestUrl"] = c.Debug.RequestURL
		m["requestBody"] = c.Debug.RequestBody
		m["responseBody"] = c.Debug.ResponseBody
	}
	return m
}

func countRequirementAndSetupCases(cases []testrunner.CaseResult) (requirement, setup int) {
	for _, c := range cases {
		if strings.HasPrefix(c.RequirementID, "setup:") {
			setup++
		} else {
			requirement++
		}
	}
	return requirement, setup
}

func printCoverageGapSummary(evaluation testcoverage.EvaluationReport) {
	for _, domain := range sortedDomainKeys(evaluation.ByDomain) {
		summary := evaluation.ByDomain[domain]
		if summary.Uncovered <= 0 {
			continue
		}
		fmt.Printf("  Domain %s: %d uncovered\n", domain, summary.Uncovered)
	}
	for _, resourceType := range sortedStringKeys(evaluation.ByResourceType) {
		summary := evaluation.ByResourceType[resourceType]
		if summary.Uncovered <= 0 {
			continue
		}
		fmt.Printf("  Resource %s: %d uncovered\n", resourceType, summary.Uncovered)
	}
	for _, variant := range sortedVariantKeys(evaluation.ByVariant) {
		summary := evaluation.ByVariant[variant]
		if summary.Uncovered <= 0 {
			continue
		}
		fmt.Printf("  Variant %s: %d uncovered\n", variant, summary.Uncovered)
	}
}

func sortedDomainKeys(m map[testcoverage.CoverageDomain]testcoverage.DomainCoverageSummary) []testcoverage.CoverageDomain {
	keys := make([]testcoverage.CoverageDomain, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i] < keys[j]
	})
	return keys
}

func sortedVariantKeys(m map[testcoverage.CoverageVariant]testcoverage.DomainCoverageSummary) []testcoverage.CoverageVariant {
	keys := make([]testcoverage.CoverageVariant, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i] < keys[j]
	})
	return keys
}

func sortedStringKeys(m map[string]testcoverage.DomainCoverageSummary) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func writeOutputFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, data, 0o644)
}

// debugOutputDir is the default directory where per-stage JSON artifacts are
// written when --debug is enabled.
var debugOutputDir = ".momus/output"

// writeDebugOutput writes stage data to the debug output directory when debug
// mode is enabled. It is a no-op otherwise. stage is the file name within the
// debug output directory, e.g. "coverage-plan.json".
func writeDebugOutput(debug bool, stage string, data []byte) error {
	if !debug {
		return nil
	}
	path := filepath.Join(debugOutputDir, stage)
	if err := writeOutputFile(path, data); err != nil {
		return fmt.Errorf("write debug output %s: %w", path, err)
	}
	fmt.Fprintf(os.Stderr, "debug: wrote %s\n", path)
	return nil
}

// writeDebugBulk writes NDJSON bulk instances to the debug output directory
// when debug mode is enabled. It is a no-op otherwise.
func writeDebugBulk(debug bool, instances []*model.ResourceInstance) error {
	if !debug {
		return nil
	}
	path := filepath.Join(debugOutputDir, "bulk.ndjson")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create debug bulk dir: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create debug bulk file %s: %w", path, err)
	}
	defer f.Close()
	w := testbulk.NewWriter(f)
	if err := w.WriteInstances(instances); err != nil {
		return fmt.Errorf("write debug bulk output %s: %w", path, err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("flush debug bulk output %s: %w", path, err)
	}
	fmt.Fprintf(os.Stderr, "debug: wrote %s\n", path)
	return nil
}

// writeDebugGraph writes the resolved dependency graph to the debug output
// directory when debug mode is enabled. It is a no-op otherwise.
func writeDebugGraph(debug bool, graph *fhirpackage.ResolvedGraph) error {
	if !debug {
		return nil
	}
	out, err := json.MarshalIndent(graph, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal resolved graph: %w", err)
	}
	return writeDebugOutput(debug, "resolved-graph.json", append(out, '\n'))
}

// parsePerTypeCounts parses repeatable Type=Count overrides into a map. Values
// that cannot be parsed as counts are ignored.
func parsePerTypeCounts(entries []string) map[string]int {
	out := make(map[string]int)
	for _, entry := range entries {
		idx := strings.LastIndex(entry, "=")
		if idx <= 0 || idx == len(entry)-1 {
			continue
		}
		typ := strings.TrimSpace(entry[:idx])
		count, err := strconv.Atoi(strings.TrimSpace(entry[idx+1:]))
		if err != nil || typ == "" {
			continue
		}
		out[typ] = count
	}
	return out
}
