package generation

import (
	"strings"
	"testing"

	"github.com/jlcoulter/momus/internal/core/ast"
	"github.com/jlcoulter/momus/internal/core/coverage"
)

func TestGenerateRoutesWriteAndReadRequestsToSeparateBaseURLs(t *testing.T) {
	reqs := []coverage.CoverageRequirement{
		{ID: "op-read", ResourceType: "Organization", Domain: coverage.CoverageDomainOperation, Variant: coverage.CoverageVariantOperationRead},
		{ID: "op-update", ResourceType: "Organization", Domain: coverage.CoverageDomainOperation, Variant: coverage.CoverageVariantOperationUpdate},
		{ID: "search-1", ResourceType: "Organization", Domain: coverage.CoverageDomainSearch, Variant: coverage.CoverageVariantSearchValid, SearchCode: "_id"},
	}
	plan, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{
		Requirements: reqs,
	}, BuildOptions{BaseURL: "http://read.example/fhir", WriteBaseURL: "http://write.example/fhir"})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}

	got := map[string]string{}
	var walk func(ast.Node)
	walk = func(node ast.Node) {
		switch n := node.(type) {
		case *ast.Sequence:
			for _, step := range n.Steps {
				walk(step)
			}
		case *ast.Parallel:
			for _, step := range n.Steps {
				walk(step)
			}
		case *ast.Request:
			if rid := n.Headers["X-Momus-Requirement-ID"]; rid != "" {
				got[rid] = n.Method + " " + n.URL
			}
		}
	}
	walk(plan.Root)

	if got["op-read"] != "GET http://read.example/fhir/Organization/"+requirementResourceID(reqs[0]) {
		t.Fatalf("read request = %q, want read base URL", got["op-read"])
	}
	if got["op-update"] != "PUT http://write.example/fhir/Organization/"+requirementResourceID(reqs[1]) {
		t.Fatalf("write request = %q, want write base URL", got["op-update"])
	}
	if got["search-1"] != "GET http://read.example/fhir/Organization?_id=momus-search" {
		t.Fatalf("search request = %q, want read base URL", got["search-1"])
	}
}

func TestGenerateOperationCasesEmitCorrectRequests(t *testing.T) {
	reqs := []coverage.CoverageRequirement{
		{ID: "op-1", ResourceType: "Organization", Domain: coverage.CoverageDomainOperation, Variant: coverage.CoverageVariantOperationRead},
		{ID: "op-2", ResourceType: "Organization", Domain: coverage.CoverageDomainOperation, Variant: coverage.CoverageVariantOperationDelete},
		{ID: "op-3", ResourceType: "Organization", Domain: coverage.CoverageDomainOperation, Variant: coverage.CoverageVariantOperationUpdate},
		{ID: "op-4", ResourceType: "Organization", Domain: coverage.CoverageDomainOperation, Variant: coverage.CoverageVariantOperationHistory},
		{ID: "st-1", ResourceType: "Organization", Domain: coverage.CoverageDomainState, Variant: coverage.CoverageVariantStateReadNonexistent},
		{ID: "st-2", ResourceType: "Organization", Domain: coverage.CoverageDomainState, Variant: coverage.CoverageVariantStateDeleteNonexistent},
	}
	plan, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{
		Requirements: reqs,
	}, BuildOptions{BaseURL: "http://localhost:8080/fhir"})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}

	got := map[string]string{} // requirement id -> "METHOD url"
	var walk func(ast.Node)
	walk = func(node ast.Node) {
		switch n := node.(type) {
		case *ast.Sequence:
			for _, step := range n.Steps {
				walk(step)
			}
		case *ast.Parallel:
			for _, step := range n.Steps {
				walk(step)
			}
		case *ast.Request:
			if rid := n.Headers["X-Momus-Requirement-ID"]; rid != "" {
				got[rid] = n.Method + " " + n.URL
			}
		}
	}
	walk(plan.Root)

	base := "http://localhost:8080/fhir/Organization"
	want := map[string]string{
		"op-1": "GET " + base + "/" + requirementResourceID(reqs[0]),
		"op-2": "DELETE " + base + "/" + requirementResourceID(reqs[1]),
		"op-3": "PUT " + base + "/" + requirementResourceID(reqs[2]),
		"op-4": "GET " + base + "/" + requirementResourceID(reqs[3]) + "/_history",
		"st-1": "GET " + base + "/momus-missing",
		"st-2": "DELETE " + base + "/momus-missing",
	}
	for id, w := range want {
		if got[id] != w {
			t.Fatalf("request for %s = %q, want %q", id, got[id], w)
		}
	}
}

func TestBuildCRUDCaseEmitsSixStepSequence(t *testing.T) {
	plan, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{
		Requirements: []coverage.CoverageRequirement{
			{ID: "crud-1", ResourceType: "Organization", Domain: coverage.CoverageDomainState, Variant: coverage.CoverageVariantStateCRUDSequence},
		},
	}, BuildOptions{BaseURL: "http://localhost:8080/fhir"})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}

	var methods []string
	var walk func(ast.Node)
	walk = func(node ast.Node) {
		switch n := node.(type) {
		case *ast.Sequence:
			for _, step := range n.Steps {
				walk(step)
			}
		case *ast.Parallel:
			for _, step := range n.Steps {
				walk(step)
			}
		case *ast.Request:
			if n.Headers["X-Momus-Requirement-ID"] == "crud-1" {
				methods = append(methods, n.Method)
			}
		}
	}
	walk(plan.Root)

	want := []string{"PUT", "GET", "PUT", "GET", "DELETE", "GET"}
	if len(methods) != len(want) {
		t.Fatalf("got %d CRUD requests, want %d", len(methods), len(want))
	}
	for i := range want {
		if methods[i] != want[i] {
			t.Fatalf("CRUD step %d method = %q, want %q (full: %v)", i, methods[i], want[i], methods)
		}
	}
}

func TestBuildCustomOperationCase(t *testing.T) {
	plan, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{
		Requirements: []coverage.CoverageRequirement{
			{ID: "op-custom", ResourceType: "Organization", Domain: coverage.CoverageDomainOperation, Variant: coverage.CoverageVariantOperationCustom, OperationName: "everything"},
		},
	}, BuildOptions{BaseURL: "http://localhost:8080/fhir"})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}

	got := map[string]string{}
	var walk func(ast.Node)
	walk = func(node ast.Node) {
		switch n := node.(type) {
		case *ast.Sequence:
			for _, step := range n.Steps {
				walk(step)
			}
		case *ast.Parallel:
			for _, step := range n.Steps {
				walk(step)
			}
		case *ast.Request:
			if rid := n.Headers["X-Momus-Requirement-ID"]; rid != "" {
				got[rid] = n.Method + " " + n.URL
			}
		}
	}
	walk(plan.Root)

	if got["op-custom"] != "GET http://localhost:8080/fhir/Organization/$everything" {
		t.Fatalf("custom operation case = %q", got["op-custom"])
	}
}

// TestWriteRequestBodyIDMatchesURLID ensures the resource id embedded in a write
// request body matches the id in the request URL. Conformant FHIR servers reject
// a PUT whose body id disagrees with the URL id (e.g. HAPI-0420), so a mismatch
// would make every update/CRUD case fail against a real server.
func TestWriteRequestBodyIDMatchesURLID(t *testing.T) {
	plan, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{
		Requirements: []coverage.CoverageRequirement{
			{ID: "crud-1", ResourceType: "Organization", Domain: coverage.CoverageDomainState, Variant: coverage.CoverageVariantStateCRUDSequence},
			{ID: "op-update", ResourceType: "Patient", Domain: coverage.CoverageDomainOperation, Variant: coverage.CoverageVariantOperationUpdate},
		},
	}, BuildOptions{BaseURL: "http://localhost:8080/fhir"})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}

	var walk func(ast.Node)
	walk = func(node ast.Node) {
		switch n := node.(type) {
		case *ast.Sequence:
			for _, step := range n.Steps {
				walk(step)
			}
		case *ast.Parallel:
			for _, step := range n.Steps {
				walk(step)
			}
		case *ast.Request:
			if !ast.IsWriteMethod(n.Method) || n.Body == nil {
				return
			}
			body, ok := n.Body.(map[string]any)
			if !ok {
				t.Fatalf("write request %s %s has a non-object body %T", n.Method, n.URL, n.Body)
			}
			bodyID, _ := body["id"].(string)
			if bodyID == "" {
				t.Fatalf("write request %s %s body has no id", n.Method, n.URL)
			}
			wantSuffix := "/" + bodyID
			if !strings.HasSuffix(n.URL, wantSuffix) {
				t.Fatalf("write request %s %s body id %q does not match the URL resource id", n.Method, n.URL, bodyID)
			}
		}
	}
	walk(plan.Root)
}

// TestOperationDeleteTargetsDedicatedInstanceNotSeed verifies that an operation
// DELETE runs against a dedicated, per-requirement instance rather than the
// shared provisioned seed resource. Deleting the seed would break every other
// case (and payload) that references it, and the outcome would depend on the
// arbitrary ordering of requirement ids.
func TestOperationDeleteTargetsDedicatedInstanceNotSeed(t *testing.T) {
	req := coverage.CoverageRequirement{ID: "op-del", ResourceType: "Organization", Domain: coverage.CoverageDomainOperation, Variant: coverage.CoverageVariantOperationDelete}
	plan, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{
		Requirements: []coverage.CoverageRequirement{req},
	}, BuildOptions{BaseURL: "http://localhost:8080/fhir"})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}

	dedicated := "Organization/" + requirementResourceID(req)
	var sawCreate, sawDelete bool
	var walk func(ast.Node)
	walk = func(node ast.Node) {
		switch n := node.(type) {
		case *ast.Sequence:
			for _, step := range n.Steps {
				walk(step)
			}
		case *ast.Parallel:
			for _, step := range n.Steps {
				walk(step)
			}
		case *ast.Request:
			if strings.Contains(n.URL, "momus-setup-organization") {
				t.Fatalf("operation case must not target the shared seed, got %s %s", n.Method, n.URL)
			}
			switch n.Method {
			case "PUT":
				if strings.HasSuffix(n.URL, dedicated) {
					sawCreate = true
				}
			case "DELETE":
				if strings.HasSuffix(n.URL, dedicated) {
					sawDelete = true
				}
			}
		}
	}
	walk(plan.Root)

	if !sawCreate {
		t.Fatal("expected the operation case to create its dedicated instance via PUT")
	}
	if !sawDelete {
		t.Fatalf("expected DELETE to target the dedicated instance %s", dedicated)
	}
}
