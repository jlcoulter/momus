package model

import "testing"

func TestDataRequirementExpressesObservationConstraint(t *testing.T) {
	req := DataRequirement{
		ID:       "obs-1",
		Resource: ResourceRequirement{Type: "Observation"},
		Purpose:  PurposeExistence,
		Constraints: []Constraint{
			{Path: "Observation.component.code.coding.code", Operator: OpEquals, Value: "1234-5"},
		},
		Cardinality: AtLeastOne(),
	}

	if req.Resource.Type != "Observation" {
		t.Fatalf("got resource type %q, want Observation", req.Resource.Type)
	}
	if req.Purpose != PurposeExistence {
		t.Fatalf("got purpose %q, want existence", req.Purpose)
	}
	if len(req.Constraints) != 1 {
		t.Fatalf("got %d constraints, want 1", len(req.Constraints))
	}

	c := req.Constraints[0]
	if c.Path != "Observation.component.code.coding.code" {
		t.Fatalf("got path %q, want Observation.component.code.coding.code", c.Path)
	}
	if c.Operator != OpEquals {
		t.Fatalf("got operator %q, want equals", c.Operator)
	}
	if c.Value != "1234-5" {
		t.Fatalf("got value %v, want 1234-5", c.Value)
	}
	if req.Cardinality.Min != 1 || req.Cardinality.Max != -1 {
		t.Fatalf("got cardinality %+v, want at least one", req.Cardinality)
	}
}
