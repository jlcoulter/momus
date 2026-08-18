package coverage

import (
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

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

	plan, err := DerivePlan(r, DeriveOptions{})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}
	var search []CoverageRequirement
	for _, req := range plan.Requirements {
		if req.Domain == CoverageDomainSearch {
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
	if !codes["_id"] {
		t.Fatal("expected universal _id search obligation")
	}
	if !codes["active"] {
		t.Fatal("expected type-specific active search obligation")
	}
}
