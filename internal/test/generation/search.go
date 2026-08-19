package generation

import (
	"net/url"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
	"github.com/jlcoulter/momus/internal/test/ast"
	"github.com/jlcoulter/momus/internal/test/coverage"
)

// buildSearchCase builds a GET search request that exercises a search
// parameter against a resource type, with the appropriate accept/reject assert.
func buildSearchCase(req coverage.CoverageRequirement, options BuildOptions) ast.Node {
	requestURL := joinURL(options.BaseURL, req.ResourceType) + "?" + searchQuery(req, options)
	return &ast.Sequence{Steps: []ast.Node{
		&ast.Request{
			Method:  "GET",
			URL:     requestURL,
			Headers: map[string]string{"X-Momus-Requirement-ID": req.ID},
		},
		searchAssert(req),
	}}
}

// searchQuery builds the query string for a search obligation, handling pairwise
// combinations (two parameters) and invalid modifiers.
func searchQuery(req coverage.CoverageRequirement, options BuildOptions) string {
	if req.Variant == coverage.CoverageVariantSearchCombination && req.SearchCodeB != "" {
		return req.SearchCode + "=" + url.QueryEscape(searchQueryValue(req, options)) +
			"&" + req.SearchCodeB + "=" + url.QueryEscape(searchQueryValue(req, options))
	}
	code := req.SearchCode
	if req.Variant == coverage.CoverageVariantSearchInvalidModifier {
		code += ":zzz"
	}
	return code + "=" + url.QueryEscape(searchQueryValue(req, options))
}

// searchQueryValue returns the query value to exercise for a search obligation.
// For reject variants it uses the sentinel that cannot match; for accept
// variants it returns a value that can actually match (and that validates on
// the provisioned seed), e.g. a real code for a value-set-bound code element.
func searchQueryValue(req coverage.CoverageRequirement, options BuildOptions) string {
	switch req.Variant {
	case coverage.CoverageVariantSearchInvalidValue:
		return "not a valid value"
	case coverage.CoverageVariantSearchNoResults:
		return "momus-no-match-zzz"
	default:
		return searchAcceptValue(req, options)
	}
}

// searchAcceptValue returns a value for an accept search obligation that both
// makes the query meaningful and validates when placed on a provisioned seed.
// A `code` element bound to a value set uses a real code from that set; a
// boolean uses "true"; everything else falls back to the "momus-search" sentinel.
func searchAcceptValue(req coverage.CoverageRequirement, options BuildOptions) string {
	if options.Registry == nil || req.SearchCode == "" || req.SearchCode == "_id" {
		return "momus-search"
	}
	sp, ok := options.Registry.SearchParameter(req.ResourceType, req.SearchCode)
	if !ok {
		return "momus-search"
	}
	elementPath := searchElementPath(sp.Expression, req.ResourceType)
	if elementPath == "" {
		return "momus-search"
	}
	def, ok := searchElementDefinition(req.ResourceType, elementPath, options.Registry)
	if !ok || def == nil {
		return "momus-search"
	}
	switch primaryTypeCode(def) {
	case "code":
		if bound, ok := resolveBoundCoding(def, options.Registry); ok && bound.Code != "" {
			return bound.Code
		}
		return "momus-search"
	case "boolean":
		return "true"
	default:
		return "momus-search"
	}
}

// searchElementDefinition resolves the element a search expression points at,
// returning its ElementDefinition from the registry.
func searchElementDefinition(resourceType, elementPath string, reg *registry.Registry) (*model.ElementDefinition, bool) {
	if reg == nil {
		return nil, false
	}
	for _, profile := range reg.ProfilesForResource(resourceType) {
		resolved, err := reg.ResolveProfile(profile.URL)
		if err != nil || resolved == nil {
			continue
		}
		if node, ok := resolved.Elements[resourceType+"."+elementPath]; ok && node != nil && node.Definition != nil {
			return node.Definition, true
		}
	}
	return nil, false
}

// searchAssert builds the assertion for a search obligation. Multiple-results
// uses a body assertion on the returned Bundle total.
func searchAssert(req coverage.CoverageRequirement) *ast.Assert {
	if req.Variant == coverage.CoverageVariantSearchMultipleResults {
		return &ast.Assert{
			Description:   "server returns multiple search results",
			RequirementID: req.ID,
			Expression:    "body.total >= 2",
			Trace: &ast.Trace{
				ProfileURL:   req.ProfileURL,
				ResourceType: req.ResourceType,
				Domain:       string(req.Domain),
				Variant:      string(req.Variant),
				Expected:     "accept",
			},
		}
	}
	return buildRequirementAssert(req)
}
