package main

import (
	"encoding/json"
	"fmt"

	fhirpackage "github.com/jlcoulter/momus/internal/fhir/package"
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
			graph, reg, err := resolvePackageGraph(cfg, rootPath)
			if err != nil {
				return err
			}

			plan, err := deriveCoveragePlan(cfg, reg, cfg.IncludeResourceTypes, cfg.IncludeProfileURLs, nil)
			if err != nil {
				return err
			}

			out, err := json.MarshalIndent(plan, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal coverage plan: %w", err)
			}
			if err := writeDebugOutput(cfg.Debug, "coverage-plan.json", append(out, '\n')); err != nil {
				return err
			}

			if cfg.OutputPath == "" {
				fmt.Println(string(out))
			} else {
				if err := writeOutputFile(cfg.OutputPath, append(out, '\n')); err != nil {
					return fmt.Errorf("write coverage plan to %s: %w", cfg.OutputPath, err)
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
			if cfg.OutputPath != "" {
				fmt.Printf("Coverage plan written to %s\n", cfg.OutputPath)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&cfg.DepsDir, "deps-dir", "", "directory to search for dependency package archives (.tgz/.tar.gz)")
	cmd.Flags().StringVar(&cfg.DownloadDir, "download-dir", "", "directory to store downloaded dependency package archives")
	cmd.Flags().StringVar(&cfg.ConflictPolicy, "conflict-policy", string(fhirpackage.ConflictPolicyRootWins), "dependency conflict policy: root-wins or strict")
	cmd.Flags().StringVar(&cfg.OutputPath, "output", "", "write derived coverage plan JSON to a file")
	cmd.Flags().StringSliceVar(&cfg.IncludeResourceTypes, "include-resource", nil, "include only these resource types (repeatable)")
	cmd.Flags().StringSliceVar(&cfg.IncludeProfileURLs, "include-profile-url", nil, "include only these profile canonical URLs (repeatable)")
	cmd.Flags().StringSliceVar(&cfg.ExcludePathPrefixes, "exclude-path-prefix", nil, "exclude element paths by prefix (repeatable)")
	cmd.Flags().BoolVar(&cfg.MustSupportOnly, "must-support-only", false, "derive only elements marked mustSupport")
	cmd.Flags().BoolVar(&cfg.IncludeOptional, "include-optional", false, "include optional non-mustSupport elements")
	cmd.Flags().BoolVar(&cfg.IncludeLowValuePaths, "include-low-value-paths", false, "include low-value infrastructure paths like meta/text/language")
	cmd.Flags().BoolVar(&cfg.IncludeUniversalSearch, "include-universal-search", false, "include universal FHIR search parameters (_id, _count, _sort, _include, _summary, _filter) for every resource type even when the server's CapabilityStatement does not declare them")
	cmd.Flags().StringSliceVar(&cfg.IncludeDomains, "include-domain", nil, "include only these coverage domains (repeatable; e.g. search, operation)")
	cmd.Flags().StringSliceVar(&cfg.ExcludeVariants, "exclude-variant", nil, "exclude these coverage variants (repeatable; e.g. operation-delete, state-crud-sequence)")
	cmd.Flags().IntVar(&cfg.InteractionStrength, "strength", 1, "interaction strength: 1 = individual requirements, 2 = pairwise interactions")
	return cmd
}
