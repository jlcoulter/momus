package generation

import (
	"net/url"

	"github.com/jlcoulter/momus/internal/test/ast"
	"github.com/jlcoulter/momus/internal/test/coverage"
)

// buildSearchCase builds a GET search request that exercises a search
// parameter against a resource type, with the appropriate accept/reject assert.
func buildSearchCase(req coverage.CoverageRequirement, options BuildOptions) ast.Node {
	query := req.SearchCode + "=" + url.QueryEscape(searchQueryValue(req))
	requestURL := joinURL(options.BaseURL, req.ResourceType) + "?" + query
	return &ast.Sequence{Steps: []ast.Node{
		&ast.Request{
			Method:  "GET",
			URL:     requestURL,
			Headers: map[string]string{"X-Momus-Requirement-ID": req.ID},
		},
		buildRequirementAssert(req),
	}}
}

// searchQueryValue returns the query value to exercise for a search obligation.
func searchQueryValue(req coverage.CoverageRequirement) string {
	switch req.Variant {
	case coverage.CoverageVariantSearchInvalidValue:
		return "not a valid value"
	case coverage.CoverageVariantSearchNoResults:
		return "momus-no-match-zzz"
	default:
		return "momus-search"
	}
}
