package main

import (
	"fmt"

	coregeneration "github.com/jlcoulter/momus/internal/core/generation"
	testgeneration "github.com/jlcoulter/momus/internal/fhir/generation"
	fhirpackage "github.com/jlcoulter/momus/internal/fhir/package"
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
			graph, reg, err := resolvePackageGraph(cfg, rootPath)
			if err != nil {
				return err
			}

			coveragePlan, err := deriveCoveragePlan(cfg, reg, cfg.IncludeResourceTypes, cfg.IncludeProfileURLs, nil)
			if err != nil {
				return err
			}

			astPlan, err := buildTestPlan(cfg, reg, coveragePlan, nil, nil, nil)
			if err != nil {
				return err
			}
			setupDataset := testgeneration.FromCoreDataset(astPlan.Dataset)

			out, err := encodeTestPlan(astPlan)
			if err != nil {
				return fmt.Errorf("encode test plan: %w", err)
			}
			if cfg.OutputPath == "" {
				fmt.Println(string(out))
			} else {
				if err := writeOutputFile(cfg.OutputPath, append(out, '\n')); err != nil {
					return fmt.Errorf("write test plan to %s: %w", cfg.OutputPath, err)
				}
			}

			fmt.Printf("Generated test plan with %d requirement cases and %d seed resources from %d resolved packages\n", coregeneration.RequirementCount(astPlan), len(setupDataset.Resources), len(graph.Packages))
			if cfg.OutputPath != "" {
				fmt.Printf("Test plan written to %s\n", cfg.OutputPath)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&cfg.DepsDir, "deps-dir", "", "directory to search for dependency package archives (.tgz/.tar.gz)")
	cmd.Flags().StringVar(&cfg.DownloadDir, "download-dir", "", "directory to store downloaded dependency package archives")
	cmd.Flags().StringVar(&cfg.ConflictPolicy, "conflict-policy", string(fhirpackage.ConflictPolicyRootWins), "dependency conflict policy: root-wins or strict")
	cmd.Flags().StringVar(&cfg.OutputPath, "output", "", "write generated test plan JSON to a file")
	cmd.Flags().StringVar(&cfg.BaseURL, "base-url", "", "target FHIR base URL for request nodes")
	cmd.Flags().StringVar(&cfg.WriteBaseURL, "write-base-url", "", "alternate FHIR base URL for resource creation (write) request nodes; defaults to --base-url")
	cmd.Flags().StringSliceVar(&cfg.IncludeResourceTypes, "include-resource", nil, "include only these resource types (repeatable)")
	cmd.Flags().StringSliceVar(&cfg.IncludeProfileURLs, "include-profile-url", nil, "include only these profile canonical URLs (repeatable)")
	cmd.Flags().StringSliceVar(&cfg.ExcludePathPrefixes, "exclude-path-prefix", nil, "exclude element paths by prefix (repeatable)")
	cmd.Flags().BoolVar(&cfg.MustSupportOnly, "must-support-only", false, "derive only elements marked mustSupport")
	cmd.Flags().BoolVar(&cfg.IncludeOptional, "include-optional", false, "include optional non-mustSupport elements")
	cmd.Flags().BoolVar(&cfg.IncludeLowValuePaths, "include-low-value-paths", false, "include low-value infrastructure paths like meta/text/language")
	cmd.Flags().BoolVar(&cfg.IncludeUniversalSearch, "include-universal-search", false, "include universal FHIR search parameters (_id, _count, _sort, _include, _summary, _filter) for every resource type even when the server's CapabilityStatement does not declare them")
	cmd.Flags().StringSliceVar(&cfg.IncludeDomains, "include-domain", nil, "include only these coverage domains (repeatable; e.g. search, operation)")
	cmd.Flags().StringSliceVar(&cfg.ExcludeVariants, "exclude-variant", nil, "exclude these coverage variants (repeatable; e.g. operation-delete, state-crud-sequence)")
	cmd.Flags().StringSliceVar(&cfg.ExcludeExtensionURLs, "exclude-extension-url", nil, "exclude obligations derived from an extension by its canonical profile URL (repeatable; e.g. http://.../StructureDefinition/suppressed)")
	cmd.Flags().IntVar(&cfg.InteractionStrength, "strength", 1, "interaction strength: 1 = individual requirements, 2 = pairwise interactions")
	cmd.Flags().BoolVar(&cfg.Exhaustive, "exhaustive", true, "populate optional elements to produce fuller, more realistic resources")
	return cmd
}
