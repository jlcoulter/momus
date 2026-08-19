package main

import (
	"fmt"
	"path/filepath"

	fhirpackage "github.com/jlcoulter/momus/internal/fhir/package"
	testcoverage "github.com/jlcoulter/momus/internal/test/coverage"
	testgeneration "github.com/jlcoulter/momus/internal/test/generation"
	"github.com/spf13/cobra"
)

// newPlanCmd returns the "coverage plan" command. It uses the exact same data
// generation pipeline as "coverage ast" — one registry-driven core synthesises
// both the seed dataset and the test AST — so there is a single data generation
// path for all test and provisioned data.
func newPlanCmd(cfg *config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan <path-to-package.tgz>",
		Short: "Generate a test plan (seed dataset + test AST) from derived coverage requirements",
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

			builder := fhirpackage.NewRegistryBuilder()
			reg, err := builder.BuildFromPackagesScoped(graph.Packages, graph.Root)
			if err != nil {
				return err
			}

			coveragePlan, err := testcoverage.DerivePlan(reg, testcoverage.DeriveOptions{
				IncludeResourceTypes: cfg.includeResourceTypes,
				IncludeProfileURLs:   cfg.includeProfileURLs,
				ExcludePathPrefixes:  cfg.excludePathPrefixes,
				MustSupportOnly:      cfg.mustSupportOnly,
				IncludeOptional:      cfg.includeOptional,
				IncludeLowValuePaths: cfg.includeLowValuePaths,
				Strength:             cfg.interactionStrength,
			})
			if err != nil {
				return err
			}

			buildOpts := testgeneration.BuildOptions{
				BaseURL:                        cfg.baseURL,
				WriteBaseURL:                   cfg.writeBaseURL,
				Registry:                       reg,
				PreferredProfileURLsByResource: nil,
				Strength:                       cfg.interactionStrength,
				Exhaustive:                     cfg.exhaustive,
			}
			astPlan, err := testgeneration.GenerateFromCoveragePlan(coveragePlan, buildOpts)
			if err != nil {
				return err
			}
			setupDataset, err := testgeneration.BuildSetupDataset(coveragePlan, buildOpts)
			if err != nil {
				return err
			}

			out, err := encodeTestPlan(astPlan, setupDataset)
			if err != nil {
				return fmt.Errorf("encode test plan: %w", err)
			}
			if cfg.outputPath == "" {
				fmt.Println(string(out))
			} else {
				if err := writeOutputFile(cfg.outputPath, append(out, '\n')); err != nil {
					return fmt.Errorf("write test plan to %s: %w", cfg.outputPath, err)
				}
			}

			fmt.Printf("Generated test plan with %d requirement cases and %d seed resources from %d resolved packages\n", testgeneration.RequirementCount(astPlan), len(setupDataset.Resources), len(graph.Packages))
			if cfg.outputPath != "" {
				fmt.Printf("Test plan written to %s\n", cfg.outputPath)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&cfg.depsDir, "deps-dir", "", "directory to search for dependency package archives (.tgz/.tar.gz)")
	cmd.Flags().StringVar(&cfg.downloadDir, "download-dir", "", "directory to store downloaded dependency package archives")
	cmd.Flags().StringVar(&cfg.conflictPolicy, "conflict-policy", string(fhirpackage.ConflictPolicyRootWins), "dependency conflict policy: root-wins or strict")
	cmd.Flags().StringVar(&cfg.outputPath, "output", "", "write generated test plan JSON to a file")
	cmd.Flags().StringVar(&cfg.baseURL, "base-url", "", "target FHIR base URL for request nodes")
	cmd.Flags().StringVar(&cfg.writeBaseURL, "write-base-url", "", "alternate FHIR base URL for resource creation (write) request nodes; defaults to --base-url")
	cmd.Flags().StringSliceVar(&cfg.includeResourceTypes, "include-resource", nil, "include only these resource types (repeatable)")
	cmd.Flags().StringSliceVar(&cfg.includeProfileURLs, "include-profile-url", nil, "include only these profile canonical URLs (repeatable)")
	cmd.Flags().StringSliceVar(&cfg.excludePathPrefixes, "exclude-path-prefix", nil, "exclude element paths by prefix (repeatable)")
	cmd.Flags().BoolVar(&cfg.mustSupportOnly, "must-support-only", false, "derive only elements marked mustSupport")
	cmd.Flags().BoolVar(&cfg.includeOptional, "include-optional", false, "include optional non-mustSupport elements")
	cmd.Flags().BoolVar(&cfg.includeLowValuePaths, "include-low-value-paths", false, "include low-value infrastructure paths like meta/text/language")
	cmd.Flags().IntVar(&cfg.interactionStrength, "strength", 1, "interaction strength: 1 = individual requirements, 2 = pairwise interactions")
	cmd.Flags().BoolVar(&cfg.exhaustive, "exhaustive", true, "populate optional elements to produce fuller, more realistic resources")
	return cmd
}
