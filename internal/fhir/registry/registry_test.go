package registry

import (
	"errors"
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
)

// TestResolveProfileResolvesParentChain verifies that ResolveProfile merges the
// parent (baseDefinition) dependency chain, so a differential profile inherits
// its base's elements and constraints, and child elements override the parent's.
func TestRegistryIndexesResourcesByType(t *testing.T) {
	r := New()
	r.AddResource(&model.Resource{ResourceType: "Patient", Raw: map[string]any{"id": "a"}})
	r.AddResource(&model.Resource{ResourceType: "Patient", Raw: map[string]any{"id": "b"}})
	r.AddResource(&model.Resource{ResourceType: "PractitionerRole", Raw: map[string]any{"id": "c"}})
	// Nil and empty-type resources are ignored.
	r.AddResource(nil)
	r.AddResource(&model.Resource{})

	patients := r.ResourcesForType("Patient")
	if len(patients) != 2 {
		t.Fatalf("got %d Patient resources, want 2", len(patients))
	}
	if got := len(r.ResourcesForType("PractitionerRole")); got != 1 {
		t.Fatalf("got %d PractitionerRole resources, want 1", got)
	}
	if got := len(r.ResourcesForType("Observation")); got != 0 {
		t.Fatalf("got %d Observation resources, want 0", got)
	}
	if got := len(r.AllResources()); got != 3 {
		t.Fatalf("got %d total resources, want 3", got)
	}
}

func TestRegistryOverlayCapabilityScope(t *testing.T) {
	r := New()
	r.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/hcpd-patient", Type: "Patient"})
	r.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/hcpd-org", Type: "Organization"})
	r.AddStructureDefinition(&model.StructureDefinition{URL: "http://hl7.org/fhir/StructureDefinition/Observation", Type: "Observation"})
	// A server-mode CapabilityStatement that only serves Patient and Organization.
	r.AddCapabilityStatement(&model.CapabilityStatement{
		URL: "http://example.org/CapabilityStatement/server",
		Rest: []model.CapabilityStatementRest{{
			Mode: "server",
			Resource: []model.CapabilityStatementRestResource{
				{Type: "Patient"},
				{Type: "Organization"},
			},
		}},
	})
	r.MarkRootCapabilityStatements(&model.CapabilityStatement{URL: "http://example.org/CapabilityStatement/server"})

	// Before the overlay, everything is in scope (no scope set yet).
	if got := len(r.ScopedStructureDefinitions()); got != 3 {
		t.Fatalf("pre-overlay scoped defs = %d, want 3", got)
	}

	r.OverlayCapabilityScope()
	scoped := r.ScopedStructureDefinitions()
	if len(scoped) != 2 {
		t.Fatalf("post-overlay scoped defs = %d, want 2", len(scoped))
	}
	for _, sd := range scoped {
		if sd.Type == "Observation" {
			t.Fatal("Observation should be out of scope (not served by the capability statement)")
		}
	}
	// Out-of-scope defs remain resolvable for dependency resolution.
	if _, ok := r.StructureDefinition("http://hl7.org/fhir/StructureDefinition/Observation"); !ok {
		t.Fatal("out-of-scope Observation should remain indexed for dependency resolution")
	}
}

func TestRegistryOverlayCapabilityScopeNoServer(t *testing.T) {
	r := New()
	r.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/patient", Type: "Patient"})
	// Only client-mode entries: overlay must not narrow.
	r.AddCapabilityStatement(&model.CapabilityStatement{
		Rest: []model.CapabilityStatementRest{{Mode: "client", Resource: []model.CapabilityStatementRestResource{{Type: "Encounter"}}}},
	})
	r.OverlayCapabilityScope()
	if got := len(r.ScopedStructureDefinitions()); got != 1 {
		t.Fatalf("scoped defs = %d, want 1 (no server-mode narrowing)", got)
	}
}

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

func TestRegistryIndexesValueSetAndCodeSystem(t *testing.T) {
	r := New()
	vs := &model.ValueSet{URL: "http://example.org/ValueSet/vs", Name: "VS"}
	cs := &model.CodeSystem{URL: "http://example.org/CodeSystem/cs", Name: "CS"}
	r.AddValueSet(vs)
	r.AddCodeSystem(cs)
	// Nil and empty-URL entries are ignored.
	r.AddValueSet(nil)
	r.AddValueSet(&model.ValueSet{})
	r.AddCodeSystem(nil)
	r.AddCodeSystem(&model.CodeSystem{})

	if got, ok := r.ValueSet("http://example.org/ValueSet/vs"); !ok || got != vs {
		t.Fatalf("ValueSet = %v, %v; want the indexed value set", got, ok)
	}
	if got, ok := r.CodeSystem("http://example.org/CodeSystem/cs"); !ok || got != cs {
		t.Fatalf("CodeSystem = %v, %v; want the indexed code system", got, ok)
	}
	if _, ok := r.ValueSet("http://example.org/missing"); ok {
		t.Fatal("did not expect a value set for an unknown URL")
	}
	if _, ok := r.CodeSystem("http://example.org/missing"); ok {
		t.Fatal("did not expect a code system for an unknown URL")
	}
}

func TestRegistryStructureDefinitionsAndCapabilityStatements(t *testing.T) {
	r := New()
	sd1 := &model.StructureDefinition{URL: "http://example.org/StructureDefinition/a", Type: "Patient"}
	sd2 := &model.StructureDefinition{URL: "http://example.org/StructureDefinition/b", Type: "Observation"}
	r.AddStructureDefinition(sd1)
	r.AddStructureDefinition(sd2)
	cs := &model.CapabilityStatement{URL: "http://example.org/CapabilityStatement/server"}
	r.AddCapabilityStatement(cs)

	defs := r.StructureDefinitions()
	if len(defs) != 2 {
		t.Fatalf("StructureDefinitions() = %d, want 2", len(defs))
	}
	seen := map[string]bool{}
	for _, sd := range defs {
		seen[sd.URL] = true
	}
	if !seen[sd1.URL] || !seen[sd2.URL] {
		t.Fatalf("StructureDefinitions() missing entries: %v", seen)
	}

	stmts := r.CapabilityStatements()
	if len(stmts) != 1 || stmts[0] != cs {
		t.Fatalf("CapabilityStatements() = %v, want the indexed statement", stmts)
	}
}

func TestRegistrySearchParametersDeduplicates(t *testing.T) {
	r := New()
	sp := &model.SearchParameter{Code: "name", Base: []string{"Patient", "Practitioner"}, Name: "name"}
	r.AddSearchParameter(sp)
	// Nil and empty-code entries are ignored.
	r.AddSearchParameter(nil)
	r.AddSearchParameter(&model.SearchParameter{})

	if got, ok := r.SearchParameter("Patient", "name"); !ok || got != sp {
		t.Fatalf("SearchParameter(Patient,name) = %v, %v; want the indexed parameter", got, ok)
	}
	if got, ok := r.SearchParameter("Practitioner", "name"); !ok || got != sp {
		t.Fatalf("SearchParameter(Practitioner,name) = %v, %v; want the indexed parameter", got, ok)
	}
	if _, ok := r.SearchParameter("Patient", "other"); ok {
		t.Fatal("did not expect a search parameter for an unknown code")
	}
	// The same parameter indexed under two resource types is returned once.
	if got := r.SearchParameters(); len(got) != 1 {
		t.Fatalf("SearchParameters() = %d, want 1 (deduplicated)", len(got))
	}
}

func TestRegistrySetScopeToResourceTypesAndProfiles(t *testing.T) {
	r := New()
	r.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/pat", Type: "Patient"})
	r.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/obs", Type: "Observation"})
	r.AddStructureDefinition(&model.StructureDefinition{URL: "http://hl7.org/fhir/StructureDefinition/Patient", Type: "Patient"})

	// Narrow to Patient only.
	r.SetScopeToResourceTypesAndProfiles([]string{"Patient"}, nil)
	scoped := r.ScopedStructureDefinitions()
	if len(scoped) != 2 {
		t.Fatalf("scoped defs = %d, want 2 (both Patient profiles)", len(scoped))
	}
	for _, sd := range scoped {
		if sd.Type != "Patient" {
			t.Fatalf("unexpected scoped type %q, want Patient", sd.Type)
		}
	}

	// Narrow further to a specific profile URL.
	r.SetScopeToResourceTypesAndProfiles([]string{"Patient"}, []string{"http://example.org/StructureDefinition/pat"})
	scoped = r.ScopedStructureDefinitions()
	if len(scoped) != 1 || scoped[0].URL != "http://example.org/StructureDefinition/pat" {
		t.Fatalf("scoped defs = %+v, want only the example Patient profile", scoped)
	}

	// Empty types + empty profiles on a fresh registry keeps everything in scope.
	fresh := New()
	fresh.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/pat", Type: "Patient"})
	fresh.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/obs", Type: "Observation"})
	fresh.SetScopeToResourceTypesAndProfiles(nil, nil)
	if got := len(fresh.ScopedStructureDefinitions()); got != 2 {
		t.Fatalf("scoped defs = %d, want 2 (no narrowing)", got)
	}
}

func TestRegistrySetScopeToResourceTypesAndProfilesIntersectsExistingScope(t *testing.T) {
	r := New()
	r.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/pat", Type: "Patient"})
	r.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/obs", Type: "Observation"})
	r.SetScope([]string{"http://example.org/StructureDefinition/pat", "http://example.org/StructureDefinition/obs"})

	// Overlay a type filter on top of the existing scope.
	r.SetScopeToResourceTypesAndProfiles([]string{"Observation"}, nil)
	scoped := r.ScopedStructureDefinitions()
	if len(scoped) != 1 || scoped[0].URL != "http://example.org/StructureDefinition/obs" {
		t.Fatalf("scoped defs = %+v, want only the Observation profile", scoped)
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

// TestRegistryEmptyButSetScopeReturnsNoDefinitions (task #31) verifies that an
// empty-but-set scope (e.g. SetScope([]string{""})) is a genuine empty selection
// and returns no definitions, rather than being treated as "no scope" and
// returning every indexed definition. Before the fix, SetScope built an empty
// non-nil map and ScopedStructureDefinitions treated len==0 as "no scope".
func TestRegistryEmptyButSetScopeReturnsNoDefinitions(t *testing.T) {
	r := New()
	r.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/root-a", Type: "Patient"})
	r.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/root-b", Type: "Observation"})

	r.SetScope([]string{""})
	if got := len(r.ScopedStructureDefinitions()); got != 0 {
		t.Fatalf("empty-but-set scope returned %d definitions, want 0", got)
	}

	// Clearing the scope restores all definitions as subjects.
	r.SetScope(nil)
	if got := len(r.ScopedStructureDefinitions()); got != 2 {
		t.Fatalf("cleared scope returned %d definitions, want 2", got)
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

// TestResolveProfileCachesResult verifies that ResolveProfile memoises its
// result: a second call for the same URL returns the identical pointer rather
// than re-resolving the profile.
func TestResolveProfileCachesResult(t *testing.T) {
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

	first, err := r.ResolveProfile("http://hl7.org/fhir/StructureDefinition/Identifier")
	if err != nil {
		t.Fatalf("first ResolveProfile: %v", err)
	}
	second, err := r.ResolveProfile("http://hl7.org/fhir/StructureDefinition/Identifier")
	if err != nil {
		t.Fatalf("second ResolveProfile: %v", err)
	}
	if first != second {
		t.Fatal("ResolveProfile returned a different pointer on the second call; cache not hit")
	}
}

// TestResolveElementsCachesResult verifies that ResolveElements memoises its
// result: a second call for the same URL returns the identical slice rather
// than re-resolving the element set.
func TestResolveElementsCachesResult(t *testing.T) {
	r := New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/org",
		Type: "Organization",
		Elements: []model.ElementDefinition{
			{Path: "Organization", Min: 0, Max: "1"},
			{Path: "Organization.name", Min: 0, Max: "1", Types: []model.ElementType{{Code: "string"}}},
		},
	})

	first, err := r.ResolveElements("http://example.org/StructureDefinition/org")
	if err != nil {
		t.Fatalf("first ResolveElements: %v", err)
	}
	second, err := r.ResolveElements("http://example.org/StructureDefinition/org")
	if err != nil {
		t.Fatalf("second ResolveElements: %v", err)
	}
	if &first[0] != &second[0] {
		t.Fatal("ResolveElements returned a different backing array on the second call; cache not hit")
	}
}

// TestResolveProfileCacheConcurrent verifies that concurrent ResolveProfile
// calls for the same URL are safe and return consistent results.
func TestResolveProfileCacheConcurrent(t *testing.T) {
	r := New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://hl7.org/fhir/StructureDefinition/Identifier",
		Type: "Identifier",
		Elements: []model.ElementDefinition{
			{Path: "Identifier", Min: 0, Max: "*"},
			{Path: "Identifier.system", Min: 0, Max: "1", Types: []model.ElementType{{Code: "uri"}}},
		},
	})

	const goroutines = 16
	const iterations = 100
	errCh := make(chan error, goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			for i := 0; i < iterations; i++ {
				res, err := r.ResolveProfile("http://hl7.org/fhir/StructureDefinition/Identifier")
				if err != nil {
					errCh <- err
					return
				}
				if res == nil || res.Root == nil {
					errCh <- errors.New("ResolveProfile returned nil root")
					return
				}
			}
			errCh <- nil
		}()
	}
	for g := 0; g < goroutines; g++ {
		if err := <-errCh; err != nil {
			t.Fatalf("concurrent ResolveProfile: %v", err)
		}
	}
}
