package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	fhirpackage "github.com/jlcoulter/momus/internal/fhir/package"
	testcoverage "github.com/jlcoulter/momus/internal/test/coverage"
	"github.com/spf13/cobra"
)

// newDeriveCmd returns the "coverage derive" command.
func newDeriveCmd(cfg *config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "derive <path-to-package.tgz>",
		Short: "Derive MVP coverage obligations from resolved profile cardinality constraints",
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

			plan, err := testcoverage.DerivePlan(reg, testcoverage.DeriveOptions{
				IncludeResourceTypes:         cfg.includeResourceTypes,
				IncludeProfileURLs:           cfg.includeProfileURLs,
				ExcludePathPrefixes:          cfg.excludePathPrefixes,
				MustSupportOnly:              cfg.mustSupportOnly,
				IncludeOptional:              cfg.includeOptional,
				IncludeLowValuePaths:         cfg.includeLowValuePaths,
				Strength:                     cfg.interactionStrength,
				IncludeUniversalSearchParams: cfg.includeUniversalSearch,
			})
			if err != nil {
				return err
			}

			out, err := json.MarshalIndent(plan, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal coverage plan: %w", err)
			}
			if err := writeDebugOutput(cfg.debug, "coverage-plan.json", append(out, '\n')); err != nil {
				return err
			}

			if cfg.outputPath == "" {
				fmt.Println(string(out))
			} else {
				if err := writeOutputFile(cfg.outputPath, append(out, '\n')); err != nil {
					return fmt.Errorf("write coverage plan to %s: %w", cfg.outputPath, err)
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
			if cfg.outputPath != "" {
				fmt.Printf("Coverage plan written to %s\n", cfg.outputPath)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&cfg.depsDir, "deps-dir", "", "directory to search for dependency package archives (.tgz/.tar.gz)")
	cmd.Flags().StringVar(&cfg.downloadDir, "download-dir", "", "directory to store downloaded dependency package archives")
	cmd.Flags().StringVar(&cfg.conflictPolicy, "conflict-policy", string(fhirpackage.ConflictPolicyRootWins), "dependency conflict policy: root-wins or strict")
	cmd.Flags().StringVar(&cfg.outputPath, "output", "", "write derived coverage plan JSON to a file")
	cmd.Flags().StringSliceVar(&cfg.includeResourceTypes, "include-resource", nil, "include only these resource types (repeatable)")
	cmd.Flags().StringSliceVar(&cfg.includeProfileURLs, "include-profile-url", nil, "include only these profile canonical URLs (repeatable)")
	cmd.Flags().StringSliceVar(&cfg.excludePathPrefixes, "exclude-path-prefix", nil, "exclude element paths by prefix (repeatable)")
	cmd.Flags().BoolVar(&cfg.mustSupportOnly, "must-support-only", false, "derive only elements marked mustSupport")
	cmd.Flags().BoolVar(&cfg.includeOptional, "include-optional", false, "include optional non-mustSupport elements")
	cmd.Flags().BoolVar(&cfg.includeLowValuePaths, "include-low-value-paths", false, "include low-value infrastructure paths like meta/text/language")
	cmd.Flags().BoolVar(&cfg.includeUniversalSearch, "include-universal-search", false, "include universal FHIR search parameters (_id, _count, _sort, _include, _summary, _filter) for every resource type even when the server's CapabilityStatement does not declare them")
	cmd.Flags().IntVar(&cfg.interactionStrength, "strength", 1, "interaction strength: 1 = individual requirements, 2 = pairwise interactions")
	return cmd
}
