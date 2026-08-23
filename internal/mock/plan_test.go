package mock

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/jlcoulter/momus/internal/core/ast"
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

func TestRequestURI(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{"http://example.org/Patient?name=x", "/Patient?name=x"},
		{"http://example.org/Patient/p1", "/Patient/p1"},
		{"/Patient?name=x", "/Patient?name=x"},
		{"Patient/p1", "/Patient/p1"},
		{"http://example.org", "/"},
	}
	for _, tc := range cases {
		got := requestURI(tc.url)
		if got != tc.want {
			t.Fatalf("requestURI(%q) = %q, want %q", tc.url, got, tc.want)
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

func TestLoadPlanRoutesErrorBranches(t *testing.T) {
	dir := t.TempDir()
	// Invalid JSON.
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("not-json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPlanRoutes(bad); err == nil {
		t.Fatal("expected error for invalid plan JSON")
	}
	// Valid JSON with no root.
	noroot := filepath.Join(dir, "noroot.json")
	if err := os.WriteFile(noroot, []byte(`{"version":"v1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPlanRoutes(noroot); err == nil {
		t.Fatal("expected error for plan with no root")
	}
	// A root that fails to decode.
	badroot := filepath.Join(dir, "badroot.json")
	if err := os.WriteFile(badroot, []byte(`{"version":"v1","root":{"type":"bogus"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPlanRoutes(badroot); err == nil {
		t.Fatal("expected error for undecodable plan root")
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
	// Store two patients with names, one without.
	put := func(id, name string) {
		body := `{"resourceType":"Patient","id":"` + id + `","name":"` + name + `"}`
		req, _ := http.NewRequest(http.MethodPut, base+"/Patient/"+id, bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/fhir+json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("PUT %s: %v", id, err)
		}
		resp.Body.Close()
	}
	put("p1", "Doe")
	put("p2", "Doe")
	put("p3", "Smith")

	// A search with no query params returns all stored resources.
	total := searchTotal(t, base+"/Patient")
	if total != 3 {
		t.Fatalf("plain search total = %d, want 3", total)
	}

	// A filtered search matches only resources whose field equals the value.
	total = searchTotal(t, base+"/Patient?name=Doe")
	if total != 2 {
		t.Fatalf("name=Doe total = %d, want 2", total)
	}
	total = searchTotal(t, base+"/Patient?name=Smith")
	if total != 1 {
		t.Fatalf("name=Smith total = %d, want 1", total)
	}

	// _count limits the returned set.
	total = searchTotal(t, base+"/Patient?_count=1")
	if total != 1 {
		t.Fatalf("_count=1 total = %d, want 1", total)
	}
}

func searchTotal(t *testing.T, url string) int {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("search %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search %s status = %d, want 200", url, resp.StatusCode)
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
	if len(bundle.Entry) != bundle.Total {
		t.Fatalf("entry count %d != total %d", len(bundle.Entry), bundle.Total)
	}
	return bundle.Total
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

func TestSetPlanEnablesPlanAware(t *testing.T) {
	// Start plan-aware with an empty plan, then feed a reject route via SetPlan.
	s := New(http.StatusOK, "", WithPlanAware())
	addr, err := s.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()

	// Before SetPlan, a PUT is accepted (stored).
	req, _ := http.NewRequest(http.MethodPut, "http://"+addr+"/Patient/p1", bytes.NewReader([]byte(`{"resourceType":"Patient","id":"p1"}`)))
	req.Header.Set("Content-Type", "application/fhir+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT before SetPlan: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT before SetPlan status = %d, want 200", resp.StatusCode)
	}

	// Feed a plan that expects PUT /Patient/reject-me to be rejected.
	plan := &ast.Plan{Root: &ast.Sequence{Steps: []ast.Node{
		&ast.Request{Method: "PUT", URL: "http://example.org/Patient/reject-me"},
		&ast.Assert{Expression: "status in [400,412,422]"},
	}}}
	s.SetPlan(plan.Root)

	// Now the reject route is honored.
	req, _ = http.NewRequest(http.MethodPut, "http://"+addr+"/Patient/reject-me", bytes.NewReader([]byte(`{"resourceType":"Patient"}`)))
	req.Header.Set("Content-Type", "application/fhir+json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT after SetPlan: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("PUT after SetPlan status = %d, want 400", resp.StatusCode)
	}
}

func TestRejectRouteMatchesQuery(t *testing.T) {
	// A reject route keyed by path+query must match a search with that query.
	s := New(http.StatusOK, "", WithPlanAware())
	addr, err := s.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()

	plan := &ast.Plan{Root: &ast.Sequence{Steps: []ast.Node{
		&ast.Request{Method: "GET", URL: "http://example.org/Organization?name:zzz=momus"},
		&ast.Assert{Expression: "status in [400,412,422]"},
	}}}
	s.SetPlan(plan.Root)

	// The search with the invalid modifier must be rejected.
	resp, err := http.Get("http://" + addr + "/Organization?name:zzz=momus")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("reject status = %d, want 400", resp.StatusCode)
	}

	// A different query on the same path must NOT be rejected (no route match).
	resp, err = http.Get("http://" + addr + "/Organization?name=other")
	if err != nil {
		t.Fatalf("GET other: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("other query status = %d, want 200", resp.StatusCode)
	}
}

func TestInstanceGetNotOverriddenByReject(t *testing.T) {
	// A reject route on a plain instance GET (the final 404 of a CRUD sequence)
	// must not override the store's natural 200 for an existing resource.
	s := New(http.StatusOK, "", WithPlanAware())
	addr, err := s.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()

	// Store a resource.
	req, _ := http.NewRequest(http.MethodPut, "http://"+addr+"/Patient/p1", bytes.NewReader([]byte(`{"resourceType":"Patient","id":"p1"}`)))
	req.Header.Set("Content-Type", "application/fhir+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	resp.Body.Close()

	// Feed a plan that expects GET /Patient/p1 to be rejected (404).
	plan := &ast.Plan{Root: &ast.Sequence{Steps: []ast.Node{
		&ast.Request{Method: "GET", URL: "http://example.org/Patient/p1"},
		&ast.Assert{Expression: "status in [404]"},
	}}}
	s.SetPlan(plan.Root)

	// The existing resource must still return 200 (store is authoritative).
	resp, err = http.Get("http://" + addr + "/Patient/p1")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET existing status = %d, want 200", resp.StatusCode)
	}

	// A missing resource returns 404 naturally.
	resp, err = http.Get("http://" + addr + "/Patient/missing")
	if err != nil {
		t.Fatalf("GET missing: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET missing status = %d, want 404", resp.StatusCode)
	}
}
