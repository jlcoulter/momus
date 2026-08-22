package generation

import (
	"net/url"

	"github.com/jlcoulter/momus/internal/core/ast"
	"github.com/jlcoulter/momus/internal/core/coverage"
	coregen "github.com/jlcoulter/momus/internal/core/generation"
)

// buildSearchCase builds a GET search request that exercises a search
// parameter against a resource type, with the appropriate accept/reject assert.
func buildSearchCase(req coverage.CoverageRequirement, options coregen.BuildOptions) ast.Node {
	requestURL := coregen.JoinURL(options.BaseURL, req.ResourceType) + "?" + searchQuery(req, options)
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
func searchQuery(req coverage.CoverageRequirement, options coregen.BuildOptions) string {
	if req.Variant == coverage.CoverageVariantSearchCombination && req.SearchCodeB != "" {
		return req.SearchCode + "=" + url.QueryEscape(SearchQueryValue(req, req.SearchCode, options.Builder)) +
			"&" + req.SearchCodeB + "=" + url.QueryEscape(SearchQueryValue(req, req.SearchCodeB, options.Builder))
	}
	code := req.SearchCode
	if req.Variant == coverage.CoverageVariantSearchInvalidModifier {
		code += ":zzz"
	}
	return code + "=" + url.QueryEscape(SearchQueryValue(req, req.SearchCode, options.Builder))
}

// SearchQueryValue returns the query value to exercise for a search obligation.
// For reject variants it uses the sentinel that cannot match; for accept
// variants it returns a value that can actually match (and that validates on
// the provisioned seed), e.g. a real code for a value-set-bound code element.
func SearchQueryValue(req coverage.CoverageRequirement, code string, builder coregen.PayloadBuilder) string {
	switch req.Variant {
	case coverage.CoverageVariantSearchInvalidValue:
		value, _ := searchInvalidValue(req, code, builder)
		return value
	case coverage.CoverageVariantSearchNoResults:
		// For date/time, number, boolean and other grammar-constrained parameter
		// types a literal sentinel like "momus-no-match-zzz" is not lexically
		// valid, so a conformant server would reject it with a 4xx instead of
		// returning an empty 200. Use a value that is type-valid but cannot
		// match any provisioned resource.
		return searchValidNonMatchValue(searchParameterType(req, code, builder))
	default:
		return searchAcceptValue(req, code, builder)
	}
}

// searchParameterType resolves the search parameter type (number, date, string,
// token, reference, composite, quantity, uri, special) for a search code,
// lowercased, or "" when it cannot be resolved.
func searchParameterType(req coverage.CoverageRequirement, code string, builder coregen.PayloadBuilder) string {
	if builder == nil || code == "" {
		return ""
	}
	return builder.SearchParamType(req, code)
}

// searchInvalidValue returns a search value for the invalid-value obligation and
// whether a conformant server is expected to reject it. For search parameter
// types with a strict lexical grammar (number, date, dateTime, instant, boolean)
// a genuinely type-invalid value is produced, which a conformant server rejects
// with a 4xx. For types where any value is type-valid (string, token, uri,
// reference, quantity, composite, special, unknown) a conformant server accepts
// the query and returns an empty 200 rather than a 4xx, so it returns a
// type-valid, non-matching value and expectReject=false.
func searchInvalidValue(req coverage.CoverageRequirement, code string, builder coregen.PayloadBuilder) (string, bool) {
	paramType := searchParameterType(req, code, builder)
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

// searchAcceptValue returns a value for an accept search obligation that both
// makes the query meaningful and validates when placed on a provisioned seed.
func searchAcceptValue(req coverage.CoverageRequirement, code string, builder coregen.PayloadBuilder) string {
	if builder == nil || code == "" || code == "_id" {
		return "momus-search"
	}
	return builder.SearchAcceptValue(req, code)
}

// searchAssert builds the assertion for a search obligation. Multiple-results
// uses a body assertion on the returned Bundle total.
func searchAssert(req coverage.CoverageRequirement, options coregen.BuildOptions) *ast.Assert {
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
		if _, expectReject := searchInvalidValue(req, req.SearchCode, options.Builder); !expectReject {
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
