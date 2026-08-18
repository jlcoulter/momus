package provisioning

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
)

func TestProvisionWritesTargetsBeforeDependents(t *testing.T) {
	var mu sync.Mutex
	var order []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		order = append(order, r.URL.Path)
		mu.Unlock()
		w.Header().Set("ETag", `W/"1"`)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"ok"}`))
	}))
	defer server.Close()

	ds := &model.Dataset{
		Resources: map[string]*model.ResourceInstance{
			"obs": {LocalID: "obs", ResourceType: "Observation", Resource: map[string]any{
				"resourceType": "Observation", "id": "obs",
				"subject": map[string]any{"reference": "Patient/pat"},
			}},
			"pat": {LocalID: "pat", ResourceType: "Patient", Resource: map[string]any{
				"resourceType": "Patient", "id": "pat",
			}},
		},
		Relationships: []model.Reference{{SourceID: "obs", Path: "Observation.subject", TargetID: "pat"}},
	}

	p := New(server.URL, &Options{HTTPClient: server.Client()})
	if err := p.Provision(context.Background(), ds); err != nil {
		t.Fatalf("Provision returned error: %v", err)
	}

	// Patient (target) must be PUT before Observation (dependent).
	if len(order) != 2 {
		t.Fatalf("got %d requests, want 2: %v", len(order), order)
	}
	if order[0] != "/Patient/pat" {
		t.Fatalf("first PUT was %s, want /Patient/pat (target first)", order[0])
	}
	if order[1] != "/Observation/obs" {
		t.Fatalf("second PUT was %s, want /Observation/obs", order[1])
	}

	if ds.Resources["pat"].ServerID != "pat" {
		t.Fatalf("expected Patient server id to be recorded, got %q", ds.Resources["pat"].ServerID)
	}
	if ds.Resources["obs"].ServerID != "obs" {
		t.Fatalf("expected Observation server id to be recorded, got %q", ds.Resources["obs"].ServerID)
	}
	if ds.Resources["pat"].Version != `W/"1"` {
		t.Fatalf("expected ETag version, got %q", ds.Resources["pat"].Version)
	}
}

func TestProvisionSendsFhirJSONContentType(t *testing.T) {
	var gotContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ds := &model.Dataset{
		Resources: map[string]*model.ResourceInstance{
			"pat": {LocalID: "pat", ResourceType: "Patient", Resource: map[string]any{"resourceType": "Patient", "id": "pat"}},
		},
	}
	if err := New(server.URL, &Options{HTTPClient: server.Client()}).Provision(context.Background(), ds); err != nil {
		t.Fatalf("Provision returned error: %v", err)
	}
	if gotContentType != "application/fhir+json" {
		t.Fatalf("got content-type %q, want application/fhir+json", gotContentType)
	}
}

func TestProvisionAppliesBearerToken(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ds := &model.Dataset{
		Resources: map[string]*model.ResourceInstance{
			"pat": {LocalID: "pat", ResourceType: "Patient", Resource: map[string]any{"resourceType": "Patient", "id": "pat"}},
		},
	}
	if err := New(server.URL, &Options{HTTPClient: server.Client(), BearerToken: "secret"}).Provision(context.Background(), ds); err != nil {
		t.Fatalf("Provision returned error: %v", err)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("got authorization %q, want Bearer secret", gotAuth)
	}
}

func TestProvisionReturnsErrorOnNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"resourceType":"OperationOutcome"}`))
	}))
	defer server.Close()

	ds := &model.Dataset{
		Resources: map[string]*model.ResourceInstance{
			"pat": {LocalID: "pat", ResourceType: "Patient", Resource: map[string]any{"resourceType": "Patient", "id": "pat"}},
		},
	}
	if err := New(server.URL, &Options{HTTPClient: server.Client()}).Provision(context.Background(), ds); err == nil {
		t.Fatal("expected error for non-2xx response")
	}
}

func TestProvisionRequiresBaseURL(t *testing.T) {
	if err := New("", nil).Provision(context.Background(), &model.Dataset{}); err == nil {
		t.Fatal("expected error for empty base URL")
	}
}

func TestProvisionSendsResourceBody(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ds := &model.Dataset{
		Resources: map[string]*model.ResourceInstance{
			"pat": {LocalID: "pat", ResourceType: "Patient", Resource: map[string]any{"resourceType": "Patient", "id": "pat", "name": "x"}},
		},
	}
	if err := New(server.URL, &Options{HTTPClient: server.Client()}).Provision(context.Background(), ds); err != nil {
		t.Fatalf("Provision returned error: %v", err)
	}
	if got["name"] != "x" {
		t.Fatalf("got body %v, expected name=x", got)
	}
}
