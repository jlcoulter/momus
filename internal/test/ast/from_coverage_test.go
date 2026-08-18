package ast

import (
	"testing"

	"github.com/jlcoulter/momus/internal/test/coverage"
)

func TestGenerateFromCoveragePlanBuildsPerRequirementSequence(t *testing.T) {
	plan, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{
		Requirements: []coverage.CoverageRequirement{
			{
				ID:           "req-1",
				ProfileURL:   "http://example.org/StructureDefinition/patient",
				ResourceType: "Patient",
				ElementPath:  "Patient.name",
				Variant:      coverage.CoverageVariantValidMin,
				Min:          1,
				Max:          "*",
			},
			{
				ID:           "req-2",
				ProfileURL:   "http://example.org/StructureDefinition/patient",
				ResourceType: "Patient",
				ElementPath:  "Patient.name",
				Variant:      coverage.CoverageVariantMissingRequired,
				Min:          1,
				Max:          "*",
			},
		},
	}, BuildOptions{BaseURL: "http://localhost:8080/fhir"})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}

	if plan.Version != "v1" {
		t.Fatalf("got version %q, want v1", plan.Version)
	}

	root, ok := plan.Root.(*Sequence)
	if !ok {
		t.Fatalf("expected root to be *Sequence, got %T", plan.Root)
	}
	if len(root.Steps) != 2 {
		t.Fatalf("got %d root steps, want 2", len(root.Steps))
	}

	case0, ok := root.Steps[0].(*Sequence)
	if !ok {
		t.Fatalf("expected case step to be *Sequence, got %T", root.Steps[0])
	}
	if len(case0.Steps) != 2 {
		t.Fatalf("got %d case steps, want 2", len(case0.Steps))
	}

	req, ok := case0.Steps[0].(*Request)
	if !ok {
		t.Fatalf("expected first case step to be *Request, got %T", case0.Steps[0])
	}
	if req.URL != "http://localhost:8080/fhir/Patient" {
		t.Fatalf("got URL %q, want %q", req.URL, "http://localhost:8080/fhir/Patient")
	}
	if req.Headers["X-Momus-Requirement-ID"] != "req-1" {
		t.Fatalf("missing requirement id header, got headers=%v", req.Headers)
	}

	assert1, ok := case0.Steps[1].(*Assert)
	if !ok {
		t.Fatalf("expected second case step to be *Assert, got %T", case0.Steps[1])
	}
	if assert1.Expression != "status in [200,201]" {
		t.Fatalf("got expression %q, want %q", assert1.Expression, "status in [200,201]")
	}

	case1 := root.Steps[1].(*Sequence)
	assert2 := case1.Steps[1].(*Assert)
	if assert2.Expression != "status in [400,422]" {
		t.Fatalf("got expression %q, want %q", assert2.Expression, "status in [400,422]")
	}
}

func TestEncodePlanIncludesTypeTags(t *testing.T) {
	plan := &Plan{
		Version: "v1",
		Root: &Sequence{Steps: []Node{
			&Request{Method: "GET", URL: "/Patient"},
			&Assert{Description: "ok", RequirementID: "r-1", Expression: "status == 200"},
		}},
	}

	encoded, err := EncodePlan(plan)
	if err != nil {
		t.Fatalf("EncodePlan returned error: %v", err)
	}

	root, ok := encoded["root"].(map[string]any)
	if !ok {
		t.Fatalf("expected encoded root object, got %T", encoded["root"])
	}
	if root["type"] != "sequence" {
		t.Fatalf("got root type %v, want sequence", root["type"])
	}

	steps, ok := root["steps"].([]any)
	if !ok {
		t.Fatalf("expected steps array, got %T", root["steps"])
	}
	if len(steps) != 2 {
		t.Fatalf("got %d steps, want 2", len(steps))
	}

	step0 := steps[0].(map[string]any)
	if step0["type"] != "request" {
		t.Fatalf("got first step type %v, want request", step0["type"])
	}
}
