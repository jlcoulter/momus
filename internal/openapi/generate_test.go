package openapi

import (
	"testing"

	"github.com/jlcoulter/momus/internal/test/ast"
)

func TestGeneratePlanRoutesWriteOperationsToWriteBaseURL(t *testing.T) {
	doc, err := ParseJSON([]byte(testDoc))
	if err != nil {
		t.Fatalf("ParseJSON returned error: %v", err)
	}
	plan, err := GeneratePlan(doc, "http://read.example", "http://write.example")
	if err != nil {
		t.Fatalf("GeneratePlan returned error: %v", err)
	}
	root, ok := plan.Root.(*ast.Sequence)
	if !ok || len(root.Steps) != 2 {
		t.Fatalf("expected root Sequence with 2 steps, got %T", plan.Root)
	}

	get := root.Steps[0].(*ast.Sequence).Steps[0].(*ast.Request)
	if get.URL != "http://read.example/patients/sample" {
		t.Fatalf("GET request = %q, want read base URL", get.URL)
	}
	post := root.Steps[1].(*ast.Sequence).Steps[0].(*ast.Request)
	if post.URL != "http://write.example/patients/sample" {
		t.Fatalf("POST request = %q, want write base URL", post.URL)
	}
}

func TestGeneratePlanRoutesDeleteToWriteBaseURL(t *testing.T) {
	doc, err := ParseJSON([]byte(`{
  "openapi": "3.0.3",
  "info": {"title": "Example API", "version": "1.0.0"},
  "paths": {
    "/items/{id}": {
      "get": {"operationId": "getItem", "responses": {"200": {"description": "ok"}}},
      "delete": {"operationId": "deleteItem", "responses": {"204": {"description": "deleted"}}}
    }
  }
}`))
	if err != nil {
		t.Fatalf("ParseJSON returned error: %v", err)
	}
	plan, err := GeneratePlan(doc, "http://read.example", "http://write.example")
	if err != nil {
		t.Fatalf("GeneratePlan returned error: %v", err)
	}
	root, ok := plan.Root.(*ast.Sequence)
	if !ok || len(root.Steps) != 2 {
		t.Fatalf("expected root Sequence with 2 steps, got %T", plan.Root)
	}

	// Methods are sorted alphabetically, so DELETE precedes GET.
	del := root.Steps[0].(*ast.Sequence).Steps[0].(*ast.Request)
	if del.Method != "DELETE" || del.URL != "http://write.example/items/sample" {
		t.Fatalf("DELETE request = %s %q, want write base URL", del.Method, del.URL)
	}
	get := root.Steps[1].(*ast.Sequence).Steps[0].(*ast.Request)
	if get.Method != "GET" || get.URL != "http://read.example/items/sample" {
		t.Fatalf("GET request = %s %q, want read base URL", get.Method, get.URL)
	}
}

func TestGeneratePlanBuildsOperationCases(t *testing.T) {
	doc, err := ParseJSON([]byte(testDoc))
	if err != nil {
		t.Fatalf("ParseJSON returned error: %v", err)
	}
	plan, err := GeneratePlan(doc, "http://localhost:8080", "")
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

func TestGeneratePlanAssertsCarryTrace(t *testing.T) {
	doc, err := ParseJSON([]byte(testDoc))
	if err != nil {
		t.Fatalf("ParseJSON returned error: %v", err)
	}
	plan, err := GeneratePlan(doc, "http://localhost:8080", "")
	if err != nil {
		t.Fatalf("GeneratePlan returned error: %v", err)
	}
	root, ok := plan.Root.(*ast.Sequence)
	if !ok || len(root.Steps) != 2 {
		t.Fatalf("expected root Sequence with 2 steps, got %T", plan.Root)
	}

	// GET /patients/{id} -> resource type from the first path segment.
	getAssert := root.Steps[0].(*ast.Sequence).Steps[1].(*ast.Assert)
	if getAssert.Trace == nil {
		t.Fatal("GET assert has no Trace")
	}
	if getAssert.Trace.Domain != "openapi" || getAssert.Trace.Variant != "GET" ||
		getAssert.Trace.Expected != "accept" || getAssert.Trace.ResourceType != "patients" {
		t.Fatalf("GET assert trace = %+v", getAssert.Trace)
	}

	// POST /patients/{id} -> variant reflects the method.
	postAssert := root.Steps[1].(*ast.Sequence).Steps[1].(*ast.Assert)
	if postAssert.Trace == nil {
		t.Fatal("POST assert has no Trace")
	}
	if postAssert.Trace.Variant != "POST" || postAssert.Trace.ResourceType != "patients" {
		t.Fatalf("POST assert trace = %+v", postAssert.Trace)
	}
}

func TestSampleFromSchema(t *testing.T) {
	if v := sampleFromSchema(nil); v != nil {
		t.Fatalf("sampleFromSchema(nil) = %v, want nil", v)
	}
	// Object with properties recurses.
	obj := sampleFromSchema(&Schema{Type: "object", Properties: map[string]*Schema{
		"id":   {Type: "string"},
		"name": {Type: "string"},
	}})
	m, ok := obj.(map[string]any)
	if !ok || m["id"] != "sample" || m["name"] != "sample" {
		t.Fatalf("sampleFromSchema(object) = %v", obj)
	}
	// Scalar types.
	if v := sampleFromSchema(&Schema{Type: "string"}); v != "sample" {
		t.Fatalf("string sample = %v", v)
	}
	if v := sampleFromSchema(&Schema{Type: "integer"}); v != 1 {
		t.Fatalf("integer sample = %v", v)
	}
	if v := sampleFromSchema(&Schema{Type: "number"}); v != 1.0 {
		t.Fatalf("number sample = %v", v)
	}
	if v := sampleFromSchema(&Schema{Type: "boolean"}); v != true {
		t.Fatalf("boolean sample = %v", v)
	}
	if v := sampleFromSchema(&Schema{Type: "array"}); v == nil {
		t.Fatal("array sample should be a non-nil empty slice")
	}
	// Unknown type -> nil.
	if v := sampleFromSchema(&Schema{Type: "file"}); v != nil {
		t.Fatalf("unknown type sample = %v, want nil", v)
	}
}

func TestSampleScalar(t *testing.T) {
	if v := sampleScalar("integer"); v != "1" {
		t.Fatalf("integer scalar = %q", v)
	}
	if v := sampleScalar("number"); v != "1" {
		t.Fatalf("number scalar = %q", v)
	}
	if v := sampleScalar("boolean"); v != "true" {
		t.Fatalf("boolean scalar = %q", v)
	}
	if v := sampleScalar("string"); v != "sample" {
		t.Fatalf("string scalar = %q", v)
	}
}

func TestOperationIDAndResourceTypeFromPath(t *testing.T) {
	if got := operationID(&Operation{OperationID: "getPatient", Method: "GET", Path: "/patients/{id}"}); got != "getPatient" {
		t.Fatalf("operationID with explicit id = %q", got)
	}
	if got := operationID(&Operation{Method: "GET", Path: "/patients/{id}"}); got != "GET /patients/{id}" {
		t.Fatalf("operationID fallback = %q", got)
	}
	if got := resourceTypeFromPath("/patients/{id}"); got != "patients" {
		t.Fatalf("resourceTypeFromPath = %q", got)
	}
	if got := resourceTypeFromPath(""); got != "" {
		t.Fatalf("resourceTypeFromPath(empty) = %q, want empty", got)
	}
}

func TestOperationRequestBody(t *testing.T) {
	// No request body -> nil.
	if v := operationRequestBody(&Operation{}); v != nil {
		t.Fatalf("operationRequestBody(no body) = %v, want nil", v)
	}
	// Request body with a schema -> sample.
	op := &Operation{RequestBody: &Schema{Type: "object", Properties: map[string]*Schema{
		"id": {Type: "string"},
	}}}
	body := operationRequestBody(op)
	m, ok := body.(map[string]any)
	if !ok || m["id"] != "sample" {
		t.Fatalf("operationRequestBody = %v", body)
	}
}
