package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	testbulk "github.com/jlcoulter/momus/internal/fhir/bulk"
	"github.com/jlcoulter/momus/internal/fhir/model"
	fhirpackage "github.com/jlcoulter/momus/internal/fhir/package"
	"github.com/jlcoulter/momus/internal/home"
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
			cacheDir := cfg.DownloadDir
			if cacheDir == "" {
				cacheDir = home.PackageCacheDir()
			}

			graph, err := fhirpackage.ResolveLocalPackageGraphWithOptions(rootPath, fhirpackage.ResolveOptions{
				DepsDir:        cfg.DepsDir,
				DownloadDir:    cacheDir,
				ConflictPolicy: fhirpackage.ConflictPolicy(cfg.ConflictPolicy),
			})
			if err != nil {
				return err
			}
			if err := writeDebugGraph(cfg.Debug, graph); err != nil {
				return err
			}

			builder := fhirpackage.NewRegistryBuilder()
			reg, err := builder.BuildFromPackagesScoped(graph.Packages, graph.Root)
			if err != nil {
				return err
			}

			resourceTypes := cfg.IncludeResourceTypes
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

			if cfg.BulkCount < 0 {
				return fmt.Errorf("--count must be non-negative, got %d", cfg.BulkCount)
			}

			corpusGenerator := testbulk.NewCorpusGenerator(reg, cfg.Exhaustive)
			corpus, err := corpusGenerator.GenerateCorpus(cmd.Context(), resourceTypes, cfg.BulkCount, parsePerTypeCounts(cfg.BulkPerTypeCounts))
			if err != nil {
				return err
			}

			var out io.Writer = os.Stdout
			var f *os.File
			if cfg.OutputPath != "" {
				outputPath, err := resolveBulkOutputPath(cfg.OutputPath)
				if err != nil {
					return err
				}
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
				cfg.OutputPath = outputPath
			}

			w := testbulk.NewWriter(out)
			instances := testbulk.Link([]*model.Dataset{corpus})
			if err := writeDebugBulk(cfg.Debug, instances); err != nil {
				return err
			}
			if err := w.WriteInstances(instances); err != nil {
				return err
			}
			if err := w.Close(); err != nil {
				return err
			}

			fmt.Printf("Generated NDJSON bulk data: %d resources across %d resource types\n", len(instances), len(resourceTypes))
			if cfg.OutputPath != "" {
				fmt.Printf("Bulk data written to %s\n", cfg.OutputPath)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&cfg.DepsDir, "deps-dir", "", "directory to search for dependency package archives (.tgz/.tar.gz)")
	cmd.Flags().StringVar(&cfg.DownloadDir, "download-dir", "", "directory to store downloaded dependency package archives")
	cmd.Flags().StringVar(&cfg.ConflictPolicy, "conflict-policy", string(fhirpackage.ConflictPolicyRootWins), "dependency conflict policy: root-wins or strict")
	cmd.Flags().StringVar(&cfg.OutputPath, "output", "", "write NDJSON bulk data to a file")
	cmd.Flags().BoolVar(&cfg.Exhaustive, "exhaustive", true, "populate optional elements to produce fuller, more complete resources")
	cmd.Flags().IntVar(&cfg.BulkCount, "count", 25, "number of resources to generate per resource type")
	cmd.Flags().StringSliceVar(&cfg.BulkPerTypeCounts, "per-type", nil, "per-type resource counts as Type=Count (repeatable); overrides --count")
	cmd.Flags().StringSliceVar(&cfg.IncludeResourceTypes, "include-resource", nil, "include only these resource types (repeatable); referenced target types are added automatically")
	return cmd
}

func resolveBulkOutputPath(outputPath string) (string, error) {
	if outputPath == "" {
		return "", nil
	}
	if strings.HasSuffix(outputPath, string(os.PathSeparator)) {
		return filepath.Join(outputPath, "bulk.ndjson"), nil
	}
	info, err := os.Stat(outputPath)
	if err == nil && info.IsDir() {
		return filepath.Join(outputPath, "bulk.ndjson"), nil
	}
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("stat bulk output path %s: %w", outputPath, err)
	}
	return outputPath, nil
}
