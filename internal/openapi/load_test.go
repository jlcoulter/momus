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

func TestFirstContentSchemaPicksSortedKeyWithoutJSON(t *testing.T) {
	doc, err := ParseJSON([]byte(testDoc))
	if err != nil {
		t.Fatalf("ParseJSON returned error: %v", err)
	}
	content := map[string]rawMediaType{
		"application/xml":   {Schema: &rawSchema{Type: "string"}},
		"text/plain":        {Schema: &rawSchema{Type: "integer"}},
		"application/octet": {Schema: &rawSchema{Type: "boolean"}},
	}
	// No application/json, so the sorted-first key (application/octet) wins.
	s := firstContentSchema(content, doc)
	if s == nil || s.Type != "boolean" {
		t.Fatalf("firstContentSchema = %+v, want boolean (application/octet)", s)
	}
}

func TestSchemaTypeResolvesRefParameter(t *testing.T) {
	doc, err := ParseJSON([]byte(`{
  "openapi": "3.0.3",
  "info": {"title": "Example API", "version": "1.0.0"},
  "paths": {
    "/items/{id}": {
      "get": {
        "operationId": "getItem",
        "parameters": [
          {"name": "id", "in": "path", "required": true, "schema": {"$ref": "#/components/schemas/ItemId"}}
        ],
        "responses": {"200": {"description": "ok"}}
      }
    }
  },
  "components": {
    "schemas": {
      "ItemId": {"type": "string"}
    }
  }
}`))
	if err != nil {
		t.Fatalf("ParseJSON returned error: %v", err)
	}
	if len(doc.Paths) != 1 {
		t.Fatalf("got %d operations, want 1", len(doc.Paths))
	}
	params := doc.Paths[0].Parameters
	if len(params) != 1 {
		t.Fatalf("got %d parameters, want 1", len(params))
	}
	if params[0].Type != "string" {
		t.Fatalf("ref parameter type = %q, want string", params[0].Type)
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
