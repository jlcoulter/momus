package ast

import "testing"

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
