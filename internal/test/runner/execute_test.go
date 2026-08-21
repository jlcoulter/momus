package runner

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jlcoulter/momus/internal/test/ast"
)

func TestExecutePreCreatedAllowsSetupReferenceValidation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"resourceType":"Patient","id":"momus-setup-patient"}`))
	}))
	defer server.Close()

	// A setup request whose body references a seed resource that is provisioned
	// ahead of execution (not created by this AST).
	plan := &ast.Sequence{Steps: []ast.Node{
		&ast.Request{
			Method:  http.MethodPut,
			URL:     "/Patient/momus-setup-patient",
			Headers: map[string]string{"Content-Type": "application/fhir+json"},
			Body: map[string]any{
				"resourceType": "Patient",
				"id":           "momus-setup-patient",
				"managingOrganization": map[string]any{
					"reference": "Organization/momus-setup-organization",
				},
			},
		},
		&ast.Assert{Description: "create", RequirementID: "req-1", Expression: "status in [200,201]"},
	}}

	// Without PreCreated the reference is unresolved and the case fails.
	without, err := Execute(context.Background(), plan, ExecuteOptions{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if without.Passed != 0 || without.Failed != 1 {
		t.Fatalf("expected failure without PreCreated, got %+v", without)
	}

	// With PreCreated the seed resource is treated as already created.
	with, err := Execute(context.Background(), plan, ExecuteOptions{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
		PreCreated: map[string]struct{}{"Organization/momus-setup-organization": {}},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if with.Passed != 1 || with.Failed != 0 {
		t.Fatalf("expected pass with PreCreated, got %+v", with)
	}
}

func TestExecuteRoutesWriteAndReadRequestsToSeparateBaseURLs(t *testing.T) {
	var writePath, readPath string
	writeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writePath = r.URL.Path
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"1"}`))
	}))
	defer writeServer.Close()
	readServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		readPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"resourceType":"Bundle","total":1}`))
	}))
	defer readServer.Close()

	plan := &ast.Sequence{Steps: []ast.Node{
		&ast.Request{Method: http.MethodPut, URL: "/Patient/test-1", Headers: map[string]string{"Content-Type": "application/fhir+json"}, Body: map[string]any{"resourceType": "Patient", "id": "test-1"}},
		&ast.Assert{Description: "create", RequirementID: "req-1", Expression: "status in [200,201]"},
		&ast.Request{Method: http.MethodGet, URL: "/Patient?name=momus-search"},
		&ast.Assert{Description: "search", RequirementID: "req-2", Expression: "status in [200]"},
	}}

	report, err := Execute(context.Background(), plan, ExecuteOptions{
		BaseURL:      readServer.URL,
		WriteBaseURL: writeServer.URL,
		HTTPClient:   readServer.Client(),
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if report.Total != 2 || report.Passed != 2 || report.Failed != 0 {
		t.Fatalf("unexpected report summary: %+v", report)
	}
	if writePath != "/Patient/test-1" {
		t.Fatalf("write request hit %q, want /Patient/test-1 on write server", writePath)
	}
	if readPath != "/Patient" {
		t.Fatalf("read request hit %q, want /Patient on read server", readPath)
	}
}

func TestExecuteEvaluatesBodyAssertion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"resourceType":"Bundle","total":3}`))
	}))
	defer server.Close()

	plan := &ast.Plan{Root: &ast.Sequence{Steps: []ast.Node{
		&ast.Request{Method: http.MethodGet, URL: server.URL + "/Observation"},
		&ast.Assert{Description: "multiple", RequirementID: "search-multi", Expression: "body.total >= 2"},
	}}}
	report, err := Execute(context.Background(), plan.Root, ExecuteOptions{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if report.Total != 1 || report.Passed != 1 || report.Failed != 0 {
		t.Fatalf("report = %+v, want 1 passed", report)
	}
}

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
		&ast.Assert{Description: "create patient", RequirementID: "req-1", Expression: "status in [200,201]", Trace: &ast.Trace{ConstraintID: "profile|Patient.name|cardinality", Domain: "cardinality", Variant: "valid-min", Expected: "accept"}},
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
	if report.Cases[0].Trace == nil || report.Cases[0].Trace.ConstraintID != "profile|Patient.name|cardinality" {
		t.Fatalf("expected trace to carry constraint id, got %+v", report.Cases[0].Trace)
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

func TestExecuteFailsFastForUnresolvedSetupReference(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"o-1"}`))
	}))
	defer server.Close()

	plan := &ast.Sequence{Steps: []ast.Node{
		&ast.Request{Method: http.MethodPut, URL: "/Observation/momus-setup-observation", Body: map[string]any{"resourceType": "Observation", "id": "momus-setup-observation", "subject": map[string]any{"reference": "Patient/momus-setup-patient"}}},
		&ast.Assert{Description: "create observation", RequirementID: "setup:Observation", Expression: "status in [200,201]"},
	}}

	report, err := Execute(context.Background(), plan, ExecuteOptions{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if report.Total != 1 || report.Passed != 0 || report.Failed != 1 {
		t.Fatalf("unexpected report summary: %+v", report)
	}
	if report.Cases[0].Error == "" || report.Cases[0].Passed {
		t.Fatalf("expected unresolved setup reference failure, got %+v", report.Cases[0])
	}
	if requestCount != 0 {
		t.Fatalf("expected preflight guard to block HTTP request, got %d requests", requestCount)
	}
}

func TestExecuteAllowsResolvedSetupReferenceAfterSetupCreation(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusCreated)
		switch r.URL.Path {
		case "/Patient/momus-setup-patient":
			_, _ = w.Write([]byte(`{"id":"momus-setup-patient"}`))
		case "/Observation/o-1":
			_, _ = w.Write([]byte(`{"id":"o-1"}`))
		default:
			_, _ = w.Write([]byte(`{"id":"unknown"}`))
		}
	}))
	defer server.Close()

	plan := &ast.Sequence{Steps: []ast.Node{
		&ast.Request{Method: http.MethodPut, URL: "/Patient/momus-setup-patient", Body: map[string]any{"resourceType": "Patient", "id": "momus-setup-patient"}},
		&ast.Assert{Description: "seed patient", RequirementID: "setup:Patient", Expression: "status in [200,201]"},
		&ast.Request{Method: http.MethodPut, URL: "/Observation/o-1", Body: map[string]any{"resourceType": "Observation", "id": "o-1", "subject": map[string]any{"reference": "Patient/momus-setup-patient"}}},
		&ast.Assert{Description: "create observation", RequirementID: "req-resolved-setup-ref", Expression: "status in [200,201]"},
	}}

	report, err := Execute(context.Background(), plan, ExecuteOptions{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if report.Total != 2 || report.Passed != 2 || report.Failed != 0 {
		t.Fatalf("unexpected report summary: %+v", report)
	}
	if requestCount != 2 {
		t.Fatalf("got %d HTTP requests, want 2", requestCount)
	}
}

func TestExecuteAppliesWriteSpecificBasicAuth(t *testing.T) {
	var writeAuth, readAuth string
	writeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"1"}`))
	}))
	defer writeServer.Close()
	readServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		readAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"resourceType":"Bundle","total":1}`))
	}))
	defer readServer.Close()

	plan := &ast.Sequence{Steps: []ast.Node{
		&ast.Request{Method: http.MethodPut, URL: "/Patient/test-1", Body: map[string]any{"resourceType": "Patient", "id": "test-1"}},
		&ast.Assert{Description: "create", RequirementID: "req-1", Expression: "status in [200,201]"},
		&ast.Request{Method: http.MethodGet, URL: "/Patient?name=momus-search"},
		&ast.Assert{Description: "search", RequirementID: "req-2", Expression: "status in [200]"},
	}}

	report, err := Execute(context.Background(), plan, ExecuteOptions{
		BaseURL:            readServer.URL,
		WriteBaseURL:       writeServer.URL,
		HTTPClient:         readServer.Client(),
		BasicUsername:      "read-user",
		BasicPassword:      "read-pass",
		WriteBasicUsername: "write-user",
		WriteBasicPassword: "write-pass",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if report.Failed != 0 {
		t.Fatalf("unexpected failure report: %+v", report)
	}
	wantWrite := "Basic " + base64.StdEncoding.EncodeToString([]byte("write-user:write-pass"))
	if writeAuth != wantWrite {
		t.Fatalf("write request auth = %q, want %q", writeAuth, wantWrite)
	}
	wantRead := "Basic " + base64.StdEncoding.EncodeToString([]byte("read-user:read-pass"))
	if readAuth != wantRead {
		t.Fatalf("read request auth = %q, want %q", readAuth, wantRead)
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

func TestExecuteTreatsWarningOnly412AsPositivePass(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPreconditionFailed)
		_, _ = w.Write([]byte(`{"resourceType":"OperationOutcome","issue":[{"severity":"warning","code":"processing","diagnostics":"validation warning"}]}`))
	}))
	defer server.Close()

	plan := &ast.Sequence{Steps: []ast.Node{
		&ast.Request{Method: http.MethodPut, URL: "/Patient/warn-1", Body: map[string]any{"resourceType": "Patient", "id": "warn-1"}},
		&ast.Assert{Description: "create patient", RequirementID: "req-warning-pass", Expression: "status in [200,201]"},
	}}

	report, err := Execute(context.Background(), plan, ExecuteOptions{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if report.Total != 1 || report.Passed != 1 || report.Failed != 0 {
		t.Fatalf("unexpected report summary: %+v", report)
	}
}

func TestExecuteDoesNotTreatError412AsPositivePass(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPreconditionFailed)
		_, _ = w.Write([]byte(`{"resourceType":"OperationOutcome","issue":[{"severity":"error","code":"processing","diagnostics":"validation error"}]}`))
	}))
	defer server.Close()

	plan := &ast.Sequence{Steps: []ast.Node{
		&ast.Request{Method: http.MethodPut, URL: "/Patient/error-1", Body: map[string]any{"resourceType": "Patient", "id": "error-1"}},
		&ast.Assert{Description: "create patient", RequirementID: "req-warning-fail", Expression: "status in [200,201]"},
	}}

	report, err := Execute(context.Background(), plan, ExecuteOptions{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if report.Total != 1 || report.Passed != 0 || report.Failed != 1 {
		t.Fatalf("unexpected report summary: %+v", report)
	}
}

func TestExecuteAggregatesOperationOutcomeFailureDiagnostics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPreconditionFailed)
		_, _ = w.Write([]byte(`{"resourceType":"OperationOutcome","issue":[{"severity":"error","code":"processing","expression":["Organization.extension[0].value"],"diagnostics":"Unable to find match for profile"}]}`))
	}))
	defer server.Close()

	plan := &ast.Sequence{Steps: []ast.Node{
		&ast.Request{Method: http.MethodPut, URL: "/Organization/org-1", Body: map[string]any{"resourceType": "Organization", "id": "org-1"}},
		&ast.Assert{Description: "create organization", RequirementID: "req-org-1", Expression: "status in [200,201]"},
		&ast.Request{Method: http.MethodPut, URL: "/Organization/org-2", Body: map[string]any{"resourceType": "Organization", "id": "org-2"}},
		&ast.Assert{Description: "create organization", RequirementID: "req-org-2", Expression: "status in [200,201]"},
	}}

	report, err := Execute(context.Background(), plan, ExecuteOptions{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if report.Total != 2 || report.Passed != 0 || report.Failed != 2 {
		t.Fatalf("unexpected report summary: %+v", report)
	}
	if report.Diagnostics == nil {
		t.Fatalf("expected diagnostics summary in report")
	}
	if report.Diagnostics.OperationOutcomeFailures != 2 {
		t.Fatalf("got %d operation outcome failures, want 2", report.Diagnostics.OperationOutcomeFailures)
	}
	if len(report.Diagnostics.TopSignatures) != 1 {
		t.Fatalf("got %d top signatures, want 1", len(report.Diagnostics.TopSignatures))
	}
	sig := report.Diagnostics.TopSignatures[0]
	if sig.Count != 2 {
		t.Fatalf("got signature count %d, want 2", sig.Count)
	}
	if sig.ExampleRequirementID != "req-org-1" {
		t.Fatalf("got example requirement %q, want req-org-1", sig.ExampleRequirementID)
	}
	if report.Cases[0].FailureFingerprint == "" {
		t.Fatalf("expected case failure fingerprint to be set")
	}
	if report.Cases[1].FailureFingerprint != report.Cases[0].FailureFingerprint {
		t.Fatalf("expected matching fingerprints, got %q and %q", report.Cases[0].FailureFingerprint, report.Cases[1].FailureFingerprint)
	}
}

func TestExecuteDoesNotCreateFailureSignatureForWarningIssue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"resourceType":"OperationOutcome","issue":[{"severity":"warning","code":"processing","diagnostics":"warning only"}]}`))
	}))
	defer server.Close()

	plan := &ast.Sequence{Steps: []ast.Node{
		&ast.Request{Method: http.MethodPut, URL: "/Patient/warn-fingerprint", Body: map[string]any{"resourceType": "Patient", "id": "warn-fingerprint"}},
		&ast.Assert{Description: "create patient", RequirementID: "req-warning-sig", Expression: "status in [200,201]"},
	}}

	report, err := Execute(context.Background(), plan, ExecuteOptions{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if report.Total != 1 || report.Passed != 0 || report.Failed != 1 {
		t.Fatalf("unexpected report summary: %+v", report)
	}
	if report.Cases[0].FailureFingerprint != "" {
		t.Fatalf("expected empty failure fingerprint for warning issue, got %q", report.Cases[0].FailureFingerprint)
	}
	if report.Diagnostics != nil {
		t.Fatalf("expected no diagnostics summary when all issues are warnings, got %+v", report.Diagnostics)
	}
}

func TestExecutePrefersErrorIssueOverLeadingWarningForSignature(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPreconditionFailed)
		_, _ = w.Write([]byte(`{"resourceType":"OperationOutcome","issue":[{"severity":"warning","code":"processing","diagnostics":"first warning"},{"severity":"error","code":"processing","diagnostics":"real error","expression":["Patient.name"]}]}`))
	}))
	defer server.Close()

	plan := &ast.Sequence{Steps: []ast.Node{
		&ast.Request{Method: http.MethodPut, URL: "/Patient/mixed-issue", Body: map[string]any{"resourceType": "Patient", "id": "mixed-issue"}},
		&ast.Assert{Description: "create patient", RequirementID: "req-mixed-sig", Expression: "status in [200,201]"},
	}}

	report, err := Execute(context.Background(), plan, ExecuteOptions{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if report.Diagnostics == nil || len(report.Diagnostics.TopSignatures) != 1 {
		t.Fatalf("expected exactly one diagnostics signature, got %+v", report.Diagnostics)
	}
	sig := report.Diagnostics.TopSignatures[0]
	if sig.Severity != "error" {
		t.Fatalf("got severity %q, want error", sig.Severity)
	}
	if !strings.Contains(sig.Signature, "diag=real error") {
		t.Fatalf("expected signature to include error diagnostics, got %q", sig.Signature)
	}
}

func TestExecuteMarksLikelyAuthFailureWhenAllFailuresMatchAuthSignature(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"resourceType":"OperationOutcome","issue":[{"severity":"error","code":"processing","diagnostics":"UPDATE operations are disabled. Please provide valid basic authentication."}]}`))
	}))
	defer server.Close()

	plan := &ast.Sequence{Steps: []ast.Node{
		&ast.Request{Method: http.MethodPut, URL: "/Organization/org-auth-1", Body: map[string]any{"resourceType": "Organization", "id": "org-auth-1"}},
		&ast.Assert{Description: "create organization", RequirementID: "req-auth-1", Expression: "status in [200,201]"},
		&ast.Request{Method: http.MethodPut, URL: "/Organization/org-auth-2", Body: map[string]any{"resourceType": "Organization", "id": "org-auth-2"}},
		&ast.Assert{Description: "create organization", RequirementID: "req-auth-2", Expression: "status in [200,201]"},
	}}

	report, err := Execute(context.Background(), plan, ExecuteOptions{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if report.Diagnostics == nil {
		t.Fatalf("expected diagnostics summary in report")
	}
	if !report.Diagnostics.LikelyAuthFailure {
		t.Fatalf("expected likely auth failure detection, got diagnostics: %+v", report.Diagnostics)
	}
	if report.Diagnostics.Hint == "" {
		t.Fatalf("expected auth hint to be populated")
	}
	if len(report.Diagnostics.TopSignatures) != 1 {
		t.Fatalf("expected one top signature, got %d", len(report.Diagnostics.TopSignatures))
	}
	sig := report.Diagnostics.TopSignatures[0]
	if sig.RootCauseCategory != "authentication" || sig.Confidence != "high" || sig.TriageRole != "root" {
		t.Fatalf("unexpected auth signature triage metadata: %+v", sig)
	}
}

func TestExecuteTagsMissingDependentResourceAsCascadeDependent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Practitioner/momus-setup-practitioner":
			w.WriteHeader(http.StatusPreconditionFailed)
			_, _ = w.Write([]byte(`{"resourceType":"OperationOutcome","issue":[{"severity":"error","code":"processing","diagnostics":"Practitioner.active: minimum required = 1"}]}`))
		case "/Composition/c-1":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"resourceType":"OperationOutcome","issue":[{"severity":"error","code":"processing","diagnostics":"HAPI-1094: Resource Practitioner/momus-setup-practitioner not found, specified in path: Composition.author"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	plan := &ast.Sequence{Steps: []ast.Node{
		&ast.Request{Method: http.MethodPut, URL: "/Practitioner/momus-setup-practitioner", Body: map[string]any{"resourceType": "Practitioner", "id": "momus-setup-practitioner"}},
		&ast.Assert{Description: "seed practitioner", RequirementID: "setup:Practitioner", Expression: "status in [200,201]"},
		&ast.Request{Method: http.MethodPut, URL: "/Composition/c-1", Body: map[string]any{"resourceType": "Composition", "id": "c-1", "author": []any{map[string]any{"reference": "Practitioner/momus-setup-practitioner"}}}},
		&ast.Assert{Description: "create composition", RequirementID: "req-composition", Expression: "status in [200,201]"},
	}}

	report, err := Execute(context.Background(), plan, ExecuteOptions{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if report.Diagnostics == nil {
		t.Fatalf("expected diagnostics summary in report")
	}
	dependentSig := findSignatureContaining(report.Diagnostics.TopSignatures, "HAPI-1094")
	if dependentSig == nil {
		t.Fatalf("expected HAPI-1094 signature in diagnostics: %+v", report.Diagnostics.TopSignatures)
	}
	if dependentSig.TriageRole != "dependent" {
		t.Fatalf("expected dependent triage role, got %+v", *dependentSig)
	}
	if dependentSig.RootCauseCategory != "missing-dependent-resource" || dependentSig.Confidence != "high" {
		t.Fatalf("unexpected dependent triage metadata: %+v", *dependentSig)
	}
}

func findSignatureContaining(signatures []FailureSignature, needle string) *FailureSignature {
	needle = strings.ToLower(needle)
	for idx := range signatures {
		if strings.Contains(strings.ToLower(signatures[idx].Signature), needle) || strings.Contains(strings.ToLower(signatures[idx].Diagnostics), needle) {
			return &signatures[idx]
		}
	}
	return nil
}

func TestExecuteReportsStandaloneRequestError(t *testing.T) {
	// A standalone Request with a relative URL and no base URL fails during
	// execution. With no following Assert to observe the error, it must be
	// recorded as a failed case rather than silently swallowed.
	plan := &ast.Sequence{Steps: []ast.Node{
		&ast.Request{Method: http.MethodGet, URL: "/Patient"},
	}}

	report, err := Execute(context.Background(), plan, ExecuteOptions{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if report.Total != 1 || report.Passed != 0 || report.Failed != 1 {
		t.Fatalf("unexpected report summary: %+v", report)
	}
	if report.Cases[0].Passed || report.Cases[0].Error == "" {
		t.Fatalf("expected failed case with error, got %+v", report.Cases[0])
	}
}

func TestExecutePreservesReportOnParallelStructuralError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// One branch succeeds; the other is a structural error (nil node). The
	// successful branch's result must be preserved in the report rather than
	// discarded when the structural error is recorded as a failed case.
	plan := &ast.Parallel{Steps: []ast.Node{
		&ast.Sequence{Steps: []ast.Node{
			&ast.Request{Method: http.MethodGet, URL: server.URL + "/A/1"},
			&ast.Assert{Description: "a", RequirementID: "a", Expression: "status in [200]"},
		}},
		nil, // unsupported node -> structural error
	}}

	report, err := Execute(context.Background(), plan, ExecuteOptions{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if report.Total != 2 || report.Passed != 1 || report.Failed != 1 {
		t.Fatalf("unexpected report summary: %+v", report)
	}
}

// failingRoundTripper always fails the request, simulating a transport-level
// error (e.g. connection refused) so request errors can be exercised.
type failingRoundTripper struct{}

func (failingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("connection refused")
}

func TestExecuteWrapsRequestErrorsWithMethodAndURL(t *testing.T) {
	plan := &ast.Sequence{Steps: []ast.Node{
		&ast.Request{Method: http.MethodGet, URL: "http://example.test/Patient"},
		&ast.Assert{Description: "get", RequirementID: "req-1", Expression: "status in [200]"},
	}}

	report, err := Execute(context.Background(), plan, ExecuteOptions{
		HTTPClient: &http.Client{Transport: failingRoundTripper{}},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if report.Failed != 1 {
		t.Fatalf("expected 1 failure, got %+v", report)
	}
	if !strings.Contains(report.Cases[0].Error, "GET http://example.test/Patient") {
		t.Fatalf("expected error to carry method and URL, got %q", report.Cases[0].Error)
	}
}

func TestExecutePopulatesStatusCodeForParseError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"resourceType":"Bundle","total":1}`))
	}))
	defer server.Close()

	// The request succeeds (200) but the assertion expression fails to parse;
	// the failed case must still carry the last response's status so triage is
	// not misclassified as ambiguous.
	plan := &ast.Sequence{Steps: []ast.Node{
		&ast.Request{Method: http.MethodGet, URL: server.URL + "/Patient"},
		&ast.Assert{Description: "get", RequirementID: "req-1", Expression: "body.total >= abc"},
	}}

	report, err := Execute(context.Background(), plan, ExecuteOptions{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if report.Failed != 1 {
		t.Fatalf("expected 1 failure, got %+v", report)
	}
	if report.Cases[0].StatusCode != http.StatusOK {
		t.Fatalf("expected status %d populated from last response, got %d", http.StatusOK, report.Cases[0].StatusCode)
	}
}

func TestExecuteTreatsWarningOnly412AsPositivePassForOpenAPIExpression(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPreconditionFailed)
		_, _ = w.Write([]byte(`{"resourceType":"OperationOutcome","issue":[{"severity":"warning","code":"processing","diagnostics":"validation warning"}]}`))
	}))
	defer server.Close()

	plan := &ast.Sequence{Steps: []ast.Node{
		&ast.Request{Method: http.MethodPut, URL: "/Patient/warn-openapi", Body: map[string]any{"resourceType": "Patient", "id": "warn-openapi"}},
		&ast.Assert{Description: "create patient", RequirementID: "req-warning-openapi", Expression: successStatusExpression},
	}}

	report, err := Execute(context.Background(), plan, ExecuteOptions{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if report.Total != 1 || report.Passed != 1 || report.Failed != 0 {
		t.Fatalf("unexpected report summary: %+v", report)
	}
}
