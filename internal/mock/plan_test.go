package mock

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestStorePutGetDelete(t *testing.T) {
	s := NewStore()
	s.Put("Patient", "p1", []byte(`{"resourceType":"Patient","id":"p1"}`))

	body, ok := s.Get("Patient", "p1")
	if !ok {
		t.Fatal("expected p1 to exist after Put")
	}
	if string(body) != `{"resourceType":"Patient","id":"p1"}` {
		t.Fatalf("body = %q", body)
	}

	if _, ok := s.Get("Patient", "missing"); ok {
		t.Fatal("expected missing resource to not exist")
	}

	s.Delete("Patient", "p1")
	if _, ok := s.Get("Patient", "p1"); ok {
		t.Fatal("expected p1 to be gone after Delete")
	}
}

func TestStoreList(t *testing.T) {
	s := NewStore()
	s.Put("Patient", "p1", []byte(`{"id":"p1"}`))
	s.Put("Patient", "p2", []byte(`{"id":"p2"}`))
	s.Put("Observation", "o1", []byte(`{"id":"o1"}`))

	if got := len(s.List("Patient")); got != 2 {
		t.Fatalf("List(Patient) = %d, want 2", got)
	}
	if got := len(s.List("Observation")); got != 1 {
		t.Fatalf("List(Observation) = %d, want 1", got)
	}
	if got := len(s.List("Encounter")); got != 0 {
		t.Fatalf("List(Encounter) = %d, want 0", got)
	}
}

func TestRejectStatus(t *testing.T) {
	cases := []struct {
		expr     string
		want     int
		wantBool bool
	}{
		{"status in [200,201]", 0, false},
		{"status in [400,412,422]", 400, true},
		{"status in [404]", 404, true},
		{"status in [200,204,404]", 404, true},
		{"body.total >= 2", 0, false},
		{"", 0, false},
	}
	for _, tc := range cases {
		got, ok := rejectStatus(tc.expr)
		if got != tc.want || ok != tc.wantBool {
			t.Fatalf("rejectStatus(%q) = (%d, %v), want (%d, %v)", tc.expr, got, ok, tc.want, tc.wantBool)
		}
	}
}

// writePlan writes a minimal test plan with a reject route and returns its path.
func writePlan(t *testing.T, rejectMethod, rejectURL string) string {
	t.Helper()
	plan := map[string]any{
		"version": "v1",
		"root": map[string]any{
			"type": "sequence",
			"steps": []any{
				map[string]any{
					"type":   "request",
					"method": rejectMethod,
					"url":    rejectURL,
				},
				map[string]any{
					"type":       "assert",
					"expression": "status in [400,412,422]",
				},
			},
		},
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	path := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	return path
}

func TestLoadPlanRoutes(t *testing.T) {
	path := writePlan(t, "PUT", "http://example.org/Patient/p1")
	routes, err := loadPlanRoutes(path)
	if err != nil {
		t.Fatalf("loadPlanRoutes: %v", err)
	}
	// The route is keyed by method + path (host stripped).
	route, ok := routes.rejects["PUT /Patient/p1"]
	if !ok {
		t.Fatalf("expected reject route for PUT /Patient/p1, got %v", routes.rejects)
	}
	if route.status != 400 {
		t.Fatalf("reject status = %d, want 400", route.status)
	}
}

func TestLoadPlanRoutesMissingFile(t *testing.T) {
	if _, err := loadPlanRoutes(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("expected error for missing plan file")
	}
}

func TestPlanAwareServerCRUD(t *testing.T) {
	path := writePlan(t, "PUT", "http://example.org/Patient/reject-me")
	s := New(http.StatusOK, "", WithPlan(path))
	addr, err := s.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()

	base := "http://" + addr

	// PUT stores a resource.
	body := `{"resourceType":"Patient","id":"p1","name":[{"family":"Doe"}]}`
	req, _ := http.NewRequest(http.MethodPut, base+"/Patient/p1", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/fhir+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", resp.StatusCode)
	}

	// GET retrieves it.
	resp, err = http.Get(base + "/Patient/p1")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", resp.StatusCode)
	}
	if string(got) != body {
		t.Fatalf("GET body = %q, want %q", got, body)
	}

	// GET on a missing resource returns 404.
	resp, err = http.Get(base + "/Patient/missing")
	if err != nil {
		t.Fatalf("GET missing: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET missing status = %d, want 404", resp.StatusCode)
	}

	// DELETE removes it.
	req, _ = http.NewRequest(http.MethodDelete, base+"/Patient/p1", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want 204", resp.StatusCode)
	}

	// GET after DELETE returns 404.
	resp, err = http.Get(base + "/Patient/p1")
	if err != nil {
		t.Fatalf("GET after delete: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET after delete status = %d, want 404", resp.StatusCode)
	}
}

func TestPlanAwareServerSearch(t *testing.T) {
	path := writePlan(t, "PUT", "http://example.org/Patient/reject-me")
	s := New(http.StatusOK, "", WithPlan(path))
	addr, err := s.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()

	base := "http://" + addr
	// Store two patients.
	for _, id := range []string{"p1", "p2"} {
		body := `{"resourceType":"Patient","id":"` + id + `"}`
		req, _ := http.NewRequest(http.MethodPut, base+"/Patient/"+id, bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/fhir+json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("PUT %s: %v", id, err)
		}
		resp.Body.Close()
	}

	// Search returns a Bundle with total 2.
	resp, err := http.Get(base + "/Patient?name=Doe")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search status = %d, want 200", resp.StatusCode)
	}
	var bundle struct {
		ResourceType string `json:"resourceType"`
		Total        int    `json:"total"`
		Entry        []any  `json:"entry"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&bundle); err != nil {
		t.Fatalf("decode bundle: %v", err)
	}
	if bundle.ResourceType != "Bundle" {
		t.Fatalf("resourceType = %q, want Bundle", bundle.ResourceType)
	}
	if bundle.Total != 2 {
		t.Fatalf("total = %d, want 2", bundle.Total)
	}
	if len(bundle.Entry) != 2 {
		t.Fatalf("entry count = %d, want 2", len(bundle.Entry))
	}
}

func TestPlanAwareServerRejectsFromPlan(t *testing.T) {
	path := writePlan(t, "PUT", "http://example.org/Patient/reject-me")
	s := New(http.StatusOK, "", WithPlan(path))
	addr, err := s.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()

	// The plan expects PUT /Patient/reject-me to be rejected with 400.
	req, _ := http.NewRequest(http.MethodPut, "http://"+addr+"/Patient/reject-me", bytes.NewReader([]byte(`{"resourceType":"Patient"}`)))
	req.Header.Set("Content-Type", "application/fhir+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT reject: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("reject status = %d, want 400", resp.StatusCode)
	}
}

func TestPlanAwareServerMetadata(t *testing.T) {
	path := writePlan(t, "PUT", "http://example.org/Patient/reject-me")
	s := New(http.StatusOK, "", WithPlan(path))
	addr, err := s.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()

	resp, err := http.Get("http://" + addr + "/metadata")
	if err != nil {
		t.Fatalf("GET metadata: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("metadata status = %d, want 200", resp.StatusCode)
	}
	var cs struct {
		ResourceType string `json:"resourceType"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&cs); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if cs.ResourceType != "CapabilityStatement" {
		t.Fatalf("resourceType = %q, want CapabilityStatement", cs.ResourceType)
	}
}

func TestWithPlanMissingFile(t *testing.T) {
	s := New(http.StatusOK, "", WithPlan(filepath.Join(t.TempDir(), "missing.json")))
	if _, err := s.Start(); err == nil {
		t.Fatal("expected error for missing plan file")
	}
}
