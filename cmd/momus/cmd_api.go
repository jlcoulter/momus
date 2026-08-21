package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/jlcoulter/momus/internal/fhir/constraint"
	"github.com/jlcoulter/momus/internal/openapi"
	testast "github.com/jlcoulter/momus/internal/test/ast"
	testrunner "github.com/jlcoulter/momus/internal/test/runner"
	"github.com/spf13/cobra"
)

// newApiCmd returns the "api" command group (OpenAPI contract operations).
func newApiCmd(cfg *config) *cobra.Command {
	apiCmd := &cobra.Command{
		Use:   "api",
		Short: "OpenAPI contract operations",
	}

	apiCmd.AddCommand(newApiConstraintsCmd(cfg), newApiAstCmd(cfg), newApiRunCmd(cfg))
	return apiCmd
}

// newApiConstraintsCmd returns the "api constraints" command.
func newApiConstraintsCmd(cfg *config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "constraints <path-to-openapi.json>",
		Short: "Derive the constraint model from an OpenAPI document",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			doc, err := loadOpenAPIDocument(args[0])
			if err != nil {
				return err
			}
			constraints := openapi.DeriveConstraints(doc)
			out, err := json.MarshalIndent(constraints, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal api constraints: %w", err)
			}

			if cfg.outputPath == "" {
				fmt.Println(string(out))
			} else {
				if err := writeOutputFile(cfg.outputPath, append(out, '\n')); err != nil {
					return fmt.Errorf("write api constraints to %s: %w", cfg.outputPath, err)
				}
			}

			fmt.Printf("Derived %d API constraints (%d operations, %d parameters) from %s\n",
				len(constraints), operationConstraintCount(constraints), parameterConstraintCount(constraints), args[0])
			if cfg.outputPath != "" {
				fmt.Printf("API constraints written to %s\n", cfg.outputPath)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&cfg.outputPath, "output", "", "write derived API constraints JSON to a file")
	return cmd
}

// newApiAstCmd returns the "api ast" command (generate the test AST).
func newApiAstCmd(cfg *config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ast <path-to-openapi.json>",
		Short: "Generate a test AST from an OpenAPI document",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			doc, err := loadOpenAPIDocument(args[0])
			if err != nil {
				return err
			}
			plan, err := openapi.GeneratePlan(doc, cfg.baseURL, cfg.writeBaseURL)
			if err != nil {
				return err
			}
			encoded, err := testast.EncodePlan(plan)
			if err != nil {
				return err
			}
			out, err := json.MarshalIndent(encoded, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal api ast: %w", err)
			}

			if cfg.outputPath == "" {
				fmt.Println(string(out))
			} else {
				if err := writeOutputFile(cfg.outputPath, append(out, '\n')); err != nil {
					return fmt.Errorf("write api ast to %s: %w", cfg.outputPath, err)
				}
			}

			fmt.Printf("Generated AST with %d operation cases from %s\n", len(doc.Paths), args[0])
			if cfg.outputPath != "" {
				fmt.Printf("API AST written to %s\n", cfg.outputPath)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&cfg.outputPath, "output", "", "write generated API AST JSON to a file")
	cmd.Flags().StringVar(&cfg.baseURL, "base-url", "", "target API base URL for request nodes")
	cmd.Flags().StringVar(&cfg.writeBaseURL, "write-base-url", "", "alternate API base URL for write (POST/PUT/PATCH) request nodes; defaults to --base-url")
	return cmd
}

// newApiRunCmd returns the "api run" command (generate and execute the AST).
func newApiRunCmd(cfg *config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run <path-to-openapi.json>",
		Short: "Generate and execute tests against an OpenAPI document",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cfg.baseURL == "" {
				return fmt.Errorf("base URL is required; provide --base-url")
			}
			doc, err := loadOpenAPIDocument(args[0])
			if err != nil {
				return err
			}
			plan, err := openapi.GeneratePlan(doc, cfg.baseURL, cfg.writeBaseURL)
			if err != nil {
				return err
			}
			report, err := testrunner.Execute(cmd.Context(), plan.Root, testrunner.ExecuteOptions{
				BaseURL:      cfg.baseURL,
				WriteBaseURL: cfg.writeBaseURL,
				Tracer:       newDebugTracer(cfg.debug),
			})
			if err != nil {
				return err
			}
			out, err := json.MarshalIndent(report, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal api report: %w", err)
			}

			if cfg.outputPath == "" {
				fmt.Println(string(out))
			} else {
				if err := writeOutputFile(cfg.outputPath, append(out, '\n')); err != nil {
					return fmt.Errorf("write api report to %s: %w", cfg.outputPath, err)
				}
			}

			fmt.Printf("Executed %d API operations: %d passed, %d failed\n", report.Total, report.Passed, report.Failed)
			if cfg.outputPath != "" {
				fmt.Printf("API report written to %s\n", cfg.outputPath)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&cfg.outputPath, "output", "", "write API test report JSON to a file")
	cmd.Flags().StringVar(&cfg.baseURL, "base-url", "", "target API base URL for request execution")
	cmd.Flags().StringVar(&cfg.writeBaseURL, "write-base-url", "", "alternate API base URL for write (POST/PUT/PATCH) request execution; defaults to --base-url")
	return cmd
}

// loadOpenAPIDocument reads and parses an OpenAPI document from a file path.
func loadOpenAPIDocument(path string) (*openapi.Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read openapi document %s: %w", path, err)
	}
	return openapi.ParseJSON(data)
}

func operationConstraintCount(constraints []constraint.Constraint) int {
	count := 0
	for _, c := range constraints {
		if c.Kind == constraint.KindAPIOperation {
			count++
		}
	}
	return count
}

func parameterConstraintCount(constraints []constraint.Constraint) int {
	count := 0
	for _, c := range constraints {
		if c.Kind == constraint.KindAPIParameter {
			count++
		}
	}
	return count
}
