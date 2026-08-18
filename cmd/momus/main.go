// Command momus is the entry point for the Momus API and FHIR conformance
// testing framework.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	fhirpackage "github.com/jlcoulter/momus/internal/fhir/package"
	"github.com/spf13/cobra"
)

// version is the Momus version. Bumped as part of releases.
const version = "0.0.0"

func main() {
	var debug bool

	rootCmd := &cobra.Command{
		Use:   "momus",
		Short: "API and FHIR conformance testing framework",
	}

	rootCmd.Version = version
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "enable verbose debug logging")
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		fhirpackage.SetDebug(debug)
	}

	packageCmd := &cobra.Command{
		Use:   "package",
		Short: "FHIR package operations",
	}
	var depsDir string
	var downloadDir string
	var conflictPolicy string

	loadCmd := &cobra.Command{
		Use:   "load <path-to-package.tgz>",
		Short: "Load and decode a FHIR package archive",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pkg, err := fhirpackage.ReadPackage(args[0])
			if err != nil {
				return err
			}

			fmt.Printf("Loaded package %s@%s with %d dependencies and %d resources\n",
				pkg.Name,
				pkg.Version,
				len(pkg.Dependencies),
				len(pkg.Resources),
			)
			return nil
		},
	}

	resolveCmd := &cobra.Command{
		Use:   "resolve <path-to-package.tgz>",
		Short: "Resolve a package and its transitive local dependencies",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rootPath := args[0]
			searchDir := depsDir
			if searchDir == "" {
				searchDir = filepath.Dir(rootPath)
			}
			cacheDir := downloadDir
			if cacheDir == "" {
				cacheDir = filepath.Join(searchDir, ".momus", "packages")
			}

			graph, err := fhirpackage.ResolveLocalPackageGraphWithOptions(rootPath, fhirpackage.ResolveOptions{
				DepsDir:        searchDir,
				DownloadDir:    cacheDir,
				ConflictPolicy: fhirpackage.ConflictPolicy(conflictPolicy),
			})
			if err != nil {
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
	resolveCmd.Flags().StringVar(&depsDir, "deps-dir", "", "directory to search for dependency package archives (.tgz/.tar.gz)")
	resolveCmd.Flags().StringVar(&downloadDir, "download-dir", "", "directory to store downloaded dependency package archives")
	resolveCmd.Flags().StringVar(&conflictPolicy, "conflict-policy", string(fhirpackage.ConflictPolicyRootWins), "dependency conflict policy: root-wins or strict")

	packageCmd.AddCommand(loadCmd)
	packageCmd.AddCommand(resolveCmd)
	rootCmd.AddCommand(packageCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
