// Package registry implements the FHIR Registry: a concurrency-safe index
// of FHIR knowledge keyed by canonical URL and resource type.
//
// It wraps the fhir-registry library's Registry for storage and tree building,
// and adds Momus-specific scope and capability-overlay behaviour on top.
package registry

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	fhir "github.com/jlcoulter/fhir-registry"
	"github.com/jlcoulter/momus/internal/fhir/model"
)

// ErrNotFound is returned by ResolveProfile when a canonical URL is not
// present in the registry.
var ErrNotFound = errors.New("registry: resource not found")

// Registry indexes FHIR knowledge by canonical URL and resource type.
//
// Build it once (using the Add* methods) and treat it as effectively
// immutable afterwards. All methods are safe for concurrent use.
type Registry struct {
	// fhir is the underlying fhir-registry index.
	fhir *fhir.Registry

	// momusSDs preserves the original Momus StructureDefinition pointers so
	// StructureDefinition(url) returns the same pointer that was added.
	momusSDs map[string]*model.StructureDefinition

	// scoped reports whether a scope has been set. It is tracked separately
	// from scopedStructureDefinitions so that an empty-but-set scope (e.g.
	// SetScope([]string{""})) is a genuine empty selection rather than being
	// indistinguishable from "no scope".
	scoped bool

	// scopedStructureDefinitions is the set of canonical URLs whose
	// StructureDefinitions belong to the selected package scope. Only these
	// are subjects of test generation; the full index remains available for
	// dependency resolution (referenced profiles, base definitions, value
	// sets, and so on). When no scope has been set, every indexed
	// StructureDefinition is considered in scope.
	scopedStructureDefinitions map[string]struct{}

	// rootCapabilityStatementURLs is the set of canonical URLs of the
	// CapabilityStatements declared by the root package (the test subject).
	// The capability-scope overlay narrows test generation to what the root
	// package's own server CapabilityStatement declares it serves, rather than
	// unioning every dependency's CapabilityStatement.
	rootCapabilityStatementURLs map[string]struct{}

	// resolvedProfiles memoises ResolveProfile results by canonical URL. The
	// registry is effectively immutable after construction, so a resolved
	// profile is deterministic and safe to share across concurrent readers.
	// This is the dominant hot path for bulk corpus generation, where the same
	// handful of profiles are resolved once per resource instance.
	resolvedProfiles sync.Map // url -> *model.ResolvedProfile

	// resolvedElements memoises ResolveElements results by canonical URL, for
	// the same reason as resolvedProfiles.
	resolvedElements sync.Map // url -> []model.ElementDefinition
}

// New returns an empty Registry.
func New() *Registry {
	return &Registry{
		fhir:                        fhir.NewRegistry(),
		momusSDs:                    make(map[string]*model.StructureDefinition),
		rootCapabilityStatementURLs: make(map[string]struct{}),
	}
}

// AddStructureDefinition indexes a StructureDefinition by canonical URL and,
// when it has a Type, by that resource type.
func (r *Registry) AddStructureDefinition(sd *model.StructureDefinition) {
	if sd == nil || sd.URL == "" {
		return
	}
	r.momusSDs[sd.URL] = sd
	r.fhir.AddStructureDefinition(sd.ToFhir())
}

// AddValueSet indexes a ValueSet by canonical URL.
func (r *Registry) AddValueSet(vs *model.ValueSet) {
	if vs == nil || vs.URL == "" {
		return
	}
	r.fhir.AddValueSet(vs)
}

// AddCodeSystem indexes a CodeSystem by canonical URL.
func (r *Registry) AddCodeSystem(cs *model.CodeSystem) {
	if cs == nil || cs.URL == "" {
		return
	}
	r.fhir.AddCodeSystem(cs)
}

// AddCapabilityStatement indexes a CapabilityStatement by canonical URL.
func (r *Registry) AddCapabilityStatement(cs *model.CapabilityStatement) {
	if cs == nil {
		return
	}
	r.fhir.AddCapabilityStatement(cs)
}

// AddSearchParameter indexes a SearchParameter by each resource type it
// applies to, combined with its code.
func (r *Registry) AddSearchParameter(sp *model.SearchParameter) {
	if sp == nil || sp.Code == "" {
		return
	}
	r.fhir.AddSearchParameter(sp)
}

// AddResource indexes an instance/example resource by its FHIR resource type.
func (r *Registry) AddResource(res *model.Resource) {
	if res == nil || res.ResourceType == "" {
		return
	}
	r.fhir.AddResource(res)
}

// ResourcesForType returns every indexed instance/example resource of a given
// FHIR resource type.
func (r *Registry) ResourcesForType(resourceType string) []*model.Resource {
	return r.fhir.ResourcesForType(resourceType)
}

// AllResources returns every indexed instance/example resource across all
// resource types.
func (r *Registry) AllResources() []*model.Resource {
	return r.fhir.AllResources()
}

// StructureDefinition returns the StructureDefinition for a canonical URL.
func (r *Registry) StructureDefinition(url string) (*model.StructureDefinition, bool) {
	sd, ok := r.momusSDs[url]
	return sd, ok
}

// StructureDefinitions returns every indexed StructureDefinition.
func (r *Registry) StructureDefinitions() []*model.StructureDefinition {
	out := make([]*model.StructureDefinition, 0, len(r.momusSDs))
	for _, sd := range r.momusSDs {
		out = append(out, sd)
	}
	return out
}

// fromFhir converts an fhir-registry StructureDefinition back into the Momus
// representation with a flat element list.
func fromFhir(sd *fhir.StructureDefinition) *model.StructureDefinition {
	if sd == nil {
		return nil
	}
	out := &model.StructureDefinition{
		URL:            sd.URL,
		Name:           sd.Name,
		Title:          sd.Title,
		Type:           sd.Type,
		BaseDefinition: sd.BaseDefinition,
		Kind:           sd.Kind,
		Derivation:     sd.Derivation,
	}
	if sd.Snapshot != nil {
		for _, raw := range sd.Snapshot.Elements {
			if e, err := fhir.ConvertElement(raw); err == nil {
				out.Elements = append(out.Elements, e)
			}
		}
	}
	return out
}

// SetScope restricts the set of StructureDefinitions that are subjects of
// test generation to those whose canonical URL is in scope. Structure
// Definitions outside the scope remain indexed and resolvable so they can
// satisfy dependencies (referenced profiles, base definitions, value sets),
// but they are not returned by ScopedStructureDefinitions. Passing an empty
// scope clears the restriction and treats every indexed StructureDefinition
// as in scope.
func (r *Registry) SetScope(scope []string) {
	if len(scope) == 0 {
		r.scoped = false
		r.scopedStructureDefinitions = nil
		return
	}
	set := make(map[string]struct{}, len(scope))
	for _, url := range scope {
		if url != "" {
			set[url] = struct{}{}
		}
	}
	r.scoped = true
	r.scopedStructureDefinitions = set
}

// ScopedStructureDefinitions returns the StructureDefinitions that are
// subjects of test generation: those in the selected package scope, or every
// indexed StructureDefinition when no scope has been set.
func (r *Registry) ScopedStructureDefinitions() []*model.StructureDefinition {
	if !r.scoped {
		return r.StructureDefinitions()
	}
	out := make([]*model.StructureDefinition, 0, len(r.scopedStructureDefinitions))
	for url := range r.scopedStructureDefinitions {
		if sd, ok := r.momusSDs[url]; ok {
			out = append(out, sd)
		}
	}
	return out
}

// SetScopeToResourceTypesAndProfiles narrows the scoped test-generation
// subjects to those whose resource type is in types AND (when non-empty) whose
// canonical URL is in profiles, intersecting with the current scope. This is
// how a reduced scope — e.g. one derived from a CapabilityStatement — is
// overlaid over the full registry, which indexes every package resource in
// full. When no scope is currently set, every indexed definition is considered.
func (r *Registry) SetScopeToResourceTypesAndProfiles(types, profiles []string) {
	typeSet := toLowerSet(types)
	profileSet := toLowerSet(profiles)

	var urls []string
	if !r.scoped {
		for _, sd := range r.fhir.StructureDefinitions() {
			urls = append(urls, sd.URL)
		}
	} else {
		for u := range r.scopedStructureDefinitions {
			urls = append(urls, u)
		}
	}
	kept := make(map[string]struct{})
	for _, u := range urls {
		sd, ok := r.momusSDs[u]
		if !ok || sd == nil {
			continue
		}
		if len(typeSet) > 0 {
			if _, ok := typeSet[strings.ToLower(sd.Type)]; !ok {
				continue
			}
		}
		if len(profileSet) > 0 {
			if _, ok := profileSet[strings.ToLower(sd.URL)]; !ok {
				continue
			}
		}
		kept[u] = struct{}{}
	}
	r.scoped = true
	r.scopedStructureDefinitions = kept
}

// toLowerSet builds a case-insensitive set from a string slice, dropping empty
// entries.
func toLowerSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		set[strings.ToLower(v)] = struct{}{}
	}
	return set
}

// ValueSet returns the ValueSet for a canonical URL.
func (r *Registry) ValueSet(url string) (*model.ValueSet, bool) {
	return r.fhir.ValueSet(url)
}

// CodeSystem returns the CodeSystem for a canonical URL.
func (r *Registry) CodeSystem(url string) (*model.CodeSystem, bool) {
	return r.fhir.CodeSystem(url)
}

// CapabilityStatements returns every indexed CapabilityStatement.
func (r *Registry) CapabilityStatements() []*model.CapabilityStatement {
	return r.fhir.CapabilityStatements()
}

// OverlayCapabilityScope narrows the scoped test-generation subjects to the
// server-mode resource types and (when declared) supported profiles from the
// ROOT package's own CapabilityStatement. This applies the reduced scope "of
// just the package capability statement" over the full registry, so generated
// resources are always 100% conformant with the package as production data.
//
// Only the root package's CapabilityStatements (marked via
// MarkRootCapabilityStatements) are considered; the union of their server-mode
// resource types and supportedProfile URLs is intersected into the current
// scope. When no root CapabilityStatement declares any server-mode resource,
// it returns without narrowing (the existing scope, or the full registry, is
// preserved).
func (r *Registry) OverlayCapabilityScope() {
	var csList []*model.CapabilityStatement
	for _, cs := range r.fhir.CapabilityStatements() {
		if _, ok := r.rootCapabilityStatementURLs[cs.URL]; ok {
			csList = append(csList, cs)
		}
	}

	types := make(map[string]struct{})
	profiles := make(map[string]struct{})
	for _, cs := range csList {
		for _, rest := range cs.Rest {
			if rest.Mode != "" && !strings.EqualFold(rest.Mode, "server") {
				continue
			}
			for _, res := range rest.Resource {
				t := strings.TrimSpace(res.Type)
				if t != "" {
					types[strings.ToLower(t)] = struct{}{}
				}
				for _, p := range res.SupportedProfile {
					p = strings.TrimSpace(p)
					if p != "" {
						profiles[strings.ToLower(p)] = struct{}{}
					}
				}
			}
		}
	}
	if len(types) == 0 && len(profiles) == 0 {
		return
	}
	typeList := make([]string, 0, len(types))
	for t := range types {
		typeList = append(typeList, t)
	}
	profileList := make([]string, 0, len(profiles))
	for p := range profiles {
		profileList = append(profileList, p)
	}
	r.SetScopeToResourceTypesAndProfiles(typeList, profileList)
}

// MarkRootCapabilityStatements records the canonical URLs of the CapabilityStatements
// declared by the root package (the test subject), so the capability-scope
// overlay considers only those rather than every dependency's CapabilityStatement.
func (r *Registry) MarkRootCapabilityStatements(cs *model.CapabilityStatement) {
	if cs == nil || cs.URL == "" {
		return
	}
	r.rootCapabilityStatementURLs[cs.URL] = struct{}{}
}

// SearchParameter returns the SearchParameter for a resource type and code.
func (r *Registry) SearchParameter(resourceType, code string) (*model.SearchParameter, bool) {
	return r.fhir.SearchParameter(resourceType, code)
}

// SearchParameters returns every distinct indexed SearchParameter.
func (r *Registry) SearchParameters() []*model.SearchParameter {
	all := r.fhir.SearchParameters()
	seen := make(map[*model.SearchParameter]struct{}, len(all))
	out := make([]*model.SearchParameter, 0, len(all))
	for _, sp := range all {
		if _, ok := seen[sp]; ok {
			continue
		}
		seen[sp] = struct{}{}
		out = append(out, sp)
	}
	return out
}

// ProfilesForResource returns all profiles (derived or base) for a resource
// type.
func (r *Registry) ProfilesForResource(resourceType string) []*model.StructureDefinition {
	var out []*model.StructureDefinition
	for _, sd := range r.momusSDs {
		if sd.Type == resourceType {
			out = append(out, sd)
		}
	}
	return out
}

// ResolveProfile resolves a StructureDefinition by canonical URL into a
// ResolvedProfile with a built element tree and path index.
func (r *Registry) ResolveProfile(url string) (*model.ResolvedProfile, error) {
	if cached, ok := r.resolvedProfiles.Load(url); ok {
		return cached.(*model.ResolvedProfile), nil
	}
	sd, ok := r.fhir.Definition(url)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, url)
	}
	tree, err := r.fhir.Tree(url)
	if err != nil {
		return nil, err
	}
	elements := treeElements(tree)
	rp := model.NewResolvedProfile(sd.URL, sd.Type, elements)
	if rp != nil {
		r.resolvedProfiles.Store(url, rp)
	}
	return rp, nil
}

// ResolveElements returns the flat, parent-merged ElementDefinition list for
// the StructureDefinition at url. It is derived from the fhir-registry element
// tree, which already merges the base definition chain.
//
// Returns ErrNotFound when url is not indexed.
func (r *Registry) ResolveElements(url string) ([]model.ElementDefinition, error) {
	if cached, ok := r.resolvedElements.Load(url); ok {
		return cached.([]model.ElementDefinition), nil
	}
	if _, ok := r.fhir.Definition(url); !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, url)
	}
	tree, err := r.fhir.Tree(url)
	if err != nil {
		return nil, err
	}
	elements := treeElements(tree)
	r.resolvedElements.Store(url, elements)
	return elements, nil
}

// treeElements flattens an fhir-registry ElementTree into a slice of
// ElementDefinition, preserving slice definitions and slice-child elements. It
// iterates every element in the tree's ByID index (which includes orphaned
// slice elements not reachable from the root), rather than walking the tree.
func treeElements(tree *fhir.ElementTree) []model.ElementDefinition {
	if tree == nil {
		return nil
	}
	out := make([]model.ElementDefinition, 0, len(tree.ByID))
	for _, elem := range tree.ByID {
		if elem == nil {
			continue
		}
		out = append(out, *elem)
	}
	return out
}
