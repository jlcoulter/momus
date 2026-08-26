package main

import (
	"fmt"

	testcoverage "github.com/jlcoulter/momus/internal/core/coverage"
	"github.com/spf13/cobra"
)

// newDescribeCmd returns the "coverage describe" command. It reads a coverage
// plan JSON (from "coverage derive") and renders it as a human-readable Markdown
// document, so a developer can see at a glance what conformance coverage the
// plan asserts — grouped by domain with plain-English descriptions of each
// obligation.
func newDescribeCmd(cfg *config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "describe <path-to-coverage-plan.json>",
		Short: "Render a coverage plan as a human-readable document",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			plan, err := loadCoveragePlanFromFile(args[0])
			if err != nil {
				return err
			}
			if plan == nil {
				return fmt.Errorf("no coverage plan loaded from %s", args[0])
			}
			doc := testcoverage.DescribePlan(plan)
			if cfg.OutputPath != "" {
				if err := writeOutputFile(cfg.OutputPath, []byte(doc)); err != nil {
					return fmt.Errorf("write description to %s: %w", cfg.OutputPath, err)
				}
				fmt.Printf("Coverage description written to %s\n", cfg.OutputPath)
				return nil
			}
			fmt.Print(doc)
			return nil
		},
	}
	cmd.Flags().StringVar(&cfg.OutputPath, "output", "", "write the coverage description to a file (default: stdout)")
	return cmd
}
