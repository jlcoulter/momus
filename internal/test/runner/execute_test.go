package runner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jlcoulter/momus/internal/test/ast"
)

func TestExecuteProducesReport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/Patient" && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"1"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	plan := &ast.Sequence{Steps: []ast.Node{
		&ast.Request{Method: http.MethodPost, URL: "/Patient", Headers: map[string]string{"Content-Type": "application/fhir+json"}, Body: map[string]any{"resourceType": "Patient"}},
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
		&ast.Request{Method: http.MethodPost, URL: "/Patient", Body: map[string]any{"resourceType": "Patient"}},
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
