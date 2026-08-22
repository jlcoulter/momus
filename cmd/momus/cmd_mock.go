package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/jlcoulter/momus/internal/mock"
	"github.com/spf13/cobra"
)

// newMockCmd returns the "mock" command, which starts a minimal mock HTTP
// server that responds to every request with a fixed status and body.
func newMockCmd(cfg *config) *cobra.Command {
	var status int
	var body string
	var port int

	cmd := &cobra.Command{
		Use:   "mock",
		Short: "Start a minimal mock HTTP server",
		RunE: func(cmd *cobra.Command, args []string) error {
			s := mock.New(status, body, mock.WithPort(port))
			addr, err := s.Start()
			if err != nil {
				return err
			}
			defer s.Close()

			fmt.Printf("Mock server listening on http://%s (status %d)\n", addr, status)
			fmt.Println("Press Ctrl-C to stop.")

			// Block until the process receives an interrupt or termination
			// signal, then shut the server down cleanly.
			sig := make(chan os.Signal, 1)
			signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
			<-sig
			return nil
		},
	}
	cmd.Flags().IntVar(&status, "status", 200, "HTTP status code to return for every request")
	cmd.Flags().StringVar(&body, "body", "", "response body to return for every request")
	cmd.Flags().IntVar(&port, "port", 0, "port to listen on (default: ephemeral)")
	return cmd
}
