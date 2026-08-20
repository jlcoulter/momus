package generation

import (
	"sort"
	"strings"
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
	"github.com/jlcoulter/momus/internal/test/ast"
	"github.com/jlcoulter/momus/internal/test/coverage"
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
	inst, ok := ds.Resources[setupResourceID("Patient")]
	if !ok {
		t.Fatalf("expected setup resource %s in dataset", setupResourceID("Patient"))
	}
	if inst.ResourceType != "Patient" {
		t.Fatalf("got resource type %q, want Patient", inst.ResourceType)
	}
	body := inst.Resource
	if body["id"] != setupResourceID("Patient") {
		t.Fatalf("got dataset id %v, want %s", body["id"], setupResourceID("Patient"))
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
	if _, ok := ds.Resources[setupResourceID("Observation")]; !ok {
		t.Fatalf("expected setup Observation resource in dataset")
	}
	patient, ok := ds.Resources[setupResourceID("Patient")]
	if !ok {
		t.Fatalf("expected setup Patient resource in dataset (transitive reference target), got %v", keysOf(ds.Resources))
	}
	if patient.ResourceType != "Patient" {
		t.Fatalf("got resource type %q, want Patient", patient.ResourceType)
	}
	found := false
	for _, rel := range ds.Relationships {
		if rel.SourceID == setupResourceID("Observation") && rel.TargetID == setupResourceID("Patient") {
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
	if _, ok := ds.Resources[setupResourceID("Resource")]; ok {
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
	if _, ok := ds.Resources[setupResourceID("Organization")]; ok {
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
	if _, ok := ds.Resources[setupResourceID("Patient")]; ok {
		t.Fatal("Patient must not be seeded when it is outside the capability scope")
	}
	if _, ok := ds.Resources[setupResourceID("Observation")]; !ok {
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
	if _, ok := ds.Resources[setupResourceID("Patient")]; !ok {
		t.Fatalf("expected setup resource %s in dataset", setupResourceID("Patient"))
	}
	if _, ok := ds.Resources[setupResourceID("Observation")]; !ok {
		t.Fatalf("expected setup resource %s in dataset", setupResourceID("Observation"))
	}
	found := false
	for _, rel := range ds.Relationships {
		if rel.SourceID == setupResourceID("Observation") && rel.TargetID == setupResourceID("Patient") {
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
	endpointLocalID := setupResourceID("Endpoint")
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
	if coding["display"] != "Organization identifier" {
		t.Fatalf("got display %v, want Organization identifier", coding["display"])
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
	setupBody := synthesizeBody("Location", "momus-setup-location", []string{locationURL}, locationURL, nil, reg, true)
	if _, ok := setupBody["partOf"]; ok {
		t.Fatalf("setup Location must not self-reference via partOf, got %+v", setupBody["partOf"])
	}
	if setupBody["name"] == nil {
		t.Fatal("expected Location.name to remain present after self-reference strip")
	}

	// Search-seed Location: partOf references the setup Location, not itself,
	// so it must be preserved when present.
	seed := synthesizeBody("Location", "momus-search-loc", []string{locationURL}, locationURL, nil, reg, true)
	if partOf, ok := seed["partOf"].(map[string]any); ok {
		if ref, _ := partOf["reference"].(string); ref == "Location/momus-search-loc" {
			t.Fatalf("search-seed Location self-reference not stripped: %v", ref)
		}
	}
}
