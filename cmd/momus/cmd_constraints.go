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
			searchDir := cfg.DepsDir
			if searchDir == "" {
				searchDir = filepath.Dir(rootPath)
			}
			cacheDir := cfg.DownloadDir
			if cacheDir == "" {
				cacheDir = filepath.Join(searchDir, ".momus", "packages")
			}

			graph, err := fhirpackage.ResolveLocalPackageGraphWithOptions(rootPath, fhirpackage.ResolveOptions{
				DepsDir:        searchDir,
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
			if err := writeDebugOutput(cfg.Debug, "constraints.json", append(out, '\n')); err != nil {
				return err
			}

			if cfg.OutputPath == "" {
				fmt.Println(string(out))
			} else {
				if err := writeOutputFile(cfg.OutputPath, append(out, '\n')); err != nil {
					return fmt.Errorf("write constraints to %s: %w", cfg.OutputPath, err)
				}
			}

			fmt.Printf("Derived %d constraints from %d resolved packages\n", len(constraints), len(graph.Packages))
			if cfg.OutputPath != "" {
				fmt.Printf("Constraints written to %s\n", cfg.OutputPath)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&cfg.DepsDir, "deps-dir", "", "directory to search for dependency package archives (.tgz/.tar.gz)")
	cmd.Flags().StringVar(&cfg.DownloadDir, "download-dir", "", "directory to store downloaded dependency package archives")
	cmd.Flags().StringVar(&cfg.ConflictPolicy, "conflict-policy", string(fhirpackage.ConflictPolicyRootWins), "dependency conflict policy: root-wins or strict")
	cmd.Flags().StringVar(&cfg.OutputPath, "output", "", "write derived constraints JSON to a file")
	return cmd
}
