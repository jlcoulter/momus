// Package mock provides a configurable HTTP server that behaves like a FHIR
// server. It can run in two modes:
//
//   - Plan-aware mode (the default): it holds resources in an in-memory store
//     and serves real FHIR semantics — PUT/POST store a resource, GET retrieves
//     it, DELETE removes it, and search returns a Bundle. It can also read a
//     test plan to learn which requests are expected to be rejected (negative
//     tests) and return the matching 4xx status.
//   - Fixed mode (--fixed): every request returns a fixed status and body.
//
// This makes it a useful stand-in target for exercising test plans end to end
// without a real server.
package mock

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jlcoulter/momus/internal/core/ast"
)

// Server is a mock HTTP server that either returns a fixed response or behaves
// like a stateful FHIR server driven by a test plan.
type Server struct {
	status    int
	body      string
	port      int
	basePath  string
	logger    bool
	plan      *planRoutes
	planErr   error
	store     *Store
	validator Validator
	server    *http.Server
	ln        net.Listener
	mu        sync.RWMutex
}

// Option configures a mock Server.
type Option func(*Server)

// WithPort sets the port the server binds to. When zero, an ephemeral port is
// chosen automatically.
func WithPort(port int) Option {
	return func(s *Server) { s.port = port }
}

// WithBasePath sets the base path the server serves under (e.g. "/fhir"). It is
// stripped from incoming request paths before routing, so a test plan targeting
// "http://host/fhir/Patient" hits the same handler as "/Patient". When empty,
// the server serves at the root.
func WithBasePath(basePath string) Option {
	return func(s *Server) { s.basePath = strings.TrimRight(basePath, "/") }
}

// WithLogger enables per-request logging to stderr (chi's Logger middleware).
// It is on by default for the standalone "mock" command, but the in-process mock
// used by "test --mock" disables it so it does not spam the terminal during a
// run.
func WithLogger(enabled bool) Option {
	return func(s *Server) { s.logger = enabled }
}

// WithPlan enables plan-aware mode, loading the reject routes from the given
// test plan file. When set, the server holds resources in memory and serves
// real FHIR semantics instead of a fixed response.
func WithPlan(planPath string) Option {
	return func(s *Server) {
		routes, err := loadPlanRoutes(planPath)
		if err != nil {
			// Defer the error to Start so the caller can surface it.
			s.planErr = err
			return
		}
		s.plan = routes
		s.store = NewStore()
	}
}

// WithPlanAware enables plan-aware mode with an empty reject-route set and an
// in-memory store, so the server serves real FHIR semantics immediately. The
// reject routes are filled in later via SetPlan once the test plan exists. This
// is used by the "test" command, which starts the mock before the plan is
// generated (the plan's base URL depends on the mock's address).
func WithPlanAware() Option {
	return func(s *Server) {
		s.plan = &planRoutes{rejects: make(map[string]rejectRoute)}
		s.store = NewStore()
	}
}

// SetPlan installs the reject routes from an in-memory test AST root and
// enables plan-aware mode. It is used by the "test" command, which starts the
// mock before the plan is generated (the plan's base URL depends on the mock's
// address), then feeds the plan in once it exists.
func (s *Server) SetPlan(root ast.Node) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.plan = buildPlanRoutes(root)
	if s.store == nil {
		s.store = NewStore()
	}
}

// New returns a mock server. In fixed mode it responds with the given status
// and body; with WithPlan it behaves as a stateful FHIR server.
func New(status int, body string, opts ...Option) *Server {
	s := &Server{status: status, body: body, logger: true}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Start binds the server to a port and begins serving. It returns the address
// the server is listening on (e.g. "127.0.0.1:54321").
func (s *Server) Start() (string, error) {
	if s.planErr != nil {
		return "", s.planErr
	}
	addr := "127.0.0.1:0"
	if s.port != 0 {
		addr = fmt.Sprintf("127.0.0.1:%d", s.port)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", fmt.Errorf("mock listen: %w", err)
	}
	s.ln = ln

	r := chi.NewRouter()
	// Basic middleware boilerplate: a request id, real client IP, request
	// logging, and panic recovery. Request logging is conditional so the
	// in-process mock used by "test --mock" does not spam the terminal.
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	if s.logger {
		r.Use(middleware.Logger)
	}
	r.Use(middleware.Recoverer)

	if s.plan != nil {
		r.Handle("/*", http.HandlerFunc(s.handlePlan))
	} else {
		r.Handle("/*", http.HandlerFunc(s.handleFixed))
	}

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

// handleFixed writes the fixed status and body for any request.
func (s *Server) handleFixed(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(s.status)
	if s.body != "" {
		_, _ = w.Write([]byte(s.body))
	}
}

// handlePlan serves a request with real FHIR semantics backed by the in-memory
// store and the test plan's reject routes.
func (s *Server) handlePlan(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	plan := s.plan
	store := s.store
	s.mu.RUnlock()

	// Strip the base path (e.g. "/fhir") so routing sees the resource path.
	path := r.URL.Path
	if s.basePath != "" && strings.HasPrefix(path, s.basePath) {
		path = strings.TrimPrefix(path, s.basePath)
		if path == "" {
			path = "/"
		}
	}

	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")

	// Reject routes from the plan take precedence for requests the store cannot
	// naturally reject: searches with invalid modifiers and writes with invalid
	// payloads. They are keyed by method + full request URI (path + query) so
	// distinct search queries match distinctly.
	//
	// Plain instance GETs (no query) are excluded: the store's natural 200/404
	// is authoritative there, and a reject route on a shared instance URL (e.g.
	// the final 404 of a CRUD sequence) must not override the intermediate 200s.
	isInstanceGet := r.Method == http.MethodGet && len(segments) == 2 && r.URL.RawQuery == ""
	if !isInstanceGet {
		if route, ok := plan.rejects[r.Method+" "+r.URL.RequestURI()]; ok {
			w.WriteHeader(route.status)
			return
		}
	}

	// /metadata returns a minimal CapabilityStatement.
	if len(segments) == 1 && segments[0] == "metadata" {
		writeJSON(w, http.StatusOK, map[string]any{
			"resourceType": "CapabilityStatement",
			"status":       "active",
			"fhirVersion":  "4.0.1",
		})
		return
	}

	// Search: GET /{resourceType}?query returns a Bundle of matching resources.
	if r.Method == http.MethodGet && len(segments) == 1 && segments[0] != "" {
		params := make(map[string]string)
		for k, vs := range r.URL.Query() {
			if len(vs) > 0 {
				params[k] = vs[0]
			}
		}
		results, err := store.Search(segments[0], params)
		if err != nil {
			writeOperationOutcome(w, http.StatusBadRequest, err.Error())
			return
		}
		writeSearchBundle(w, results)
		return
	}

	// History: GET /{resourceType}/{id}/_history returns a Bundle.
	if r.Method == http.MethodGet && len(segments) == 3 && segments[2] == "_history" {
		writeSearchBundle(w, nil)
		return
	}

	// Instance operations: /{resourceType}/{id}
	if len(segments) == 2 && segments[0] != "" && segments[1] != "" {
		resourceType, id := segments[0], segments[1]
		switch r.Method {
		case http.MethodGet:
			if body, ok := store.Get(resourceType, id); ok {
				w.Header().Set("Content-Type", "application/fhir+json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(body)
				return
			}
			writeOperationOutcome(w, http.StatusNotFound, "Resource not found")
			return
		case http.MethodPut, http.MethodPost:
			body, err := readBody(r)
			if err != nil {
				writeOperationOutcome(w, http.StatusBadRequest, "invalid request body")
				return
			}
			// Semantic validation: when a validator is installed and the payload
			// declares a profile, reject non-conformant payloads with 422 + an
			// OperationOutcome naming each issue.
			if s.validator != nil {
				profileURL, resource, hasProfile := profileAndResource(body)
				if hasProfile {
					issues, verr := s.validator.Validate(r.Context(), profileURL, resource)
					if verr == nil && len(issues) > 0 {
						writeValidationFailure(w, issues)
						return
					}
				}
			}
			store.Put(resourceType, id, body)
			w.Header().Set("Content-Type", "application/fhir+json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body)
			return
		case http.MethodDelete:
			store.Delete(resourceType, id)
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}

	// Fallback: accept with the configured status.
	w.WriteHeader(s.status)
	if s.body != "" {
		_, _ = w.Write([]byte(s.body))
	}
}

// writeSearchBundle returns a searchset Bundle of the given resources.
func writeSearchBundle(w http.ResponseWriter, resources []map[string]any) {
	entries := make([]map[string]any, 0, len(resources))
	for _, res := range resources {
		entries = append(entries, map[string]any{"resource": res})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"resourceType": "Bundle",
		"type":         "searchset",
		"total":        len(entries),
		"entry":        entries,
	})
}

// writeOperationOutcome returns a minimal FHIR OperationOutcome.
func writeOperationOutcome(w http.ResponseWriter, status int, diagnostics string) {
	writeJSON(w, status, map[string]any{
		"resourceType": "OperationOutcome",
		"issue": []any{
			map[string]any{
				"severity":    "error",
				"code":        "not-found",
				"diagnostics": diagnostics,
			},
		},
	})
}

// writeJSON writes a JSON response with the given status.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/fhir+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// readBody reads the request body, returning an error when it is empty.
func readBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("empty request body")
	}
	return body, nil
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
