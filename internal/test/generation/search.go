package generation

import (
	"net/url"
	"strings"

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
		searchAssert(req, options),
	}}
}

// searchQuery builds the query string for a search obligation, handling pairwise
// combinations (two parameters) and invalid modifiers.
func searchQuery(req coverage.CoverageRequirement, options BuildOptions) string {
	if req.Variant == coverage.CoverageVariantSearchCombination && req.SearchCodeB != "" {
		return req.SearchCode + "=" + url.QueryEscape(searchQueryValue(req, req.SearchCode, options)) +
			"&" + req.SearchCodeB + "=" + url.QueryEscape(searchQueryValue(req, req.SearchCodeB, options))
	}
	code := req.SearchCode
	if req.Variant == coverage.CoverageVariantSearchInvalidModifier {
		code += ":zzz"
	}
	return code + "=" + url.QueryEscape(searchQueryValue(req, req.SearchCode, options))
}

// searchQueryValue returns the query value to exercise for a search obligation.
// For reject variants it uses the sentinel that cannot match; for accept
// variants it returns a value that can actually match (and that validates on
// the provisioned seed), e.g. a real code for a value-set-bound code element.
func searchQueryValue(req coverage.CoverageRequirement, code string, options BuildOptions) string {
	switch req.Variant {
	case coverage.CoverageVariantSearchInvalidValue:
		value, _ := searchInvalidValue(req, code, options)
		return value
	case coverage.CoverageVariantSearchNoResults:
		return "momus-no-match-zzz"
	default:
		return searchAcceptValue(req, code, options)
	}
}

// searchParameterType resolves the FHIR search parameter type (number, date,
// string, token, reference, composite, quantity, uri, special) for a search
// code, lowercased, or "" when it cannot be resolved.
func searchParameterType(req coverage.CoverageRequirement, code string, options BuildOptions) string {
	if options.Registry == nil || code == "" {
		return ""
	}
	if sp, ok := options.Registry.SearchParameter(req.ResourceType, code); ok {
		return strings.ToLower(sp.Type)
	}
	return ""
}

// searchInvalidValue returns a search value for the invalid-value obligation and
// whether a conformant server is expected to reject it. For search parameter
// types with a strict lexical grammar (number, date, dateTime, instant, boolean)
// a genuinely type-invalid value is produced, which a conformant server rejects
// with a 4xx. For types where any value is type-valid (string, token, uri,
// reference, quantity, composite, special, unknown) a conformant server accepts
// the query and returns an empty 200 rather than a 4xx, so it returns a
// type-valid, non-matching value and expectReject=false.
func searchInvalidValue(req coverage.CoverageRequirement, code string, options BuildOptions) (string, bool) {
	paramType := searchParameterType(req, code, options)
	switch paramType {
	case "boolean":
		return "notabool", true
	case "number":
		// Multiple decimal points violate FHIR's number lexical form.
		return "12.3.4", true
	case "date", "dateTime", "instant":
		return "not-a-date", true
	default:
		return searchValidNonMatchValue(paramType), false
	}
}

// searchValidNonMatchValue returns a syntactically valid value, appropriate to
// the search parameter type, that cannot match any provisioned resource. Such a
// value is accepted by a conformant server (200 with no results).
func searchValidNonMatchValue(paramType string) string {
	switch paramType {
	case "token":
		return "momus|nomatch"
	case "reference":
		return "Patient/momus-nomatch"
	case "uri":
		return "http://example.org/momus-nomatch"
	case "quantity":
		return "999|http://example.org/sys|nomatch"
	case "date", "dateTime", "instant":
		return "1900-01-01"
	case "number":
		return "123456.789"
	case "boolean":
		return "true"
	default: // string, composite, special, and unknown
		return "momus-invalid-zzz"
	}
}

// searchAcceptValue returns a value for an accept search obligation that both
// makes the query meaningful and validates when placed on a provisioned seed.
// A `code` element bound to a value set uses a real code from that set; a
// boolean uses "true"; everything else falls back to the "momus-search" sentinel.
func searchAcceptValue(req coverage.CoverageRequirement, code string, options BuildOptions) string {
	if options.Registry == nil || code == "" || code == "_id" {
		return "momus-search"
	}
	sp, ok := options.Registry.SearchParameter(req.ResourceType, code)
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
	case "code", "Coding", "CodeableConcept":
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
func searchAssert(req coverage.CoverageRequirement, options BuildOptions) *ast.Assert {
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
	if req.Variant == coverage.CoverageVariantSearchInvalidValue {
		if _, expectReject := searchInvalidValue(req, req.SearchCode, options); !expectReject {
			// For search parameter types where any value is type-valid (string,
			// token, uri, reference, quantity, ...) a conformant server accepts the
			// query and returns an empty 200 rather than a 4xx. Assert that instead
			// of a rejection the server would not produce.
			return &ast.Assert{
				Description:   "server accepts a type-valid, non-matching search value",
				RequirementID: req.ID,
				Expression:    "status in [200]",
				Trace: &ast.Trace{
					ConstraintID: req.ConstraintID,
					ProfileURL:   req.ProfileURL,
					ResourceType: req.ResourceType,
					ElementPath:  req.ElementPath,
					Domain:       string(req.Domain),
					Variant:      string(req.Variant),
					Expected:     "accept",
				},
			}
		}
	}
	return buildRequirementAssert(req)
}
