package main

import (
	"fmt"

	fhirpackage "github.com/jlcoulter/momus/internal/fhir/package"
	testgeneration "github.com/jlcoulter/momus/internal/test/generation"
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

			coverageResourceTypes, coverageProfileURLs, preferredProfilesByResource, coverageSearchCodes, err := resourceScopeForRun(cmd, cfg, newDebugTracer(cfg.debug))
			if err != nil {
				return err
			}

			coveragePlan, err := deriveCoveragePlan(cfg, reg, coverageResourceTypes, coverageProfileURLs, coverageSearchCodes)
			if err != nil {
				return err
			}

			astPlan, setupDataset, err := buildTestPlan(cfg, reg, coveragePlan, preferredProfilesByResource, coverageResourceTypes, coverageProfileURLs)
			if err != nil {
				return err
			}

			// The server's CapabilityStatement defines the test plan: derivation was
			// scoped to the resource types/profiles it declares. Surface hard evidence
			// that the plan we are about to write only sends server-supported things.
			ev := verifyPlanAgainstCapability(cmd.Context(), cfg, reg, setupDataset)
			reportCapabilityEvidence(ev, cfg.baseURL)

			out, err := encodeTestPlan(astPlan, setupDataset)
			if err != nil {
				return fmt.Errorf("encode test plan: %w", err)
			}
			if err := writeDebugOutput(cfg.debug, "test-plan.json", append(out, '\n')); err != nil {
				return err
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
	cmd.Flags().StringVar(&cfg.outputPath, "output", "", "write generated AST plan JSON to a file")
	cmd.Flags().StringSliceVar(&cfg.includeResourceTypes, "include-resource", nil, "include only these resource types (repeatable)")
	cmd.Flags().StringSliceVar(&cfg.includeProfileURLs, "include-profile-url", nil, "include only these profile canonical URLs (repeatable)")
	cmd.Flags().StringSliceVar(&cfg.excludePathPrefixes, "exclude-path-prefix", nil, "exclude element paths by prefix (repeatable)")
	cmd.Flags().BoolVar(&cfg.mustSupportOnly, "must-support-only", false, "derive only elements marked mustSupport")
	cmd.Flags().BoolVar(&cfg.includeOptional, "include-optional", false, "include optional non-mustSupport elements")
	cmd.Flags().BoolVar(&cfg.includeLowValuePaths, "include-low-value-paths", false, "include low-value infrastructure paths like meta/text/language")
	cmd.Flags().BoolVar(&cfg.includeUniversalSearch, "include-universal-search", false, "include universal FHIR search parameters (_id, _count, _sort, _include, _summary, _filter) for every resource type even when the server's CapabilityStatement does not declare them")
	cmd.Flags().StringVar(&cfg.baseURL, "base-url", "", "target FHIR base URL for request nodes")
	cmd.Flags().StringVar(&cfg.writeBaseURL, "write-base-url", "", "alternate FHIR base URL for resource creation (write) request nodes; defaults to --base-url")
	cmd.Flags().StringVar(&cfg.capabilityBaseURL, "capability-base-url", "", "optional alternate FHIR base URL to fetch CapabilityStatement metadata for scope/profile selection")
	cmd.Flags().StringVar(&cfg.metadataFile, "metadata", "", "path to a local CapabilityStatement JSON file to use for scope/profile selection and capability evidence instead of fetching /metadata")
	cmd.Flags().IntVar(&cfg.interactionStrength, "strength", 1, "interaction strength: 1 = individual requirements, 2 = pairwise interactions")
	cmd.Flags().BoolVar(&cfg.exhaustive, "exhaustive", true, "populate optional elements to produce fuller, more realistic payloads")
	return cmd
}
