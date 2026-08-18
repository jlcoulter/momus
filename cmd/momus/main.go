// Command momus is the entry point for the Momus API and FHIR conformance
// testing framework.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	fhirpackage "github.com/jlcoulter/momus/internal/fhir/package"
	testcoverage "github.com/jlcoulter/momus/internal/test/coverage"
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
	coverageCmd := &cobra.Command{
		Use:   "coverage",
		Short: "Coverage planning operations",
	}
	var depsDir string
	var downloadDir string
	var conflictPolicy string
	var outputPath string

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

	deriveCmd := &cobra.Command{
		Use:   "derive <path-to-package.tgz>",
		Short: "Derive MVP coverage obligations from resolved profile cardinality constraints",
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

			builder := fhirpackage.NewRegistryBuilder()
			reg, err := builder.BuildFromPackages(graph.Packages)
			if err != nil {
				return err
			}

			plan, err := testcoverage.DeriveMVPPlan(reg)
			if err != nil {
				return err
			}

			out, err := json.MarshalIndent(plan, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal coverage plan: %w", err)
			}

			if outputPath == "" {
				fmt.Println(string(out))
			} else {
				if err := os.WriteFile(outputPath, append(out, '\n'), 0o644); err != nil {
					return fmt.Errorf("write coverage plan to %s: %w", outputPath, err)
				}
			}

			domainCounts := make(map[testcoverage.CoverageDomain]int)
			for _, req := range plan.Requirements {
				domainCounts[req.Domain]++
			}

			fmt.Printf("Derived %d coverage requirements from %d resolved packages\n", len(plan.Requirements), len(graph.Packages))
			for domain, count := range domainCounts {
				fmt.Printf("- %s: %d\n", domain, count)
			}
			if outputPath != "" {
				fmt.Printf("Coverage plan written to %s\n", outputPath)
			}
			return nil
		},
	}
	deriveCmd.Flags().StringVar(&depsDir, "deps-dir", "", "directory to search for dependency package archives (.tgz/.tar.gz)")
	deriveCmd.Flags().StringVar(&downloadDir, "download-dir", "", "directory to store downloaded dependency package archives")
	deriveCmd.Flags().StringVar(&conflictPolicy, "conflict-policy", string(fhirpackage.ConflictPolicyRootWins), "dependency conflict policy: root-wins or strict")
	deriveCmd.Flags().StringVar(&outputPath, "output", "", "write derived coverage plan JSON to a file")

	packageCmd.AddCommand(loadCmd)
	packageCmd.AddCommand(resolveCmd)
	coverageCmd.AddCommand(deriveCmd)
	rootCmd.AddCommand(packageCmd)
	rootCmd.AddCommand(coverageCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
