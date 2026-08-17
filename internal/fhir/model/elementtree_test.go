package model

import "testing"

func TestElementNodeRepresentsDeeplyNestedPaths(t *testing.T) {
	defs := []ElementDefinition{
		{Path: "Observation", Name: "Observation"},
		{Path: "Observation.component", Name: "component"},
		{Path: "Observation.component.code", Name: "code"},
		{Path: "Observation.component.code.coding", Name: "coding"},
		{Path: "Observation.component.code.coding.code", Name: "code"},
	}

	root, _ := BuildElementTree(defs)
	if root == nil {
		t.Fatal("expected a root node")
	}
	if root.Name != "Observation" {
		t.Fatalf("got root name %q, want Observation", root.Name)
	}

	component := root.Children["component"]
	if component == nil {
		t.Fatal("expected component child")
	}
	code := component.Children["code"]
	if code == nil {
		t.Fatal("expected code child")
	}
	coding := code.Children["coding"]
	if coding == nil {
		t.Fatal("expected coding child")
	}
	if coding.Children["code"] == nil {
		t.Fatal("expected nested code child")
	}
}

func TestResolvedProfilePathLookup(t *testing.T) {
	profile := NewResolvedProfile(
		"http://hl7.org/fhir/StructureDefinition/Observation",
		"Observation",
		[]ElementDefinition{
			{Path: "Observation", Name: "Observation"},
			{Path: "Observation.component", Name: "component"},
			{Path: "Observation.component.code", Name: "code"},
			{Path: "Observation.component.code.coding", Name: "coding"},
			{Path: "Observation.component.code.coding.code", Name: "code"},
		},
	)

	node, ok := profile.Elements["Observation.component.code.coding.code"]
	if !ok {
		t.Fatal("expected element to be found by path")
	}
	if node.Definition == nil {
		t.Fatal("expected node to carry a definition")
	}
}
