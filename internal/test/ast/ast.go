// Package ast defines the abstract syntax tree for test plans.
package ast

// Node is a node in a test plan AST.
type Node interface {
	node()
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
type Assert struct {
	Description string
	// Expression is intentionally minimal for now.
}

func (*Sequence) node() {}
func (*Parallel) node() {}
func (*Request) node()  {}
func (*Capture) node()  {}
func (*Assert) node()   {}
