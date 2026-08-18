package planner

import (
	"context"
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/test/ast"
)

type fakeGenerator struct {
	ds *model.Dataset
}

func (f fakeGenerator) Generate(ctx context.Context, req model.DataRequirement) (*model.Dataset, error) {
	return f.ds, nil
}

func testDataset() *model.Dataset {
	return &model.Dataset{
		Resources: map[string]*model.ResourceInstance{
			"patient": {LocalID: "patient", ResourceType: "Patient", Resource: map[string]any{"resourceType": "Patient", "id": "patient"}},
			"prac":    {LocalID: "prac", ResourceType: "Practitioner", Resource: map[string]any{"resourceType": "Practitioner", "id": "prac"}},
			"obs":     {LocalID: "obs", ResourceType: "Observation", Resource: map[string]any{"resourceType": "Observation", "id": "obs", "subject": map[string]any{"reference": "Patient/patient"}}},
		},
		Relationships: []model.Reference{
			{SourceID: "obs", TargetID: "patient", Path: "Observation.subject"},
		},
	}
}

// collectPutOrder returns the resource ids of every PUT request in AST order.
func collectPutOrder(root ast.Node) []string {
	var order []string
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
			if n.Method == "PUT" {
				order = append(order, n.Body.(map[string]any)["id"].(string))
			}
		}
	}
	walk(root)
	return order
}

func testRequirement() []model.DataRequirement {
	return []model.DataRequirement{{ID: "req-1", Resource: model.ResourceRequirement{Type: "Observation"}}}
}

func TestPlanGeneratesDataset(t *testing.T) {
	ds := testDataset()
	p := NewDefaultPlanner(fakeGenerator{ds})
	tp, err := p.Plan(context.Background(), Input{BaseURL: "http://localhost:8080/fhir", Requirements: testRequirement()})
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if tp.Dataset == nil || len(tp.Dataset.Resources) != 3 {
		t.Fatalf("expected generated dataset with 3 resources, got %v", tp.Dataset)
	}
	if tp.Root == nil {
		t.Fatal("expected non-nil plan root")
	}
}

func TestPlanProvisionsTargetsBeforeDependents(t *testing.T) {
	p := NewDefaultPlanner(fakeGenerator{testDataset()})
	tp, err := p.Plan(context.Background(), Input{BaseURL: "http://localhost:8080/fhir", Requirements: testRequirement()})
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	order := collectPutOrder(tp.Root)
	// patient must be provisioned before obs (obs references patient).
	patientIdx, obsIdx := -1, -1
	for i, id := range order {
		if id == "patient" {
			patientIdx = i
		}
		if id == "obs" {
			obsIdx = i
		}
	}
	if patientIdx == -1 || obsIdx == -1 {
		t.Fatalf("expected patient and obs in plan, got %v", order)
	}
	if patientIdx > obsIdx {
		t.Fatalf("patient (idx %d) must be provisioned before obs (idx %d): %v", patientIdx, obsIdx, order)
	}
}

func TestPlanUsesParallelForIndependentResources(t *testing.T) {
	p := NewDefaultPlanner(fakeGenerator{testDataset()})
	tp, err := p.Plan(context.Background(), Input{BaseURL: "http://localhost:8080/fhir", Requirements: testRequirement()})
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	// The plan root must be a Sequence whose first step is a Parallel carrying
	// the independent level-0 resources (patient and practitioner).
	root, ok := tp.Root.(*ast.Sequence)
	if !ok || len(root.Steps) == 0 {
		t.Fatalf("expected root Sequence with levels, got %T", tp.Root)
	}
	parallel, ok := root.Steps[0].(*ast.Parallel)
	if !ok {
		t.Fatalf("expected first level to be Parallel, got %T", root.Steps[0])
	}
	if len(parallel.Steps) != 2 {
		t.Fatalf("expected 2 independent resources in first parallel level, got %d", len(parallel.Steps))
	}
}

func TestPlanRequiresGenerator(t *testing.T) {
	p := NewDefaultPlanner(nil)
	if _, err := p.Plan(context.Background(), Input{}); err == nil {
		t.Fatal("expected error for nil generator")
	}
}

func TestDependencyLevels(t *testing.T) {
	levels := dependencyLevels(testDataset())
	if len(levels) != 2 {
		t.Fatalf("got %d levels, want 2", len(levels))
	}
	if len(levels[0]) != 2 {
		t.Fatalf("level 0 should have patient and practitioner, got %v", levels[0])
	}
	if len(levels[1]) != 1 || levels[1][0] != "obs" {
		t.Fatalf("level 1 should contain obs, got %v", levels[1])
	}
}
