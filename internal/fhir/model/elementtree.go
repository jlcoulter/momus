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
// Elements that carry a SliceName are attached to their parent's Slices map
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
		var parent *ElementNode
		for i := 1; i < len(segments); i++ {
			child := cur.Children[segments[i]]
			if child == nil {
				path := strings.Join(segments[:i+1], ".")
				child = newNode(segments[i], path)
				cur.Children[segments[i]] = child
				byPath[path] = child
			}
			parent = cur
			cur = child
		}

		if def.SliceName != "" {
			if parent != nil {
				parent.Slices[def.SliceName] = &SliceNode{Name: def.SliceName, Definition: def}
			}
			continue
		}

		cur.Definition = def
		byPath[cur.Path] = cur
	}

	return root, byPath
}

func newNode(name, path string) *ElementNode {
	return &ElementNode{
		Name:     name,
		Path:     path,
		Children: make(map[string]*ElementNode),
		Slices:   make(map[string]*SliceNode),
	}
}
