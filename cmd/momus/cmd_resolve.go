package main

import (
	"fmt"
	"path/filepath"

	fhirpackage "github.com/jlcoulter/momus/internal/fhir/package"
	"github.com/jlcoulter/momus/internal/home"
	"github.com/spf13/cobra"
)

// newResolveCmd returns the "package resolve" command.
func newResolveCmd(cfg *config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resolve <path-to-package.tgz>",
		Short: "Resolve a package and its transitive local dependencies",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rootPath := args[0]
			searchDir := cfg.DepsDir
			if searchDir == "" {
				searchDir = filepath.Dir(rootPath)
			}
			cacheDir := cfg.DownloadDir
			if cacheDir == "" {
				cacheDir = home.PackageCacheDir()
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

			totalResources := 0
			for _, p := range graph.Packages {
				totalResources += len(p.Resources)
			}

			fmt.Printf("Resolved %d packages from %s using download dir %s with %d total resources\n", len(graph.Packages), searchDir, cacheDir, totalResources)
			for _, p := range graph.Packages {
				fmt.Printf("- %s@%s (deps=%d, resources=%d)\n", p.Name, p.Version, len(p.Dependencies), len(p.Resources))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&cfg.DepsDir, "deps-dir", "", "directory to search for dependency package archives (.tgz/.tar.gz)")
	cmd.Flags().StringVar(&cfg.DownloadDir, "download-dir", "", "directory to store downloaded dependency package archives")
	cmd.Flags().StringVar(&cfg.ConflictPolicy, "conflict-policy", string(fhirpackage.ConflictPolicyRootWins), "dependency conflict policy: root-wins or strict")
	return cmd
}
