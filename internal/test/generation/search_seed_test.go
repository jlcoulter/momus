package generation

import (
	"strings"
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
	"github.com/jlcoulter/momus/internal/test/ast"
	"github.com/jlcoulter/momus/internal/test/coverage"
)

func TestBuildSetupDatasetAddsSearchMatchSeed(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/patient", Type: "Patient", Elements: []model.ElementDefinition{
		{Path: "Patient", Min: 0, Max: "*"},
		{Path: "Patient.name", Min: 1, Max: "*", Types: []model.ElementType{{Code: "HumanName"}}},
	}})
	reg.AddSearchParameter(&model.SearchParameter{URL: "http://hl7.org/fhir/SearchParameter/Patient-name", Name: "name", Code: "name", Base: []string{"Patient"}, Type: "string", Expression: "Patient.name"})

	plan := &coverage.CoveragePlan{Requirements: []coverage.CoverageRequirement{
		{ID: "search|Patient|name|search-multiple-results", ResourceType: "Patient", Domain: coverage.CoverageDomainSearch, Variant: coverage.CoverageVariantSearchMultipleResults, SearchCode: "name"},
	}}
	opts := BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: reg}

	ds, err := BuildSetupDataset(plan, opts)
	if err != nil {
		t.Fatalf("BuildSetupDataset returned error: %v", err)
	}

	// Two matching resources must exist so `name=momus-search` returns >= 2.
	var matching int
	for _, inst := range ds.Resources {
		name, ok := inst.Resource["name"].([]any)
		if !ok || len(name) == 0 {
			continue
		}
		first, ok := name[0].(map[string]any)
		if !ok {
			continue
		}
		if first["family"] == "momus-search" || first["text"] == "momus-search" {
			matching++
		}
	}
	if matching != 2 {
		t.Fatalf("expected 2 search-matching seed resources, got %d", matching)
	}
}

func TestBuildSetupDatasetAddsIDSearchSeed(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/patient", Type: "Patient", Elements: []model.ElementDefinition{
		{Path: "Patient", Min: 0, Max: "*"},
		{Path: "Patient.name", Min: 1, Max: "*", Types: []model.ElementType{{Code: "HumanName"}}},
	}})
	reg.AddSearchParameter(&model.SearchParameter{URL: "http://hl7.org/fhir/SearchParameter/Resource-id", Name: "_id", Code: "_id", Base: []string{"Resource"}, Type: "token"})

	plan := &coverage.CoveragePlan{Requirements: []coverage.CoverageRequirement{
		{ID: "search|Patient|_id|valid", ResourceType: "Patient", Domain: coverage.CoverageDomainSearch, Variant: coverage.CoverageVariantSearchValid, SearchCode: "_id"},
	}}
	opts := BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: reg}

	ds, err := BuildSetupDataset(plan, opts)
	if err != nil {
		t.Fatalf("BuildSetupDataset returned error: %v", err)
	}

	// A resource with id "momus-search" must exist so `_id=momus-search` matches,
	// and its LocalID (used for the PUT URL) must equal the id so the server
	// accepts the update (HAPI-0420 requires body and URL ids to agree).
	found := false
	for _, inst := range ds.Resources {
		if inst.Resource["id"] == "momus-search" {
			found = true
			if inst.LocalID != "momus-search" {
				t.Fatalf("LocalID = %q, want momus-search so it matches the body id", inst.LocalID)
			}
		}
	}
	if !found {
		t.Fatalf("expected a seed resource with id momus-search for the _id search")
	}
}

// TestSearchSeedIDStaysWithinFHIRLimit verifies that search seed ids derived
// from long requirement ids never exceed FHIR's 64-character id limit (a longer
// id is rejected by servers, e.g. HAPI-0521).
func TestSearchSeedIDStaysWithinFHIRLimit(t *testing.T) {
	longID := "search|Endpoint|connection-type|search-multiple-results"
	for i := 0; i < 3; i++ {
		id := searchSeedID(coverage.CoverageRequirement{ID: longID, ResourceType: "Endpoint"}, i)
		if len(id) > 64 {
			t.Fatalf("search seed id %q is %d chars, want <= 64", id, len(id))
		}
		if !validFHIRID(id) {
			t.Fatalf("search seed id %q is not a valid FHIR id", id)
		}
	}
}

// validFHIRID reports whether s matches the FHIR id regex [A-Za-z0-9\-\.]{1,64}.
func validFHIRID(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '.':
		default:
			return false
		}
	}
	return true
}

// TestSearchSeedUsesValidBoundCode verifies that a token search on a `code`
// element bound to a value set (e.g. Endpoint.status) uses a real code from that
// set, consistently in both the provisioned seed and the search query — a
// generic placeholder like "momus-search" would be rejected by servers.
func TestSearchSeedUsesValidBoundCode(t *testing.T) {
	reg := registry.New()
	reg.AddValueSet(&model.ValueSet{URL: "http://hl7.org/fhir/ValueSet/endpoint-status", ComposeIncludes: []model.ValueSetInclude{{System: "http://hl7.org/fhir/ValueSet/endpoint-status", Concepts: []model.ConceptReference{{Code: "active"}, {Code: "off"}}}}})
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/endpoint", Type: "Endpoint", Elements: []model.ElementDefinition{
		{Path: "Endpoint", Min: 0, Max: "*"},
		{Path: "Endpoint.status", Min: 1, Max: "1", Types: []model.ElementType{{Code: "code"}}, Binding: &model.Binding{Strength: "required", ValueSet: "http://hl7.org/fhir/ValueSet/endpoint-status"}},
		{Path: "Endpoint.connectionType", Min: 1, Max: "1", Types: []model.ElementType{{Code: "Coding"}}},
	}})
	reg.AddSearchParameter(&model.SearchParameter{URL: "http://hl7.org/fhir/SearchParameter/Endpoint-status", Name: "status", Code: "status", Base: []string{"Endpoint"}, Type: "token", Expression: "Endpoint.status"})

	plan := &coverage.CoveragePlan{Requirements: []coverage.CoverageRequirement{
		{ID: "search|Endpoint|status|search-multiple-results", ResourceType: "Endpoint", Domain: coverage.CoverageDomainSearch, Variant: coverage.CoverageVariantSearchMultipleResults, SearchCode: "status"},
	}}
	opts := BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: reg}

	ds, err := BuildSetupDataset(plan, opts)
	if err != nil {
		t.Fatalf("BuildSetupDataset returned error: %v", err)
	}
	valid := map[string]bool{"active": true, "off": true}
	matching := 0
	for _, inst := range ds.Resources {
		s, ok := inst.Resource["status"].(string)
		if !ok {
			continue
		}
		if s == "momus-search" {
			t.Fatalf("seed status = %q, must use a valid EndpointStatus code", s)
		}
		if !valid[s] {
			t.Fatalf("seed status = %q, not a valid EndpointStatus code", s)
		}
		if s == "active" {
			matching++
		}
	}
	if matching < 2 {
		t.Fatalf("expected 2 matching seeds with status=active, got %d", matching)
	}

	// The generated search query must use the same valid code.
	astPlan, err := GenerateFromCoveragePlan(plan, opts)
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}
	foundQuery := false
	var walk func(ast.Node)
	walk = func(node ast.Node) {
		switch n := node.(type) {
		case *ast.Sequence:
			for _, step := range n.Steps {
				walk(step)
			}
		case *ast.Parallel:
			for _, step := range n.Steps {
				walk(step)
			}
		case *ast.Request:
			if strings.Contains(n.URL, "status=active") {
				foundQuery = true
			}
		}
	}
	walk(astPlan.Root)
	if !foundQuery {
		t.Fatal("expected search query status=active")
	}
}

// TestSearchSeedKeepsRepeatableAddressArray verifies that setting a nested
// search value (e.g. address.city) keeps a repeatable complex container as an
// array rather than collapsing it to an object.
func TestSearchSeedKeepsRepeatableAddressArray(t *testing.T) {
	// Existing array container: descend into its first element, keep the array.
	body := map[string]any{"address": []any{map[string]any{"city": "Erewhon"}}}
	setPathLeaf(body, "address.city", "momus-search")
	arr, ok := body["address"].([]any)
	if !ok {
		t.Fatalf("address = %T %v, want a JSON array", body["address"], body["address"])
	}
	if a, ok := arr[0].(map[string]any); !ok || a["city"] != "momus-search" {
		t.Fatalf("address[0].city = %v, want momus-search", arr[0])
	}
}

// TestSearchSeedUsesValidBoundCodeableConcept verifies that a token search on a
// CodeableConcept bound to a value set uses a valid code from that set.
func TestSearchSeedUsesValidBoundCodeableConcept(t *testing.T) {
	reg := registry.New()
	reg.AddValueSet(&model.ValueSet{URL: "http://example.org/ValueSet/spc", ComposeIncludes: []model.ValueSetInclude{{System: "http://example.org/cs/spc", Concepts: []model.ConceptReference{{Code: "spc1"}}}}})
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/hs", Type: "HealthcareService", Elements: []model.ElementDefinition{
		{Path: "HealthcareService", Min: 0, Max: "*"},
		{Path: "HealthcareService.active", Min: 1, Max: "1", Types: []model.ElementType{{Code: "boolean"}}},
		{Path: "HealthcareService.serviceProvisionCode", Min: 0, Max: "*", Types: []model.ElementType{{Code: "CodeableConcept"}}, Binding: &model.Binding{Strength: "required", ValueSet: "http://example.org/ValueSet/spc"}},
	}})
	reg.AddSearchParameter(&model.SearchParameter{URL: "http://hl7.org/fhir/SearchParameter/HealthcareService-service-provision-code", Name: "service-provision-code", Code: "service-provision-code", Base: []string{"HealthcareService"}, Type: "token", Expression: "HealthcareService.serviceProvisionCode"})

	plan := &coverage.CoveragePlan{Requirements: []coverage.CoverageRequirement{
		{ID: "search|HealthcareService|service-provision-code|valid", ResourceType: "HealthcareService", Domain: coverage.CoverageDomainSearch, Variant: coverage.CoverageVariantSearchValid, SearchCode: "service-provision-code"},
	}}
	opts := BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: reg}

	ds, err := BuildSetupDataset(plan, opts)
	if err != nil {
		t.Fatalf("BuildSetupDataset returned error: %v", err)
	}
	for _, inst := range ds.Resources {
		raw, ok := inst.Resource["serviceProvisionCode"]
		if !ok {
			continue
		}
		arr, ok := raw.([]any)
		if !ok {
			t.Fatalf("serviceProvisionCode = %T, want array", raw)
		}
		first, ok := arr[0].(map[string]any)
		if !ok {
			t.Fatalf("serviceProvisionCode[0] = %T", arr[0])
		}
		coding, ok := first["coding"].([]any)
		if !ok || len(coding) == 0 {
			t.Fatalf("serviceProvisionCode[0] missing coding: %v", first)
		}
		c, _ := coding[0].(map[string]any)
		if code, _ := c["code"].(string); code != "spc1" {
			t.Fatalf("serviceProvisionCode code = %v, want spc1 (valid bound code)", c["code"])
		}
	}
}

// TestSliceAppliesDiscriminatorPattern verifies that a required slice's child
// Pattern (e.g. address:physical type="physical") is stamped onto the generated
// element so the slice is matched.
func TestSliceAppliesDiscriminatorPattern(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/org", Type: "Organization", Elements: []model.ElementDefinition{
		{Path: "Organization", Min: 0, Max: "*"},
		{Path: "Organization.name", Min: 1, Max: "1", Types: []model.ElementType{{Code: "string"}}},
		{Path: "Organization.address", Min: 1, Max: "*", Types: []model.ElementType{{Code: "Address"}}},
		{ID: "Organization.address:physical", Path: "Organization.address", SliceName: "physical", Min: 1, Max: "1", Types: []model.ElementType{{Code: "Address"}}},
		{ID: "Organization.address:physical.type", Path: "Organization.address.type", SliceName: "", Min: 1, Max: "1", Types: []model.ElementType{{Code: "code"}}, Pattern: "physical"},
	}})
	plan := &coverage.CoveragePlan{Requirements: []coverage.CoverageRequirement{
		{ID: "c1", ProfileURL: "http://example.org/StructureDefinition/org", ResourceType: "Organization", ElementPath: "Organization.name", Variant: coverage.CoverageVariantValidMin},
	}}
	opts := BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: reg}

	ds, err := BuildSetupDataset(plan, opts)
	if err != nil {
		t.Fatalf("BuildSetupDataset returned error: %v", err)
	}
	inst, ok := ds.Resources[setupResourceID("Organization")]
	if !ok {
		t.Fatalf("missing Organization seed")
	}
	addr, ok := inst.Resource["address"].([]any)
	if !ok || len(addr) == 0 {
		t.Fatalf("address = %T, want array", inst.Resource["address"])
	}
	first, ok := addr[0].(map[string]any)
	if !ok {
		t.Fatalf("address[0] = %T", addr[0])
	}
	if first["type"] != "physical" {
		t.Fatalf("address[0].type = %v, want physical (slice discriminator)", first["type"])
	}
}

// TestSearchSeedSkipsNonMatchableSearch verifies that a boolean search (where
// "momus-search" is not a valid value) produces no matching seed; the search
// remains status-only.
func TestSearchSeedSkipsNonMatchableSearch(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/patient", Type: "Patient", Elements: []model.ElementDefinition{
		{Path: "Patient", Min: 0, Max: "*"},
		{Path: "Patient.active", Min: 1, Max: "1", Types: []model.ElementType{{Code: "boolean"}}},
	}})
	// `active` is a boolean: the placeholder value "momus-search" cannot match,
	// so no matching seed is added (the search remains status-only).
	reg.AddSearchParameter(&model.SearchParameter{URL: "http://hl7.org/fhir/SearchParameter/Patient-active", Name: "active", Code: "active", Base: []string{"Patient"}, Type: "token", Expression: "Patient.active"})

	plan := &coverage.CoveragePlan{Requirements: []coverage.CoverageRequirement{
		{ID: "search|Patient|active|multiple", ResourceType: "Patient", Domain: coverage.CoverageDomainSearch, Variant: coverage.CoverageVariantSearchMultipleResults, SearchCode: "active"},
	}}
	opts := BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: reg}

	ds, err := BuildSetupDataset(plan, opts)
	if err != nil {
		t.Fatalf("BuildSetupDataset returned error: %v", err)
	}
	for _, inst := range ds.Resources {
		if b, ok := inst.Resource["active"].(bool); ok && b {
			t.Fatalf("expected no active:true seed for a non-matchable search; got %s", inst.LocalID)
		}
	}
}
