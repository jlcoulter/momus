// Package registry implements the FHIR Registry: a concurrency-safe index
// of FHIR knowledge keyed by canonical URL and resource type.
package registry

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

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
	mu sync.RWMutex

	structureDefinitions map[string]*model.StructureDefinition
	valueSets            map[string]*model.ValueSet
	codeSystems          map[string]*model.CodeSystem
	capabilityStatements map[string]*model.CapabilityStatement

	searchParameters map[string]*model.SearchParameter

	profilesByResource map[string][]*model.StructureDefinition

	// resourcesByType indexes instance/example resources by FHIR resource type
	// (e.g. all example Patient resources). The registry represents the package
	// and its dependencies in full, so these are indexed alongside the
	// conformance types.
	resourcesByType map[string][]*model.Resource

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
}

// New returns an empty Registry.
func New() *Registry {
	return &Registry{
		structureDefinitions:        make(map[string]*model.StructureDefinition),
		valueSets:                   make(map[string]*model.ValueSet),
		codeSystems:                 make(map[string]*model.CodeSystem),
		capabilityStatements:        make(map[string]*model.CapabilityStatement),
		searchParameters:            make(map[string]*model.SearchParameter),
		profilesByResource:          make(map[string][]*model.StructureDefinition),
		resourcesByType:             make(map[string][]*model.Resource),
		rootCapabilityStatementURLs: make(map[string]struct{}),
	}
}

// AddStructureDefinition indexes a StructureDefinition by canonical URL and,
// when it has a Type, by that resource type.
func (r *Registry) AddStructureDefinition(sd *model.StructureDefinition) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if sd == nil || sd.URL == "" {
		return
	}
	r.structureDefinitions[sd.URL] = sd
	if sd.Type != "" {
		r.profilesByResource[sd.Type] = append(r.profilesByResource[sd.Type], sd)
	}
}

// AddValueSet indexes a ValueSet by canonical URL.
func (r *Registry) AddValueSet(vs *model.ValueSet) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if vs != nil && vs.URL != "" {
		r.valueSets[vs.URL] = vs
	}
}

// AddCodeSystem indexes a CodeSystem by canonical URL.
func (r *Registry) AddCodeSystem(cs *model.CodeSystem) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cs != nil && cs.URL != "" {
		r.codeSystems[cs.URL] = cs
	}
}

// AddCapabilityStatement indexes a CapabilityStatement by canonical URL.
func (r *Registry) AddCapabilityStatement(cs *model.CapabilityStatement) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cs != nil && cs.URL != "" {
		r.capabilityStatements[cs.URL] = cs
	}
}

// AddSearchParameter indexes a SearchParameter by each resource type it
// applies to, combined with its code.
func (r *Registry) AddSearchParameter(sp *model.SearchParameter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if sp == nil || sp.Code == "" {
		return
	}
	for _, base := range sp.Base {
		r.searchParameters[searchParameterKey(base, sp.Code)] = sp
	}
}

func searchParameterKey(resourceType, code string) string {
	return resourceType + "\x00" + code
}

// AddResource indexes an instance/example resource by its FHIR resource type.
func (r *Registry) AddResource(res *model.Resource) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if res == nil || res.ResourceType == "" {
		return
	}
	r.resourcesByType[res.ResourceType] = append(r.resourcesByType[res.ResourceType], res)
}

// ResourcesForType returns every indexed instance/example resource of a given
// FHIR resource type.
func (r *Registry) ResourcesForType(resourceType string) []*model.Resource {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*model.Resource, 0, len(r.resourcesByType[resourceType]))
	out = append(out, r.resourcesByType[resourceType]...)
	return out
}

// AllResources returns every indexed instance/example resource across all
// resource types.
func (r *Registry) AllResources() []*model.Resource {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var total int
	for _, list := range r.resourcesByType {
		total += len(list)
	}
	out := make([]*model.Resource, 0, total)
	for _, list := range r.resourcesByType {
		out = append(out, list...)
	}
	return out
}

// StructureDefinition returns the StructureDefinition for a canonical URL.
func (r *Registry) StructureDefinition(url string) (*model.StructureDefinition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sd, ok := r.structureDefinitions[url]
	return sd, ok
}

// StructureDefinitions returns every indexed StructureDefinition.
func (r *Registry) StructureDefinitions() []*model.StructureDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*model.StructureDefinition, 0, len(r.structureDefinitions))
	for _, sd := range r.structureDefinitions {
		out = append(out, sd)
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
	r.mu.Lock()
	defer r.mu.Unlock()
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
	r.mu.RLock()
	defer r.mu.RUnlock()
	if !r.scoped {
		return r.structureDefinitionsSnapshot()
	}
	out := make([]*model.StructureDefinition, 0, len(r.scopedStructureDefinitions))
	for url := range r.scopedStructureDefinitions {
		if sd, ok := r.structureDefinitions[url]; ok {
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

	r.mu.Lock()
	defer r.mu.Unlock()

	var urls []string
	if !r.scoped {
		for u := range r.structureDefinitions {
			urls = append(urls, u)
		}
	} else {
		for u := range r.scopedStructureDefinitions {
			urls = append(urls, u)
		}
	}
	kept := make(map[string]struct{})
	for _, u := range urls {
		sd := r.structureDefinitions[u]
		if sd == nil {
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

func (r *Registry) structureDefinitionsSnapshot() []*model.StructureDefinition {
	out := make([]*model.StructureDefinition, 0, len(r.structureDefinitions))
	for _, sd := range r.structureDefinitions {
		out = append(out, sd)
	}
	return out
}

// ValueSet returns the ValueSet for a canonical URL.
func (r *Registry) ValueSet(url string) (*model.ValueSet, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	vs, ok := r.valueSets[url]
	return vs, ok
}

// CodeSystem returns the CodeSystem for a canonical URL.
func (r *Registry) CodeSystem(url string) (*model.CodeSystem, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cs, ok := r.codeSystems[url]
	return cs, ok
}

// CapabilityStatements returns every indexed CapabilityStatement.
func (r *Registry) CapabilityStatements() []*model.CapabilityStatement {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*model.CapabilityStatement, 0, len(r.capabilityStatements))
	for _, cs := range r.capabilityStatements {
		out = append(out, cs)
	}
	return out
}

// SearchIncludesForType returns the aggregated _include parameter values
// declared for a resource type across the registry's server-mode
// CapabilityStatement entries. Each value is the "Type.param" form declared in
// the server's searchInclude, e.g. "Patient.organization". Values are
// de-duplicated and returned sorted. When no server declaration exists the
// result is nil.
func (r *Registry) SearchIncludesForType(resourceType string) []string {
	return includesForType(r, resourceType, true)
}

// SearchRevIncludesForType returns the aggregated _revinclude parameter values
// declared for a resource type across the registry's server-mode
// CapabilityStatement entries. Each value is the "Type.param" form declared in
// the server's searchRevInclude. Values are de-duplicated and returned sorted;
// when no declaration exists the result is nil.
func (r *Registry) SearchRevIncludesForType(resourceType string) []string {
	return includesForType(r, resourceType, false)
}

// includesForType is the shared implementation for the include/revinclude
// accessors. When include is true it reads SearchInclude; otherwise
// SearchRevInclude. Only server-mode rest blocks are considered.
func includesForType(r *Registry, resourceType string, include bool) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for _, cs := range r.capabilityStatements {
		for _, rest := range cs.Rest {
			if rest.Mode != "" && !strings.EqualFold(rest.Mode, "server") {
				continue
			}
			for _, res := range rest.Resource {
				if !strings.EqualFold(res.Type, resourceType) {
					continue
				}
				var values []string
				if include {
					values = res.SearchInclude
				} else {
					values = res.SearchRevInclude
				}
				for _, v := range values {
					v = strings.TrimSpace(v)
					if v == "" {
						continue
					}
					if _, ok := seen[v]; ok {
						continue
					}
					seen[v] = struct{}{}
					out = append(out, v)
				}
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
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
	r.mu.RLock()
	csList := make([]*model.CapabilityStatement, 0, len(r.rootCapabilityStatementURLs))
	for url := range r.rootCapabilityStatementURLs {
		if cs, ok := r.capabilityStatements[url]; ok {
			csList = append(csList, cs)
		}
	}
	r.mu.RUnlock()

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
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rootCapabilityStatementURLs[cs.URL] = struct{}{}
}

// SearchParameter returns the SearchParameter for a resource type and code.
func (r *Registry) SearchParameter(resourceType, code string) (*model.SearchParameter, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sp, ok := r.searchParameters[searchParameterKey(resourceType, code)]
	return sp, ok
}

// SearchParameters returns every distinct indexed SearchParameter.
func (r *Registry) SearchParameters() []*model.SearchParameter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	seen := make(map[*model.SearchParameter]struct{}, len(r.searchParameters))
	out := make([]*model.SearchParameter, 0, len(r.searchParameters))
	for _, sp := range r.searchParameters {
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
	r.mu.RLock()
	defer r.mu.RUnlock()
	profiles := r.profilesByResource[resourceType]
	out := make([]*model.StructureDefinition, len(profiles))
	copy(out, profiles)
	return out
}

// ResolveProfile resolves a StructureDefinition by canonical URL into a
// ResolvedProfile with a built element tree and path index.
//
// This is a minimal implementation; profile inheritance and slicing
// resolution will be extended later.
func (r *Registry) ResolveProfile(url string) (*model.ResolvedProfile, error) {
	sd, ok := r.StructureDefinition(url)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, url)
	}
	elements := r.resolveElements(sd, make(map[string]bool))
	return model.NewResolvedProfile(sd.URL, sd.Type, elements), nil
}

// ResolveElements returns the flat, parent-merged ElementDefinition list for
// the StructureDefinition at url. Parent elements (walked via BaseDefinition)
// are merged first; child elements override parent elements with the same
// elementKey (path, or path:sliceName when sliced) and append new paths. The
// returned slice preserves slice definitions and slice-child elements, which
// the ResolvedProfile.Elements map intentionally drops.
//
// Returns ErrNotFound when url is not indexed.
func (r *Registry) ResolveElements(url string) ([]model.ElementDefinition, error) {
	sd, ok := r.StructureDefinition(url)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, url)
	}
	return r.resolveElements(sd, make(map[string]bool)), nil
}

// resolveElements returns the full element set for sd by resolving its parent
// (baseDefinition) dependency chain and merging: child elements override parent
// elements with the same path, preserving order. This ensures inherited elements
// and constraints (e.g. a profile's base Identifier structure) are available to
// generation even when a profile is a differential.
func (r *Registry) resolveElements(sd *model.StructureDefinition, seen map[string]bool) []model.ElementDefinition {
	if sd == nil || seen[sd.URL] {
		return nil
	}
	seen[sd.URL] = true
	parentSD, _ := r.StructureDefinition(sd.BaseDefinition)
	parent := r.resolveElements(parentSD, seen)
	merged := make([]model.ElementDefinition, 0, len(parent)+len(sd.Elements))
	index := make(map[string]int, len(parent)+len(sd.Elements))
	for _, el := range parent {
		index[elementKey(el)] = len(merged)
		merged = append(merged, el)
	}
	for _, el := range sd.Elements {
		if idx, ok := index[elementKey(el)]; ok {
			merged[idx] = el
		} else {
			index[elementKey(el)] = len(merged)
			merged = append(merged, el)
		}
	}
	return merged
}

// elementKey returns a unique merge key for an element: its path plus slice
// name when sliced (slices share a path), otherwise its path. For slice children
// whose slice context lives only in their ID (e.g. an ID of
// "Organization.extension:suppressed.url" with a plain path), the ID's slice
// segment is preserved so the slice member keys distinct from its base element
// (task #30); otherwise a SliceName is used when present.
func elementKey(el model.ElementDefinition) string {
	if key := model.ElementSliceKey(el.ID, el.Path); key != el.Path {
		return key
	}
	if el.SliceName != "" {
		return el.Path + ":" + el.SliceName
	}
	return el.Path
}
