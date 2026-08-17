// Command momus is the entry point for the Momus API and FHIR conformance
// testing framework.
package main

import (
	"fmt"
	"os"

	fhirpackage "github.com/jlcoulter/momus/internal/fhir/package"
	"github.com/spf13/cobra"
)

// version is the Momus version. Bumped as part of releases.
const version = "0.0.0"

func main() {
	rootCmd := &cobra.Command{
		Use:   "momus",
		Short: "API and FHIR conformance testing framework",
	}

	rootCmd.Version = version

	packageCmd := &cobra.Command{
		Use:   "package",
		Short: "FHIR package operations",
	}

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

	packageCmd.AddCommand(loadCmd)
	rootCmd.AddCommand(packageCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
