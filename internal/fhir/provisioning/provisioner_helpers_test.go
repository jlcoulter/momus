package provisioning

import (
	"net/http"
	"testing"
)

func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Fatalf("truncate(short) = %q", got)
	}
	if got := truncate("longer", 4); got != "long…" {
		t.Fatalf("truncate(long) = %q", got)
	}
}

func TestOperationOutcomeReason(t *testing.T) {
	// Not an OperationOutcome.
	if got := operationOutcomeReason([]byte("not-json")); got != "" {
		t.Fatalf("operationOutcomeReason(invalid) = %q", got)
	}
	if got := operationOutcomeReason([]byte(`{"resourceType":"Patient"}`)); got != "" {
		t.Fatalf("operationOutcomeReason(non-OO) = %q", got)
	}
	// Empty issues.
	if got := operationOutcomeReason([]byte(`{"resourceType":"OperationOutcome","issue":[]}`)); got != "" {
		t.Fatalf("operationOutcomeReason(empty issues) = %q", got)
	}
	// Issue with diagnostics and location.
	got := operationOutcomeReason([]byte(`{"resourceType":"OperationOutcome","issue":[{"severity":"error","diagnostics":"bad value","location":["Patient.name"]}]}`))
	if got != "error: bad value (Patient.name)" {
		t.Fatalf("operationOutcomeReason = %q", got)
	}
	// Issue with details.text and expression fallback.
	got = operationOutcomeReason([]byte(`{"resourceType":"OperationOutcome","issue":[{"severity":"warning","details":{"text":"warned"},"expression":["Patient.active"]}]}`))
	if got != "warning: warned (Patient.active)" {
		t.Fatalf("operationOutcomeReason(details) = %q", got)
	}
	// Issue with diagnostics only.
	got = operationOutcomeReason([]byte(`{"resourceType":"OperationOutcome","issue":[{"severity":"error","diagnostics":"only"}]}`))
	if got != "error: only" {
		t.Fatalf("operationOutcomeReason(diag only) = %q", got)
	}
	// Issue with location only.
	got = operationOutcomeReason([]byte(`{"resourceType":"OperationOutcome","issue":[{"severity":"error","location":["Patient.x"]}]}`))
	if got != "error: at Patient.x" {
		t.Fatalf("operationOutcomeReason(loc only) = %q", got)
	}
	// Issue with nothing meaningful.
	if got := operationOutcomeReason([]byte(`{"resourceType":"OperationOutcome","issue":[{"severity":"error"}]}`)); got != "" {
		t.Fatalf("operationOutcomeReason(blank issue) = %q", got)
	}
}

func TestApplyAuth(t *testing.T) {
	// Existing auth header preserved.
	p := New("http://x", &Options{BearerToken: "tok"})
	req, _ := http.NewRequest("GET", "http://x", nil)
	req.Header.Set("Authorization", "Existing")
	p.applyAuth(req)
	if req.Header.Get("Authorization") != "Existing" {
		t.Fatalf("existing auth overwritten: %q", req.Header.Get("Authorization"))
	}

	// Bearer token.
	p = New("http://x", &Options{BearerToken: "tok"})
	req, _ = http.NewRequest("GET", "http://x", nil)
	p.applyAuth(req)
	if req.Header.Get("Authorization") != "Bearer tok" {
		t.Fatalf("bearer auth = %q", req.Header.Get("Authorization"))
	}

	// Basic auth.
	p = New("http://x", &Options{BasicUsername: "u", BasicPassword: "p"})
	req, _ = http.NewRequest("GET", "http://x", nil)
	p.applyAuth(req)
	if _, _, ok := req.BasicAuth(); !ok {
		t.Fatal("expected basic auth")
	}

	// No auth configured.
	p = New("http://x", &Options{})
	req, _ = http.NewRequest("GET", "http://x", nil)
	p.applyAuth(req)
	if req.Header.Get("Authorization") != "" {
		t.Fatalf("no auth should set nothing: %q", req.Header.Get("Authorization"))
	}
}
