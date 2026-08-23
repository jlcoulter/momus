package fhircoverage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
)

func TestSupportedProfileURLsFromCapabilityStatement(t *testing.T) {
	cs := &model.CapabilityStatement{
		Rest: []model.CapabilityStatementRest{{
			Mode: "server",
			Resource: []model.CapabilityStatementRestResource{
				{
					Type:             "Organization",
					SupportedProfile: []string{"http://example.org/StructureDefinition/org-a", "http://example.org/StructureDefinition/org-b"},
					Interaction:      []model.CapabilityStatementInteraction{{Code: "create"}},
				},
				{
					Type:             "Observation",
					SupportedProfile: []string{"http://example.org/StructureDefinition/obs-a"},
					Interaction:      []model.CapabilityStatementInteraction{{Code: "read"}},
				},
			},
		}},
	}

	got := SupportedProfileURLsFromCapabilityStatement(cs, true)
	want := []string{
		"http://example.org/StructureDefinition/org-a",
		"http://example.org/StructureDefinition/org-b",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSupportedProfileURLsByResourceFromCapabilityStatement(t *testing.T) {
	cs := &model.CapabilityStatement{
		Rest: []model.CapabilityStatementRest{{
			Mode: "server",
			Resource: []model.CapabilityStatementRestResource{
				{
					Type:             "PractitionerRole",
					SupportedProfile: []string{"http://example.org/StructureDefinition/practitionerrole-a", "http://example.org/StructureDefinition/practitionerrole-b"},
					Interaction:      []model.CapabilityStatementInteraction{{Code: "create"}},
				},
				{
					Type:             "Observation",
					SupportedProfile: []string{"http://example.org/StructureDefinition/observation-a"},
					Interaction:      []model.CapabilityStatementInteraction{{Code: "read"}},
				},
			},
		}},
	}

	got := SupportedProfileURLsByResourceFromCapabilityStatement(cs, true)
	want := map[string][]string{
		"PractitionerRole": {
			"http://example.org/StructureDefinition/practitionerrole-a",
			"http://example.org/StructureDefinition/practitionerrole-b",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestFetchCapabilityStatementUsesMetadataEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metadata" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/fhir+json")
		_, _ = w.Write([]byte(`{
			"resourceType":"CapabilityStatement",
			"url":"http://example.org/CapabilityStatement/server",
			"rest":[{
				"mode":"server",
				"resource":[
					{"type":"Patient","interaction":[{"code":"create"},{"code":"read"}]},
					{"type":"Observation","interaction":[{"code":"read"}]}
				]
			}]
		}`))
	}))
	defer server.Close()

	cs, err := FetchCapabilityStatement(context.Background(), server.URL, CapabilityFetchOptions{HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("FetchCapabilityStatement returned error: %v", err)
	}
	got := ResourceTypesFromCapabilityStatement(cs, true)
	want := []string{"Patient"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestFetchCapabilityStatementAppliesBasicAuth(t *testing.T) {
	var gotUsername string
	var gotPassword string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metadata" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		gotUsername, gotPassword, _ = r.BasicAuth()
		w.Header().Set("Content-Type", "application/fhir+json")
		_, _ = w.Write([]byte(`{"resourceType":"CapabilityStatement","rest":[{"mode":"server","resource":[{"type":"Patient","interaction":[{"code":"create"}]}]}]}`))
	}))
	defer server.Close()

	_, err := FetchCapabilityStatement(context.Background(), server.URL, CapabilityFetchOptions{
		HTTPClient:    server.Client(),
		BasicUsername: "admin",
		BasicPassword: "admin123",
	})
	if err != nil {
		t.Fatalf("FetchCapabilityStatement returned error: %v", err)
	}
	if gotUsername != "admin" || gotPassword != "admin123" {
		t.Fatalf("got basic auth %q/%q, want admin/admin123", gotUsername, gotPassword)
	}
}

func TestResourceTypesFromCapabilityStatementWithoutCreateRequirement(t *testing.T) {
	cs := &model.CapabilityStatement{
		Rest: []model.CapabilityStatementRest{
			{
				Mode: "server",
				Resource: []model.CapabilityStatementRestResource{
					{Type: "Patient", Interaction: []model.CapabilityStatementInteraction{{Code: "create"}}},
					{Type: "Observation", Interaction: []model.CapabilityStatementInteraction{{Code: "read"}}},
					{Type: "  ", Interaction: []model.CapabilityStatementInteraction{{Code: "read"}}},
				},
			},
			{
				Mode: "client",
				Resource: []model.CapabilityStatementRestResource{
					{Type: "Encounter", Interaction: []model.CapabilityStatementInteraction{{Code: "read"}}},
				},
			},
		},
	}

	// Without the create requirement, every server-mode type is included even if
	// it lacks a create interaction; client-mode entries are ignored.
	got := ResourceTypesFromCapabilityStatement(cs, false)
	want := []string{"Observation", "Patient"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	// With the create requirement, only types advertising create are included.
	gotCreate := ResourceTypesFromCapabilityStatement(cs, true)
	wantCreate := []string{"Patient"}
	if !reflect.DeepEqual(gotCreate, wantCreate) {
		t.Fatalf("got %v, want %v", gotCreate, wantCreate)
	}
}

func TestResourceTypesFromCapabilityStatementNil(t *testing.T) {
	if got := ResourceTypesFromCapabilityStatement(nil, false); got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

func TestSearchCodesFromCapabilityStatement(t *testing.T) {
	cs := &model.CapabilityStatement{
		Rest: []model.CapabilityStatementRest{
			{
				Mode: "server",
				Resource: []model.CapabilityStatementRestResource{
					{Type: "Patient", SearchParam: []model.CapabilityStatementSearchParam{{Name: "name"}, {Name: "_id"}}},
					{Type: "Observation", SearchParam: []model.CapabilityStatementSearchParam{{Name: "status"}}},
				},
			},
			{
				Mode: "client",
				Resource: []model.CapabilityStatementRestResource{
					{Type: "Encounter", SearchParam: []model.CapabilityStatementSearchParam{{Name: "identifier"}}},
				},
			},
		},
	}

	got := SearchCodesFromCapabilityStatement(cs)
	want := map[string][]string{
		"Patient":     {"_id", "name"},
		"Observation": {"status"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSearchCodesFromCapabilityStatementNil(t *testing.T) {
	if got := SearchCodesFromCapabilityStatement(nil); got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

func TestFetchCapabilityStatementAppliesBearerToken(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metadata" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/fhir+json")
		_, _ = w.Write([]byte(`{"resourceType":"CapabilityStatement","rest":[{"mode":"server","resource":[{"type":"Patient","interaction":[{"code":"create"}]}]}]}`))
	}))
	defer server.Close()

	_, err := FetchCapabilityStatement(context.Background(), server.URL, CapabilityFetchOptions{
		HTTPClient:  server.Client(),
		BearerToken: "secret-token",
	})
	if err != nil {
		t.Fatalf("FetchCapabilityStatement returned error: %v", err)
	}
	if gotAuth != "Bearer secret-token" {
		t.Fatalf("got Authorization %q, want %q", gotAuth, "Bearer secret-token")
	}
}

func TestSearchCodesFromCapabilityStatementEmptySearchParam(t *testing.T) {
	cs := &model.CapabilityStatement{
		Rest: []model.CapabilityStatementRest{{
			Mode: "server",
			Resource: []model.CapabilityStatementRestResource{
				{Type: "Patient", SearchParam: nil},
				{Type: "Observation", SearchParam: []model.CapabilityStatementSearchParam{{Name: "status"}}},
			},
		}},
	}

	codes := SearchCodesFromCapabilityStatement(cs)
	// Patient is present but declares no searchParam: it must map to an empty
	// slice so it gets no allowed codes, rather than being absent (which would
	// allow every code).
	patient, ok := codes["Patient"]
	if !ok {
		t.Fatal("expected Patient present in capability search codes")
	}
	if len(patient) != 0 {
		t.Fatalf("got Patient codes %v, want empty", patient)
	}
	if isSearchCodeAllowed("Patient", "_id", codes) {
		t.Fatal("expected no search codes allowed for Patient with empty searchParam")
	}
	if !isSearchCodeAllowed("Observation", "status", codes) {
		t.Fatal("expected status allowed for Observation")
	}
}

func TestSearchCodesFromCapabilityStatementUnion(t *testing.T) {
	csA := &model.CapabilityStatement{
		Rest: []model.CapabilityStatementRest{{
			Mode: "server",
			Resource: []model.CapabilityStatementRestResource{
				{Type: "Practitioner", SearchParam: []model.CapabilityStatementSearchParam{{Name: "name"}, {Name: "_parameters"}}},
			},
		}},
	}
	csB := &model.CapabilityStatement{
		Rest: []model.CapabilityStatementRest{{
			Mode: "server",
			Resource: []model.CapabilityStatementRestResource{
				{Type: "Practitioner", SearchParam: []model.CapabilityStatementSearchParam{{Name: "active"}}},
			},
		}},
	}

	got := SearchCodesFromCapabilityStatementUnion([]*model.CapabilityStatement{csA, csB})
	want := map[string][]string{
		"Practitioner": {"_parameters", "active", "name"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	if got := SearchCodesFromCapabilityStatementUnion(nil); got != nil {
		t.Fatalf("got %v, want nil for empty input", got)
	}
	if got := SearchCodesFromCapabilityStatementUnion([]*model.CapabilityStatement{nil}); got != nil {
		t.Fatalf("got %v, want nil for nil statements", got)
	}
}

func TestFetchCapabilityStatementLimitsResponseBody(t *testing.T) {
	padding := strings.Repeat("x", 2<<20) // 2 MiB, exceeds the 1 MiB limit
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/fhir+json")
		_, _ = w.Write([]byte(`{"resourceType":"CapabilityStatement","rest":[],"padding":"` + padding + `"}`))
	}))
	defer server.Close()

	if _, err := FetchCapabilityStatement(context.Background(), server.URL, CapabilityFetchOptions{HTTPClient: server.Client()}); err == nil {
		t.Fatal("expected error when /metadata body exceeds size limit")
	}
}

func TestApplyCapabilityRequestAuth(t *testing.T) {
	applyCapabilityRequestAuth(nil, CapabilityFetchOptions{}) // no panic

	// Existing auth preserved.
	req, _ := http.NewRequest("GET", "http://x", nil)
	req.Header.Set("Authorization", "Existing")
	applyCapabilityRequestAuth(req, CapabilityFetchOptions{BearerToken: "tok"})
	if req.Header.Get("Authorization") != "Existing" {
		t.Fatal("existing auth overwritten")
	}
	// Bearer token.
	req, _ = http.NewRequest("GET", "http://x", nil)
	applyCapabilityRequestAuth(req, CapabilityFetchOptions{BearerToken: "tok"})
	if req.Header.Get("Authorization") != "Bearer tok" {
		t.Fatal("bearer auth not applied")
	}
	// Basic auth.
	req, _ = http.NewRequest("GET", "http://x", nil)
	applyCapabilityRequestAuth(req, CapabilityFetchOptions{BasicUsername: "u", BasicPassword: "p"})
	if _, _, ok := req.BasicAuth(); !ok {
		t.Fatal("basic auth not applied")
	}
}

func TestHasInteraction(t *testing.T) {
	interactions := []model.CapabilityStatementInteraction{{Code: "read"}, {Code: "Create"}}
	if !hasInteraction(interactions, "read") {
		t.Fatal("read should be present")
	}
	if !hasInteraction(interactions, "create") {
		t.Fatal("create should be present (case-insensitive)")
	}
	if hasInteraction(interactions, "delete") {
		t.Fatal("delete should be absent")
	}
}

func TestFetchCapabilityStatementErrorPaths(t *testing.T) {
	if _, err := FetchCapabilityStatement(context.Background(), "  ", CapabilityFetchOptions{}); err == nil {
		t.Fatal("expected error for empty base URL")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("denied"))
	}))
	defer server.Close()
	if _, err := FetchCapabilityStatement(context.Background(), server.URL, CapabilityFetchOptions{HTTPClient: server.Client()}); err == nil {
		t.Fatal("expected error for non-2xx status")
	}
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not-json"))
	}))
	defer server.Close()
	if _, err := FetchCapabilityStatement(context.Background(), server.URL, CapabilityFetchOptions{HTTPClient: server.Client()}); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"resourceType":"Patient"}`))
	}))
	defer server.Close()
	if _, err := FetchCapabilityStatement(context.Background(), server.URL, CapabilityFetchOptions{HTTPClient: server.Client()}); err == nil {
		t.Fatal("expected error for wrong resource type")
	}
}
