package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/jlcoulter/momus/internal/core/ast"
	testgeneration "github.com/jlcoulter/momus/internal/fhir/generation"
	"github.com/jlcoulter/momus/internal/fhir/model"
)

// TestProvisionCmdUploadsDatasetFromTestPlan verifies that "coverage provision"
// consumes a test plan (not a package) and uploads the seed dataset it carries,
// in dependency order, to the target server.
func TestProvisionCmdUploadsDatasetFromTestPlan(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.Header().Set("ETag", `W/"1"`)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"ok"}`))
	}))
	defer server.Close()

	// Dataset with a dependency: Observation -> Patient. The Patient (target)
	// must be uploaded before the Observation (dependent).
	dataset := &model.Dataset{
		Resources: map[string]*model.ResourceInstance{
			"momus-setup-patient": {LocalID: "momus-setup-patient", ResourceType: "Patient", Resource: map[string]any{"resourceType": "Patient", "id": "momus-setup-patient"}},
			"momus-setup-obs":     {LocalID: "momus-setup-obs", ResourceType: "Observation", Resource: map[string]any{"resourceType": "Observation", "id": "momus-setup-obs", "subject": map[string]any{"reference": "Patient/momus-setup-patient"}}},
		},
		Relationships: []model.Reference{{SourceID: "momus-setup-obs", Path: "Observation.subject", TargetID: "momus-setup-patient"}},
	}
	// The AST itself is irrelevant to provisioning; the dataset is what matters.
	testPlan := &ast.Plan{Version: "v1", Root: &ast.Sequence{Steps: []ast.Node{
		&ast.Request{Method: "GET", URL: server.URL + "/Patient?name=momus-search"},
	}}}
	planPath := writeTestPlan(t, testPlan, dataset)

	cfg := &config{}
	cmd := newProvisionCmd(cfg)
	cfg.BaseURL = server.URL
	cmd.SetContext(context.Background())
	if err := cmd.RunE(cmd, []string{planPath}); err != nil {
		t.Fatalf("provision command failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(paths) != 2 {
		t.Fatalf("expected 2 PUTs, got %d: %v", len(paths), paths)
	}
	// Target (Patient) must be provisioned before dependent (Observation).
	if paths[0] != "/Patient/momus-setup-patient" {
		t.Fatalf("expected Patient first, got order %v", paths)
	}
	if paths[1] != "/Observation/momus-setup-obs" {
		t.Fatalf("expected Observation second, got order %v", paths)
	}
}

// TestProvisionCmdSkipsWhenPlanHasNoSeedResources verifies provision handles a
// plan with an empty dataset gracefully.
func TestProvisionCmdSkipsWhenPlanHasNoSeedResources(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("expected no requests for empty dataset, got %s %s", r.Method, r.URL)
	}))
	defer server.Close()

	testPlan := &ast.Plan{Version: "v1", Root: &ast.Sequence{Steps: []ast.Node{
		&ast.Request{Method: "GET", URL: server.URL + "/Patient"},
	}}}
	planPath := writeTestPlan(t, testPlan, &model.Dataset{Resources: map[string]*model.ResourceInstance{}})

	cfg := &config{}
	cmd := newProvisionCmd(cfg)
	cfg.BaseURL = server.URL
	cmd.SetContext(context.Background())
	if err := cmd.RunE(cmd, []string{planPath}); err != nil {
		t.Fatalf("provision command failed: %v", err)
	}
}

// TestEncodeDecodeTestPlanRoundTrips verifies the test plan envelope (dataset +
// AST) survives a marshal/unmarshal round trip so provision and run can both
// consume it.
func TestEncodeDecodeTestPlanRoundTrips(t *testing.T) {
	plan := &ast.Plan{Version: "v1", Root: &ast.Sequence{Steps: []ast.Node{
		&ast.Request{Method: "GET", URL: "http://example/fhir/Patient?name=momus-search", Headers: map[string]string{"X-Momus-Requirement-ID": "req-1"}},
		&ast.Assert{Description: "ok", RequirementID: "req-1", Expression: "status in [200]"},
	}}}
	dataset := &model.Dataset{
		Resources: map[string]*model.ResourceInstance{
			"momus-setup-patient": {LocalID: "momus-setup-patient", ResourceType: "Patient", Profile: "http://example/patient", Resource: map[string]any{"resourceType": "Patient", "id": "momus-setup-patient", "name": "x"}},
		},
		Relationships: []model.Reference{{SourceID: "momus-setup-patient", Path: "p", TargetID: "momus-setup-patient"}},
	}
	plan.Dataset = testgeneration.ToCoreDataset(dataset)
	out, err := encodeTestPlan(plan)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decodedPlan, decodedDataset, err := decodeTestPlan(append(out, '\n'))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decodedPlan.Version != "v1" {
		t.Fatalf("version = %q", decodedPlan.Version)
	}
	req, ok := decodedPlan.Root.(*ast.Sequence).Steps[0].(*ast.Request)
	if !ok || req.URL != "http://example/fhir/Patient?name=momus-search" {
		t.Fatalf("decoded AST root wrong: %T", decodedPlan.Root.(*ast.Sequence).Steps[0])
	}
	inst, ok := decodedDataset.Resources["momus-setup-patient"]
	if !ok {
		t.Fatal("missing seed resource after round trip")
	}
	if inst.ResourceType != "Patient" || inst.LocalID != "momus-setup-patient" {
		t.Fatalf("decoded resource = %+v", inst)
	}
	if inst.Resource["id"] != "momus-setup-patient" {
		t.Fatalf("decoded resource body = %+v", inst.Resource)
	}
}
