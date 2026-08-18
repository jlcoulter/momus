package openapi

import (
	"testing"

	"github.com/jlcoulter/momus/internal/test/ast"
)

func TestGeneratePlanBuildsOperationCases(t *testing.T) {
	doc, err := ParseJSON([]byte(testDoc))
	if err != nil {
		t.Fatalf("ParseJSON returned error: %v", err)
	}
	plan, err := GeneratePlan(doc, "http://localhost:8080")
	if err != nil {
		t.Fatalf("GeneratePlan returned error: %v", err)
	}
	root, ok := plan.Root.(*ast.Sequence)
	if !ok || len(root.Steps) != 2 {
		t.Fatalf("expected root Sequence with 2 steps, got %T", plan.Root)
	}

	// GET /patients/{id} -> path param substituted, no body.
	get := root.Steps[0].(*ast.Sequence).Steps[0].(*ast.Request)
	if get.Method != "GET" || get.URL != "http://localhost:8080/patients/sample" {
		t.Fatalf("get request = %s %s", get.Method, get.URL)
	}
	if get.Body != nil {
		t.Fatalf("GET request should have no body, got %v", get.Body)
	}

	// POST /patients/{id} -> request body from the resolved Patient schema.
	post := root.Steps[1].(*ast.Sequence).Steps[0].(*ast.Request)
	if post.Method != "POST" || post.URL != "http://localhost:8080/patients/sample" {
		t.Fatalf("post request = %s %s", post.Method, post.URL)
	}
	body, ok := post.Body.(map[string]any)
	if !ok {
		t.Fatalf("post body = %T, want map", post.Body)
	}
	if body["id"] != "sample" || body["name"] != "sample" {
		t.Fatalf("post body = %v", body)
	}

	// Asserts carry the operation identity and a 2xx expression.
	postAssert := root.Steps[1].(*ast.Sequence).Steps[1].(*ast.Assert)
	if postAssert.RequirementID != "createPatient" || postAssert.Expression != successStatusExpression {
		t.Fatalf("post assert = %+v", postAssert)
	}
}
