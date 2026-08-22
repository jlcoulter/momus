package fhircoverage

import (
	"testing"

	"github.com/jlcoulter/momus/internal/core/coverage"
	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

func TestDerivePlanAddsSearchModifierAndCombination(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/org-profile",
		Type: "Organization",
		Elements: []model.ElementDefinition{
			{Path: "Organization", Min: 0, Max: "*"},
			{Path: "Organization.name", Min: 1, Max: "1"},
		},
	})
	r.AddSearchParameter(&model.SearchParameter{URL: "http://hl7.org/fhir/SearchParameter/Resource-id", Name: "_id", Code: "_id", Base: []string{"Resource"}, Type: "token"})
	r.AddSearchParameter(&model.SearchParameter{URL: "http://hl7.org/fhir/SearchParameter/Organization-active", Name: "active", Code: "active", Base: []string{"Organization"}, Type: "token"})

	plan, err := DerivePlan(r, coverage.DeriveOptions{})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}
	var hasModifier bool
	for _, req := range plan.Requirements {
		if req.Variant == coverage.CoverageVariantSearchInvalidModifier {
			hasModifier = true
		}
	}
	if !hasModifier {
		t.Fatal("expected search-invalid-modifier obligation")
	}

	// Pairwise combinations are opt-in at strength 2. Declare both search codes
	// (including the universal _id) so a pair is available to combine.
	plan2, err := DerivePlan(r, coverage.DeriveOptions{
		Strength: 2,
		CapabilitySearchCodes: map[string][]string{
			"Organization": {"_id", "active"},
		},
	})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}
	var hasCombination bool
	for _, req := range plan2.Requirements {
		if req.Variant == coverage.CoverageVariantSearchCombination {
			hasCombination = true
			if req.SearchCode == "" || req.SearchCodeB == "" {
				t.Fatalf("combination requirement missing codes: %+v", req)
			}
		}
	}
	if !hasCombination {
		t.Fatal("expected search-combination obligation at strength 2")
	}
}

func TestDerivePlanAddsSearchObligations(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/org-profile",
		Type: "Organization",
		Elements: []model.ElementDefinition{
			{Path: "Organization", Min: 0, Max: "*"},
			{Path: "Organization.name", Min: 1, Max: "1"},
		},
	})
	r.AddSearchParameter(&model.SearchParameter{
		URL:  "http://hl7.org/fhir/SearchParameter/Resource-id",
		Name: "_id",
		Code: "_id",
		Base: []string{"Resource"},
		Type: "token",
	})
	r.AddSearchParameter(&model.SearchParameter{
		URL:  "http://hl7.org/fhir/SearchParameter/Organization-active",
		Name: "active",
		Code: "active",
		Base: []string{"Organization"},
		Type: "token",
	})

	plan, err := DerivePlan(r, coverage.DeriveOptions{})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}
	var search []coverage.CoverageRequirement
	for _, req := range plan.Requirements {
		if req.Domain == coverage.CoverageDomainSearch {
			search = append(search, req)
		}
	}
	if len(search) == 0 {
		t.Fatalf("expected search obligations, got none (total reqs: %d)", len(plan.Requirements))
	}

	codes := map[string]bool{}
	for _, req := range search {
		codes[req.SearchCode] = true
		if req.ResourceType != "Organization" {
			t.Fatalf("search obligation resource type = %q, want Organization", req.ResourceType)
		}
	}
	if codes["_id"] {
		t.Fatal("expected universal _id excluded when no capability search codes are provided")
	}
	if !codes["active"] {
		t.Fatal("expected type-specific active search obligation")
	}
}

func TestDerivePlanUniversalSearchCodesRequireCapabilityDeclaration(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/org-profile",
		Type: "Organization",
		Elements: []model.ElementDefinition{
			{Path: "Organization", Min: 0, Max: "*"},
			{Path: "Organization.name", Min: 1, Max: "1"},
		},
	})
	r.AddSearchParameter(&model.SearchParameter{
		URL:  "http://hl7.org/fhir/SearchParameter/Resource-id",
		Name: "_id",
		Code: "_id",
		Base: []string{"Resource"},
		Type: "token",
	})
	r.AddSearchParameter(&model.SearchParameter{
		URL:  "http://hl7.org/fhir/SearchParameter/Resource-content",
		Name: "_content",
		Code: "_content",
		Base: []string{"Resource"},
		Type: "string",
	})
	r.AddSearchParameter(&model.SearchParameter{
		URL:  "http://hl7.org/fhir/SearchParameter/Organization-active",
		Name: "active",
		Code: "active",
		Base: []string{"Organization"},
		Type: "token",
	})

	// With no capability scope, universal parameters are excluded for every type.
	plan, err := DerivePlan(r, coverage.DeriveOptions{})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}
	codes := map[string]bool{}
	for _, req := range plan.Requirements {
		if req.Domain == coverage.CoverageDomainSearch {
			codes[req.SearchCode] = true
		}
	}
	if codes["_id"] || codes["_content"] {
		t.Fatalf("expected universal codes excluded without capability scope, got %v", codes)
	}
	if !codes["active"] {
		t.Fatal("expected type-specific active search obligation without capability scope")
	}

	// When the server declares a universal code, it is included for the type.
	plan2, err := DerivePlan(r, coverage.DeriveOptions{
		CapabilitySearchCodes: map[string][]string{
			"Organization": {"_id", "active"},
		},
	})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}
	codes2 := map[string]bool{}
	for _, req := range plan2.Requirements {
		if req.Domain == coverage.CoverageDomainSearch {
			codes2[req.SearchCode] = true
		}
	}
	if !codes2["_id"] {
		t.Fatal("expected declared universal _id search obligation")
	}
	if codes2["_content"] {
		t.Fatal("expected undeclared universal _content excluded")
	}
	if !codes2["active"] {
		t.Fatal("expected declared type-specific active search obligation")
	}
}

func TestDerivePlanIncludeUniversalSearchParams(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/org-profile",
		Type: "Organization",
		Elements: []model.ElementDefinition{
			{Path: "Organization", Min: 0, Max: "*"},
			{Path: "Organization.name", Min: 1, Max: "1"},
		},
	})
	r.AddSearchParameter(&model.SearchParameter{
		URL:  "http://hl7.org/fhir/SearchParameter/Resource-id",
		Name: "_id",
		Code: "_id",
		Base: []string{"Resource"},
		Type: "token",
	})
	r.AddSearchParameter(&model.SearchParameter{
		URL:  "http://hl7.org/fhir/SearchParameter/Resource-count",
		Name: "_count",
		Code: "_count",
		Base: []string{"Resource"},
		Type: "number",
	})
	r.AddSearchParameter(&model.SearchParameter{
		URL:  "http://hl7.org/fhir/SearchParameter/Organization-active",
		Name: "active",
		Code: "active",
		Base: []string{"Organization"},
		Type: "token",
	})

	// With no capability scope and no opt-in, universal parameters are excluded.
	plan, err := DerivePlan(r, coverage.DeriveOptions{})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}
	codes := map[string]bool{}
	for _, req := range plan.Requirements {
		if req.Domain == coverage.CoverageDomainSearch {
			codes[req.SearchCode] = true
		}
	}
	if codes["_id"] || codes["_count"] {
		t.Fatalf("expected universal codes excluded without opt-in, got %v", codes)
	}
	if !codes["active"] {
		t.Fatal("expected type-specific active search obligation without opt-in")
	}

	// With IncludeUniversalSearchParams, universal parameters are included for
	// every in-scope type even when the server does not declare them.
	plan2, err := DerivePlan(r, coverage.DeriveOptions{IncludeUniversalSearchParams: true})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}
	codes2 := map[string]bool{}
	for _, req := range plan2.Requirements {
		if req.Domain == coverage.CoverageDomainSearch {
			codes2[req.SearchCode] = true
		}
	}
	if !codes2["_id"] {
		t.Fatal("expected universal _id included with IncludeUniversalSearchParams")
	}
	if !codes2["_count"] {
		t.Fatal("expected universal _count included with IncludeUniversalSearchParams")
	}
	if !codes2["active"] {
		t.Fatal("expected type-specific active search obligation with IncludeUniversalSearchParams")
	}
}
