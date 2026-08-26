package main

import (
	"os"

	fhirpackage "github.com/jlcoulter/momus/internal/fhir/package"
	"github.com/spf13/cobra"
)

// version is the Momus version. Bumped as part of releases. It is a var so
// release builds can inject the version at link time via
// -ldflags "-X main.version=<version>".
var version = "0.0.0"

// abstractResourceTypes are FHIR types with kind "resource" that are abstract
// base types and cannot be instantiated as concrete data.
var abstractResourceTypes = map[string]bool{
	"Resource":          true,
	"DomainResource":    true,
	"CanonicalResource": true,
	"MetadataResource":  true,
}

func main() {
	cfg := &config{}
	if err := newRootCmd(cfg).Execute(); err != nil {
		os.Exit(1)
	}
}

// newRootCmd assembles the full command tree, binding shared flags onto cfg.
func newRootCmd(cfg *config) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "momus",
		Short: "API and FHIR conformance testing framework",
	}
	rootCmd.Version = version
	rootCmd.PersistentFlags().StringVar(&cfg.ConfigFile, "config", "", "config file (default: ./momus.toml, else $HOME/.momus/config.toml)")
	rootCmd.PersistentFlags().BoolVar(&cfg.Debug, "debug", false, "enable verbose debug logging")
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if err := initializeViper(cfg, cmd); err != nil {
			return err
		}
		fhirpackage.SetDebug(cfg.Debug)
		return nil
	}

	packageCmd := &cobra.Command{
		Use:   "package",
		Short: "FHIR package operations",
	}
	packageCmd.AddCommand(newLoadCmd(cfg), newResolveCmd(cfg))

	coverageCmd := &cobra.Command{
		Use:   "coverage",
		Short: "Coverage planning operations",
	}
	coverageCmd.AddCommand(
		newDeriveCmd(cfg),
		newConstraintsCmd(cfg),
		newAstCmd(cfg),
		newProvisionCmd(cfg),
		newRunCmd(cfg),
		newPlanCmd(cfg),
		newBulkCmd(cfg),
		newExplainCmd(cfg),
		newDescribeCmd(cfg),
		newKarateCmd(cfg),
	)

	rootCmd.AddCommand(packageCmd, coverageCmd, newApiCmd(cfg), newMockCmd(cfg), newTestCmd(cfg), newValidateCmd(cfg), newConformanceCmd(cfg))
	return rootCmd
}

// newConformanceCmd returns the "conformance" command group.
func newConformanceCmd(cfg *config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "conformance",
		Short: "Self-conformance operations",
	}
	cmd.AddCommand(newConformanceSelfTestCmd(cfg))
	return cmd
}
