package generation

import (
	"sort"
	"strings"
	"testing"

	"github.com/jlcoulter/momus/internal/core/ast"
	"github.com/jlcoulter/momus/internal/core/coverage"
	coregen "github.com/jlcoulter/momus/internal/core/generation"
	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

// TestBuildSetupDatasetProducesSeedResources verifies that the seed dataset
// built for provisioning contains one resource per type with the deterministic
// setup id and the right profile, so provisioned data is exactly what generated
// test cases reference (by setup id). Provisioning is a separate stage from the
// test AST, which no longer emits setup requests.
func TestBuildSetupDatasetProducesSeedResources(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/patient", Type: "Patient", Elements: []model.ElementDefinition{
		{Path: "Patient", Min: 0, Max: "*"},
		{Path: "Patient.name", Min: 1, Max: "*", Types: []model.ElementType{{Code: "HumanName"}}},
	}})
	plan := &coverage.CoveragePlan{Requirements: []coverage.CoverageRequirement{
		{ID: "req-1", ProfileURL: "http://example.org/StructureDefinition/patient", ResourceType: "Patient", ElementPath: "Patient.name", Variant: coverage.CoverageVariantValidMin},
	}}
	opts := BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: reg}

	ds, err := BuildSetupDataset(plan, opts)
	if err != nil {
		t.Fatalf("BuildSetupDataset returned error: %v", err)
	}
	inst, ok := ds.Resources[coregen.SetupResourceID("Patient")]
	if !ok {
		t.Fatalf("expected setup resource %s in dataset", coregen.SetupResourceID("Patient"))
	}
	if inst.ResourceType != "Patient" {
		t.Fatalf("got resource type %q, want Patient", inst.ResourceType)
	}
	body := inst.Resource
	if body["id"] != coregen.SetupResourceID("Patient") {
		t.Fatalf("got dataset id %v, want %s", body["id"], coregen.SetupResourceID("Patient"))
	}
	meta := body["meta"].(map[string]any)
	profiles := meta["profile"].([]any)
	if len(profiles) != 1 || profiles[0] != "http://example.org/StructureDefinition/patient" {
		t.Fatalf("got meta.profile %v, want patient profile", meta["profile"])
	}

	// The generated test AST references the same setup id but does not provision
	// it: provisioning is a separate stage.
	astPlan, err := GenerateFromCoveragePlan(plan, opts)
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}
	if hasSetupStep(astPlan.Root) {
		t.Fatal("expected no provisioning steps in generated AST; provisioning is separate")
	}
}

// TestBuildSetupDatasetRecordsDependencyRelationships verifies that the seed
// dataset records relationships so provisioning orders targets before
// dependents.
// TestBuildSetupDatasetIncludesTransitiveReferenceTargets verifies that the
// seed dataset seeds every type a test transitively references, even when that
// type is not itself a coverage obligation. Here only Observation is a coverage
// requirement, but its profile references Patient, so Patient must be seeded and
// provisioned before Observation so the generated test's reference resolves.
func TestBuildSetupDatasetIncludesTransitiveReferenceTargets(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/patient", Type: "Patient", Elements: []model.ElementDefinition{
		{Path: "Patient", Min: 0, Max: "*"},
		{Path: "Patient.name", Min: 1, Max: "*", Types: []model.ElementType{{Code: "HumanName"}}},
	}})
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/observation", Type: "Observation", Elements: []model.ElementDefinition{
		{Path: "Observation", Min: 0, Max: "*"},
		{Path: "Observation.status", Min: 1, Max: "1", Types: []model.ElementType{{Code: "code"}}},
		{Path: "Observation.subject", Min: 1, Max: "1", Types: []model.ElementType{{Code: "Reference", TargetProfile: []string{"http://example.org/StructureDefinition/patient"}}}},
	}})
	// Only Observation is a coverage obligation; Patient is reached only via the
	// Observation profile's subject reference.
	plan := &coverage.CoveragePlan{Requirements: []coverage.CoverageRequirement{
		{ID: "o-1", ProfileURL: "http://example.org/StructureDefinition/observation", ResourceType: "Observation", ElementPath: "Observation.subject", Variant: coverage.CoverageVariantValidMin},
	}}
	opts := BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: reg}

	ds, err := BuildSetupDataset(plan, opts)
	if err != nil {
		t.Fatalf("BuildSetupDataset returned error: %v", err)
	}
	if _, ok := ds.Resources[coregen.SetupResourceID("Observation")]; !ok {
		t.Fatalf("expected setup Observation resource in dataset")
	}
	patient, ok := ds.Resources[coregen.SetupResourceID("Patient")]
	if !ok {
		t.Fatalf("expected setup Patient resource in dataset (transitive reference target), got %v", keysOf(ds.Resources))
	}
	if patient.ResourceType != "Patient" {
		t.Fatalf("got resource type %q, want Patient", patient.ResourceType)
	}
	found := false
	for _, rel := range ds.Relationships {
		if rel.SourceID == coregen.SetupResourceID("Observation") && rel.TargetID == coregen.SetupResourceID("Patient") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected relationship Observation -> Patient, got %+v", ds.Relationships)
	}

	// The generated AST must not emit empty test scaffolding for Patient: it has
	// no coverage obligations of its own, and provisioning is a separate stage.
	astPlan, err := GenerateFromCoveragePlan(plan, opts)
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}
	if hasSetupStep(astPlan.Root) {
		t.Fatal("expected no provisioning steps in AST")
	}
	// Only the Observation resource seq should carry cases; Patient has none.
	root := astPlan.Root.(*ast.Sequence)
	if len(root.Steps) != 1 {
		t.Fatalf("expected 1 resource sequence in AST (Observation only), got %d", len(root.Steps))
	}
}

// keysOf returns the keys of a dataset resource map, sorted, for diagnostics.
func keysOf(m map[string]*model.ResourceInstance) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestBuildSetupDatasetExcludesAbstractReferenceTypes verifies that abstract
// base types (Resource, DomainResource) are never seeded as reference targets,
// even when a Reference element carries an abstract target profile.
func TestBuildSetupDatasetExcludesAbstractReferenceTypes(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://hl7.org/fhir/StructureDefinition/Resource", Type: "Resource", Kind: "resource", Elements: []model.ElementDefinition{{Path: "Resource", Min: 0, Max: "*"}}})
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/observation", Type: "Observation", Elements: []model.ElementDefinition{
		{Path: "Observation", Min: 0, Max: "*"},
		{Path: "Observation.status", Min: 1, Max: "1", Types: []model.ElementType{{Code: "code"}}},
		{Path: "Observation.subject", Min: 1, Max: "1", Types: []model.ElementType{{Code: "Reference", TargetProfile: []string{"http://hl7.org/fhir/StructureDefinition/Resource"}}}},
	}})
	plan := &coverage.CoveragePlan{Requirements: []coverage.CoverageRequirement{
		{ID: "o-1", ProfileURL: "http://example.org/StructureDefinition/observation", ResourceType: "Observation", ElementPath: "Observation.subject", Variant: coverage.CoverageVariantValidMin},
	}}
	opts := BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: reg}

	ds, err := BuildSetupDataset(plan, opts)
	if err != nil {
		t.Fatalf("BuildSetupDataset returned error: %v", err)
	}
	if _, ok := ds.Resources[coregen.SetupResourceID("Resource")]; ok {
		t.Fatal("abstract type Resource must not be seeded as a reference target")
	}
}

// TestBuildSetupDatasetRespectsCapabilityProfileScope verifies that a resource
// whose selected profile the server's CapabilityStatement does not declare is not
// seeded (capability-gated filtering).
func TestBuildSetupDatasetRespectsCapabilityProfileScope(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/org-unsupported", Type: "Organization", Elements: []model.ElementDefinition{
		{Path: "Organization", Min: 0, Max: "*"},
		{Path: "Organization.name", Min: 1, Max: "1", Types: []model.ElementType{{Code: "string"}}},
	}})
	plan := &coverage.CoveragePlan{Requirements: []coverage.CoverageRequirement{
		{ID: "o-1", ProfileURL: "http://example.org/StructureDefinition/org-unsupported", ResourceType: "Organization", ElementPath: "Organization.name", Variant: coverage.CoverageVariantValidMin},
	}}
	opts := BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: reg, CapabilityProfiles: map[string]struct{}{"http://example.org/StructureDefinition/other": {}}}
	ds, err := BuildSetupDataset(plan, opts)
	if err != nil {
		t.Fatalf("BuildSetupDataset returned error: %v", err)
	}
	if _, ok := ds.Resources[coregen.SetupResourceID("Organization")]; ok {
		t.Fatal("Organization must not be seeded when its profile is outside the capability scope")
	}
}

// TestBuildSetupDatasetRespectsCapabilityScope verifies that when the server's
// CapabilityStatement declares a resource-type scope, the seed dataset (and the
// transitive reference closure) is restricted to those types — the capability
// statement defines the test plan.
func TestBuildSetupDatasetRespectsCapabilityScope(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/patient", Type: "Patient", Elements: []model.ElementDefinition{
		{Path: "Patient", Min: 0, Max: "*"},
		{Path: "Patient.name", Min: 1, Max: "*", Types: []model.ElementType{{Code: "HumanName"}}},
	}})
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/observation", Type: "Observation", Elements: []model.ElementDefinition{
		{Path: "Observation", Min: 0, Max: "*"},
		{Path: "Observation.status", Min: 1, Max: "1", Types: []model.ElementType{{Code: "code"}}},
		{Path: "Observation.subject", Min: 1, Max: "1", Types: []model.ElementType{{Code: "Reference", TargetProfile: []string{"http://example.org/StructureDefinition/patient"}}}},
	}})
	plan := &coverage.CoveragePlan{Requirements: []coverage.CoverageRequirement{
		{ID: "o-1", ProfileURL: "http://example.org/StructureDefinition/observation", ResourceType: "Observation", ElementPath: "Observation.subject", Variant: coverage.CoverageVariantValidMin},
	}}

	// Server only supports Observation, not Patient (a reference target).
	opts := BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: reg, CapabilityResourceTypes: map[string]struct{}{"Observation": {}}}
	ds, err := BuildSetupDataset(plan, opts)
	if err != nil {
		t.Fatalf("BuildSetupDataset returned error: %v", err)
	}
	if _, ok := ds.Resources[coregen.SetupResourceID("Patient")]; ok {
		t.Fatal("Patient must not be seeded when it is outside the capability scope")
	}
	if _, ok := ds.Resources[coregen.SetupResourceID("Observation")]; !ok {
		t.Fatal("Observation must be seeded (supported by the capability statement)")
	}
}

// TestBuildSetupDatasetRecordsDependencyRelationships verifies that the seed
// dataset records relationships so provisioning orders targets before
// dependents.
func TestBuildSetupDatasetRecordsDependencyRelationships(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/patient", Type: "Patient", Elements: []model.ElementDefinition{
		{Path: "Patient", Min: 0, Max: "*"},
		{Path: "Patient.name", Min: 1, Max: "*", Types: []model.ElementType{{Code: "HumanName"}}},
	}})
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/observation", Type: "Observation", Elements: []model.ElementDefinition{
		{Path: "Observation", Min: 0, Max: "*"},
		{Path: "Observation.status", Min: 1, Max: "1", Types: []model.ElementType{{Code: "code"}}},
		{Path: "Observation.subject", Min: 1, Max: "1", Types: []model.ElementType{{Code: "Reference", TargetProfile: []string{"http://example.org/StructureDefinition/patient"}}}},
	}})
	plan := &coverage.CoveragePlan{Requirements: []coverage.CoverageRequirement{
		{ID: "p-1", ProfileURL: "http://example.org/StructureDefinition/patient", ResourceType: "Patient", ElementPath: "Patient.name", Variant: coverage.CoverageVariantValidMin},
		{ID: "o-1", ProfileURL: "http://example.org/StructureDefinition/observation", ResourceType: "Observation", ElementPath: "Observation.subject", DependencyTargets: []string{"Patient"}, Variant: coverage.CoverageVariantValidMin},
	}}
	opts := BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: reg}

	ds, err := BuildSetupDataset(plan, opts)
	if err != nil {
		t.Fatalf("BuildSetupDataset returned error: %v", err)
	}
	if _, ok := ds.Resources[coregen.SetupResourceID("Patient")]; !ok {
		t.Fatalf("expected setup resource %s in dataset", coregen.SetupResourceID("Patient"))
	}
	if _, ok := ds.Resources[coregen.SetupResourceID("Observation")]; !ok {
		t.Fatalf("expected setup resource %s in dataset", coregen.SetupResourceID("Observation"))
	}
	found := false
	for _, rel := range ds.Relationships {
		if rel.SourceID == coregen.SetupResourceID("Observation") && rel.TargetID == coregen.SetupResourceID("Patient") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected relationship Observation -> Patient, got %+v", ds.Relationships)
	}
}

// TestBuildSetupDatasetRecordsReferencesFromResourceBody verifies that the
// seed dataset records relationships for references that appear in the
// generated resource body but were not modelled as dependency targets — e.g. a
// search seed resource referencing momus-setup-<Type>. Without this, the
// provisioner would order dependents before their targets and fail with
// HAPI-1094 "not found".
func TestBuildSetupDatasetRecordsReferencesFromResourceBody(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/endpoint", Type: "Endpoint", Elements: []model.ElementDefinition{
		{Path: "Endpoint", Min: 0, Max: "*"},
		{Path: "Endpoint.connectionType", Min: 1, Max: "1", Types: []model.ElementType{{Code: "Coding"}}},
	}})
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/healthcareservice", Type: "HealthcareService", Elements: []model.ElementDefinition{
		{Path: "HealthcareService", Min: 0, Max: "*"},
		{Path: "HealthcareService.characteristic", Min: 0, Max: "*", Types: []model.ElementType{{Code: "CodeableConcept"}}},
		{Path: "HealthcareService.endpoint", Min: 1, Max: "*", Types: []model.ElementType{{Code: "Reference", TargetProfile: []string{"http://example.org/StructureDefinition/endpoint"}}}},
	}})
	reg.AddSearchParameter(&model.SearchParameter{URL: "http://example.org/SearchParameter/hs-characteristic", Name: "characteristic", Code: "characteristic", Base: []string{"HealthcareService"}, Type: "token", Expression: "HealthcareService.characteristic"})

	plan := &coverage.CoveragePlan{Requirements: []coverage.CoverageRequirement{
		{ID: "hs-char", ProfileURL: "http://example.org/StructureDefinition/healthcareservice", ResourceType: "HealthcareService", ElementPath: "HealthcareService.endpoint", Variant: coverage.CoverageVariantValidMin},
		{ID: "hs-search", ResourceType: "HealthcareService", Domain: coverage.CoverageDomainSearch, Variant: coverage.CoverageVariantSearchValid, SearchCode: "characteristic"},
	}}
	opts := BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: reg}

	ds, err := BuildSetupDataset(plan, opts)
	if err != nil {
		t.Fatalf("BuildSetupDataset returned error: %v", err)
	}

	// The setup Endpoint is seeded because HealthcareService depends on it.
	endpointLocalID := coregen.SetupResourceID("Endpoint")
	if _, ok := ds.Resources[endpointLocalID]; !ok {
		t.Fatalf("expected setup Endpoint %s in dataset, got %v", endpointLocalID, keysOf(ds.Resources))
	}

	// Find a search seed HealthcareService that references the setup Endpoint in
	// its generated body, and assert the relationship was recorded by the body
	// scan even though the search requirement carried no DependencyTargets.
	var searchSeedID string
	for id, inst := range ds.Resources {
		if strings.HasPrefix(id, "momus-search-") && inst.ResourceType == "HealthcareService" {
			searchSeedID = id
			break
		}
	}
	if searchSeedID == "" {
		t.Fatalf("expected a search seed HealthcareService resource, got %v", keysOf(ds.Resources))
	}

	found := false
	for _, rel := range ds.Relationships {
		if rel.SourceID == searchSeedID && rel.TargetID == endpointLocalID {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected relationship search seed %s -> Endpoint %s, got %+v", searchSeedID, endpointLocalID, ds.Relationships)
	}
}

// TestApplySliceConstractionsNormalisesCodingDisplay verifies that a slice
// pattern coding whose display is absent is normalised to the canonical
// CodeSystem display via applySliceConstractions.
func TestApplySliceConstractionsNormalisesCodingDisplay(t *testing.T) {
	reg := registry.New()
	reg.AddCodeSystem(&model.CodeSystem{URL: "http://terminology.hl7.org/CodeSystem/v2-0203", Concepts: []model.CodeSystemConcept{
		{Code: "XX", Display: "Organization identifier"},
	}})

	slice := &model.SliceNode{
		Name:       "Local",
		Definition: &model.ElementDefinition{Path: "Endpoint.identifier", Min: 1, Max: "1"},
		Children: map[string]*model.ElementNode{
			"type": {
				Name: "type",
				Path: "Endpoint.identifier.type",
				Definition: &model.ElementDefinition{
					Path: "Endpoint.identifier.type",
					Pattern: map[string]any{
						"coding": []any{map[string]any{"system": "http://terminology.hl7.org/CodeSystem/v2-0203", "code": "XX"}},
					},
				},
			},
		},
	}

	value := map[string]any{"type": map[string]any{"coding": []any{map[string]any{"system": "http://example.org", "code": "other"}}}}
	applySliceConstractions(value, slice, reg)

	typ, ok := value["type"].(map[string]any)
	if !ok {
		t.Fatalf("expected type map, got %T", value["type"])
	}
	codings, ok := typ["coding"].([]any)
	if !ok || len(codings) == 0 {
		t.Fatalf("expected codings, got %#v", typ["coding"])
	}
	coding, ok := codings[0].(map[string]any)
	if !ok {
		t.Fatalf("expected coding map, got %T", codings[0])
	}
	if coding["code"] != "XX" {
		t.Fatalf("got code %v, want XX", coding["code"])
	}
	// The slice pattern fixes only system+code; a display is not permitted on a
	// fixed coding (HAPI rejects it), so it must be absent.
	if _, hasDisplay := coding["display"]; hasDisplay {
		t.Fatalf("fixed coding must not carry a display, got %v", coding["display"])
	}
}

// TestSynthesizeBodyStripsSelfReferences verifies that a generated resource
// never references itself. The setup Location's optional partOf (Reference ->
// Location) resolves to Location/momus-setup-location, which is the resource's
// own logical reference and would fail create-time referential integrity; it
// must be stripped. A search-seed Location's partOf referencing the setup
// Location is not a self-reference and must be preserved.
func TestSynthesizeBodyStripsSelfReferences(t *testing.T) {
	locationURL := "http://example.org/StructureDefinition/location"
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: locationURL, Type: "Location", Kind: "resource", Elements: []model.ElementDefinition{
		{Path: "Location", Min: 0, Max: "*"},
		{Path: "Location.name", Min: 1, Max: "1", Types: []model.ElementType{{Code: "string"}}},
		{Path: "Location.partOf", Min: 0, Max: "1", Types: []model.ElementType{{Code: "Reference", TargetProfile: []string{locationURL}}}},
	}})

	// Setup Location: its partOf resolves to its own reference and must be
	// stripped.
	setupBody := SynthesizeBody("Location", "momus-setup-location", []string{locationURL}, locationURL, nil, reg, true)
	if _, ok := setupBody["partOf"]; ok {
		t.Fatalf("setup Location must not self-reference via partOf, got %+v", setupBody["partOf"])
	}
	if setupBody["name"] == nil {
		t.Fatal("expected Location.name to remain present after self-reference strip")
	}

	// Search-seed Location: partOf references the setup Location, not itself,
	// so it must be preserved when present.
	seed := SynthesizeBody("Location", "momus-search-loc", []string{locationURL}, locationURL, nil, reg, true)
	if partOf, ok := seed["partOf"].(map[string]any); ok {
		if ref, _ := partOf["reference"].(string); ref == "Location/momus-search-loc" {
			t.Fatalf("search-seed Location self-reference not stripped: %v", ref)
		}
	}
}

// TestApplySliceConstraintsRecursesIntoNestedCoding verifies that a slice's
// Fixed value carried several levels deep is applied to the generated value. The
// suppressedBy sub-extension constrains its value[x].coding to a fixed
// organisation-initiated coding; without recursion applySliceConstractions only
// touched the slice's direct children, leaving a generic placeholder coding the
// server rejects.
func TestApplySliceConstraintsRecursesIntoNestedCoding(t *testing.T) {
	reg := registry.New()
	reg.AddCodeSystem(&model.CodeSystem{URL: "http://example.org/CodeSystem/responsible-party-type", Concepts: []model.CodeSystemConcept{
		{Code: "organisation-initiated", Display: "Organisation initiated"},
	}})

	fixedCoding := map[string]any{
		"system": "http://example.org/CodeSystem/responsible-party-type",
		"code":   "organisation-initiated",
	}
	slice := &model.SliceNode{
		Name:       "suppressedBy",
		Definition: &model.ElementDefinition{Path: "Organization.extension.extension", SliceName: "suppressedBy", Min: 1, Max: "1"},
		Children: map[string]*model.ElementNode{
			"url": {
				Name: "url", Path: "Organization.extension.extension.url",
				Definition: &model.ElementDefinition{Path: "Organization.extension.extension.url", Min: 1, Max: "1", Fixed: "suppressedBy"},
			},
			"value[x]": {
				Name: "value[x]", Path: "Organization.extension.extension.value[x]",
				Definition: &model.ElementDefinition{Path: "Organization.extension.extension.value[x]", Min: 1, Max: "1", Types: []model.ElementType{{Code: "CodeableConcept"}}},
				Children: map[string]*model.ElementNode{
					"coding": {
						Name: "coding", Path: "Organization.extension.extension.value[x].coding",
						Definition: &model.ElementDefinition{Path: "Organization.extension.extension.value[x].coding", Min: 1, Max: "1", Fixed: fixedCoding, Types: []model.ElementType{{Code: "Coding"}}},
					},
				},
			},
		},
	}

	// A value generated generically with a placeholder coding and a stale
	// placeholder text (e.g. "Value[x]" synthesized by the fallback generator).
	value := map[string]any{
		"url": "suppressedBy",
		"valueCodeableConcept": map[string]any{
			"coding": []any{map[string]any{"system": "http://example.org", "code": "value-x"}},
			"text":   "Value[x]",
		},
	}
	applySliceConstractions(value, slice, reg)

	cc, ok := value["valueCodeableConcept"].(map[string]any)
	if !ok {
		t.Fatalf("expected valueCodeableConcept map, got %T", value["valueCodeableConcept"])
	}
	// The stale generic placeholder text must be dropped when the coding is fixed.
	if _, hasText := cc["text"]; hasText {
		t.Fatalf("stale placeholder text should be removed once coding is fixed, got %#v", cc["text"])
	}
	codings, ok := cc["coding"].([]any)
	if !ok || len(codings) != 1 {
		t.Fatalf("expected a single coding, got %#v", cc["coding"])
	}
	coding, ok := codings[0].(map[string]any)
	if !ok {
		t.Fatalf("expected coding map, got %T", codings[0])
	}
	if coding["code"] != "organisation-initiated" {
		t.Fatalf("got code %v, want organisation-initiated", coding["code"])
	}
	if coding["system"] != "http://example.org/CodeSystem/responsible-party-type" {
		t.Fatalf("got system %v, want responsible-party-type", coding["system"])
	}
	// A fixed coding may carry only system+code: the display HAPI would otherwise
	// reject must be absent.
	if _, hasDisplay := coding["display"]; hasDisplay {
		t.Fatalf("fixed coding must not carry a display, got %v", coding["display"])
	}
}

// TestSynthesizeBodyGivesSimpleExtensionAValue verifies that a simple extension
// slice (e.g. the HCPD active-period extension, whose Extension.extension is
// Max 0) is emitted with a value[x], not as an empty {"url": ...} which would
// violate ext-1 and be rejected by HAPI.
func TestSynthesizeBodyGivesSimpleExtensionAValue(t *testing.T) {
	r := registry.New()
	activePeriodURL := "http://digitalhealth.gov.au/fhir/cc/StructureDefinition/active-period"
	r.AddStructureDefinition(&model.StructureDefinition{URL: activePeriodURL, Type: "Extension", Kind: "complex-type", Elements: []model.ElementDefinition{
		{Path: "Extension", Min: 0, Max: "*"},
		{Path: "Extension.extension", Min: 0, Max: "0", Types: []model.ElementType{{Code: "Extension"}}},
		{Path: "Extension.url", Min: 1, Max: "1", Types: []model.ElementType{{Code: "uri"}}, Fixed: activePeriodURL},
		{Path: "Extension.value[x]", Min: 0, Max: "1", Types: []model.ElementType{{Code: "Period"}}},
	}})
	orgURL := "http://example.org/StructureDefinition/org"
	r.AddStructureDefinition(&model.StructureDefinition{URL: orgURL, Type: "Organization", Elements: []model.ElementDefinition{
		{Path: "Organization", Min: 0, Max: "*"},
		{Path: "Organization.name", Min: 1, Max: "1", Types: []model.ElementType{{Code: "string"}}},
		{Path: "Organization.extension", Min: 0, Max: "*", Types: []model.ElementType{{Code: "Extension"}}},
		{Path: "Organization.extension", Min: 1, Max: "1", SliceName: "active-period", Types: []model.ElementType{{Code: "Extension", Profile: []string{activePeriodURL}}}},
	}})

	body := SynthesizeBody("Organization", "momus-test", []string{orgURL}, orgURL, nil, r, true)
	rawExt, ok := body["extension"].([]any)
	if !ok || len(rawExt) == 0 {
		t.Fatalf("expected extension array, got %#v", body["extension"])
	}
	ext := rawExt[0].(map[string]any)
	if _, hasValue := ext["valuePeriod"]; !hasValue {
		t.Fatalf("simple extension must carry a valuePeriod, got %#v", ext)
	}
	if _, hasEmpty := ext["extension"]; hasEmpty {
		t.Fatalf("simple extension must not carry sub-extensions, got %#v", ext)
	}
}

// TestSynthesizeBodyFixedCodingCarriesOnlySystemAndCode verifies that a sliced
// extension whose coding is fixed to {system, code} is emitted without display or
// text (HAPI rejects extra elements on a fixed value), and that the display/text
// normalisation passes do not re-add them and the internal marker is stripped.
func TestSynthesizeBodyFixedCodingCarriesOnlySystemAndCode(t *testing.T) {
	r := registry.New()
	r.AddCodeSystem(&model.CodeSystem{URL: "http://digitalhealth.gov.au/fhir/cc/CodeSystem/responsible-party-type", Concepts: []model.CodeSystemConcept{
		{Code: "organisation-initiated", Display: "Organisation initiated"},
	}})

	fixedCoding := map[string]any{
		"system": "http://digitalhealth.gov.au/fhir/cc/CodeSystem/responsible-party-type",
		"code":   "organisation-initiated",
	}
	slice := &model.SliceNode{
		Name:       "suppressedBy",
		Definition: &model.ElementDefinition{Path: "Organization.extension.extension", SliceName: "suppressedBy", Min: 1, Max: "1"},
		Children: map[string]*model.ElementNode{
			"value[x]": {
				Name: "value[x]", Path: "Organization.extension.extension.value[x]",
				Definition: &model.ElementDefinition{Path: "Organization.extension.extension.value[x]", Min: 1, Max: "1", Types: []model.ElementType{{Code: "CodeableConcept"}}},
				Children: map[string]*model.ElementNode{
					"coding": {
						Name: "coding", Path: "Organization.extension.extension.value[x].coding",
						Definition: &model.ElementDefinition{Path: "Organization.extension.extension.value[x].coding", Min: 1, Max: "1", Fixed: fixedCoding, Types: []model.ElementType{{Code: "Coding"}}},
					},
				},
			},
		},
	}

	value := map[string]any{
		"url": "suppressedBy",
		"valueCodeableConcept": map[string]any{
			"coding": []any{map[string]any{"system": "http://example.org", "code": "value-x"}},
			"text":   "Value[x]",
		},
	}
	applySliceConstractions(value, slice, r)
	// Run the full display/text normalisation passes that synthesizeBody applies
	// after generation, then strip markers, to prove nothing re-adds display/text.
	normalizeGeneratedPayload(value)
	normalisePayloadCodingDisplays(value, r)
	stripFixedCodingMarkers(value)

	cc := value["valueCodeableConcept"].(map[string]any)
	codings := cc["coding"].([]any)
	coding := codings[0].(map[string]any)
	if coding["code"] != "organisation-initiated" {
		t.Fatalf("got code %v, want organisation-initiated", coding["code"])
	}
	if _, hasDisplay := coding["display"]; hasDisplay {
		t.Fatalf("fixed coding must not carry a display, got %v", coding["display"])
	}
	if _, hasText := cc["text"]; hasText {
		t.Fatalf("fixed CodeableConcept must not carry a text, got %v", cc["text"])
	}
	// The internal marker must never leak.
	if _, has := coding[fixedCodingKey]; has {
		t.Fatalf("fixed coding marker leaked into payload: %q", fixedCodingKey)
	}
}

// TestGenerateAHPRAProducesValidRegistrationNumber verifies the Ahpra registration
// number satisfies the au-ahpraregistrationnumber inv-ahpra-0 invariant: three
// uppercase letters followed by ten digits.
func TestGenerateAHPRAProducesValidRegistrationNumber(t *testing.T) {
	v := generateAHPRA()
	if len(v) != 13 {
		t.Fatalf("generateAHPRA()=%q length %d, want 13", v, len(v))
	}
	for i, r := range v {
		switch {
		case i < 3 && (r < 'A' || r > 'Z'):
			t.Fatalf("generateAHPRA()=%q: char %d must be uppercase letter", v, i)
		case i >= 3 && (r < '0' || r > '9'):
			t.Fatalf("generateAHPRA()=%q: char %d must be digit", v, i)
		}
	}
}

// TestNormalizeGeneratedIdentifierFixesAhpraValue verifies that an identifier with
// the Ahpra registration-number system gets a valid value via normalisation.
func TestNormalizeGeneratedIdentifierFixesAhpraValue(t *testing.T) {
	identifier := map[string]any{
		"system": "http://hl7.org.au/id/ahpra-registration-number",
		"value":  "123456",
	}
	normalizeGeneratedIdentifier(identifier)
	v, _ := identifier["value"].(string)
	if len(v) != 13 {
		t.Fatalf("got value %q, want 13-char Ahpra number", v)
	}
}

// TestStripEmptyExtensionsDropsInvalidExtensions verifies that an extension with
// neither a value[x] nor nested sub-extensions (which violates ext-1) is removed,
// while extensions with a value or sub-extensions are preserved.
func TestStripEmptyExtensionsDropsInvalidExtensions(t *testing.T) {
	body := map[string]any{
		"extension": []any{
			map[string]any{"url": "http://example.org/empty"},
			map[string]any{"url": "http://example.org/with-value", "valueString": "x"},
			map[string]any{"url": "http://example.org/with-sub", "extension": []any{map[string]any{"url": "sub", "valueBoolean": true}}},
		},
	}
	stripEmptyExtensions(body)
	ext := body["extension"].([]any)
	if len(ext) != 2 {
		t.Fatalf("expected 2 extensions after cleanup, got %d: %#v", len(ext), ext)
	}
	for _, raw := range ext {
		url, _ := raw.(map[string]any)["url"].(string)
		if url == "http://example.org/empty" {
			t.Fatalf("empty extension not dropped")
		}
	}
}

// TestStripEmptyExtensionsDropsWholeArray verifies that when every extension in an
// array is empty the whole extension array is removed.
func TestStripEmptyExtensionsDropsWholeArray(t *testing.T) {
	body := map[string]any{"extension": []any{map[string]any{"url": "http://example.org/only-empty"}}}
	stripEmptyExtensions(body)
	if _, has := body["extension"]; has {
		t.Fatalf("expected extension array removed when all members empty, got %#v", body["extension"])
	}
}
