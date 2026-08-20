package generation

import (
	"sort"
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
