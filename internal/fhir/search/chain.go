package search

import (
	"sort"
	"strings"

	"github.com/jlcoulter/momus/internal/fhir/registry"
)

// ChainParam identifies a single search parameter reachable via chaining from
// a starting resource type.
type ChainParam struct {
	// Path is the dot-joined chain, e.g. "organization.name" (one level) or
	// "organization.address.city" (two levels). The first segment is a
	// reference search parameter code on the start type; the final segment is
	// a search parameter code on the terminal target type.
	Path string
	// TargetType is the resource type the final search parameter applies to.
	TargetType string
	// TargetCode is the search parameter code on TargetType.
	TargetCode string
	// Depth is the number of chain segments after the start resource type
	// (1 for "organization.name", 2 for "organization.address.city").
	Depth int
}

// Chains returns every valid one-level chained search parameter reachable from
// a starting resource type. A chain is "<refParam>.<targetParam>", built from
// every reference-type search parameter on resourceType whose declared target
// type T has a search parameter of its own.
//
// Only reference-type parameters support chaining; a chain's first segment must
// be a reference parameter on the start type, and its final segment a search
// parameter on one of that reference parameter's Target types. Universal
// (_-prefixed) parameters are excluded from the final segment because they are
// handled separately. Chains are de-duplicated and returned sorted by path.
func Chains(r *registry.Registry, resourceType string) []ChainParam {
	return chains(r, resourceType, 1)
}

// ChainsWithDepth returns every chained search parameter reachable from a
// starting resource type up to maxDepth levels deep. Depth 1 yields the same
// result as Chains; depth >= 2 additionally follows reference parameters on
// intermediate types to build multi-level paths (e.g.
// "general-practitioner.organization.name").
func ChainsWithDepth(r *registry.Registry, resourceType string, maxDepth int) []ChainParam {
	return chains(r, resourceType, maxDepth)
}

// chains is the shared implementation of Chains and ChainsWithDepth. It
// recursively follows reference-type parameters on each intermediate type,
// terminating at a non-reference parameter (or maxDepth).
func chains(r *registry.Registry, resourceType string, maxDepth int) []ChainParam {
	if r == nil || maxDepth < 1 {
		return nil
	}
	seen := make(map[string]struct{})
	out := make([]ChainParam, 0)

	var build func(typeName string, prefix string, depth int, used int)
	build = func(typeName string, prefix string, depth int, used int) {
		for _, sp := range r.SearchParameters() {
			if !stringSliceContains(sp.Base, typeName) {
				continue
			}
			if normalizeType(sp.Type) != "reference" {
				continue
			}
			for _, targetType := range sp.Target {
				// Emit terminal chains through this reference parameter: every
				// non-universal search parameter on the target type.
				for _, targetSP := range r.SearchParameters() {
					if !stringSliceContains(targetSP.Base, targetType) {
						continue
					}
					if strings.HasPrefix(targetSP.Code, "_") {
						continue
					}
					seg := sp.Code + "." + targetSP.Code
					path := prefix + seg
					if _, ok := seen[path]; ok {
						continue
					}
					seen[path] = struct{}{}
					out = append(out, ChainParam{
						Path:       path,
						TargetType: targetType,
						TargetCode: targetSP.Code,
						Depth:      used + 1,
					})
				}
				// When deeper levels are requested, follow reference parameters
				// on the intermediate target type to build multi-level chains.
				if depth > 1 {
					nextPrefix := prefix + sp.Code + "."
					build(targetType, nextPrefix, depth-1, used+1)
				}
			}
		}
	}

	build(resourceType, "", maxDepth, 0)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func stringSliceContains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
