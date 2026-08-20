package registry

import (
	"errors"
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
)

// TestResolveProfileResolvesParentChain verifies that ResolveProfile merges the
// parent (baseDefinition) dependency chain, so a differential profile inherits
// its base's elements and constraints, and child elements override the parent's.
func TestResolveProfileResolvesParentChain(t *testing.T) {
	r := New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://hl7.org/fhir/StructureDefinition/Identifier",
		Type: "Identifier",
		Elements: []model.ElementDefinition{
			{Path: "Identifier", Min: 0, Max: "*"},
			{Path: "Identifier.system", Min: 0, Max: "1", Types: []model.ElementType{{Code: "uri"}}},
			{Path: "Identifier.value", Min: 0, Max: "1", Types: []model.ElementType{{Code: "string"}}},
		},
	})
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:            "http://example.org/StructureDefinition/abn",
		Type:           "Identifier",
		BaseDefinition: "http://hl7.org/fhir/StructureDefinition/Identifier",
		Elements: []model.ElementDefinition{
			{Path: "Identifier", Min: 0, Max: "*"},
			{Path: "Identifier.system", Min: 1, Max: "1", Types: []model.ElementType{{Code: "uri"}}, Fixed: "http://hl7.org.au/id/abn"},
			{Path: "Identifier.value", Min: 1, Max: "1", Types: []model.ElementType{{Code: "string"}}, Constraints: []model.ElementConstraint{{Key: "inv-abn-0", Severity: "error", Expression: "value.matches('^([0-9]{11})$')"}}},
		},
	})

	res, err := r.ResolveProfile("http://example.org/StructureDefinition/abn")
	if err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}
	system := res.Elements["Identifier.system"]
	if system == nil || system.Definition == nil {
		t.Fatal("Identifier.system missing after parent-chain resolution")
	}
	if system.Definition.Fixed != "http://hl7.org.au/id/abn" {
		t.Fatalf("system fixed = %v, want the child's override", system.Definition.Fixed)
	}
	value := res.Elements["Identifier.value"]
	if value == nil || value.Definition == nil || len(value.Definition.Constraints) == 0 {
		t.Fatal("Identifier.value must inherit the child's invariant constraint")
	}
}

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

	if _, err := r.ResolveProfile("http://example.org/missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got error %v, want ErrNotFound", err)
	}
}

func TestRegistryScopeRestrictsScopedStructureDefinitions(t *testing.T) {
	r := New()
	r.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/root-a", Type: "Patient"})
	r.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/root-b", Type: "Observation"})
	r.AddStructureDefinition(&model.StructureDefinition{URL: "http://hl7.org/fhir/StructureDefinition/Patient", Type: "Patient"})

	// Without a scope, every indexed definition is a subject.
	if got := len(r.ScopedStructureDefinitions()); got != 3 {
		t.Fatalf("unscoped ScopedStructureDefinitions returned %d, want 3", got)
	}

	r.SetScope([]string{"http://example.org/StructureDefinition/root-a", "http://example.org/StructureDefinition/root-b"})
	scoped := r.ScopedStructureDefinitions()
	if len(scoped) != 2 {
		t.Fatalf("scoped ScopedStructureDefinitions returned %d, want 2", len(scoped))
	}
	for _, sd := range scoped {
		if sd.URL == "http://hl7.org/fhir/StructureDefinition/Patient" {
			t.Fatalf("out-of-scope definition %q returned as a subject", sd.URL)
		}
	}

	// Out-of-scope definitions remain resolvable for dependency resolution.
	if _, ok := r.StructureDefinition("http://hl7.org/fhir/StructureDefinition/Patient"); !ok {
		t.Fatal("out-of-scope definition should remain indexed for dependency resolution")
	}

	// Clearing the scope restores all definitions as subjects.
	r.SetScope(nil)
	if got := len(r.ScopedStructureDefinitions()); got != 3 {
		t.Fatalf("cleared scope returned %d, want 3", got)
	}
}

func TestRegistryScopeIgnoresUnknownURLs(t *testing.T) {
	r := New()
	r.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/root", Type: "Patient"})
	r.SetScope([]string{"http://example.org/StructureDefinition/root", "http://example.org/StructureDefinition/missing"})
	if got := len(r.ScopedStructureDefinitions()); got != 1 {
		t.Fatalf("scoped ScopedStructureDefinitions returned %d, want 1", got)
	}
}

// TestResolveElementsKeepsIDBasedSliceChildrenDistinct (task #30) verifies that a
// slice child whose slice context lives only in its ID does not override its base
// element during the parent-chain merge. Before the fix, elementKey ignored the ID
// slice segment, so the base extension.url and the suppressed slice's extension.url
// collided and one clobbered the other.
func TestResolveElementsKeepsIDBasedSliceChildrenDistinct(t *testing.T) {
	r := New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/base-org",
		Type: "Organization",
		Elements: []model.ElementDefinition{
			{Path: "Organization", Min: 0, Max: "1"},
			{Path: "Organization.extension", Min: 0, Max: "*", Types: []model.ElementType{{Code: "Extension"}}},
			// The base element that the child's ID-based slice child shares a path with.
			{Path: "Organization.extension.url", Min: 1, Max: "1", Types: []model.ElementType{{Code: "uri"}}},
		},
	})
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:            "http://example.org/StructureDefinition/org",
		Type:           "Organization",
		BaseDefinition: "http://example.org/StructureDefinition/base-org",
		Elements: []model.ElementDefinition{
			{Path: "Organization.extension", Min: 0, Max: "*"},
			{ID: "Organization.extension:suppressed", Path: "Organization.extension", SliceName: "suppressed", Min: 0, Max: "1"},
			// A slice child whose slice context lives only in its ID (no SliceName).
			{ID: "Organization.extension:suppressed.url", Path: "Organization.extension.url", Min: 1, Max: "1", Fixed: "http://example.org/suppressed"},
		},
	})

	els, err := r.ResolveElements("http://example.org/StructureDefinition/org")
	if err != nil {
		t.Fatalf("ResolveElements: %v", err)
	}
	var hasBaseURL, hasSliceURL bool
	for _, el := range els {
		if el.Path == "Organization.extension.url" && el.ID == "" && el.Fixed == nil {
			hasBaseURL = true
		}
		if el.ID == "Organization.extension:suppressed.url" && el.Fixed == "http://example.org/suppressed" {
			hasSliceURL = true
		}
	}
	if !hasBaseURL {
		t.Fatal("base Organization.extension.url element was clobbered by the ID-based slice child")
	}
	if !hasSliceURL {
		t.Fatal("ID-based slice child Organization.extension:suppressed.url missing after merge")
	}
}
