// Package registry implements the FHIR Registry: a concurrency-safe index
// of FHIR knowledge keyed by canonical URL and resource type.
package registry

import (
	"errors"
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

	searchParameters            map[string]*model.SearchParameter
	searchParametersForResource map[string][]*model.SearchParameter

	profilesByResource map[string][]*model.StructureDefinition
}

// New returns an empty Registry.
func New() *Registry {
	return &Registry{
		structureDefinitions:        make(map[string]*model.StructureDefinition),
		valueSets:                   make(map[string]*model.ValueSet),
		codeSystems:                 make(map[string]*model.CodeSystem),
		capabilityStatements:        make(map[string]*model.CapabilityStatement),
		searchParameters:            make(map[string]*model.SearchParameter),
		searchParametersForResource: make(map[string][]*model.SearchParameter),
		profilesByResource:          make(map[string][]*model.StructureDefinition),
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
		r.searchParametersForResource[base] = append(r.searchParametersForResource[base], sp)
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

// CapabilityStatement returns the CapabilityStatement for a canonical URL.
func (r *Registry) CapabilityStatement(url string) (*model.CapabilityStatement, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cs, ok := r.capabilityStatements[url]
	return cs, ok
}

// SearchParameter returns the SearchParameter for a resource type and code.
func (r *Registry) SearchParameter(resourceType, code string) (*model.SearchParameter, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sp, ok := r.searchParameters[searchParameterKey(resourceType, code)]
	return sp, ok
}

// SearchParametersForResource returns all SearchParameters for a resource
// type.
func (r *Registry) SearchParametersForResource(resourceType string) []*model.SearchParameter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	params := r.searchParametersForResource[resourceType]
	out := make([]*model.SearchParameter, len(params))
	copy(out, params)
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
		return nil, ErrNotFound
	}
	return model.NewResolvedProfile(sd.URL, sd.Type, sd.Elements), nil
}
