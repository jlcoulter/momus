package registry

import (
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
)

func TestRegistryIndexesStructureDefinitionByURL(t *testing.T) {
	r := New()
	sd := &model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/observation-profile",
		Type: "Observation",
		Name: "ObservationProfile",
	}
	r.AddStructureDefinition(sd)

	got, ok := r.StructureDefinition(sd.URL)
	if !ok {
		t.Fatal("expected structure definition to be found")
	}
	if got != sd {
		t.Fatalf("got %p, want %p", got, sd)
	}
}

func TestRegistryIndexesProfilesByResourceType(t *testing.T) {
	r := New()
	profiles := []*model.StructureDefinition{
		{URL: "http://example.org/StructureDefinition/obs-a", Type: "Observation"},
		{URL: "http://example.org/StructureDefinition/obs-b", Type: "Observation"},
		{URL: "http://example.org/StructureDefinition/pat", Type: "Patient"},
	}
	for _, p := range profiles {
		r.AddStructureDefinition(p)
	}

	got := r.ProfilesForResource("Observation")
	if len(got) != 2 {
		t.Fatalf("got %d profiles, want 2", len(got))
	}
}

func TestRegistryIndexesSearchParameterByResourceAndCode(t *testing.T) {
	r := New()
	sp := &model.SearchParameter{Code: "code", Base: []string{"Observation"}, Name: "code"}
	r.AddSearchParameter(sp)

	got, ok := r.SearchParameter("Observation", "code")
	if !ok {
		t.Fatal("expected search parameter to be found")
	}
	if got != sp {
		t.Fatalf("got %p, want %p", got, sp)
	}

	if _, ok := r.SearchParameter("Patient", "code"); ok {
		t.Fatal("did not expect search parameter for a different resource type")
	}
}

func TestRegistryResolveProfileBuildsElementTree(t *testing.T) {
	r := New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://hl7.org/fhir/StructureDefinition/Observation",
		Type: "Observation",
		Name: "Observation",
		Elements: []model.ElementDefinition{
			{Path: "Observation", Name: "Observation"},
			{Path: "Observation.component", Name: "component"},
			{Path: "Observation.component.code", Name: "code"},
			{Path: "Observation.component.code.coding", Name: "coding"},
			{Path: "Observation.component.code.coding.code", Name: "code"},
		},
	})

	profile, err := r.ResolveProfile("http://hl7.org/fhir/StructureDefinition/Observation")
	if err != nil {
		t.Fatalf("ResolveProfile returned error: %v", err)
	}
	if profile.ResourceType != "Observation" {
		t.Fatalf("got resource type %q, want Observation", profile.ResourceType)
	}
	if _, ok := profile.Elements["Observation.component.code.coding.code"]; !ok {
		t.Fatal("expected path lookup to succeed")
	}

	if _, err := r.ResolveProfile("http://example.org/missing"); err != ErrNotFound {
		t.Fatalf("got error %v, want ErrNotFound", err)
	}
}
