package main

import (
	"fmt"

	coregeneration "github.com/jlcoulter/momus/internal/core/generation"
	testgeneration "github.com/jlcoulter/momus/internal/fhir/generation"
	fhirpackage "github.com/jlcoulter/momus/internal/fhir/package"
	"github.com/spf13/cobra"
)

// newAstCmd returns the "coverage ast" command.
func newAstCmd(cfg *config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ast <path-to-package.tgz>",
		Short: "Generate a test plan (seed dataset + test AST) from derived coverage requirements",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rootPath := args[0]
			graph, reg, err := resolvePackageGraph(cfg, rootPath)
			if err != nil {
				return err
			}
			// Overlay the package's own CapabilityStatement as the reduced scope
			// over the full registry: the registry indexes every package resource
			// in full, but test generation is scoped to what the package declares
			// it serves (server-mode resource types and supported profiles). This
			// is distinct from the live-server capability fetch below; it applies
			// the package's declared scope even when no server is reachable.
			reg.OverlayCapabilityScope()

			coverageResourceTypes, coverageProfileURLs, preferredProfilesByResource, coverageSearchCodes, err := resourceScopeForRun(cmd, cfg, newDebugTracer(cfg.Debug))
			if err != nil {
				return err
			}

			coveragePlan, err := deriveCoveragePlan(cfg, reg, coverageResourceTypes, coverageProfileURLs, coverageSearchCodes)
			if err != nil {
				return err
			}

			astPlan, err := buildTestPlan(cfg, reg, coveragePlan, preferredProfilesByResource, coverageResourceTypes, coverageProfileURLs)
			if err != nil {
				return err
			}
			setupDataset := testgeneration.FromCoreDataset(astPlan.Dataset)

			// The server's CapabilityStatement defines the test plan: derivation was
			// scoped to the resource types/profiles it declares. Surface hard evidence
			// that the plan we are about to write only sends server-supported things.
			ev := verifyPlanAgainstCapability(cmd.Context(), cfg, reg, setupDataset)
			reportCapabilityEvidence(ev, cfg.BaseURL)

			out, err := encodeTestPlan(astPlan)
			if err != nil {
				return fmt.Errorf("encode test plan: %w", err)
			}
			if err := writeDebugOutput(cfg.Debug, "test-plan.json", append(out, '\n')); err != nil {
				return err
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
	cmd.Flags().StringVar(&cfg.OutputPath, "output", "", "write generated AST plan JSON to a file")
	cmd.Flags().StringSliceVar(&cfg.IncludeResourceTypes, "include-resource", nil, "include only these resource types (repeatable)")
	cmd.Flags().StringSliceVar(&cfg.IncludeProfileURLs, "include-profile-url", nil, "include only these profile canonical URLs (repeatable)")
	cmd.Flags().StringSliceVar(&cfg.ExcludePathPrefixes, "exclude-path-prefix", nil, "exclude element paths by prefix (repeatable)")
	cmd.Flags().BoolVar(&cfg.MustSupportOnly, "must-support-only", false, "derive only elements marked mustSupport")
	cmd.Flags().BoolVar(&cfg.IncludeOptional, "include-optional", false, "include optional non-mustSupport elements")
	cmd.Flags().BoolVar(&cfg.IncludeLowValuePaths, "include-low-value-paths", false, "include low-value infrastructure paths like meta/text/language")
	cmd.Flags().BoolVar(&cfg.IncludeUniversalSearch, "include-universal-search", false, "include universal FHIR search parameters (_id, _count, _sort, _include, _summary, _filter) for every resource type even when the server's CapabilityStatement does not declare them")
	cmd.Flags().StringSliceVar(&cfg.IncludeDomains, "include-domain", nil, "include only these coverage domains (repeatable; e.g. search, operation)")
	cmd.Flags().StringSliceVar(&cfg.ExcludeVariants, "exclude-variant", nil, "exclude these coverage variants (repeatable; e.g. operation-delete, state-crud-sequence)")
	cmd.Flags().StringVar(&cfg.BaseURL, "base-url", "", "target FHIR base URL for request nodes")
	cmd.Flags().StringVar(&cfg.WriteBaseURL, "write-base-url", "", "alternate FHIR base URL for resource creation (write) request nodes; defaults to --base-url")
	cmd.Flags().StringVar(&cfg.CapabilityBaseURL, "capability-base-url", "", "optional alternate FHIR base URL to fetch CapabilityStatement metadata for scope/profile selection")
	cmd.Flags().StringVar(&cfg.MetadataFile, "metadata", "", "path to a local CapabilityStatement JSON file to use for scope/profile selection and capability evidence instead of fetching /metadata")
	cmd.Flags().IntVar(&cfg.InteractionStrength, "strength", 1, "interaction strength: 1 = individual requirements, 2 = pairwise interactions")
	cmd.Flags().BoolVar(&cfg.Exhaustive, "exhaustive", true, "populate optional elements to produce fuller, more realistic payloads")
	return cmd
}
