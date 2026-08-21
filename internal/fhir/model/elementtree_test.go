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

// TestBuildElementTreeSliceResolutionIsOrderIndependent (task #28) verifies that
// a slice child whose slice context lives only in its ID is attached under the
// correct slice even when the base element and slice declaration appear AFTER it
// in the definition list. The old implementation resolved slice children by
// looking up the base element's plain path in a map, which failed when the base
// had not been indexed yet and mis-attached the node (output depended on def
// ordering).
func TestBuildElementTreeSliceResolutionIsOrderIndependent(t *testing.T) {
	defs := []ElementDefinition{
		// Slice child before the slice declaration and before the base element.
		{ID: "Location.identifier:phone.value", Path: "Location.identifier.value", Min: 1, Max: "1"},
		{ID: "Location.identifier:phone", Path: "Location.identifier", SliceName: "phone", Min: 1, Max: "1"},
		{Path: "Location", Name: "Location"},
		{Path: "Location.identifier", Name: "identifier", Min: 2, Max: "*"},
	}

	root, _ := BuildElementTree(defs)
	identifier := root.Children["identifier"]
	if identifier == nil {
		t.Fatal("expected identifier child node")
	}
	phone := identifier.Slices["phone"]
	if phone == nil {
		t.Fatal("expected phone slice to be attached to identifier node")
	}
	if phone.Definition == nil || phone.Definition.Min != 1 {
		t.Fatalf("phone slice definition lost or wrong: %+v", phone.Definition)
	}
	value := phone.Children["value"]
	if value == nil || value.Definition == nil {
		t.Fatal("expected phone slice child value definition to be attached")
	}
	if value.Definition.Path != "Location.identifier.value" {
		t.Fatalf("got slice child path %q, want Location.identifier.value", value.Definition.Path)
	}
	// The base element must remain attached to the base node, not clobbered by
	// the slice declaration.
	if identifier.Definition == nil || identifier.Definition.Min != 2 {
		t.Fatalf("base identifier definition clobbered: %+v", identifier.Definition)
	}
}

// TestBuildElementTreeNestedSliceResolution (tasks #28/#29) verifies that nested
// slices (a slice of a slice's child, whose slice context appears in the ID) are
// attached under the parent slice's children, with leaf slice definitions set and
// intermediate nodes on correct paths. This is the shape used by the HCPD
// suppressed extension (Organization.extension:suppressed.extension:suppressedBy).
func TestBuildElementTreeNestedSliceResolution(t *testing.T) {
	defs := []ElementDefinition{
		{Path: "Organization", Name: "Organization"},
		{Path: "Organization.extension", Name: "extension", Min: 0, Max: "*"},
		{ID: "Organization.extension:suppressed", Path: "Organization.extension", SliceName: "suppressed", Min: 0, Max: "1"},
		{ID: "Organization.extension:suppressed.url", Path: "Organization.extension.url", Min: 1, Max: "1", Fixed: "http://example.org/suppressed"},
		{ID: "Organization.extension:suppressed.extension", Path: "Organization.extension.extension", Min: 1, Max: "*"},
		{ID: "Organization.extension:suppressed.extension:suppressedBy", Path: "Organization.extension.extension", SliceName: "suppressedBy", Min: 1, Max: "1"},
		{ID: "Organization.extension:suppressed.extension:suppressedBy.url", Path: "Organization.extension.extension.url", Min: 1, Max: "1", Fixed: "suppressedBy"},
		{ID: "Organization.extension:suppressed.extension:includeSelf", Path: "Organization.extension.extension", SliceName: "includeSelf", Min: 0, Max: "1"},
	}

	root, _ := BuildElementTree(defs)
	ext := root.Children["extension"]
	if ext == nil {
		t.Fatal("expected extension child node")
	}
	suppressed := ext.Slices["suppressed"]
	if suppressed == nil {
		t.Fatal("expected suppressed slice")
	}
	if suppressed.Children["url"] == nil || suppressed.Children["url"].Definition == nil {
		t.Fatal("suppressed slice url child definition missing")
	}
	if f, _ := suppressed.Children["url"].Definition.Fixed.(string); f != "http://example.org/suppressed" {
		t.Fatalf("suppressed url fixed = %v, want http://example.org/suppressed", suppressed.Children["url"].Definition.Fixed)
	}
	suppExt := suppressed.Children["extension"]
	if suppExt == nil {
		t.Fatal("expected suppressed slice extension child")
	}
	suppBy := suppExt.Slices["suppressedBy"]
	if suppBy == nil {
		t.Fatal("expected suppressedBy nested slice under suppressed.extension")
	}
	if suppBy.Definition == nil || suppBy.Definition.Min != 1 {
		t.Fatalf("suppressedBy nested slice definition missing/wrong: %+v", suppBy.Definition)
	}
	subURL := suppBy.Children["url"]
	if subURL == nil || subURL.Definition == nil {
		t.Fatal("suppressedBy url child definition missing")
	}
	if subURL.Definition.Path != "Organization.extension.extension.url" {
		t.Fatalf("suppressedBy url path = %q, want Organization.extension.extension.url", subURL.Definition.Path)
	}
	if _, ok := suppExt.Slices["includeSelf"]; !ok {
		t.Fatal("expected includeSelf nested slice under suppressed.extension")
	}
}

// TestBuildElementTreeLeafIDSliceKeepsDefinition (task #29) verifies that an
// element whose slice context is expressed only as a leaf in its ID (e.g.
// ID "address:foo" with Path "address") still gets its definition set on the
// slice, rather than being dropped by an empty-tail early return.
func TestBuildElementTreeLeafIDSliceKeepsDefinition(t *testing.T) {
	defs := []ElementDefinition{
		{Path: "Address", Name: "Address"},
		{ID: "Address:foo", Path: "Address", SliceName: "foo", Min: 1, Max: "1"},
		{ID: "Address:foo.line", Path: "Address.line", Min: 1, Max: "*"},
	}

	root, _ := BuildElementTree(defs)
	foo := root.Slices["foo"]
	if foo == nil {
		t.Fatal("expected foo slice on root")
	}
	if foo.Definition == nil || foo.Definition.Min != 1 {
		t.Fatalf("foo slice definition missing/wrong: %+v", foo.Definition)
	}
	if foo.Children["line"] == nil || foo.Children["line"].Definition == nil {
		t.Fatal("foo slice line child definition missing")
	}
}

// TestNewResolvedProfileNilRootGuard (task #32) verifies that a profile whose
// definitions all carry an empty Path (so BuildElementTree yields a nil root)
// is guarded: NewResolvedProfile returns nil rather than a profile with a nil
// Root that would panic downstream consumers dereferencing Root.Children.
func TestNewResolvedProfileNilRootGuard(t *testing.T) {
	profile := NewResolvedProfile(
		"http://example.org/StructureDefinition/Empty",
		"Empty",
		[]ElementDefinition{
			{Path: "", Name: "Empty"},
			{Path: "", Name: "child"},
		},
	)
	if profile != nil {
		t.Fatalf("expected nil profile for empty paths, got %+v", profile)
	}
}

// TestElementSliceKeyPreservesIDSliceContext (task #30) verifies that a merge key
// derived from an element's ID keeps ID-based slice children distinct from their
// base element, while plain paths and SliceName-only elements keep their expected
// keys.
func TestElementSliceKeyPreservesIDSliceContext(t *testing.T) {
	cases := []struct {
		id, path, want string
	}{
		{"Organization.extension:suppressed.url", "Organization.extension.url", "Organization.extension:suppressed.url"},
		{"Organization.extension.url", "Organization.extension.url", "Organization.extension.url"},
		{"Organization.extension:suppressed.extension:suppressedBy.url", "Organization.extension.extension.url", "Organization.extension:suppressed.extension:suppressedBy.url"},
		{"", "Organization.extension", "Organization.extension"},
	}
	for _, c := range cases {
		if got := ElementSliceKey(c.id, c.path); got != c.want {
			t.Errorf("ElementSliceKey(%q, %q) = %q, want %q", c.id, c.path, got, c.want)
		}
	}
}

// TestStampProfileStampsSlicesAndChildren verifies that NewResolvedProfile
// stamps the profile URL onto every node in the tree, including slice nodes and
// their children, so generation can locate profile-matched example data.
func TestStampProfileStampsSlicesAndChildren(t *testing.T) {
	canonical := "http://example.org/StructureDefinition/org"
	profile := NewResolvedProfile(canonical, "Organization", []ElementDefinition{
		{Path: "Organization", Name: "Organization"},
		{Path: "Organization.extension", Name: "extension", Min: 0, Max: "*"},
		{ID: "Organization.extension:suppressed", Path: "Organization.extension", SliceName: "suppressed", Min: 0, Max: "1"},
		{ID: "Organization.extension:suppressed.url", Path: "Organization.extension.url", Min: 1, Max: "1"},
	})
	if profile == nil || profile.Root == nil {
		t.Fatal("expected a resolved profile with a root")
	}
	if profile.Root.ProfileURL != canonical {
		t.Fatalf("root ProfileURL = %q, want %q", profile.Root.ProfileURL, canonical)
	}
	ext := profile.Root.Children["extension"]
	if ext == nil {
		t.Fatal("expected extension child node")
	}
	if ext.ProfileURL != canonical {
		t.Fatalf("extension child ProfileURL = %q, want %q", ext.ProfileURL, canonical)
	}
	suppressed := ext.Slices["suppressed"]
	if suppressed == nil {
		t.Fatal("expected suppressed slice")
	}
	if suppressed.ProfileURL != canonical {
		t.Fatalf("slice ProfileURL = %q, want %q", suppressed.ProfileURL, canonical)
	}
	urlNode := suppressed.Children["url"]
	if urlNode == nil {
		t.Fatal("expected suppressed slice url child")
	}
	if urlNode.ProfileURL != canonical {
		t.Fatalf("slice child ProfileURL = %q, want %q", urlNode.ProfileURL, canonical)
	}
}
