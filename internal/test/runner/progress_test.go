package runner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jlcoulter/momus/internal/test/ast"
)

func TestExecuteProgressCallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"resourceType":"Patient","id":"p1"}`))
	}))
	defer server.Close()

	plan := &ast.Sequence{Steps: []ast.Node{
		&ast.Request{Method: http.MethodGet, URL: "/Patient/p1"},
		&ast.Assert{Description: "read", RequirementID: "req-1", Expression: "status in [200]"},
		&ast.Request{Method: http.MethodGet, URL: "/Patient/p2"},
		&ast.Assert{Description: "read", RequirementID: "req-2", Expression: "status in [200]"},
	}}

	var calls []int
	report, err := Execute(context.Background(), plan, ExecuteOptions{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
		Progress: func(done, total int) {
			calls = append(calls, done)
			if total != 2 {
				t.Fatalf("total = %d, want 2", total)
			}
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if report.Passed != 2 {
		t.Fatalf("passed = %d, want 2", report.Passed)
	}
	if len(calls) != 2 {
		t.Fatalf("progress calls = %d, want 2 (got %v)", len(calls), calls)
	}
	if calls[0] != 1 || calls[1] != 2 {
		t.Fatalf("progress sequence = %v, want [1 2]", calls)
	}
}

func TestExecuteProgressSkipsSetupCases(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"resourceType":"Patient","id":"p1"}`))
	}))
	defer server.Close()

	// A setup case (RequirementID prefixed "setup:") must not count toward the
	// progress total.
	plan := &ast.Sequence{Steps: []ast.Node{
		&ast.Request{Method: http.MethodGet, URL: "/Patient/p1"},
		&ast.Assert{Description: "setup", RequirementID: "setup:seed", Expression: "status in [200]"},
		&ast.Request{Method: http.MethodGet, URL: "/Patient/p2"},
		&ast.Assert{Description: "read", RequirementID: "req-1", Expression: "status in [200]"},
	}}

	var total int
	_, err := Execute(context.Background(), plan, ExecuteOptions{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
		Progress: func(done, t int) {
			total = t
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if total != 1 {
		t.Fatalf("progress total = %d, want 1 (setup case excluded)", total)
	}
}
