package model

import "strings"

// ElementNode is a single node in the resolved element tree of a profile.
//
// The tree mirrors the canonical FHIR element paths: deeply nested elements
// are represented by nested Children. The tree is used for structural
// traversal and generation.
type ElementNode struct {
	Name       string
	Path       string
	Definition *ElementDefinition

	Children map[string]*ElementNode
	Slices   map[string]*SliceNode
}

// SliceNode represents a single slice of a sliced element.
type SliceNode struct {
	Name       string
	Definition *ElementDefinition
	Children   map[string]*ElementNode
}

// ResolvedProfile is a profile whose elements have been resolved into both
// a tree (for structural traversal) and a path index (for fast lookup by
// canonical FHIR path).
type ResolvedProfile struct {
	Canonical    string
	ResourceType string

	Root *ElementNode

	// Elements maps canonical FHIR paths to their node.
	Elements map[string]*ElementNode
}

// NewResolvedProfile builds a ResolvedProfile from element definitions.
func NewResolvedProfile(canonical, resourceType string, defs []ElementDefinition) *ResolvedProfile {
	root, byPath := BuildElementTree(defs)
	return &ResolvedProfile{
		Canonical:    canonical,
		ResourceType: resourceType,
		Root:         root,
		Elements:     byPath,
	}
}

// BuildElementTree resolves element definitions into a tree plus a path
// index. The root node is the element whose path is a bare type name (e.g.
// "Observation"); all other elements are nested beneath it according to
// their dot-separated canonical paths.
//
// Elements that carry a SliceName are attached to the sliced element node's Slices map
// and do not replace the base element's definition. Nested slices (a slice whose
// slice context appears in an element's ID, e.g. "Organization.extension:suppressed.
// extension:suppressedBy") are attached under the parent slice's children, resolved
// by walking the ID's slice segments against the path, independent of definition
// order.
func BuildElementTree(defs []ElementDefinition) (root *ElementNode, byPath map[string]*ElementNode) {
	byPath = make(map[string]*ElementNode)

	for i := range defs {
		def := &defs[i]
		if def.Path == "" {
			continue
		}
		pathSegments := strings.Split(def.Path, ".")
		if root == nil {
			root = newNode(pathSegments[0], pathSegments[0])
			byPath[root.Path] = root
		}
		attachElementDefinition(root, def, pathSegments, strings.Split(def.ID, "."), byPath)
	}

	return root, byPath
}

// attachElementDefinition places a single element definition into the tree.
// It walks the element's path segment by segment; whenever the corresponding ID
// segment carries a slice name ("name:slice") the walk descends into that slice,
// and the final segment's definition is set on either the slice (sliced) or the
// base child. Descending by ID slice context — rather than looking up a plain
// path in a map — makes resolution independent of definition order (task #28) and
// keeps intermediate nodes on their correct incremental paths (task #29).
func attachElementDefinition(root *ElementNode, def *ElementDefinition, pathSegments, idSegments []string, byPath map[string]*ElementNode) {
	// children is the container to descend into for the NEXT path segment.
	// It starts as root.Children; when a segment carries a slice it becomes that
	// slice's children.
	children := root.Children
	inSlice := false

	// Handle the root segment (index 0): the node already exists, so only a slice
	// context on the root (e.g. ID "Address:foo" on Path "Address") needs action.
	rootSlice := idSliceAt(idSegments, 0)
	if len(pathSegments) == 1 {
		if rootSlice != "" {
			slice := root.Slices[rootSlice]
			if slice == nil {
				slice = newSliceNode(rootSlice)
				root.Slices[rootSlice] = slice
			}
			slice.Definition = def
		} else {
			root.Definition = def
		}
		return
	}

	// A root slice with further path segments (e.g. ID "Address:foo.line" on
	// Path "Address.line"): descend into the root slice's children before the
	// loop processes the remaining segments.
	if rootSlice != "" {
		slice := root.Slices[rootSlice]
		if slice == nil {
			slice = newSliceNode(rootSlice)
			root.Slices[rootSlice] = slice
		}
		children = slice.Children
		inSlice = true
	}

	for i := 1; i < len(pathSegments); i++ {
		name := pathSegments[i]
		path := strings.Join(pathSegments[:i+1], ".")
		child := children[name]
		if child == nil {
			child = newNode(name, path)
			children[name] = child
			// Slice children are not indexed by plain path (the Elements map
			// intentionally drops them); only base path nodes are.
			if !inSlice {
				byPath[path] = child
			}
		}

		sliceName := idSliceAt(idSegments, i)
		last := i == len(pathSegments)-1
		if last {
			if sliceName == "" {
				sliceName = def.SliceName
			}
			if sliceName != "" {
				slice := child.Slices[sliceName]
				if slice == nil {
					slice = newSliceNode(sliceName)
					child.Slices[sliceName] = slice
				}
				slice.Definition = def
			} else {
				child.Definition = def
			}
			return
		}

		if sliceName != "" {
			slice := child.Slices[sliceName]
			if slice == nil {
				slice = newSliceNode(sliceName)
				child.Slices[sliceName] = slice
			}
			children = slice.Children
			inSlice = true
		} else {
			children = child.Children
		}
	}
}

func newSliceNode(name string) *SliceNode {
	return &SliceNode{Name: name, Children: make(map[string]*ElementNode)}
}

// idSliceAt returns the slice name carried by the idx-th ID segment, e.g.
// "extension:suppressed" -> "suppressed"; "" when the segment is not sliced.
func idSliceAt(idSegments []string, idx int) string {
	if idx >= len(idSegments) {
		return ""
	}
	parts := strings.SplitN(idSegments[idx], ":", 2)
	if len(parts) != 2 || parts[1] == "" {
		return ""
	}
	return parts[1]
}

// ElementSliceKey returns a merge key for an element that preserves ID-based
// slice context, mirroring how the element tree descends slices from the ID. An
// ID like "Organization.extension:suppressed.url" (whose slice name lives only in
// the ID, not in the element's SliceName field) yields
// "Organization.extension:suppressed.url", distinct from the base
// "Organization.extension.url". When the ID carries no slice segment the plain
// path is returned unchanged. This lets the registry merge slice members as
// distinct elements instead of letting them override their base element.
func ElementSliceKey(id, path string) string {
	if id == "" || path == "" {
		return path
	}
	idSegments := strings.Split(id, ".")
	pathSegments := strings.Split(path, ".")
	if len(idSegments) != len(pathSegments) {
		return path
	}
	var hasSlice bool
	out := make([]string, len(pathSegments))
	for i, p := range pathSegments {
		out[i] = p
		if s := idSliceAt(idSegments, i); s != "" {
			out[i] = p + ":" + s
			hasSlice = true
		}
	}
	if !hasSlice {
		return path
	}
	return strings.Join(out, ".")
}

func newNode(name, path string) *ElementNode {
	return &ElementNode{
		Name:     name,
		Path:     path,
		Children: make(map[string]*ElementNode),
		Slices:   make(map[string]*SliceNode),
	}
}
