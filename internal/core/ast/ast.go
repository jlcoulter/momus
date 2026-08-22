// Package ast defines the core test-plan abstract syntax tree.
package ast

import "net/http"

// Node is a node in a test plan AST.
type Node interface {
	node()
}

// Plan is a serializable test plan artifact.
type Plan struct {
	Version string `json:"version"`
	Root    Node   `json:"root"`
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
