package generation

import (
	"strings"
	"testing"

	"github.com/jlcoulter/momus/internal/test/ast"
	"github.com/jlcoulter/momus/internal/test/coverage"
)

func TestBuildSearchModifierAndCombinationQueries(t *testing.T) {
	plan, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{
		Requirements: []coverage.CoverageRequirement{
			{ID: "mod-1", ResourceType: "Organization", Domain: coverage.CoverageDomainSearch, Variant: coverage.CoverageVariantSearchInvalidModifier, SearchCode: "name"},
			{ID: "combo-1", ResourceType: "Organization", Domain: coverage.CoverageDomainSearch, Variant: coverage.CoverageVariantSearchCombination, SearchCode: "name", SearchCodeB: "active"},
		},
	}, BuildOptions{BaseURL: "http://localhost:8080/fhir"})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}

	got := map[string]string{}
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
				got[rid] = n.URL
			}
		}
	}
	walk(plan.Root)

	if got["mod-1"] != "http://localhost:8080/fhir/Organization?name:zzz=momus-search" {
		t.Fatalf("modifier query = %q", got["mod-1"])
	}
	if got["combo-1"] != "http://localhost:8080/fhir/Organization?name=momus-search&active=momus-search" {
		t.Fatalf("combination query = %q", got["combo-1"])
	}
}

func TestBuildSearchCasesEmitGETRequests(t *testing.T) {
	plan, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{
		Requirements: []coverage.CoverageRequirement{
			{ID: "search-1", ResourceType: "Organization", Domain: coverage.CoverageDomainSearch, Variant: coverage.CoverageVariantSearchValid, SearchCode: "_id"},
			{ID: "search-2", ResourceType: "Organization", Domain: coverage.CoverageDomainSearch, Variant: coverage.CoverageVariantSearchInvalidValue, SearchCode: "active"},
		},
	}, BuildOptions{BaseURL: "http://localhost:8080/fhir"})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}

	var reqs []*ast.Request
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
			reqs = append(reqs, n)
		}
	}
	walk(plan.Root)

	var searchGETs int
	for _, req := range reqs {
		if req.Method != "GET" {
			continue
		}
		searchGETs++
		if !strings.Contains(req.URL, "?") {
			t.Fatalf("search request missing query: %s", req.URL)
		}
	}
	if searchGETs != 2 {
		t.Fatalf("got %d search GET requests, want 2", searchGETs)
	}
}
