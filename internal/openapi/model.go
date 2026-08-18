// Package openapi loads OpenAPI documents into Momus's architectural roles:
// operation contracts, parameter definitions, and request/response schemas,
// and derives constraint-model obligations from them (the "FHIR/API" duality).
package openapi

// Document is a normalised OpenAPI 3.x document.
type Document struct {
	Title   string
	Version string
	// Paths are the HTTP operations declared by the document, in
	// document order.
	Paths []*Operation
	// Schemas are the resolved component schemas, keyed by schema name.
	Schemas map[string]*Schema
}

// Operation is a single HTTP operation contract (one path + method).
type Operation struct {
	Method      string // GET, POST, PUT, PATCH, DELETE, ...
	Path        string // e.g. /patients/{id}
	OperationID string
	Summary     string
	Parameters  []Parameter
	RequestBody *Schema
	// Responses are the schemas declared for 2xx response statuses, keyed by
	// status code.
	Responses map[string]*Schema
}

// Parameter is a path/query/header/cookie parameter definition.
type Parameter struct {
	Name     string
	In       string // path, query, header, cookie
	Required bool
	Type     string
}

// Schema is a JSON-Schema subset used for request/response payloads.
type Schema struct {
	// Ref is the resolved component schema name when the schema is a $ref.
	Ref string
	// Type is the JSON type (object, array, string, number, integer, boolean).
	Type string
	// Properties are the object properties, keyed by name.
	Properties map[string]*Schema
	// Required lists the required property names for an object schema.
	Required []string
}
