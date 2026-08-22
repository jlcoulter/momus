package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	testconstraint "github.com/jlcoulter/momus/internal/fhir/constraintderive"
	fhirpackage "github.com/jlcoulter/momus/internal/fhir/package"
	"github.com/spf13/cobra"
)

// newConstraintsCmd returns the "coverage constraints" command.
func newConstraintsCmd(cfg *config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "constraints <path-to-package.tgz>",
		Short: "Derive the constraint model from resolved package definitions",
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
			reg, err := builder.BuildFromPackages(graph.Packages)
			if err != nil {
				return err
			}

			constraints, err := testconstraint.Derive(reg)
			if err != nil {
				return err
			}

			out, err := json.MarshalIndent(constraints, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal constraints: %w", err)
			}
			if err := writeDebugOutput(cfg.debug, "constraints.json", append(out, '\n')); err != nil {
				return err
			}

			if cfg.outputPath == "" {
				fmt.Println(string(out))
			} else {
				if err := writeOutputFile(cfg.outputPath, append(out, '\n')); err != nil {
					return fmt.Errorf("write constraints to %s: %w", cfg.outputPath, err)
				}
			}

			fmt.Printf("Derived %d constraints from %d resolved packages\n", len(constraints), len(graph.Packages))
			if cfg.outputPath != "" {
				fmt.Printf("Constraints written to %s\n", cfg.outputPath)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&cfg.depsDir, "deps-dir", "", "directory to search for dependency package archives (.tgz/.tar.gz)")
	cmd.Flags().StringVar(&cfg.downloadDir, "download-dir", "", "directory to store downloaded dependency package archives")
	cmd.Flags().StringVar(&cfg.conflictPolicy, "conflict-policy", string(fhirpackage.ConflictPolicyRootWins), "dependency conflict policy: root-wins or strict")
	cmd.Flags().StringVar(&cfg.outputPath, "output", "", "write derived constraints JSON to a file")
	return cmd
}
