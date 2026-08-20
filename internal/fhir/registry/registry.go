// Package registry implements the FHIR Registry: a concurrency-safe index
// of FHIR knowledge keyed by canonical URL and resource type.
package registry

import (
	"errors"
	"fmt"
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

	// scopedStructureDefinitions is the set of canonical URLs whose
	// StructureDefinitions belong to the selected package scope. Only these
	// are subjects of test generation; the full index remains available for
	// dependency resolution (referenced profiles, base definitions, value
	// sets, and so on). When empty, every indexed StructureDefinition is
	// considered in scope.
	scopedStructureDefinitions map[string]struct{}
}

// New returns an empty Registry.
func New() *Registry {
	return &Registry{
		structureDefinitions: make(map[string]*model.StructureDefinition),
		valueSets:            make(map[string]*model.ValueSet),
		codeSystems:          make(map[string]*model.CodeSystem),
		capabilityStatements: make(map[string]*model.CapabilityStatement),
		searchParameters:     make(map[string]*model.SearchParameter),
		profilesByResource:   make(map[string][]*model.StructureDefinition),
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
		r.scopedStructureDefinitions = nil
		return
	}
	set := make(map[string]struct{}, len(scope))
	for _, url := range scope {
		if url != "" {
			set[url] = struct{}{}
		}
	}
	r.scopedStructureDefinitions = set
}

// ScopedStructureDefinitions returns the StructureDefinitions that are
// subjects of test generation: those in the selected package scope, or every
// indexed StructureDefinition when no scope has been set.
func (r *Registry) ScopedStructureDefinitions() []*model.StructureDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.scopedStructureDefinitions) == 0 {
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

// elementKey returns a unique key for an element: its path plus slice name when
// sliced (slices share a path), otherwise its path.
func elementKey(el model.ElementDefinition) string {
	if el.SliceName != "" {
		return el.Path + ":" + el.SliceName
	}
	return el.Path
}
