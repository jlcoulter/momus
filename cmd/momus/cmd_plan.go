package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	fhirpackage "github.com/jlcoulter/momus/internal/fhir/package"
	fhirplanner "github.com/jlcoulter/momus/internal/fhir/planner"
	fhirresource "github.com/jlcoulter/momus/internal/fhir/resource"
	testast "github.com/jlcoulter/momus/internal/test/ast"
	testcoverage "github.com/jlcoulter/momus/internal/test/coverage"
	"github.com/spf13/cobra"
)

// newPlanCmd returns the "coverage plan" command.
func newPlanCmd(cfg *config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan <path-to-package.tgz>",
		Short: "Generate a Dataset and executable TestPlan from data requirements",
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

			requirements, err := testcoverage.PlanToDataRequirements(coveragePlan)
			if err != nil {
				return err
			}

			generator := fhirresource.NewGeneratorWithOptions(reg, fhirresource.Options{Exhaustive: cfg.exhaustive})
			planner := fhirplanner.NewDefaultPlanner(generator)
			testPlan, err := planner.Plan(cmd.Context(), fhirplanner.Input{BaseURL: cfg.baseURL, Requirements: requirements})
			if err != nil {
				return err
			}

			encoded, err := testast.EncodePlan(&testast.Plan{Version: "v1", Root: testPlan.Root})
			if err != nil {
				return err
			}
			out, err := json.MarshalIndent(encoded, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal test plan: %w", err)
			}

			fmt.Printf("Planned %d resources from %d data requirements across %d dependency levels\n", len(testPlan.Dataset.Resources), len(requirements), dependencyLevelCount(testPlan.Root))
			if cfg.outputPath == "" {
				fmt.Println(string(out))
			} else {
				if err := writeOutputFile(cfg.outputPath, append(out, '\n')); err != nil {
					return fmt.Errorf("write test plan to %s: %w", cfg.outputPath, err)
				}
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
