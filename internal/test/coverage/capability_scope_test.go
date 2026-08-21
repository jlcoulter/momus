package coverage

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
