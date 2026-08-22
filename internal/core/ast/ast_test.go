package ast

import (
	"net/http"
	"testing"
)

func TestIsWriteMethod(t *testing.T) {
	writes := []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}
	for _, m := range writes {
		if !IsWriteMethod(m) {
			t.Errorf("IsWriteMethod(%q) = false, want true", m)
		}
	}
	reads := []string{http.MethodGet, http.MethodHead, http.MethodOptions, ""}
	for _, m := range reads {
		if IsWriteMethod(m) {
			t.Errorf("IsWriteMethod(%q) = true, want false", m)
		}
	}
}

func TestASTRepresentsSequence(t *testing.T) {
	plan := &Sequence{Steps: []Node{
		&Request{Method: "GET", URL: "/Observation"},
		&Request{Method: "GET", URL: "/Patient"},
		&Assert{Description: "status ok"},
	}}

	if len(plan.Steps) != 3 {
		t.Fatalf("got %d steps, want 3", len(plan.Steps))
	}
}

func TestASTRepresentsParallel(t *testing.T) {
	plan := &Parallel{Steps: []Node{
		&Request{Method: "GET", URL: "/Patient/1"},
		&Request{Method: "GET", URL: "/Observation/1"},
	}}

	if len(plan.Steps) != 2 {
		t.Fatalf("got %d steps, want 2", len(plan.Steps))
	}
}

func TestEncodeNodeEncodesAssertTrace(t *testing.T) {
	node := &Assert{
		Description:   "accept",
		RequirementID: "req-1",
		Expression:    "status in [200,201]",
		Trace: &Trace{
			ConstraintID: "profile|Patient.name|cardinality",
			ProfileURL:   "http://example.org/StructureDefinition/patient",
			ResourceType: "Patient",
			ElementPath:  "Patient.name",
			Domain:       "cardinality",
			Variant:      "valid-min",
			Expected:     "accept",
		},
	}

	encoded, err := EncodeNode(node)
	if err != nil {
		t.Fatalf("EncodeNode returned error: %v", err)
	}
	req, ok := encoded["requirement"].(map[string]any)
	if !ok {
		t.Fatal("expected encoded requirement trace")
	}
	if req["constraintId"] != "profile|Patient.name|cardinality" {
		t.Fatalf("got constraintId %v", req["constraintId"])
	}
	if req["expected"] != "accept" {
		t.Fatalf("got expected %v", req["expected"])
	}
}

func TestEncodeNodeOmitsTraceWhenNil(t *testing.T) {
	encoded, err := EncodeNode(&Assert{Description: "no trace", RequirementID: "r", Expression: "status == 200"})
	if err != nil {
		t.Fatalf("EncodeNode returned error: %v", err)
	}
	if _, ok := encoded["requirement"]; ok {
		t.Fatal("did not expect requirement trace when Trace is nil")
	}
}

// TestNodeMethodsSatisfyInterface verifies each concrete node type implements
// the Node interface (its unexported node() marker method) so the AST can be
// traversed uniformly by the runner and encoder.
func TestNodeMethodsSatisfyInterface(t *testing.T) {
	nodes := []Node{
		&Sequence{},
		&Parallel{},
		&Request{},
		&Capture{},
		&Assert{},
	}
	for _, n := range nodes {
		// Calling node() directly exercises the marker method; a nil receiver
		// would panic if the method were not implemented on the concrete type.
		n.node()
	}
}
