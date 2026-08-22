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

// TestSearchCombinationUsesPerCodeValues verifies that a combination search
// computes a value per search code rather than reusing one value for both
// parameters (task #25). With mixed value types (a string `name` and a boolean
// `active`) each parameter must get a type-appropriate value, so the query is
// not `name=momus-search&active=momus-search` (which some servers reject).
func TestSearchCombinationUsesPerCodeValues(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/patient",
		Type: "Patient",
		Elements: []model.ElementDefinition{
			{Path: "Patient", Min: 0, Max: "*"},
			{Path: "Patient.name", Min: 0, Max: "*", Types: []model.ElementType{{Code: "string"}}},
			{Path: "Patient.active", Min: 0, Max: "1", Types: []model.ElementType{{Code: "boolean"}}},
		},
	})
	reg.AddSearchParameter(&model.SearchParameter{Code: "name", Base: []string{"Patient"}, Type: "string", Expression: "Patient.name"})
	reg.AddSearchParameter(&model.SearchParameter{Code: "active", Base: []string{"Patient"}, Type: "boolean", Expression: "Patient.active"})

	req := coverage.CoverageRequirement{
		ID: "combo-1", ResourceType: "Patient", Domain: coverage.CoverageDomainSearch,
		Variant: coverage.CoverageVariantSearchCombination, SearchCode: "name", SearchCodeB: "active",
	}
	options := coregen.BuildOptions{BaseURL: "http://localhost:8080/fhir", Builder: NewBuilder(reg, false)}
	query := searchQuery(req, options)
	// name is a string -> "momus-search"; active is a boolean -> "true".
	if query != "name=momus-search&active=true" {
		t.Fatalf("combination query = %q, want per-code values name=momus-search&active=true", query)
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

// TestSearchInvalidValuePerType is a matrix check that the invalid-value search
// obligation produces a genuinely invalid value (and a reject assertion) only for
// search parameter types with a strict lexical grammar, and a type-valid,
// non-matching value (with an accept-200 assertion) for types where a conformant
// server accepts any syntactically valid value instead of returning a 4xx.
func TestSearchInvalidValuePerType(t *testing.T) {
	reg := registry.New()
	addSP := func(code, typ string) {
		reg.AddSearchParameter(&model.SearchParameter{Code: code, Base: []string{"Observation"}, Type: typ})
	}
	addSP("num", "number")
	addSP("dt", "date")
	addSP("str", "string")
	addSP("tok", "token")
	addSP("uri", "uri")
	addSP("ref", "reference")
	addSP("qty", "quantity")

	options := coregen.BuildOptions{Builder: NewBuilder(reg, false)}
	builder := options.Builder
	tests := []struct {
		code       string
		wantReject bool
	}{
		{"num", true},
		{"dt", true},
		{"str", false},
		{"tok", false},
		{"uri", false},
		{"ref", false},
		{"qty", false},
	}
	for _, tc := range tests {
		req := coverage.CoverageRequirement{ID: "inv-" + tc.code, ResourceType: "Observation", Domain: coverage.CoverageDomainSearch, Variant: coverage.CoverageVariantSearchInvalidValue, SearchCode: tc.code}
		value, expectReject := searchInvalidValue(req, tc.code, builder)
		if expectReject != tc.wantReject {
			t.Errorf("%s: expectReject = %v, want %v", tc.code, expectReject, tc.wantReject)
		}
		if value == "" {
			t.Errorf("%s: empty invalid value", tc.code)
		}

		assert := searchAssert(req, options)
		if tc.wantReject {
			if !strings.Contains(assert.Expression, "400") {
				t.Errorf("%s: reject assertion expression = %q, want a 4xx", tc.code, assert.Expression)
			}
		} else {
			if assert.Expression != "status in [200]" {
				t.Errorf("%s: accept assertion expression = %q, want status in [200]", tc.code, assert.Expression)
			}
			if assert.Trace == nil || assert.Trace.Expected != "accept" {
				t.Errorf("%s: accept assertion trace expected = %+v, want accept", tc.code, assert.Trace)
			}
		}
	}
}
