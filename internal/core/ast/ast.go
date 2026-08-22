// Package ast defines the core test-plan abstract syntax tree.
package ast

import "net/http"

// Node is a node in a test plan AST.
type Node interface {
	node()
}

// Plan is a serializable test plan artifact.
//
// Dataset, when non-nil, is the seed data the plan provisions ahead of
// execution. Embedding it in the plan makes the AST the single artifact that
// drives provisioning and execution: both stages work from the plan alone,
// without the source package. It is intentionally generic (opaque resource
// bodies) so the AST does not depend on any domain model.
type Plan struct {
	Version string   `json:"version"`
	Root    Node     `json:"root"`
	Dataset *Dataset `json:"dataset,omitempty"`
}

// Dataset is the seed data a test plan provisions ahead of execution. It is
// the generic, domain-free representation of generated resources; a domain
// adapter (e.g. FHIR) converts its typed dataset to and from this shape.
type Dataset struct {
	Resources     map[string]*ResourceInstance `json:"resources"`
	Relationships []Reference                  `json:"relationships,omitempty"`
}

// ResourceInstance is a single generated resource instance.
//
// LocalID is the Momus-assigned ID used within a dataset; ServerID is the ID
// assigned by the target server once the resource is provisioned.
type ResourceInstance struct {
	LocalID      string         `json:"localId"`
	ResourceType string         `json:"resourceType"`
	Profile      string         `json:"profile,omitempty"`
	Resource     map[string]any `json:"resource"`
	ServerID     string         `json:"serverId,omitempty"`
	Version      string         `json:"version,omitempty"`
}

// Reference is a relationship between two generated resource instances.
type Reference struct {
	SourceID string `json:"sourceId"`
	Path     string `json:"path"`
	TargetID string `json:"targetId"`
}

// Sequence runs its steps in order; later steps depend on earlier ones.
type Sequence struct {
	Steps []Node
}

// Parallel runs its steps concurrently; the steps are independent.
type Parallel struct {
	Steps []Node
}

// Request is an HTTP request to issue.
type Request struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    any
}

// Capture stores a value extracted from a response.
type Capture struct {
	Name string
	Path string
}

// Assert is a test assertion against a result.
//
// Trace, when non-nil, carries the coverage requirement this assertion is
// bound to. It lives in the ast package (rather than referencing the coverage
// model) to keep ast free of dependencies on the coverage package.
type Assert struct {
	Description   string
	RequirementID string
	Expression    string
	Trace         *Trace
}

// Trace is the coverage requirement a generated assertion is bound to. It
// provides end-to-end traceability from an executed test back to its source
// constraint.
type Trace struct {
	ConstraintID string
	ProfileURL   string
	ResourceType string
	ElementPath  string
	Domain       string
	Variant      string
	Expected     string // "accept" or "reject"
}

func (*Sequence) node() {}
func (*Parallel) node() {}
func (*Request) node()  {}
func (*Capture) node()  {}
func (*Assert) node()   {}

// IsWriteMethod reports whether the HTTP method creates or modifies a resource
// (as opposed to a read/search). Write methods are routed to the write base URL
// when a split read/write endpoint is configured.
func IsWriteMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}
