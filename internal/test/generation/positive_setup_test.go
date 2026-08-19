package generation

import (
	"reflect"
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
	"github.com/jlcoulter/momus/internal/test/ast"
	"github.com/jlcoulter/momus/internal/test/coverage"
)

// firstSetupBody returns the body of the first setup (non-requirement) request
// in a generated plan.
func firstSetupBody(t *testing.T, plan *ast.Plan) map[string]any {
	t.Helper()
	var body map[string]any
	var walk func(ast.Node)
	walk = func(node ast.Node) {
		if body != nil {
			return
		}
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
			if _, isRequirement := n.Headers["X-Momus-Requirement-ID"]; !isRequirement {
				if m, ok := n.Body.(map[string]any); ok {
					body = m
				}
			}
		}
	}
	walk(plan.Root)
	if body == nil {
		t.Fatal("no setup request body found in plan")
	}
	return body
}

// TestBuildSetupDatasetMatchesASTSetupRequests verifies that the seed dataset
// built for provisioning uses the exact same body-generation logic as the AST
// setup requests, so provisioned data conforms to the same profiles and is
// what the generated tests reference.
func TestBuildSetupDatasetMatchesASTSetupRequests(t *testing.T) {
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
	meta := inst.Resource["meta"].(map[string]any)
	profiles := meta["profile"].([]any)
	if len(profiles) != 1 || profiles[0] != "http://example.org/StructureDefinition/patient" {
		t.Fatalf("got meta.profile %v, want patient profile", meta["profile"])
	}

	astPlan, err := GenerateFromCoveragePlan(plan, opts)
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}
	setupBody := firstSetupBody(t, astPlan)
	if !reflect.DeepEqual(inst.Resource, setupBody) {
		t.Fatalf("setup dataset body %v does not match AST setup body %v", inst.Resource, setupBody)
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
