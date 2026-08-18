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
// and do not replace the base element's definition.
func BuildElementTree(defs []ElementDefinition) (root *ElementNode, byPath map[string]*ElementNode) {
	byPath = make(map[string]*ElementNode)

	for i := range defs {
		def := &defs[i]
		if def.Path == "" {
			continue
		}
		segments := strings.Split(def.Path, ".")

		if root == nil {
			root = newNode(segments[0], segments[0])
			byPath[root.Path] = root
		}

		cur := root
		for i := 1; i < len(segments); i++ {
			child := cur.Children[segments[i]]
			if child == nil {
				path := strings.Join(segments[:i+1], ".")
				child = newNode(segments[i], path)
				cur.Children[segments[i]] = child
				byPath[path] = child
			}
			cur = child
		}

		if def.SliceName != "" {
			if cur != nil {
				slice := cur.Slices[def.SliceName]
				if slice == nil {
					slice = newSliceNode(def.SliceName)
					cur.Slices[def.SliceName] = slice
				}
				slice.Definition = def
			}
			continue
		}

		slicePath, sliceName, sliceTail, hasSliceContext := parseSliceContext(def.ID, def.Path)
		if hasSliceContext {
			if owner, ok := byPath[slicePath]; ok {
				slice := owner.Slices[sliceName]
				if slice == nil {
					slice = newSliceNode(sliceName)
					owner.Slices[sliceName] = slice
				}
				attachSliceChildDefinition(slice, sliceTail, def)
				continue
			}
		}

		cur.Definition = def
		byPath[cur.Path] = cur
	}

	return root, byPath
}

func newSliceNode(name string) *SliceNode {
	return &SliceNode{Name: name, Children: make(map[string]*ElementNode)}
}

func parseSliceContext(id, path string) (slicePath string, sliceName string, tail []string, ok bool) {
	if id == "" || path == "" || !strings.Contains(id, ":") {
		return "", "", nil, false
	}
	idSegments := strings.Split(id, ".")
	pathSegments := strings.Split(path, ".")
	for idx, segment := range idSegments {
		parts := strings.SplitN(segment, ":", 2)
		if len(parts) != 2 || parts[1] == "" {
			continue
		}
		if idx >= len(pathSegments) {
			return "", "", nil, false
		}
		slicePath = strings.Join(pathSegments[:idx+1], ".")
		sliceName = parts[1]
		tail = pathSegments[idx+1:]
		return slicePath, sliceName, tail, true
	}
	return "", "", nil, false
}

func attachSliceChildDefinition(slice *SliceNode, tail []string, def *ElementDefinition) {
	if slice == nil || def == nil {
		return
	}
	if len(tail) == 0 {
		return
	}
	children := slice.Children
	var cur *ElementNode
	for i, segment := range tail {
		child := children[segment]
		if child == nil {
			child = newNode(segment, def.Path)
			children[segment] = child
		}
		cur = child
		children = child.Children
		if i == len(tail)-1 {
			cur.Definition = def
		}
	}
}

func newNode(name, path string) *ElementNode {
	return &ElementNode{
		Name:     name,
		Path:     path,
		Children: make(map[string]*ElementNode),
		Slices:   make(map[string]*SliceNode),
	}
}
