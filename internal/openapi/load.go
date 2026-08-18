package openapi

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ParseJSON loads an OpenAPI 3.x document from JSON bytes and normalises it
// into operation contracts and schemas.
func ParseJSON(data []byte) (*Document, error) {
	var raw openAPIDocument
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse openapi document: %w", err)
	}
	return normalize(raw)
}

// openAPIDocument is the raw JSON shape we read from an OpenAPI 3.x document.
type openAPIDocument struct {
	OpenAPI string `json:"openapi"`
	Info    struct {
		Title   string `json:"title"`
		Version string `json:"version"`
	} `json:"info"`
	Paths      map[string]map[string]rawOperation `json:"paths"`
	Components struct {
		Schemas map[string]rawSchema `json:"schemas"`
	} `json:"components"`
}

type rawOperation struct {
	OperationID string                 `json:"operationId"`
	Summary     string                 `json:"summary"`
	Parameters  []rawParameter         `json:"parameters"`
	RequestBody *rawRequestBody        `json:"requestBody"`
	Responses   map[string]rawResponse `json:"responses"`
}

type rawParameter struct {
	Name     string     `json:"name"`
	In       string     `json:"in"`
	Required bool       `json:"required"`
	Schema   *rawSchema `json:"schema"`
}

type rawRequestBody struct {
	Required bool                    `json:"required"`
	Content  map[string]rawMediaType `json:"content"`
}

type rawResponse struct {
	Content map[string]rawMediaType `json:"content"`
}

type rawMediaType struct {
	Schema *rawSchema `json:"schema"`
}

type rawSchema struct {
	Ref        string               `json:"$ref"`
	Type       string               `json:"type"`
	Required   []string             `json:"required"`
	Properties map[string]rawSchema `json:"properties"`
}

// normalize builds the resolved Document from the raw JSON shape.
func normalize(raw openAPIDocument) (*Document, error) {
	if raw.OpenAPI == "" {
		return nil, fmt.Errorf("document is not an OpenAPI document (missing openapi field)")
	}
	doc := &Document{
		Title:   raw.Info.Title,
		Version: raw.Info.Version,
		Schemas: make(map[string]*Schema, len(raw.Components.Schemas)),
	}

	// Resolve component schemas first so $refs can be dereferenced.
	for name, s := range raw.Components.Schemas {
		doc.Schemas[name] = resolveSchema(s)
	}

	// Iterate paths in sorted order for deterministic output.
	for _, path := range sortedPaths(raw.Paths) {
		methods := raw.Paths[path]
		for _, method := range sortedMethods(methods) {
			op := methods[method]
			operation := &Operation{
				Method:      strings.ToUpper(method),
				Path:        path,
				OperationID: op.OperationID,
				Summary:     op.Summary,
				Responses:   make(map[string]*Schema),
			}
			for _, p := range op.Parameters {
				param := Parameter{
					Name:     p.Name,
					In:       p.In,
					Required: p.Required || p.In == "path",
					Type:     schemaType(p.Schema),
				}
				operation.Parameters = append(operation.Parameters, param)
			}
			if op.RequestBody != nil {
				operation.RequestBody = firstContentSchema(op.RequestBody.Content, doc)
			}
			for status, resp := range op.Responses {
				if strings.HasPrefix(status, "2") {
					operation.Responses[status] = firstContentSchema(resp.Content, doc)
				}
			}
			doc.Paths = append(doc.Paths, operation)
		}
	}
	return doc, nil
}

// resolveSchema normalises a raw schema, dereferencing component refs.
func resolveSchema(s rawSchema) *Schema {
	sc := &Schema{Ref: schemaRefName(s.Ref), Type: s.Type, Required: s.Required}
	if len(s.Properties) > 0 {
		sc.Properties = make(map[string]*Schema, len(s.Properties))
		for name, prop := range s.Properties {
			sc.Properties[name] = resolveSchema(prop)
		}
	}
	return sc
}

// firstContentSchema returns the schema for the first content media type,
// dereferencing a component ref against the document's schemas.
func firstContentSchema(content map[string]rawMediaType, doc *Document) *Schema {
	if len(content) == 0 {
		return nil
	}
	// Deterministic: prefer application/json, else the first key.
	var mt rawMediaType
	if jsonMT, ok := content["application/json"]; ok {
		mt = jsonMT
	} else {
		for _, m := range content {
			mt = m
			break
		}
	}
	if mt.Schema == nil {
		return nil
	}
	return deref(*mt.Schema, doc)
}

func deref(raw rawSchema, doc *Document) *Schema {
	s := resolveSchema(raw)
	if name := s.Ref; name != "" {
		if resolved, ok := doc.Schemas[name]; ok {
			return resolved
		}
	}
	return s
}

// schemaRefName extracts the component schema name from a $ref of the form
// "#/components/schemas/Name". Returns "" for non-component refs.
func schemaRefName(ref string) string {
	const prefix = "#/components/schemas/"
	if !strings.HasPrefix(ref, prefix) {
		return ""
	}
	return strings.TrimPrefix(ref, prefix)
}

// schemaType returns a parameter's type.
func schemaType(s *rawSchema) string {
	if s == nil {
		return ""
	}
	if s.Type != "" {
		return s.Type
	}
	return resolveSchema(*s).Type
}

func sortedPaths(paths map[string]map[string]rawOperation) []string {
	out := make([]string, 0, len(paths))
	for path := range paths {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func sortedMethods(methods map[string]rawOperation) []string {
	out := make([]string, 0, len(methods))
	for method := range methods {
		out = append(out, method)
	}
	sort.Strings(out)
	return out
}
