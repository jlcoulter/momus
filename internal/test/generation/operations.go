package generation

import (
	"strings"

	"github.com/jlcoulter/momus/internal/test/ast"
	"github.com/jlcoulter/momus/internal/test/coverage"
)

// buildOperationCase builds a single operation or state-transition case: a
// request (read/update/patch/delete/history or a negative transition) with an
// assertion carrying the coverage trace. Instance operations create their own
// dedicated resource first so they never operate on (or destroy) the shared
// provisioned seed that other cases and payloads depend on.
func buildOperationCase(req coverage.CoverageRequirement, options BuildOptions) ast.Node {
	method, path, expression, expected := operationSpec(req, options)

	// Negative state transitions and server-level custom operations run a single
	// request against a well-known target and must not create an instance.
	if isStandaloneOperation(req) {
		url := joinURL(baseURLForMethod(options, method), req.ResourceType) + path
		return &ast.Sequence{Steps: []ast.Node{
			&ast.Request{
				Method:  method,
				URL:     url,
				Headers: map[string]string{"Content-Type": "application/fhir+json", "X-Momus-Requirement-ID": req.ID},
			},
			operationAssert(req, expression, expected),
		}}
	}

	// Instance operations (read/update/patch/delete/history and the default)
	// run against a dedicated, per-requirement instance that this case creates
	// itself. This isolates each operation from the shared seed: a DELETE removes
	// only the dedicated instance and can never delete a seed resource that other
	// cases or payloads reference. The dedicated id is deterministic.
	id := requirementResourceID(req)
	createBody := operationUpdateBody(req, options, id)
	createHeaders := map[string]string{"Content-Type": "application/fhir+json", "X-Momus-Requirement-ID": req.ID}
	createURL := joinInstanceURL(baseURLForMethod(options, "PUT"), req.ResourceType, id)
	opURL := joinURL(baseURLForMethod(options, method), req.ResourceType) + path

	var opBody any
	opContentType := "application/fhir+json"
	switch req.Variant {
	case coverage.CoverageVariantOperationUpdate:
		opBody = createBody
	case coverage.CoverageVariantOperationPatch:
		opBody = []any{map[string]any{"op": "add", "path": "/", "value": map[string]any{"status": "active"}}}
		opContentType = "application/json-patch+json"
	}
	opHeaders := map[string]string{"Content-Type": opContentType, "X-Momus-Requirement-ID": req.ID}

	return &ast.Sequence{Steps: []ast.Node{
		&ast.Request{Method: "PUT", URL: createURL, Headers: createHeaders, Body: createBody},
		operationAssert(req, "status in [200,201]", "accept"),
		&ast.Request{Method: method, URL: opURL, Headers: opHeaders, Body: opBody},
		operationAssert(req, expression, expected),
	}}
}

// isStandaloneOperation reports whether the variant runs a single request with
// no instance: state transitions operate on a known-missing id and custom
// operations are server-level URLs.
func isStandaloneOperation(req coverage.CoverageRequirement) bool {
	switch req.Variant {
	case coverage.CoverageVariantStateReadNonexistent,
		coverage.CoverageVariantStateDeleteNonexistent,
		coverage.CoverageVariantOperationCustom:
		return true
	default:
		return false
	}
}

// buildCRUDCase builds a full create -> read -> update -> read -> delete ->
// read (404) state-transition sequence for a resource type.
func buildCRUDCase(req coverage.CoverageRequirement, options BuildOptions) ast.Node {
	resourceType := req.ResourceType
	id := crudResourceID(resourceType)
	body := operationUpdateBody(req, options, id)
	headers := map[string]string{"Content-Type": "application/fhir+json", "X-Momus-Requirement-ID": req.ID}
	crudURL := func(method string) string {
		return joinURL(baseURLForMethod(options, method), resourceType) + "/" + id
	}
	return &ast.Sequence{Steps: []ast.Node{
		&ast.Request{Method: "PUT", URL: crudURL("PUT"), Headers: headers, Body: body},
		operationAssert(req, "status in [200,201]", "accept"),
		&ast.Request{Method: "GET", URL: crudURL("GET"), Headers: headers},
		operationAssert(req, "status in [200]", "accept"),
		&ast.Request{Method: "PUT", URL: crudURL("PUT"), Headers: headers, Body: body},
		operationAssert(req, "status in [200]", "accept"),
		&ast.Request{Method: "GET", URL: crudURL("GET"), Headers: headers},
		operationAssert(req, "status in [200]", "accept"),
		&ast.Request{Method: "DELETE", URL: crudURL("DELETE"), Headers: headers},
		operationAssert(req, "status in [200,204]", "accept"),
		&ast.Request{Method: "GET", URL: crudURL("GET"), Headers: headers},
		operationAssert(req, "status in [404]", "reject"),
	}}
}

// operationAssert builds an assertion carrying the coverage trace.
func operationAssert(req coverage.CoverageRequirement, expression, expected string) *ast.Assert {
	return &ast.Assert{
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
	}
}

// operationSpec returns the HTTP method, resource-relative path, assertion
// expression, and expected outcome for an operation or state variant.
func operationSpec(req coverage.CoverageRequirement, options BuildOptions) (method, path, expression, expected string) {
	_ = options
	target := requirementResourceID(req)
	missing := "momus-missing"
	switch req.Variant {
	case coverage.CoverageVariantOperationRead:
		return "GET", "/" + target, "status in [200]", "accept"
	case coverage.CoverageVariantOperationUpdate:
		return "PUT", "/" + target, "status in [200]", "accept"
	case coverage.CoverageVariantOperationPatch:
		return "PATCH", "/" + target, "status in [200]", "accept"
	case coverage.CoverageVariantOperationDelete:
		return "DELETE", "/" + target, "status in [200,204]", "accept"
	case coverage.CoverageVariantOperationHistory:
		return "GET", "/" + target + "/_history", "status in [200]", "accept"
	case coverage.CoverageVariantOperationCustom:
		name := strings.TrimPrefix(req.OperationName, "$")
		return "GET", "/$" + name, "status in [200]", "accept"
	case coverage.CoverageVariantStateReadNonexistent:
		return "GET", "/" + missing, "status in [404]", "reject"
	case coverage.CoverageVariantStateDeleteNonexistent:
		return "DELETE", "/" + missing, "status in [404]", "reject"
	default:
		return "GET", "/" + target, "status in [200]", "accept"
	}
}

// operationUpdateBody synthesises a resource body for an update operation using
// the same profile-driven generation as positive test payloads. The id must
// match the resource id used in the request URL (servers reject a PUT whose
// body id disagrees with the URL id, e.g. HAPI-0420).
func operationUpdateBody(req coverage.CoverageRequirement, options BuildOptions, id string) map[string]any {
	profiles := orderedProfilesForResource(req.ResourceType, req.ProfileURL, options.PreferredProfileURLsByResource)
	primaryProfile := firstProfileURL(profiles)
	return buildBodyTemplate(req, id, profiles, primaryProfile, nil, options.Registry, true)
}

// crudResourceID returns the deterministic resource id used by a CRUD sequence.
func crudResourceID(resourceType string) string {
	return "momus-crud-" + sanitizeFHIRID(resourceType)
}
