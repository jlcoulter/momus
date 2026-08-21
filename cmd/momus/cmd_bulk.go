package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	testbulk "github.com/jlcoulter/momus/internal/fhir/bulk"
	"github.com/jlcoulter/momus/internal/fhir/model"
	fhirpackage "github.com/jlcoulter/momus/internal/fhir/package"
	"github.com/spf13/cobra"
)

// newBulkCmd returns the "coverage bulk" command.
func newBulkCmd(cfg *config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bulk <path-to-package.tgz>",
		Short: "Generate NDJSON bulk data from derived coverage requirements",
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

			resourceTypes := cfg.includeResourceTypes
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

			if cfg.bulkCount < 0 {
				return fmt.Errorf("--count must be non-negative, got %d", cfg.bulkCount)
			}

			corpusGenerator := testbulk.NewCorpusGenerator(reg, cfg.exhaustive)
			corpus, err := corpusGenerator.GenerateCorpus(cmd.Context(), resourceTypes, cfg.bulkCount, parsePerTypeCounts(cfg.bulkPerTypeCounts))
			if err != nil {
				return err
			}

			var out io.Writer = os.Stdout
			var f *os.File
			if cfg.outputPath != "" {
				if dir := filepath.Dir(cfg.outputPath); dir != "" && dir != "." {
					if err := os.MkdirAll(dir, 0o755); err != nil {
						return fmt.Errorf("create bulk output dir %s: %w", dir, err)
					}
				}
				f, err = os.Create(cfg.outputPath)
				if err != nil {
					return fmt.Errorf("create bulk file %s: %w", cfg.outputPath, err)
				}
				defer f.Close()
				out = f
			}

			w := testbulk.NewWriter(out)
			instances := testbulk.Link([]*model.Dataset{corpus})
			if err := writeDebugBulk(cfg.debug, instances); err != nil {
				return err
			}
			if err := w.WriteInstances(instances); err != nil {
				return err
			}
			if err := w.Close(); err != nil {
				return err
			}

			fmt.Printf("Generated NDJSON bulk data: %d resources across %d resource types\n", len(instances), len(resourceTypes))
			if cfg.outputPath != "" {
				fmt.Printf("Bulk data written to %s\n", cfg.outputPath)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&cfg.depsDir, "deps-dir", "", "directory to search for dependency package archives (.tgz/.tar.gz)")
	cmd.Flags().StringVar(&cfg.downloadDir, "download-dir", "", "directory to store downloaded dependency package archives")
	cmd.Flags().StringVar(&cfg.conflictPolicy, "conflict-policy", string(fhirpackage.ConflictPolicyRootWins), "dependency conflict policy: root-wins or strict")
	cmd.Flags().StringVar(&cfg.outputPath, "output", "", "write NDJSON bulk data to a file")
	cmd.Flags().BoolVar(&cfg.exhaustive, "exhaustive", true, "populate optional elements to produce fuller, more complete resources")
	cmd.Flags().IntVar(&cfg.bulkCount, "count", 25, "number of resources to generate per resource type")
	cmd.Flags().StringSliceVar(&cfg.bulkPerTypeCounts, "per-type", nil, "per-type resource counts as Type=Count (repeatable); overrides --count")
	cmd.Flags().StringSliceVar(&cfg.includeResourceTypes, "include-resource", nil, "include only these resource types (repeatable); referenced target types are added automatically")
	return cmd
}
