package openapi

import (
	"testing"

	"github.com/jlcoulter/momus/internal/core/constraint"
)

func TestDeriveConstraintsInline(t *testing.T) {
	doc := &Document{
		Title:   "Example",
		Version: "1.0",
		Paths: []*Operation{
			{
				Method: "GET",
				Path:   "/patients/{id}",
				Parameters: []Parameter{
					{Name: "id", In: "path", Required: true},
				},
			},
			{
				Method: "POST",
				Path:   "/patients",
				Parameters: []Parameter{
					{Name: "X-Trace", In: "header"},
				},
			},
		},
	}
	cs := DeriveConstraints(doc)
	// 2 operations + 2 parameters.
	if len(cs) != 4 {
		t.Fatalf("DeriveConstraints returned %d constraints, want 4", len(cs))
	}

	var opCount, paramCount int
	for _, c := range cs {
		if c.Kind == constraint.KindAPIOperation {
			opCount++
		}
		if c.Kind == constraint.KindAPIParameter {
			paramCount++
		}
	}
	if opCount != 2 || paramCount != 2 {
		t.Fatalf("op=%d param=%d, want op=2 param=2", opCount, paramCount)
	}

	// Verify a specific operation constraint's fields.
	for _, c := range cs {
		if c.Kind == constraint.KindAPIOperation && c.APIMethod == "GET" {
			if c.ID != "api|GET|/patients/{id}|operation" || c.APIMethod != "GET" || c.APIPath != "/patients/{id}" {
				t.Fatalf("GET operation constraint = %+v", c)
			}
		}
	}
}

func TestDeriveConstraintsNilDocument(t *testing.T) {
	if cs := DeriveConstraints(nil); cs != nil {
		t.Fatalf("DeriveConstraints(nil) = %v, want nil", cs)
	}
}

func TestDeriveConstraintsSkipsNilOperations(t *testing.T) {
	doc := &Document{Paths: []*Operation{nil, {Method: "GET", Path: "/a"}}}
	cs := DeriveConstraints(doc)
	if len(cs) != 1 {
		t.Fatalf("DeriveConstraints returned %d constraints, want 1", len(cs))
	}
	if cs[0].APIPath != "/a" {
		t.Fatalf("constraint path = %q, want /a", cs[0].APIPath)
	}
}

func TestDeriveConstraintsOperationWithNoParameters(t *testing.T) {
	doc := &Document{Paths: []*Operation{{Method: "DELETE", Path: "/items/{id}"}}}
	cs := DeriveConstraints(doc)
	if len(cs) != 1 {
		t.Fatalf("DeriveConstraints returned %d constraints, want 1", len(cs))
	}
	if cs[0].Kind != constraint.KindAPIOperation || cs[0].APIMethod != "DELETE" {
		t.Fatalf("constraint = %+v", cs[0])
	}
}
