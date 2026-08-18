package openapi

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/jlcoulter/momus/internal/test/ast"
)

// pathParamPattern matches a `{param}` placeholder in an operation path.
var pathParamPattern = regexp.MustCompile(`\{[^}]+\}`)

// successStatusExpression is the assertion expression for a successful 2xx
// response to an operation.
const successStatusExpression = "status in [200,201,202,203,204]"

// GeneratePlan builds an executable test plan (AST) from an OpenAPI document:
// one request+assert case per HTTP operation, targeting baseURL. Operations run
// in document order (Sequence) so create-then-read flows execute predictably.
func GeneratePlan(doc *Document, baseURL string) (*ast.Plan, error) {
	if doc == nil {
		return nil, fmt.Errorf("openapi document is required")
	}
	base := strings.TrimRight(baseURL, "/")
	steps := make([]ast.Node, 0, len(doc.Paths))
	for _, op := range doc.Paths {
		if op == nil {
			continue
		}
		steps = append(steps, operationCase(op, base))
	}
	return &ast.Plan{Version: "v1", Root: &ast.Sequence{Steps: steps}}, nil
}

// operationCase builds a single request+assert case for an operation.
func operationCase(op *Operation, base string) ast.Node {
	url := operationURL(op, base)
	body := operationRequestBody(op)
	return &ast.Sequence{Steps: []ast.Node{
		&ast.Request{
			Method:  op.Method,
			URL:     url,
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    body,
		},
		&ast.Assert{
			Description:   op.Method + " " + op.Path,
			RequirementID: operationID(op),
			Expression:    successStatusExpression,
		},
	}}
}

// operationURL builds the absolute request URL, substituting path parameters
// with sample values (declared ones by type, any remaining placeholders with a
// generic sample).
func operationURL(op *Operation, base string) string {
	path := op.Path
	for _, param := range op.Parameters {
		if param.In == "path" {
			path = strings.ReplaceAll(path, "{"+param.Name+"}", sampleScalar(param.Type))
		}
	}
	path = pathParamPattern.ReplaceAllString(path, sampleScalar(""))
	return base + path
}

// operationRequestBody returns a sample JSON body for write operations that
// declare a request body schema.
func operationRequestBody(op *Operation) any {
	switch op.Method {
	case http.MethodGet, http.MethodDelete, http.MethodHead, http.MethodOptions:
		return nil
	}
	if op.RequestBody == nil {
		return nil
	}
	if body := sampleFromSchema(op.RequestBody); body != nil {
		return body
	}
	return nil
}

// sampleFromSchema builds a sample JSON value from a schema.
func sampleFromSchema(s *Schema) any {
	if s == nil {
		return nil
	}
	if len(s.Properties) > 0 {
		obj := make(map[string]any, len(s.Properties))
		for name, prop := range s.Properties {
			obj[name] = sampleFromSchema(prop)
		}
		return obj
	}
	switch s.Type {
	case "object":
		return map[string]any{}
	case "string":
		return "sample"
	case "integer":
		return 1
	case "number":
		return 1.0
	case "boolean":
		return true
	case "array":
		return []any{}
	default:
		return nil
	}
}

// sampleScalar returns a sample value for a path parameter of the given type.
func sampleScalar(typ string) string {
	switch typ {
	case "integer", "number":
		return "1"
	case "boolean":
		return "true"
	default:
		return "sample"
	}
}

// operationID returns a stable requirement identifier for an operation.
func operationID(op *Operation) string {
	if strings.TrimSpace(op.OperationID) != "" {
		return op.OperationID
	}
	return op.Method + " " + op.Path
}
