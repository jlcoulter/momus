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

func TestDerivePlanImplicitUniversalSearchParams(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/org-profile",
		Type: "Organization",
		Elements: []model.ElementDefinition{
			{Path: "Organization", Min: 0, Max: "*"},
			{Path: "Organization.name", Min: 1, Max: "1"},
		},
	})

	// Without opt-in, no implicit universal obligations are derived.
	plan, err := DerivePlan(r, coverage.DeriveOptions{})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}
	for _, req := range plan.Requirements {
		if req.SearchCode == "_include" || req.SearchCode == "_revinclude" || req.SearchCode == "_sort" {
			t.Fatalf("unexpected implicit universal obligation without opt-in: %+v", req)
		}
	}

	// With IncludeUniversalSearchParams, implicit universals are derived with
	// their dedicated variants.
	plan2, err := DerivePlan(r, coverage.DeriveOptions{IncludeUniversalSearchParams: true})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}
	byCodeVariant := map[string]map[coverage.CoverageVariant]bool{}
	for _, req := range plan2.Requirements {
		if req.Domain != coverage.CoverageDomainSearch {
			continue
		}
		if byCodeVariant[req.SearchCode] == nil {
			byCodeVariant[req.SearchCode] = map[coverage.CoverageVariant]bool{}
		}
		byCodeVariant[req.SearchCode][req.Variant] = true
	}
	if !byCodeVariant["_include"][coverage.CoverageVariantSearchInclude] {
		t.Fatal("expected _include search-include obligation")
	}
	if !byCodeVariant["_revinclude"][coverage.CoverageVariantSearchRevInclude] {
		t.Fatal("expected _revinclude search-revinclude obligation")
	}
	if !byCodeVariant["_has"][coverage.CoverageVariantSearchChaining] {
		t.Fatal("expected _has search-chaining obligation")
	}
	if !byCodeVariant["_sort"][coverage.CoverageVariantSearchValid] {
		t.Fatal("expected _sort search-valid obligation")
	}
	if !byCodeVariant["_sort"][coverage.CoverageVariantSearchNoResults] {
		t.Fatal("expected _sort search-no-results obligation")
	}
}

func TestDerivePlanIncludeSearchModifiers(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/pat-profile",
		Type: "Patient",
		Elements: []model.ElementDefinition{
			{Path: "Patient", Min: 0, Max: "*"},
			{Path: "Patient.name", Min: 1, Max: "1"},
		},
	})
	r.AddSearchParameter(&model.SearchParameter{
		URL:  "http://hl7.org/fhir/SearchParameter/Patient-name",
		Name: "name",
		Code: "name",
		Base: []string{"Patient"},
		Type: "string",
	})
	r.AddSearchParameter(&model.SearchParameter{
		URL:  "http://hl7.org/fhir/SearchParameter/Patient-active",
		Name: "active",
		Code: "active",
		Base: []string{"Patient"},
		Type: "token",
	})

	plan, err := DerivePlan(r, coverage.DeriveOptions{IncludeSearchModifiers: true})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}
	var foundExact, foundNot bool
	for _, req := range plan.Requirements {
		if req.Variant != coverage.CoverageVariantSearchValid || req.SearchModifier == "" {
			continue
		}
		switch req.SearchModifier {
		case "exact", "contains":
			if req.SearchCode == "name" {
				foundExact = true
			}
		case "not":
			if req.SearchCode == "active" {
				foundNot = true
			}
		}
	}
	// string params support :exact and :contains.
	if !foundExact {
		t.Fatal("expected a string search modifier (exact/contains) on name")
	}
	// token params support :not.
	if !foundNot {
		t.Fatal("expected a token search modifier (not) on active")
	}
}

func TestDerivePlanIncludeSearchChains(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/pat-profile",
		Type: "Patient",
		Elements: []model.ElementDefinition{
			{Path: "Patient", Min: 0, Max: "*"},
			{Path: "Patient.name", Min: 1, Max: "1"},
			{Path: "Patient.managingOrganization", Min: 0, Max: "1"},
		},
	})
	r.AddSearchParameter(&model.SearchParameter{
		URL:    "http://hl7.org/fhir/SearchParameter/Patient-organization",
		Name:   "organization",
		Code:   "organization",
		Base:   []string{"Patient"},
		Type:   "reference",
		Target: []string{"Organization"},
	})
	r.AddSearchParameter(&model.SearchParameter{
		URL:  "http://hl7.org/fhir/SearchParameter/Organization-name",
		Name: "name",
		Code: "name",
		Base: []string{"Organization"},
		Type: "string",
	})

	// Chains require IncludeSearchChains and strength >= 2.
	plan, err := DerivePlan(r, coverage.DeriveOptions{IncludeSearchChains: true, Strength: 2})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}
	var found bool
	for _, req := range plan.Requirements {
		if req.Variant == coverage.CoverageVariantSearchChaining && req.SearchCode == "organization.name" {
			found = true
			if req.SearchTargetType != "Organization" || req.SearchTargetCode != "name" {
				t.Fatalf("chaining requirement missing target info: %+v", req)
			}
		}
	}
	if !found {
		t.Fatal("expected search-chaining obligation organization.name")
	}
}

func TestDerivePlanIncludeSearchIncludesFromCapability(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/pat-profile",
		Type: "Patient",
		Elements: []model.ElementDefinition{
			{Path: "Patient", Min: 0, Max: "*"},
			{Path: "Patient.name", Min: 1, Max: "1"},
		},
	})
	r.AddCapabilityStatement(&model.CapabilityStatement{
		URL: "http://example.org/CapabilityStatement/server",
		Rest: []model.CapabilityStatementRest{{
			Mode: "server",
			Resource: []model.CapabilityStatementRestResource{{
				Type:             "Patient",
				SearchInclude:    []string{"Patient.organization", "Patient.general-practitioner"},
				SearchRevInclude: []string{"Observation.patient"},
			}},
		}},
	})
	r.AddSearchParameter(&model.SearchParameter{
		URL:    "http://hl7.org/fhir/SearchParameter/Patient-organization",
		Name:   "organization",
		Code:   "organization",
		Base:   []string{"Patient"},
		Type:   "reference",
		Target: []string{"Organization"},
	})
	r.AddSearchParameter(&model.SearchParameter{
		URL:    "http://hl7.org/fhir/SearchParameter/Patient-general-practitioner",
		Name:   "general-practitioner",
		Code:   "general-practitioner",
		Base:   []string{"Patient"},
		Type:   "reference",
		Target: []string{"Practitioner", "Organization"},
	})
	r.AddSearchParameter(&model.SearchParameter{
		URL:    "http://hl7.org/fhir/SearchParameter/Observation-patient",
		Name:   "patient",
		Code:   "patient",
		Base:   []string{"Observation"},
		Type:   "reference",
		Target: []string{"Patient"},
	})

	plan, err := DerivePlan(r, coverage.DeriveOptions{IncludeSearchIncludes: true})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}
	var includeOrg, includePract, revinclude bool
	for _, req := range plan.Requirements {
		if req.ResourceType != "Patient" {
			continue
		}
		switch req.Variant {
		case coverage.CoverageVariantSearchInclude:
			switch req.SearchTargetType {
			case "Organization":
				includeOrg = true
			case "Practitioner":
				includePract = true
			default:
				t.Fatalf("unexpected _include target type %q", req.SearchTargetType)
			}
		case coverage.CoverageVariantSearchRevInclude:
			revinclude = true
			if req.SearchTargetType != "Observation" {
				t.Fatalf("_revinclude target type = %q, want Observation", req.SearchTargetType)
			}
		}
	}
	if !includeOrg {
		t.Fatal("expected _include of Organization resources")
	}
	if !includePract {
		t.Fatal("expected _include of Practitioner resources")
	}
	if !revinclude {
		t.Fatal("expected search-revinclude obligation from CapabilityStatement")
	}
}
