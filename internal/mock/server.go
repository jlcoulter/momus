// Package mock provides a minimal HTTP server that returns a fixed response
// for every request. It is useful as a stand-in target when developing or
// exercising test plans without a real server.
package mock

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// Server is a minimal mock HTTP server that responds to every request with a
// fixed status code and body.
type Server struct {
	status int
	body   string
	server *http.Server
	ln     net.Listener
}

// New returns a mock server that responds with the given status and body.
func New(status int, body string) *Server {
	return &Server{status: status, body: body}
}

// Start binds the server to an ephemeral port and begins serving. It returns
// the address the server is listening on (e.g. "127.0.0.1:54321").
func (s *Server) Start() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("mock listen: %w", err)
	}
	s.ln = ln
	s.server = &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(s.status)
			if s.body != "" {
				_, _ = w.Write([]byte(s.body))
			}
		}),
	}
	go func() {
		_ = s.server.Serve(ln)
	}()
	return ln.Addr().String(), nil
}

// Close shuts the server down, waiting up to a short grace period for in-flight
// requests to complete.
func (s *Server) Close() error {
	if s.server == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.server.Shutdown(ctx)
}
