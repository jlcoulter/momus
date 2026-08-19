// Package resource defines the interface for the FHIR resource generator and
// provides a registry-backed implementation that turns DataRequirements into
// concrete Datasets.
package resource

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

// Generator produces a Dataset satisfying a DataRequirement.
//
// Implementations depend on the Registry rather than parsing
// StructureDefinitions themselves.
type Generator interface {
	Generate(ctx context.Context, requirement model.DataRequirement) (*model.Dataset, error)
}

// Options configures the DatasetGenerator.
type Options struct {
	// Exhaustive populates optional elements in addition to required ones,
	// producing fuller, more realistic resources suitable for bulk data.
	// When false, only required (Min > 0) elements are populated.
	Exhaustive bool
}

// DatasetGenerator synthesises concrete resources from a DataRequirement using
// the registry's resolved profiles. It is safe to reuse across requirements.
type DatasetGenerator struct {
	reg        *registry.Registry
	exhaustive bool
}

// NewGeneratorWithOptions returns a DatasetGenerator backed by reg with the
// given options applied.
func NewGeneratorWithOptions(reg *registry.Registry, opts Options) *DatasetGenerator {
	return &DatasetGenerator{reg: reg, exhaustive: opts.Exhaustive}
}

// Generate produces a Dataset satisfying req: one or more instances of the
// requested resource type, with required elements populated and relationship
// references wired to generated target resources.
func (g *DatasetGenerator) Generate(ctx context.Context, req model.DataRequirement) (*model.Dataset, error) {
	_ = ctx
	if g.reg == nil {
		return nil, errors.New("generator requires a registry")
	}
	resourceType := strings.TrimSpace(req.Resource.Type)
	if resourceType == "" {
		return nil, errors.New("data requirement missing resource type")
	}

	ds := &model.Dataset{
		Resources:     make(map[string]*model.ResourceInstance),
		Relationships: make([]model.Reference, 0),
	}

	// Resolve relationship targets first so the primary resources can
	// reference them.
	refs := make(map[string]refTarget)
	for _, rel := range req.Relationships {
		targetType := strings.TrimSpace(rel.Target.Type)
		if targetType == "" {
			continue
		}
		// Skip abstract base types (e.g. Resource, DomainResource): they cannot
		// be instantiated as concrete data and FHIR servers reject them.
		if isAbstractResourceType(targetType) {
			continue
		}
		targetProfile := primaryProfile(rel.Target.Profile)
		targetID := targetInstanceID(targetType, targetProfile)
		body, err := synthesizeResource(g.reg, targetType, targetProfile, targetID, nil, g.exhaustive, newRNG(targetID))
		if err != nil {
			return nil, fmt.Errorf("generate relationship target %s: %w", targetType, err)
		}
		ds.Resources[targetID] = &model.ResourceInstance{
			LocalID:      targetID,
			ResourceType: targetType,
			Profile:      targetProfile,
			Resource:     body,
		}
		refs[rel.Path] = refTarget{resourceType: targetType, localID: targetID}
	}

	count := req.Cardinality.Min
	if count < 1 {
		count = 1
	}
	profile := primaryProfile(req.Resource.Profile)
	for i := 0; i < count; i++ {
		localID := instanceID(req, i)
		body, err := synthesizeResource(g.reg, resourceType, profile, localID, refs, g.exhaustive, newRNG(localID))
		if err != nil {
			return nil, fmt.Errorf("generate %s instance %d: %w", resourceType, i, err)
		}
		applyEqualsConstraints(body, req.Constraints)
		for _, rel := range req.Relationships {
			if target, ok := refs[rel.Path]; ok {
				setReferencePath(body, rel.Path, target)
				ds.Relationships = append(ds.Relationships, model.Reference{
					SourceID: localID,
					Path:     rel.Path,
					TargetID: target.localID,
				})
			}
		}
		ds.Resources[localID] = &model.ResourceInstance{
			LocalID:      localID,
			ResourceType: resourceType,
			Profile:      profile,
			Resource:     body,
		}
	}

	return ds, nil
}

func primaryProfile(profiles []string) string {
	for _, p := range profiles {
		if strings.TrimSpace(p) != "" {
			return strings.TrimSpace(p)
		}
	}
	return ""
}

// abstractResourceTypes are FHIR types with kind "resource" that are abstract
// base types and cannot be instantiated as concrete data.
var abstractResourceTypes = map[string]bool{
	"Resource":          true,
	"DomainResource":    true,
	"CanonicalResource": true,
	"MetadataResource":  true,
}

// isAbstractResourceType reports whether resourceType is an abstract FHIR base
// type that must not be instantiated as a concrete resource.
func isAbstractResourceType(resourceType string) bool {
	return abstractResourceTypes[strings.TrimSpace(resourceType)]
}

func instanceID(req model.DataRequirement, index int) string {
	base := "momus-" + sanitizeID(req.Resource.Type)
	if req.ID != "" {
		base = "momus-" + sanitizeID(req.ID)
	}
	if index == 0 {
		return base
	}
	return fmt.Sprintf("%s-%d", base, index+1)
}

func targetInstanceID(resourceType, profile string) string {
	if profile != "" {
		return "momus-target-" + sanitizeID(resourceType) + "-" + sanitizeID(profile)
	}
	return "momus-target-" + sanitizeID(resourceType)
}

// sanitizeID reduces an arbitrary string to a FHIR-compatible id segment.
func sanitizeID(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '.':
			b.WriteRune(r)
		case r == ' ' || r == '|' || r == '/' || r == ':':
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-.")
}

// applyEqualsConstraints applies Constraint{Operator: equals} values to the
// corresponding element path in the generated body.
func applyEqualsConstraints(body map[string]any, constraints []model.Constraint) {
	for _, c := range constraints {
		if c.Operator != model.OpEquals {
			continue
		}
		setPathValue(body, c.Path, c.Value)
	}
}

func setPathValue(body map[string]any, path string, value any) {
	parts := strings.Split(path, ".")
	if len(parts) <= 1 {
		return
	}
	cur := body
	for i := 1; i < len(parts)-1; i++ {
		next, ok := cur[parts[i]].(map[string]any)
		if !ok {
			return
		}
		cur = next
	}
	cur[parts[len(parts)-1]] = value
}

// setReferencePath places a FHIR reference at an element path, creating
// intermediate containers as needed, so relationship targets are always wired
// into the body even when the element is optional.
func setReferencePath(body map[string]any, path string, target refTarget) {
	parts := strings.Split(path, ".")
	if len(parts) <= 1 {
		return
	}
	cur := body
	for i := 1; i < len(parts)-1; i++ {
		next, ok := cur[parts[i]].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[parts[i]] = next
		}
		cur = next
	}
	cur[parts[len(parts)-1]] = map[string]any{"reference": target.resourceType + "/" + target.localID}
}
