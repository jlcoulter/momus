package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/jlcoulter/momus/internal/mock"
	"github.com/spf13/cobra"
)

// newMockCmd returns the "mock" command, which starts a mock HTTP server. By
// default it is plan-aware: it holds resources in memory and serves real FHIR
// semantics (PUT/POST store, GET retrieves, DELETE removes, search returns a
// Bundle), and with --plan it reads a test plan to reject the requests the plan
// expects to be rejected. With --fixed it instead responds to every request with
// a fixed status and body.
func newMockCmd(cfg *config) *cobra.Command {
	var status int
	var body string
	var port int
	var planPath string
	var basePath string
	var fixed bool
	var semantic bool
	var pkgPath string

	cmd := &cobra.Command{
		Use:   "mock",
		Short: "Start a mock HTTP server (plan-aware FHIR by default, or fixed)",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := []mock.Option{mock.WithPort(port), mock.WithBasePath(basePath)}
			mode := "plan-aware"
			switch {
			case fixed:
				// Fixed mode: respond with the configured status/body.
				mode = "fixed"
			case planPath != "":
				// Plan-aware with reject routes from a plan file.
				opts = append(opts, mock.WithPlan(planPath))
			default:
				// Plan-aware with an empty reject set (real FHIR semantics).
				opts = append(opts, mock.WithPlanAware())
			}
			// Semantic mode: wire the profile validator so the mock rejects
			// non-conformant PUT/POST payloads with 422 + OperationOutcome.
			if semantic {
				if pkgPath == "" {
					return fmt.Errorf("--semantic requires --package to load the profiles to validate against")
				}
				graph, reg, err := resolvePackageGraph(cfg, pkgPath)
				if err != nil {
					return fmt.Errorf("resolve package %s: %w", pkgPath, err)
				}
				_ = graph
				opts = append(opts, mock.WithValidator(mockValidatorAdapterFrom(reg)))
				mode = "plan-aware + semantic validation"
			}
			s := mock.New(status, body, opts...)
			addr, err := s.Start()
			if err != nil {
				return err
			}
			defer s.Close()

			fmt.Printf("Mock server listening on http://%s (mode: %s)\n", addr, mode)
			fmt.Println("Press Ctrl-C to stop.")

			// Block until the process receives an interrupt or termination
			// signal, then shut the server down cleanly.
			sig := make(chan os.Signal, 1)
			signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
			<-sig
			return nil
		},
	}
	cmd.Flags().IntVar(&status, "status", 200, "HTTP status code to return for every request (fixed mode)")
	cmd.Flags().StringVar(&body, "body", "", "response body to return for every request (fixed mode)")
	cmd.Flags().IntVar(&port, "port", 0, "port to listen on (default: ephemeral)")
	cmd.Flags().StringVar(&planPath, "plan", "", "path to a test plan JSON; loads its reject routes (plan-aware mode)")
	cmd.Flags().StringVar(&basePath, "base-path", "/fhir", "base path the server serves under (e.g. /fhir); stripped from request paths before routing")
	cmd.Flags().BoolVar(&fixed, "fixed", false, "respond to every request with a fixed status and body instead of plan-aware FHIR semantics")
	cmd.Flags().BoolVar(&semantic, "semantic", false, "enforce profile conformance on writes using a validator (requires --package)")
	cmd.Flags().StringVar(&pkgPath, "package", "", "FHIR package archive to load profiles from for --semantic validation")
	return cmd
}
