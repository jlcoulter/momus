package main

import (
	"github.com/spf13/cobra"
)

// newTestCmd returns the "test" command group, which runs the full end-to-end
// conformance pipeline for a given server type. Each subcommand (e.g. "fhir")
// is a server-type-specific front-end that produces a test plan, which is then
// executed by the same generic back-end (provision, execute, evaluate, report)
// shared across all server types.
func newTestCmd(cfg *config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Run the full end-to-end conformance test pipeline",
		Long: "Run the full end-to-end conformance test pipeline for a server type.\n\n" +
			"Each subcommand is a server-type-specific front-end that turns a source\n" +
			"artifact (e.g. a FHIR package) into a test plan, which is then executed\n" +
			"by the same generic back-end shared across all server types.",
	}
	cmd.AddCommand(newTestFhirCmd(cfg), newTestOpenapiCmd(cfg))
	return cmd
}
