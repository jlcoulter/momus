package coverage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

func TestResourceTypesFromCapabilityStatementsRequireCreate(t *testing.T) {
	r := registry.New()
	r.AddCapabilityStatement(&model.CapabilityStatement{
		URL: "http://example.org/CapabilityStatement/server",
		Rest: []model.CapabilityStatementRest{
			{
				Mode: "server",
				Resource: []model.CapabilityStatementRestResource{
					{Type: "Patient", Interaction: []model.CapabilityStatementInteraction{{Code: "read"}, {Code: "create"}}},
					{Type: "Observation", Interaction: []model.CapabilityStatementInteraction{{Code: "read"}}},
				},
			},
		},
	})

	got := ResourceTypesFromCapabilityStatements(r, true)
	want := []string{"Patient"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestResourceTypesFromCapabilityStatementsIncludesAllWhenCreateNotRequired(t *testing.T) {
	r := registry.New()
	r.AddCapabilityStatement(&model.CapabilityStatement{
		URL: "http://example.org/CapabilityStatement/server",
		Rest: []model.CapabilityStatementRest{
			{
				Mode: "server",
				Resource: []model.CapabilityStatementRestResource{
					{Type: "Patient", Interaction: []model.CapabilityStatementInteraction{{Code: "read"}}},
					{Type: "Observation", Interaction: []model.CapabilityStatementInteraction{{Code: "search-type"}}},
				},
			},
		},
	})

	got := ResourceTypesFromCapabilityStatements(r, false)
	want := []string{"Observation", "Patient"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestResourceTypesFromCapabilityStatementsIgnoresNonServerMode(t *testing.T) {
	r := registry.New()
	r.AddCapabilityStatement(&model.CapabilityStatement{
		URL: "http://example.org/CapabilityStatement/client",
		Rest: []model.CapabilityStatementRest{
			{
				Mode: "client",
				Resource: []model.CapabilityStatementRestResource{
					{Type: "Patient", Interaction: []model.CapabilityStatementInteraction{{Code: "create"}}},
				},
			},
		},
	})

	if got := ResourceTypesFromCapabilityStatements(r, true); got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

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
