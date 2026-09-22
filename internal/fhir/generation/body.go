package generation

import (
	"hash/fnv"
	"strings"

	fhirgen "github.com/jlcoulter/fhir-generator"
	"github.com/jlcoulter/momus/internal/fhir/registry"

	fhir "github.com/jlcoulter/fhir-registry"
)

// registryBindingResolver adapts the momus registry's three-tier bound-coding
// resolution (ValueSet expansion -> compose/CodeSystem -> package examples) to
// the fhirgen.BindingResolver interface, so fhir-generator produces the same
// conformant codings momus's own generator did.
type registryBindingResolver struct {
	reg *registry.Registry
}

// ResolveBinding resolves a bound coding for a coded element. It first tries
// the element's own binding via the registry, then falls back to the package's
// example instances at the element's path.
func (r *registryBindingResolver) ResolveBinding(elem *fhir.ElementDefinition, tree *fhir.ElementTree) (fhirgen.ResolvedCoding, bool) {
	if r.reg == nil || elem == nil {
		return fhirgen.ResolvedCoding{}, false
	}
	if coding, ok := resolveBoundCoding(elem, r.reg); ok {
		return fhirgen.ResolvedCoding{System: coding.System, Code: coding.Code, Display: coding.Display}, true
	}
	// A CodeableConcept's binding often lives on its "coding" child rather than
	// on the concept itself (common for nested extension value[x].coding). Defer
	// to that child's binding before falling back to examples.
	if child, ok := elementCodingChild(elem); ok {
		if coding, ok := resolveBoundCoding(child, r.reg); ok {
			return fhirgen.ResolvedCoding{System: coding.System, Code: coding.Code, Display: coding.Display}, true
		}
	}
	// Example-based fallback: resolve a real coding at the element's path from
	// the package's example instances. The element path (e.g. "Patient.gender")
	// and the tree's root resource type drive the lookup.
	if coding, ok := resolveBoundCodingFromExamplePath(elem, tree, r.reg); ok {
		return fhirgen.ResolvedCoding{System: coding.System, Code: coding.Code, Display: coding.Display}, true
	}
	return fhirgen.ResolvedCoding{}, false
}

// elementCodingChild returns the "coding" child element of a CodeableConcept
// element, when present. Coded concepts bind on the coding child.
func elementCodingChild(elem *fhir.ElementDefinition) (*fhir.ElementDefinition, bool) {
	if elem == nil || elem.Children == nil {
		return nil, false
	}
	for _, child := range elem.Children {
		if child != nil && child.Path != "" && (child.Path == elem.Path+".coding" || child.ID == elem.ID+".coding") {
			return child, true
		}
	}
	return nil, false
}

// resolveBoundCodingFromExamplePath resolves a real coding at an element's path
// from the package's example instances, using the element's full path and the
// tree's root resource type. It is the example-based fallback for the
// BindingResolver adapter.
func resolveBoundCodingFromExamplePath(elem *fhir.ElementDefinition, tree *fhir.ElementTree, reg *registry.Registry) (generatedCoding, bool) {
	if elem == nil || tree == nil || tree.Root == nil || reg == nil {
		return generatedCoding{}, false
	}
	resourceType := tree.Root.Path
	path := strings.TrimPrefix(elem.Path, resourceType+".")
	if path == "" || path == elem.Path {
		return generatedCoding{}, false
	}
	return resolveBoundCodingFromExampleUncached(resourceType, path, "", reg)
}

// registryDisplayResolver adapts the momus registry's CodeSystem display
// resolution to the fhirgen.CodingDisplayResolver interface.
type registryDisplayResolver struct {
	reg *registry.Registry
}

// ResolveDisplay returns the canonical CodeSystem display for a coding.
func (r *registryDisplayResolver) ResolveDisplay(system, code string) (string, bool) {
	if r.reg == nil {
		return "", false
	}
	if display := resolveCodingDisplay(r.reg, system, code); display != "" {
		return display, true
	}
	return "", false
}

// newBodyGenerator constructs a fhirgen.Generator configured with the momus
// registry's binding/display resolvers and the momus post-generation
// normalizer. It seeds deterministically from the resource id so output is
// reproducible, matching momus's historical fnv-based seeding. It returns nil
// when reg is nil (the legacy no-registry call path), in which case callers
// fall back to a minimal body.
func newBodyGenerator(resourceType, id string, profileURLs []string, reg *registry.Registry, exhaustive bool) *fhirgen.Generator {
	if reg == nil {
		return nil
	}
	opts := []fhirgen.Option{
		fhirgen.WithID(id),
		fhirgen.WithMetaProfiles(profileURLs),
		fhirgen.WithBindingResolver(&registryBindingResolver{reg: reg}),
		fhirgen.WithCodingDisplayResolver(&registryDisplayResolver{reg: reg}),
		fhirgen.WithNormaliser(func(body map[string]any) {
			normalizeGeneratedPayload(body)
			normalizeResourceSpecificPayloadSeeded(body, id)
			normalisePayloadCodingDisplays(body, reg)
		}),
		fhirgen.WithStripEmptyExtensions(),
	}
	if exhaustive {
		opts = append(opts, fhirgen.WithProbabilityFillMode(optionalInclusionProbability))
	} else {
		opts = append(opts, fhirgen.WithMinFillMode())
	}
	opts = append(opts, fhirgen.WithSeed(int64(fnvSeed(id))))
	return fhirgen.New(reg.Fhir(), opts...)
}

// fnvSeed derives a deterministic int64 seed from a string, mirroring momus's
// historical fnv-1a seeding of the per-resource RNG.
func fnvSeed(s string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return h.Sum32()
}
