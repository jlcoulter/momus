// Package mock provides a minimal HTTP server that returns a fixed response
// for every request. It is useful as a stand-in target when developing or
// exercising test plans without a real server.
package mock

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
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

	r := chi.NewRouter()
	// Basic middleware boilerplate: a request id, real client IP, request
	// logging, and panic recovery.
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Handle("/*", http.HandlerFunc(s.handle))

	s.server = &http.Server{
		Handler: r,
	}
	go func() {
		if err := s.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("mock server: %v", err)
		}
	}()
	return ln.Addr().String(), nil
}

// handle writes the fixed status and body for any request.
func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(s.status)
	if s.body != "" {
		_, _ = w.Write([]byte(s.body))
	}
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
