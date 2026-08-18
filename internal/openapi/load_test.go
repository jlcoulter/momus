package openapi

import (
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/constraint"
)

const testDoc = `{
  "openapi": "3.0.3",
  "info": {"title": "Example API", "version": "1.0.0"},
  "paths": {
    "/patients/{id}": {
      "get": {
        "operationId": "getPatient",
        "parameters": [
          {"name": "id", "in": "path", "required": true, "schema": {"type": "string"}},
          {"name": "_summary", "in": "query", "schema": {"type": "string"}}
        ],
        "responses": {"200": {"description": "ok", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/Patient"}}}}}
      },
      "post": {
        "operationId": "createPatient",
        "requestBody": {"required": true, "content": {"application/json": {"schema": {"$ref": "#/components/schemas/Patient"}}}},
        "responses": {"201": {"description": "created"}}
      }
    }
  },
  "components": {
    "schemas": {
      "Patient": {
        "type": "object",
        "required": ["id", "name"],
        "properties": {
          "id": {"type": "string"},
          "name": {"type": "string"}
        }
      }
    }
  }
}`

func TestParseJSONBuildsOperations(t *testing.T) {
	doc, err := ParseJSON([]byte(testDoc))
	if err != nil {
		t.Fatalf("ParseJSON returned error: %v", err)
	}
	if doc.Title != "Example API" || doc.Version != "1.0.0" {
		t.Fatalf("doc title/version = %q/%q", doc.Title, doc.Version)
	}
	if len(doc.Paths) != 2 {
		t.Fatalf("got %d operations, want 2", len(doc.Paths))
	}

	get := doc.Paths[0]
	if get.Method != "GET" || get.Path != "/patients/{id}" {
		t.Fatalf("first operation = %s %s", get.Method, get.Path)
	}
	if len(get.Parameters) != 2 {
		t.Fatalf("get parameters = %d, want 2", len(get.Parameters))
	}
	if !get.Parameters[0].Required {
		t.Fatal("path parameter should be required")
	}
	if get.Parameters[0].Type != "string" {
		t.Fatalf("path parameter type = %q, want string", get.Parameters[0].Type)
	}
	// Response schema resolves the Patient component ref.
	resp := get.Responses["200"]
	if resp == nil || resp.Type != "object" || len(resp.Required) != 2 {
		t.Fatalf("response schema not resolved: %+v", resp)
	}

	post := doc.Paths[1]
	if post.Method != "POST" || post.RequestBody == nil {
		t.Fatalf("post operation = %s, requestBody=%v", post.Method, post.RequestBody)
	}
	if post.RequestBody.Type != "object" {
		t.Fatalf("request body schema type = %q, want object", post.RequestBody.Type)
	}
}

func TestParseJSONRejectsNonOpenAPI(t *testing.T) {
	if _, err := ParseJSON([]byte(`{"foo":"bar"}`)); err == nil {
		t.Fatal("expected error for non-OpenAPI document")
	}
}

func TestParseJSONRejectsInvalidJSON(t *testing.T) {
	if _, err := ParseJSON([]byte("not json")); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestDeriveConstraints(t *testing.T) {
	doc, err := ParseJSON([]byte(testDoc))
	if err != nil {
		t.Fatalf("ParseJSON returned error: %v", err)
	}
	cons := DeriveConstraints(doc)
	// 2 operations + 2 parameters (both on GET; POST has a request body, not
	// parameters).
	if len(cons) != 4 {
		t.Fatalf("got %d constraints, want 4", len(cons))
	}
	var ops, params int
	for _, c := range cons {
		switch c.Kind {
		case constraint.KindAPIOperation:
			ops++
			if c.APIMethod == "" || c.APIPath == "" {
				t.Fatalf("operation constraint missing method/path: %+v", c)
			}
		case constraint.KindAPIParameter:
			params++
			if c.ParameterName == "" || c.ParameterIn == "" {
				t.Fatalf("parameter constraint missing name/in: %+v", c)
			}
		}
	}
	if ops != 2 {
		t.Fatalf("got %d operation constraints, want 2", ops)
	}
	if params != 2 {
		t.Fatalf("got %d parameter constraints, want 2", params)
	}
}
