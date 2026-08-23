package generation

import (
	"strings"
	"testing"

	"github.com/jlcoulter/momus/internal/core/ast"
	"github.com/jlcoulter/momus/internal/core/coverage"
	coregen "github.com/jlcoulter/momus/internal/core/generation"
	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
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
	inst, ok := ds.Resources[coregen.SetupResourceID("Organization")]
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

// TestSearchSeedSkipsNonMatchableSearch verifies that a composite/special
// search (with no single leaf a search value can be placed on) produces no
// matching seed; the search remains status-only.
func TestSearchSeedSetsIdentifierValue(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/patient", Type: "Patient", Elements: []model.ElementDefinition{
		{Path: "Patient", Min: 0, Max: "*"},
		{Path: "Patient.identifier", Min: 0, Max: "*", Types: []model.ElementType{{Code: "Identifier"}}},
	}})
	reg.AddSearchParameter(&model.SearchParameter{URL: "http://example.org/SearchParameter/Patient-identifier", Name: "identifier", Code: "identifier", Base: []string{"Patient"}, Type: "token", Expression: "Patient.identifier"})

	plan := &coverage.CoveragePlan{Requirements: []coverage.CoverageRequirement{
		{ID: "search|Patient|identifier|multiple", ResourceType: "Patient", Domain: coverage.CoverageDomainSearch, Variant: coverage.CoverageVariantSearchMultipleResults, SearchCode: "identifier"},
	}}
	opts := BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: reg}

	ds, err := BuildSetupDataset(plan, opts)
	if err != nil {
		t.Fatalf("BuildSetupDataset returned error: %v", err)
	}
	var seeds int
	for _, inst := range ds.Resources {
		if !strings.HasPrefix(inst.LocalID, "momus-search-") {
			continue
		}
		seeds++
		ids, _ := inst.Resource["identifier"].([]any)
		if len(ids) == 0 {
			t.Fatalf("seed %s has no identifier", inst.LocalID)
		}
		if ids[0].(map[string]any)["value"] != "momus-search" {
			t.Fatalf("seed %s identifier.value not set to search value: %+v", inst.LocalID, ids[0])
		}
	}
	if seeds != 2 {
		t.Fatalf("expected 2 identifier-matching seeds, got %d", seeds)
	}
}

func TestSplitUnion(t *testing.T) {
	got := splitUnion("Patient.gender | Patient.deceasedBoolean")
	if len(got) != 2 {
		t.Fatalf("splitUnion = %v", got)
	}
	// Union inside parentheses is preserved.
	got = splitUnion("a | (b | c)")
	if len(got) != 2 {
		t.Fatalf("splitUnion(paren) = %v", got)
	}
}

func TestPlainSearchPath(t *testing.T) {
	cases := []struct{ in, rt, want string }{
		{"Patient.name", "Patient", "name"},
		{"Patient.name.family", "Patient", "name.family"},
		{"name", "Patient", "name"},
		{"Patient.code as CodeableConcept", "Patient", "code"},
		{"Patient.communication.where(language.exists())", "Patient", "communication"},
		{"Resource.gender", "Patient", "gender"},
		{"DomainResource.meta", "Patient", "meta"},
		{"", "Patient", ""},
		{"Patient.", "Patient", ""},
		{"a[b]", "Patient", ""},
	}
	for _, c := range cases {
		if got := plainSearchPath(c.in, c.rt); got != c.want {
			t.Errorf("plainSearchPath(%q, %q) = %q, want %q", c.in, c.rt, got, c.want)
		}
	}
}

func TestResolveNestedLeafType(t *testing.T) {
	reg := registry.New()
	// An Identifier complex datatype definition.
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://hl7.org/fhir/StructureDefinition/Identifier", Type: "Identifier", Elements: []model.ElementDefinition{
		{Path: "Identifier", Min: 0, Max: "*"},
		{Path: "Identifier.value", Min: 0, Max: "1", Types: []model.ElementType{{Code: "string"}}},
	}})
	// A Patient with an identifier element typed as Identifier.
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/patient", Type: "Patient", Elements: []model.ElementDefinition{
		{Path: "Patient", Min: 0, Max: "*"},
		{Path: "Patient.identifier", Min: 0, Max: "*", Types: []model.ElementType{{Code: "Identifier"}}},
	}})
	resolved, err := reg.ResolveProfile("http://example.org/StructureDefinition/patient")
	if err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}
	typeCode, repeatable, found := resolveNestedLeafType(resolved, "Patient", "identifier.value", reg)
	if !found || typeCode != "string" || repeatable {
		t.Fatalf("resolveNestedLeafType = %q, %v, %v; want string, false, true", typeCode, repeatable, found)
	}
	// A single-segment path returns not-found.
	if _, _, found := resolveNestedLeafType(resolved, "Patient", "identifier", reg); found {
		t.Fatal("single-segment path should not resolve via nested type")
	}
}

func TestApplySearchMatch(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/patient", Type: "Patient", Elements: []model.ElementDefinition{
		{Path: "Patient", Min: 0, Max: "*"},
		{Path: "Patient.name", Min: 0, Max: "*", Types: []model.ElementType{{Code: "HumanName"}}},
		{Path: "Patient.address", Min: 0, Max: "*", Types: []model.ElementType{{Code: "Address"}}},
		{Path: "Patient.telecom", Min: 0, Max: "*", Types: []model.ElementType{{Code: "ContactPoint"}}},
		{Path: "Patient.generalPractitioner", Min: 0, Max: "*", Types: []model.ElementType{{Code: "Reference"}}},
		{Path: "Patient.valueQuantity", Min: 0, Max: "1", Types: []model.ElementType{{Code: "Quantity"}}},
		{Path: "Patient.score", Min: 0, Max: "1", Types: []model.ElementType{{Code: "integer"}}},
	}})
	cases := []struct {
		code, typ, expr, want string
		check                 func(map[string]any) bool
	}{
		{"name", "string", "Patient.name", "momus-search", func(b map[string]any) bool { return b["name"] != nil }},
		{"address", "string", "Patient.address", "momus-search", func(b map[string]any) bool { return b["address"] != nil }},
		{"telecom", "token", "Patient.telecom", "momus-search", func(b map[string]any) bool { return b["telecom"] != nil }},
		{"general-practitioner", "reference", "Patient.generalPractitioner", "Patient/x", func(b map[string]any) bool { return b["generalPractitioner"] != nil }},
		{"value-quantity", "quantity", "Patient.valueQuantity", "5.4|http://sys|mg", func(b map[string]any) bool { return b["valueQuantity"] != nil }},
		{"score", "number", "Patient.score", "10", func(b map[string]any) bool { return b["score"] != nil }},
	}
	for _, c := range cases {
		sp := &model.SearchParameter{Code: c.code, Type: c.typ, Expression: c.expr}
		body := map[string]any{}
		if ok := applySearchMatch(body, "Patient", sp, c.want, reg); !ok {
			t.Errorf("applySearchMatch(%s) = false, want true", c.code)
			continue
		}
		if !c.check(body) {
			t.Errorf("applySearchMatch(%s) body = %v, did not set element", c.code, body)
		}
	}
}

func TestSearchSeedSkipsNonMatchableSearch(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/patient", Type: "Patient", Elements: []model.ElementDefinition{
		{Path: "Patient", Min: 0, Max: "*"},
		{Path: "Patient.active", Min: 1, Max: "1", Types: []model.ElementType{{Code: "boolean"}}},
	}})
	// `unknown-param` points at a non-existent element path, so the engine
	// cannot resolve a leaf to seed and no matching seed is added (the search
	// remains status-only).
	reg.AddSearchParameter(&model.SearchParameter{URL: "http://example.org/SearchParameter/Patient-unknown", Name: "unknown-param", Code: "unknown-param", Base: []string{"Patient"}, Type: "token", Expression: "Patient.noSuchElement"})

	plan := &coverage.CoveragePlan{Requirements: []coverage.CoverageRequirement{
		{ID: "search|Patient|unknown-param|multiple", ResourceType: "Patient", Domain: coverage.CoverageDomainSearch, Variant: coverage.CoverageVariantSearchMultipleResults, SearchCode: "unknown-param"},
	}}
	opts := BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: reg}

	ds, err := BuildSetupDataset(plan, opts)
	if err != nil {
		t.Fatalf("BuildSetupDataset returned error: %v", err)
	}
	for _, inst := range ds.Resources {
		if strings.HasPrefix(inst.LocalID, "momus-search-") {
			t.Fatalf("expected no search seed for a non-matchable search; got %s", inst.LocalID)
		}
	}
}

// TestSetSearchCodeValueClearsStaleSystemDisplay verifies that overwriting an
// existing coding's code with a search value drops the stale system/display
// (and CodeableConcept text) from the replaced concept, so the coding is
// internally consistent rather than a Frankenstein (e.g. connectionType code
// "dicom-wado-rs" with the smd-interfaces system).
func TestSetSearchCodeValueClearsStaleSystemDisplay(t *testing.T) {
	// Coding element: stale system/display removed.
	body := map[string]any{
		"connectionType": map[string]any{
			"system":  "http://hl7.org.au/fhir/CodeSystem/smd-interfaces",
			"code":    "http://ns.electronichealth.net.au/smd/intf/SealedImmediateMessageDelivery/TLS/2010",
			"display": "Sealed Immediate Message Delivery",
		},
	}
	setSearchCodeValue(body, "connectionType", "dicom-wado-rs", "Coding", false, "")
	ct, ok := body["connectionType"].(map[string]any)
	if !ok {
		t.Fatalf("expected connectionType map, got %T", body["connectionType"])
	}
	if ct["code"] != "dicom-wado-rs" {
		t.Fatalf("got code %v, want dicom-wado-rs", ct["code"])
	}
	if _, ok := ct["system"]; ok {
		t.Fatalf("stale system not cleared: %v", ct["system"])
	}
	if _, ok := ct["display"]; ok {
		t.Fatalf("stale display not cleared: %v", ct["display"])
	}

	// CodeableConcept example: stale coding system/display and text removed.
	concept := map[string]any{
		"type": map[string]any{
			"coding": []any{map[string]any{"system": "http://example.org", "code": "old", "display": "Old"}},
			"text":   "Old",
		},
	}
	setSearchCodeValue(concept, "type", "new", "CodeableConcept", false, "")
	typ, ok := concept["type"].(map[string]any)
	if !ok {
		t.Fatalf("expected type map, got %T", concept["type"])
	}
	if _, ok := typ["text"]; ok {
		t.Fatalf("stale CodeableConcept text not cleared: %v", typ["text"])
	}
	codings, ok := typ["coding"].([]any)
	if !ok || len(codings) == 0 {
		t.Fatalf("expected codings, got %#v", typ["coding"])
	}
	coding, ok := codings[0].(map[string]any)
	if !ok {
		t.Fatalf("expected coding map, got %T", codings[0])
	}
	if coding["code"] != "new" {
		t.Fatalf("got code %v, want new", coding["code"])
	}
	if _, ok := coding["system"]; ok {
		t.Fatalf("stale coding system not cleared: %v", coding["system"])
	}
	if _, ok := coding["display"]; ok {
		t.Fatalf("stale coding display not cleared: %v", coding["display"])
	}
}

// TestSetSearchCodeValueKeepsResolvedSystem verifies that when a bound element's
// code system is known it is applied to the coding, so a required value-set
// binding (e.g. HealthcareService.serviceProvisionCode) is not shipped with a
// system-less coding the server rejects.
func TestSetSearchCodeValueKeepsResolvedSystem(t *testing.T) {
	body := map[string]any{}
	// New CodeableConcept: system applied alongside the search code.
	setSearchCodeValue(body, "serviceProvisionCode", "free", "CodeableConcept", true, "http://digitalhealth.gov.au/fhir/hcpd/CodeSystem/service-provision-cs")
	arr := body["serviceProvisionCode"].([]any)
	cc := arr[0].(map[string]any)
	coding := cc["coding"].([]any)[0].(map[string]any)
	if coding["code"] != "free" {
		t.Fatalf("got code %v, want free", coding["code"])
	}
	if coding["system"] != "http://digitalhealth.gov.au/fhir/hcpd/CodeSystem/service-provision-cs" {
		t.Fatalf("got system %v, want service-provision-cs", coding["system"])
	}

	// Existing Coding: system realigned to the resolved system, stale display dropped.
	existing := map[string]any{
		"connectionType": map[string]any{"system": "http://hl7.org.au/fhir/CodeSystem/smd-interfaces", "code": "old", "display": "Old"},
	}
	setSearchCodeValue(existing, "connectionType", "dicom-wado-rs", "Coding", false, "http://hl7.org/fhir/ValueSet/endpoint-connection-type")
	ct := existing["connectionType"].(map[string]any)
	if ct["code"] != "dicom-wado-rs" {
		t.Fatalf("got code %v, want dicom-wado-rs", ct["code"])
	}
	if ct["system"] != "http://hl7.org/fhir/ValueSet/endpoint-connection-type" {
		t.Fatalf("got system %v, want endpoint-connection-type", ct["system"])
	}
	if _, has := ct["display"]; has {
		t.Fatalf("stale display should be cleared, got %v", ct["display"])
	}
}

func TestSetSearchCodeValueBranches(t *testing.T) {
	// Primitive repeatable code, absent.
	body := map[string]any{}
	setSearchCodeValue(body, "status", "active", "code", true, "")
	if got := body["status"].([]any)[0]; got != "active" {
		t.Fatalf("repeatable code = %v", got)
	}
	// Primitive non-repeatable code.
	body = map[string]any{}
	setSearchCodeValue(body, "status", "active", "code", false, "")
	if body["status"] != "active" {
		t.Fatalf("non-repeatable code = %v", body["status"])
	}
	// Existing primitive array (repeatable).
	body = map[string]any{"status": []any{"old"}}
	setSearchCodeValue(body, "status", "new", "code", true, "")
	if body["status"].([]any)[0] != "new" {
		t.Fatalf("existing repeatable code = %v", body["status"])
	}
	// Coding type, absent -> coding map.
	body = map[string]any{}
	setSearchCodeValue(body, "connectionType", "dicom-wado-rs", "Coding", false, "http://sys")
	if body["connectionType"].(map[string]any)["code"] != "dicom-wado-rs" {
		t.Fatalf("Coding = %v", body["connectionType"])
	}
	// CodeableConcept repeatable absent.
	body = map[string]any{}
	setSearchCodeValue(body, "type", "x", "CodeableConcept", true, "")
	if body["type"].([]any)[0].(map[string]any)["coding"] == nil {
		t.Fatalf("CodeableConcept repeatable = %v", body["type"])
	}
	// Empty array case.
	body = map[string]any{"status": []any{}}
	setSearchCodeValue(body, "status", "active", "code", true, "")
	if body["status"].([]any)[0] != "active" {
		t.Fatalf("empty array code = %v", body["status"])
	}
	// Existing string field.
	body = map[string]any{"status": "old"}
	setSearchCodeValue(body, "status", "new", "code", false, "")
	if body["status"] != "new" {
		t.Fatalf("existing string = %v", body["status"])
	}
	// Existing array of codings.
	body = map[string]any{"type": []any{map[string]any{"coding": []any{map[string]any{"code": "old"}}}}}
	setSearchCodeValue(body, "type", "new", "CodeableConcept", false, "")
	codings := body["type"].([]any)[0].(map[string]any)["coding"].([]any)
	if codings[0].(map[string]any)["code"] != "new" {
		t.Fatalf("array of codings = %v", codings)
	}
	// Existing map with a "code" field directly (bare coding).
	body = map[string]any{"type": map[string]any{"code": "old"}}
	setSearchCodeValue(body, "type", "new", "Coding", false, "")
	if body["type"].(map[string]any)["code"] != "new" {
		t.Fatalf("bare coding = %v", body["type"])
	}
}

func TestSetFieldLeafAndForce(t *testing.T) {
	// setFieldLeaf with existing array preserves first element's leaf if present.
	body := map[string]any{"contact": []any{map[string]any{"city": "X"}}}
	setFieldLeaf(body, "contact", "city", "Y")
	if body["contact"].([]any)[0].(map[string]any)["city"] != "X" {
		t.Fatalf("setFieldLeaf preserved = %v", body["contact"])
	}
	// setFieldLeaf with missing leaf.
	body = map[string]any{"contact": []any{map[string]any{"phone": "1"}}}
	setFieldLeaf(body, "contact", "city", "Y")
	if body["contact"].([]any)[0].(map[string]any)["city"] != "Y" {
		t.Fatalf("setFieldLeaf added = %v", body["contact"])
	}
	// setFieldLeafForce overwrites.
	body = map[string]any{"contact": []any{map[string]any{"city": "X"}}}
	setFieldLeafForce(body, "contact", "city", "Y")
	if body["contact"].([]any)[0].(map[string]any)["city"] != "Y" {
		t.Fatalf("setFieldLeafForce = %v", body["contact"])
	}
	// setFieldLeafForce with a scalar map field.
	body = map[string]any{"addr": map[string]any{"city": "X"}}
	setFieldLeafForce(body, "addr", "city", "Y")
	if body["addr"].(map[string]any)["city"] != "Y" {
		t.Fatalf("setFieldLeafForce(map) = %v", body["addr"])
	}
}
