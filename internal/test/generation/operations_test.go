package generation

import (
	"testing"

	"github.com/jlcoulter/momus/internal/test/ast"
	"github.com/jlcoulter/momus/internal/test/coverage"
)

func TestGenerateOperationCasesEmitCorrectRequests(t *testing.T) {
	plan, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{
		Requirements: []coverage.CoverageRequirement{
			{ID: "op-1", ResourceType: "Organization", Domain: coverage.CoverageDomainOperation, Variant: coverage.CoverageVariantOperationRead},
			{ID: "op-2", ResourceType: "Organization", Domain: coverage.CoverageDomainOperation, Variant: coverage.CoverageVariantOperationDelete},
			{ID: "op-3", ResourceType: "Organization", Domain: coverage.CoverageDomainOperation, Variant: coverage.CoverageVariantOperationUpdate},
			{ID: "op-4", ResourceType: "Organization", Domain: coverage.CoverageDomainOperation, Variant: coverage.CoverageVariantOperationHistory},
			{ID: "st-1", ResourceType: "Organization", Domain: coverage.CoverageDomainState, Variant: coverage.CoverageVariantStateReadNonexistent},
			{ID: "st-2", ResourceType: "Organization", Domain: coverage.CoverageDomainState, Variant: coverage.CoverageVariantStateDeleteNonexistent},
		},
	}, BuildOptions{BaseURL: "http://localhost:8080/fhir"})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}

	got := map[string]string{} // requirement id -> "METHOD url"
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
			if rid := n.Headers["X-Momus-Requirement-ID"]; rid != "" {
				got[rid] = n.Method + " " + n.URL
			}
		}
	}
	walk(plan.Root)

	want := map[string]string{
		"op-1": "GET http://localhost:8080/fhir/Organization/momus-setup-organization",
		"op-2": "DELETE http://localhost:8080/fhir/Organization/momus-setup-organization",
		"op-3": "PUT http://localhost:8080/fhir/Organization/momus-setup-organization",
		"op-4": "GET http://localhost:8080/fhir/Organization/momus-setup-organization/_history",
		"st-1": "GET http://localhost:8080/fhir/Organization/momus-missing",
		"st-2": "DELETE http://localhost:8080/fhir/Organization/momus-missing",
	}
	for id, w := range want {
		if got[id] != w {
			t.Fatalf("request for %s = %q, want %q", id, got[id], w)
		}
	}
}
