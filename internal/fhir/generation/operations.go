package generation

import (
	"sort"
	"strings"

	"github.com/jlcoulter/momus/internal/core/ast"
	"github.com/jlcoulter/momus/internal/core/coverage"
	coregen "github.com/jlcoulter/momus/internal/core/generation"
)

// buildOperationCase builds a single operation or state-transition case: a
// request (read/update/patch/delete/history or a negative transition) with an
// assertion carrying the coverage trace. Instance operations create their own
// dedicated resource first so they never operate on (or destroy) the shared
// provisioned seed that other cases and payloads depend on.
func buildOperationCase(req coverage.CoverageRequirement, options coregen.BuildOptions) ast.Node {
	method, path, expression, expected := operationSpec(req, options)

	// Negative state transitions and server-level custom operations run a single
	// request against a well-known target and must not create an instance.
	if isStandaloneOperation(req) {
		url := coregen.JoinURL(coregen.BaseURLForMethod(options, method), req.ResourceType) + path
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
	id := coregen.RequirementResourceID(req)
	createBody := operationUpdateBody(req, options, id)
	createHeaders := map[string]string{"Content-Type": "application/fhir+json", "X-Momus-Requirement-ID": req.ID}
	createURL := coregen.JoinInstanceURL(coregen.BaseURLForMethod(options, "PUT"), req.ResourceType, id)
	opURL := coregen.JoinURL(coregen.BaseURLForMethod(options, method), req.ResourceType) + path

	var opBody any
	opContentType := "application/fhir+json"
	switch req.Variant {
	case coverage.CoverageVariantOperationUpdate:
		opBody = createBody
	case coverage.CoverageVariantOperationPatch:
		prop, value := patchProperty(req, options)
		opBody = []any{map[string]any{"op": "add", "path": "/", "value": map[string]any{prop: value}}}
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
func buildCRUDCase(req coverage.CoverageRequirement, options coregen.BuildOptions) ast.Node {
	resourceType := req.ResourceType
	id := crudResourceID(resourceType)
	body := operationUpdateBody(req, options, id)
	headers := map[string]string{"Content-Type": "application/fhir+json", "X-Momus-Requirement-ID": req.ID}
	crudURL := func(method string) string {
		return coregen.JoinURL(coregen.BaseURLForMethod(options, method), resourceType) + "/" + id
	}
	return &ast.Sequence{Steps: []ast.Node{
		&ast.Request{Method: "PUT", URL: crudURL("PUT"), Headers: headers, Body: body},
		operationAssert(req, "status in [200,201]", "accept"),
		&ast.Request{Method: "GET", URL: crudURL("GET"), Headers: headers},
		operationAssert(req, "status in [200]", "accept"),
		&ast.Request{Method: "PUT", URL: crudURL("PUT"), Headers: headers, Body: body},
		operationAssert(req, "status in [200,201]", "accept"),
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
	description := req.Description
	if description == "" {
		description = coverage.DescribeCoverageRequirement(req)
	}
	humanID := req.HumanID
	if humanID == "" {
		humanID = coverage.HumanID(req)
	}
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
			Description:  description,
			HumanID:      humanID,
			SearchCode:   req.SearchCode,
			SearchCodeB:  req.SearchCodeB,
		},
	}
}

// operationSpec returns the HTTP method, resource-relative path, assertion
// expression, and expected outcome for an operation or state variant.
func operationSpec(req coverage.CoverageRequirement, options coregen.BuildOptions) (method, path, expression, expected string) {
	_ = options
	target := coregen.RequirementResourceID(req)
	missing := "momus-missing"
	switch req.Variant {
	case coverage.CoverageVariantOperationRead:
		return "GET", "/" + target, "status in [200]", "accept"
	case coverage.CoverageVariantOperationUpdate:
		return "PUT", "/" + target, "status in [200,201]", "accept"
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
		// A DELETE on a nonexistent resource is not a strict error: conformant
		// servers may return an idempotent 200/204 instead of 404. Accept the
		// portable set so the test is not flaky across servers.
		return "DELETE", "/" + missing, "status in [200,204,404]", "accept"
	default:
		return "GET", "/" + target, "status in [200]", "accept"
	}
}

// operationUpdateBody synthesises a resource body for an update operation using
// the same profile-driven generation as positive test payloads. The id must
// match the resource id used in the request URL (servers reject a PUT whose
// body id disagrees with the URL id, e.g. HAPI-0420).
func operationUpdateBody(req coverage.CoverageRequirement, options coregen.BuildOptions, id string) map[string]any {
	profiles := coregen.OrderedProfilesForResource(req.ResourceType, req.ProfileURL, options.PreferredProfileURLsByResource)
	primaryProfile := coregen.FirstProfileURL(profiles)
	body, _ := options.Builder.BuildBody(req, id, profiles, primaryProfile, nil, true)
	return body
}

// patchProperty derives a top-level property (and a valid value for it) to patch
// from the resource's synthesized body, so a PATCH test does not hard-code a
// `status` property that resources without one would reject. It falls back to
// `status`/`active` only when no simple top-level property is available.
func patchProperty(req coverage.CoverageRequirement, options coregen.BuildOptions) (string, any) {
	id := coregen.RequirementResourceID(req)
	body := operationUpdateBody(req, options, id)
	keys := make([]string, 0, len(body))
	for k := range body {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if key == "resourceType" || key == "id" {
			continue
		}
		switch v := body[key].(type) {
		case string, bool, float64, int, int64:
			return key, v
		}
	}
	return "status", "active"
}

// crudResourceID returns the deterministic resource id used by a CRUD sequence.
func crudResourceID(resourceType string) string {
	return "momus-crud-" + coregen.SanitizeFHIRID(resourceType)
}
