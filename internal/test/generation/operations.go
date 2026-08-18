package generation

import (
	"github.com/jlcoulter/momus/internal/test/ast"
	"github.com/jlcoulter/momus/internal/test/coverage"
)

// buildOperationCase builds a single operation or state-transition case: a
// request (read/update/delete/history or a negative transition) with an
// assertion carrying the coverage trace.
func buildOperationCase(req coverage.CoverageRequirement, options BuildOptions) ast.Node {
	method, path, expression, expected := operationSpec(req, options)
	var body any
	if req.Variant == coverage.CoverageVariantOperationUpdate {
		body = operationUpdateBody(req, options)
	}
	url := joinURL(options.BaseURL, req.ResourceType) + path
	return &ast.Sequence{Steps: []ast.Node{
		&ast.Request{
			Method:  method,
			URL:     url,
			Headers: map[string]string{"Content-Type": "application/fhir+json", "X-Momus-Requirement-ID": req.ID},
			Body:    body,
		},
		&ast.Assert{
			Description:   "exercise " + string(req.Variant),
			RequirementID: req.ID,
			Expression:    expression,
			Trace: &ast.Trace{
				ProfileURL:   req.ProfileURL,
				ResourceType: req.ResourceType,
				Domain:       string(req.Domain),
				Variant:      string(req.Variant),
				Expected:     expected,
			},
		},
	}}
}

// operationSpec returns the HTTP method, resource-relative path, assertion
// expression, and expected outcome for an operation or state variant.
func operationSpec(req coverage.CoverageRequirement, options BuildOptions) (method, path, expression, expected string) {
	_ = options
	target := setupResourceID(req.ResourceType)
	missing := "momus-missing"
	switch req.Variant {
	case coverage.CoverageVariantOperationRead:
		return "GET", "/" + target, "status in [200]", "accept"
	case coverage.CoverageVariantOperationUpdate:
		return "PUT", "/" + target, "status in [200]", "accept"
	case coverage.CoverageVariantOperationDelete:
		return "DELETE", "/" + target, "status in [200,204]", "accept"
	case coverage.CoverageVariantOperationHistory:
		return "GET", "/" + target + "/_history", "status in [200]", "accept"
	case coverage.CoverageVariantStateReadNonexistent:
		return "GET", "/" + missing, "status in [404]", "reject"
	case coverage.CoverageVariantStateDeleteNonexistent:
		return "DELETE", "/" + missing, "status in [404]", "reject"
	default:
		return "GET", "/" + target, "status in [200]", "accept"
	}
}

// operationUpdateBody synthesises a resource body for an update operation using
// the same profile-driven generation as positive test payloads.
func operationUpdateBody(req coverage.CoverageRequirement, options BuildOptions) map[string]any {
	profiles := orderedProfilesForResource(req.ResourceType, req.ProfileURL, options.PreferredProfileURLsByResource)
	primaryProfile := firstProfileURL(profiles)
	return buildBodyTemplate(req, setupResourceID(req.ResourceType), profiles, primaryProfile, nil, options.Registry, true)
}
