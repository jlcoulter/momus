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

func TestBuildElementTreeAttachesSlicesToSlicedNode(t *testing.T) {
	defs := []ElementDefinition{
		{Path: "Location", Name: "Location"},
		{Path: "Location.identifier", Name: "identifier", Min: 2, Max: "*"},
		{Path: "Location.identifier", Name: "identifier", SliceName: "source", Min: 1, Max: "1"},
	}

	root, _ := BuildElementTree(defs)
	if root == nil {
		t.Fatal("expected root node")
	}
	identifier := root.Children["identifier"]
	if identifier == nil {
		t.Fatal("expected identifier child node")
	}
	if _, ok := identifier.Slices["source"]; !ok {
		t.Fatal("expected slice to be attached to identifier node")
	}
	if _, ok := root.Slices["source"]; ok {
		t.Fatal("did not expect slice to be attached to root node")
	}
}

func TestBuildElementTreeAttachesSliceChildDefinitions(t *testing.T) {
	defs := []ElementDefinition{
		{Path: "Organization", Name: "Organization"},
		{Path: "Organization.address", Name: "address", Min: 1, Max: "1"},
		{Path: "Organization.address", Name: "address", SliceName: "physical", Min: 1, Max: "1"},
		{ID: "Organization.address:physical.type", Path: "Organization.address.type", Name: "type", Min: 1, Max: "1"},
	}

	root, _ := BuildElementTree(defs)
	address := root.Children["address"]
	if address == nil {
		t.Fatal("expected address child node")
	}
	physical := address.Slices["physical"]
	if physical == nil {
		t.Fatal("expected physical slice")
	}
	typeNode := physical.Children["type"]
	if typeNode == nil || typeNode.Definition == nil {
		t.Fatal("expected slice child definition to be attached under slice")
	}
	if typeNode.Definition.Path != "Organization.address.type" {
		t.Fatalf("got slice child path %q, want Organization.address.type", typeNode.Definition.Path)
	}
}
