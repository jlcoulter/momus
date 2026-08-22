package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/jlcoulter/momus/internal/mock"
	"github.com/spf13/cobra"
)

// newMockCmd returns the "mock" command, which starts a mock HTTP server. In
// fixed mode it responds to every request with a fixed status and body. With
// --plan it behaves like a stateful FHIR server: it holds resources in memory
// and serves real FHIR semantics (PUT/POST store, GET retrieves, DELETE removes,
// search returns a Bundle), and it reads the test plan to reject the requests
// the plan expects to be rejected.
func newMockCmd(cfg *config) *cobra.Command {
	var status int
	var body string
	var port int
	var planPath string
	var basePath string

	cmd := &cobra.Command{
		Use:   "mock",
		Short: "Start a mock HTTP server (fixed or plan-aware FHIR)",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := []mock.Option{mock.WithPort(port), mock.WithBasePath(basePath)}
			if planPath != "" {
				opts = append(opts, mock.WithPlan(planPath))
			}
			s := mock.New(status, body, opts...)
			addr, err := s.Start()
			if err != nil {
				return err
			}
			defer s.Close()

			mode := "fixed"
			if planPath != "" {
				mode = "plan-aware"
			}
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
	cmd.Flags().StringVar(&planPath, "plan", "", "path to a test plan JSON; enables plan-aware FHIR mode with an in-memory store")
	cmd.Flags().StringVar(&basePath, "base-path", "/fhir", "base path the server serves under (e.g. /fhir); stripped from request paths before routing")
	return cmd
}
