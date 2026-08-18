package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jlcoulter/momus/internal/test/ast"
)

func TestExecuteProducesReport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/Patient/test-1" && r.Method == http.MethodPut {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"1"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	plan := &ast.Sequence{Steps: []ast.Node{
		&ast.Request{Method: http.MethodPut, URL: "/Patient/test-1", Headers: map[string]string{"Content-Type": "application/fhir+json"}, Body: map[string]any{"resourceType": "Patient", "id": "test-1"}},
		&ast.Assert{Description: "create patient", RequirementID: "req-1", Expression: "status in [200,201]"},
	}}

	report, err := Execute(context.Background(), plan, ExecuteOptions{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if report.Total != 1 || report.Passed != 1 || report.Failed != 0 {
		t.Fatalf("unexpected report summary: %+v", report)
	}
	if len(report.Cases) != 1 {
		t.Fatalf("got %d cases, want 1", len(report.Cases))
	}
	if !report.Cases[0].Passed {
		t.Fatalf("expected case to pass: %+v", report.Cases[0])
	}
}

func TestExecuteCapturesAssertionFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	plan := &ast.Sequence{Steps: []ast.Node{
		&ast.Request{Method: http.MethodPut, URL: "/Patient/test-2", Body: map[string]any{"resourceType": "Patient", "id": "test-2"}},
		&ast.Assert{Description: "create patient", RequirementID: "req-2", Expression: "status in [200,201]"},
	}}

	report, err := Execute(context.Background(), plan, ExecuteOptions{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if report.Total != 1 || report.Passed != 0 || report.Failed != 1 {
		t.Fatalf("unexpected report summary: %+v", report)
	}
	if report.Cases[0].Error == "" {
		t.Fatalf("expected case failure error text: %+v", report.Cases[0])
	}
}

func TestExecuteResolvesCapturedTemplateVariables(t *testing.T) {
	var observationSubjectRef string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Patient/p-123":
			w.WriteHeader(http.StatusCreated)
			// Simulate a server that accepts PUT but does not echo a resource body.
		case "/Observation/o-456":
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if subject, ok := payload["subject"].(map[string]any); ok {
				if ref, ok := subject["reference"].(string); ok {
					observationSubjectRef = ref
				}
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"o-456"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	plan := &ast.Sequence{Steps: []ast.Node{
		&ast.Request{Method: http.MethodPut, URL: "/Patient/p-123", Body: map[string]any{"resourceType": "Patient", "id": "p-123"}},
		&ast.Assert{Description: "seed patient", RequirementID: "setup:Patient", Expression: "status in [200,201]"},
		&ast.Capture{Name: "Patient.id", Path: "id"},
		&ast.Request{Method: http.MethodPut, URL: "/Observation/o-456", Body: map[string]any{"resourceType": "Observation", "id": "o-456", "subject": map[string]any{"reference": "Patient/{{Patient.id}}"}}},
		&ast.Assert{Description: "create observation", RequirementID: "req-obs", Expression: "status in [200,201]"},
	}}

	report, err := Execute(context.Background(), plan, ExecuteOptions{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if report.Total != 2 || report.Passed != 2 || report.Failed != 0 {
		t.Fatalf("unexpected report summary: %+v", report)
	}
	if observationSubjectRef != "Patient/p-123" {
		t.Fatalf("got observation subject reference %q, want Patient/p-123", observationSubjectRef)
	}
}

func TestExecuteAppliesBearerToken(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"p-1"}`))
	}))
	defer server.Close()

	plan := &ast.Sequence{Steps: []ast.Node{
		&ast.Request{Method: http.MethodPut, URL: "/Patient/auth-1", Body: map[string]any{"resourceType": "Patient", "id": "auth-1"}},
		&ast.Assert{Description: "create patient", RequirementID: "req-auth", Expression: "status in [200,201]"},
	}}

	report, err := Execute(context.Background(), plan, ExecuteOptions{BaseURL: server.URL, HTTPClient: server.Client(), BearerToken: "abc123"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if report.Failed != 0 {
		t.Fatalf("unexpected failure report: %+v", report)
	}
	if authHeader != "Bearer abc123" {
		t.Fatalf("got authorization header %q, want %q", authHeader, "Bearer abc123")
	}
}

func TestExecuteIncludesDebugDetailsWhenEnabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"resourceType":"OperationOutcome","issue":[{"diagnostics":"missing auth"}]}`))
	}))
	defer server.Close()

	plan := &ast.Sequence{Steps: []ast.Node{
		&ast.Request{Method: http.MethodPut, URL: "/Patient/debug-1", Body: map[string]any{"resourceType": "Patient", "id": "debug-1"}},
		&ast.Assert{Description: "create patient", RequirementID: "req-debug", Expression: "status in [200,201]"},
	}}

	report, err := Execute(context.Background(), plan, ExecuteOptions{BaseURL: server.URL, HTTPClient: server.Client(), IncludeDebug: true})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(report.Cases) != 1 {
		t.Fatalf("got %d cases, want 1", len(report.Cases))
	}
	if report.Cases[0].Debug == nil {
		t.Fatalf("expected debug details in case result")
	}
	if report.Cases[0].Debug.RequestMethod != http.MethodPut {
		t.Fatalf("got request method %q, want %q", report.Cases[0].Debug.RequestMethod, http.MethodPut)
	}
	if report.Cases[0].Debug.StatusCode != http.StatusForbidden {
		t.Fatalf("got debug status %d, want %d", report.Cases[0].Debug.StatusCode, http.StatusForbidden)
	}
	if report.Cases[0].Debug.ResponseBody == "" {
		t.Fatalf("expected response body in debug details")
	}
}

func TestExecuteOmitsDebugDetailsByDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"resourceType":"OperationOutcome"}`))
	}))
	defer server.Close()

	plan := &ast.Sequence{Steps: []ast.Node{
		&ast.Request{Method: http.MethodPut, URL: "/Patient/debug-2", Body: map[string]any{"resourceType": "Patient", "id": "debug-2"}},
		&ast.Assert{Description: "create patient", RequirementID: "req-debug-default", Expression: "status in [200,201]"},
	}}

	report, err := Execute(context.Background(), plan, ExecuteOptions{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(report.Cases) != 1 {
		t.Fatalf("got %d cases, want 1", len(report.Cases))
	}
	if report.Cases[0].Debug != nil {
		t.Fatalf("expected debug details to be omitted when IncludeDebug is false")
	}
}
