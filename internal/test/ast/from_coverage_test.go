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
	if len(root.Steps) != 1 {
		t.Fatalf("got %d root steps, want 1", len(root.Steps))
	}

	resourceSeq, ok := root.Steps[0].(*Sequence)
	if !ok {
		t.Fatalf("expected resource step to be *Sequence, got %T", root.Steps[0])
	}
	if len(resourceSeq.Steps) != 5 {
		t.Fatalf("got %d resource steps, want 5", len(resourceSeq.Steps))
	}

	setupReq, ok := resourceSeq.Steps[0].(*Request)
	if !ok {
		t.Fatalf("expected first resource step to be *Request, got %T", resourceSeq.Steps[0])
	}
	if setupReq.Method != "PUT" {
		t.Fatalf("got method %q, want PUT", setupReq.Method)
	}
	if setupReq.URL != "http://localhost:8080/fhir/Patient/momus-setup-patient" {
		t.Fatalf("got URL %q, want %q", setupReq.URL, "http://localhost:8080/fhir/Patient/momus-setup-patient")
	}
	if _, ok := setupReq.Headers["X-Momus-Requirement-ID"]; ok {
		t.Fatalf("did not expect setup request to carry requirement header")
	}
	setupBody := setupReq.Body.(map[string]any)
	if setupBody["id"] != "momus-setup-patient" {
		t.Fatalf("got setup id %v, want momus-setup-patient", setupBody["id"])
	}

	case0, ok := resourceSeq.Steps[3].(*Sequence)
	if !ok {
		t.Fatalf("expected first case to be *Sequence, got %T", resourceSeq.Steps[3])
	}
	assert1, ok := case0.Steps[1].(*Assert)
	if !ok {
		t.Fatalf("expected case assertion to be *Assert, got %T", case0.Steps[1])
	}
	if assert1.Expression != "status in [200,201]" {
		t.Fatalf("got expression %q, want %q", assert1.Expression, "status in [200,201]")
	}

	case1 := resourceSeq.Steps[4].(*Sequence)
	assert2 := case1.Steps[1].(*Assert)
	if assert2.Expression != "status in [400,422]" {
		t.Fatalf("got expression %q, want %q", assert2.Expression, "status in [400,422]")
	}
}

func TestGenerateFromCoveragePlanUsesDependencyTemplate(t *testing.T) {
	plan, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{
		Requirements: []coverage.CoverageRequirement{
			{ID: "p-1", ResourceType: "Patient", ElementPath: "Patient.name", Variant: coverage.CoverageVariantValidMin},
			{ID: "o-1", ResourceType: "Observation", ElementPath: "Observation.subject", DependencyTargets: []string{"Patient"}, Variant: coverage.CoverageVariantValidMin},
		},
	}, BuildOptions{BaseURL: "http://localhost:8080/fhir"})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}

	root := plan.Root.(*Sequence)
	if len(root.Steps) != 2 {
		t.Fatalf("got %d levels, want 2", len(root.Steps))
	}

	obsResourceSeq := root.Steps[1].(*Sequence)
	setupReq := obsResourceSeq.Steps[0].(*Request)
	if setupReq.Method != "PUT" {
		t.Fatalf("got method %q, want PUT", setupReq.Method)
	}
	body := setupReq.Body.(map[string]any)
	subject := body["subject"].(map[string]any)
	if subject["reference"] != "Patient/{{Patient.id}}" {
		t.Fatalf("got subject reference %v, want Patient/{{Patient.id}}", subject["reference"])
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
