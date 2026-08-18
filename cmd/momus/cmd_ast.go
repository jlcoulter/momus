package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	fhirpackage "github.com/jlcoulter/momus/internal/fhir/package"
	testast "github.com/jlcoulter/momus/internal/test/ast"
	testcoverage "github.com/jlcoulter/momus/internal/test/coverage"
	testgeneration "github.com/jlcoulter/momus/internal/test/generation"
	"github.com/spf13/cobra"
)

// newAstCmd returns the "coverage ast" command.
func newAstCmd(cfg *config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ast <path-to-package.tgz>",
		Short: "Generate a test AST from derived coverage requirements",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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

			coverageResourceTypes, coverageProfileURLs, preferredProfilesByResource, err := resourceScopeForRun(cmd, cfg)
			if err != nil {
				return err
			}

			coveragePlan, err := testcoverage.DerivePlan(reg, testcoverage.DeriveOptions{
				IncludeResourceTypes: coverageResourceTypes,
				IncludeProfileURLs:   coverageProfileURLs,
				ExcludePathPrefixes:  cfg.excludePathPrefixes,
				MustSupportOnly:      cfg.mustSupportOnly,
				IncludeOptional:      cfg.includeOptional,
				IncludeLowValuePaths: cfg.includeLowValuePaths,
				Strength:             cfg.interactionStrength,
			})
			if err != nil {
				return err
			}

			astPlan, err := testgeneration.GenerateFromCoveragePlan(coveragePlan, testgeneration.BuildOptions{BaseURL: cfg.baseURL, Registry: reg, PreferredProfileURLsByResource: preferredProfilesByResource, Strength: cfg.interactionStrength, Exhaustive: cfg.exhaustive})
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
			if err := writeDebugOutput(cfg.debug, "ast-plan.json", append(out, '\n')); err != nil {
				return err
			}

			if cfg.outputPath == "" {
				fmt.Println(string(out))
			} else {
				if err := writeOutputFile(cfg.outputPath, append(out, '\n')); err != nil {
					return fmt.Errorf("write AST plan to %s: %w", cfg.outputPath, err)
				}
			}

			fmt.Printf("Generated AST with %d requirement cases from %d resolved packages\n", testgeneration.RequirementCount(astPlan), len(graph.Packages))
			if cfg.outputPath != "" {
				fmt.Printf("AST plan written to %s\n", cfg.outputPath)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&cfg.depsDir, "deps-dir", "", "directory to search for dependency package archives (.tgz/.tar.gz)")
	cmd.Flags().StringVar(&cfg.downloadDir, "download-dir", "", "directory to store downloaded dependency package archives")
	cmd.Flags().StringVar(&cfg.conflictPolicy, "conflict-policy", string(fhirpackage.ConflictPolicyRootWins), "dependency conflict policy: root-wins or strict")
	cmd.Flags().StringVar(&cfg.outputPath, "output", "", "write generated AST plan JSON to a file")
	cmd.Flags().StringSliceVar(&cfg.includeResourceTypes, "include-resource", nil, "include only these resource types (repeatable)")
	cmd.Flags().StringSliceVar(&cfg.includeProfileURLs, "include-profile-url", nil, "include only these profile canonical URLs (repeatable)")
	cmd.Flags().StringSliceVar(&cfg.excludePathPrefixes, "exclude-path-prefix", nil, "exclude element paths by prefix (repeatable)")
	cmd.Flags().BoolVar(&cfg.mustSupportOnly, "must-support-only", false, "derive only elements marked mustSupport")
	cmd.Flags().BoolVar(&cfg.includeOptional, "include-optional", false, "include optional non-mustSupport elements")
	cmd.Flags().BoolVar(&cfg.includeLowValuePaths, "include-low-value-paths", false, "include low-value infrastructure paths like meta/text/language")
	cmd.Flags().StringVar(&cfg.baseURL, "base-url", "", "target FHIR base URL for request nodes")
	cmd.Flags().StringVar(&cfg.capabilityBaseURL, "capability-base-url", "", "optional alternate FHIR base URL to fetch CapabilityStatement metadata for scope/profile selection")
	cmd.Flags().IntVar(&cfg.interactionStrength, "strength", 1, "interaction strength: 1 = individual requirements, 2 = pairwise interactions")
	cmd.Flags().BoolVar(&cfg.exhaustive, "exhaustive", true, "populate optional elements to produce fuller, more realistic payloads")
	return cmd
}
