package resource

import (
	"context"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

// commonReferenceTargets supplies a sensible target resource type for reference
// fields whose profile element does not carry a target profile, keyed by
// resource type then element leaf name. This covers the most common FHIR
// relationships so a generated corpus links sensibly even when the registry's
// structure definitions are sparse.
var commonReferenceTargets = map[string]map[string]string{
	"Patient":              {"generalPractitioner": "Practitioner", "managingOrganization": "Organization"},
	"Observation":          {"subject": "Patient", "performer": "Practitioner", "encounter": "Encounter", "specimen": "Specimen", "device": "Device", "basedOn": "ServiceRequest"},
	"HealthcareService":    {"providedBy": "Organization", "location": "Location"},
	"PractitionerRole":     {"practitioner": "Practitioner", "organization": "Organization", "location": "Location"},
	"Practitioner":         {"organization": "Organization"},
	"Organization":         {"partOf": "Organization"},
	"Encounter":            {"subject": "Patient", "participant": "Practitioner", "serviceProvider": "Organization"},
	"Condition":            {"subject": "Patient", "encounter": "Encounter", "asserter": "Practitioner"},
	"Procedure":            {"subject": "Patient", "performer": "Practitioner", "encounter": "Encounter"},
	"MedicationRequest":    {"subject": "Patient", "medication": "Medication", "encounter": "Encounter"},
	"MedicationStatement":  {"subject": "Patient", "medication": "Medication"},
	"MedicationDispense":   {"subject": "Patient", "medication": "Medication"},
	"Immunization":         {"patient": "Patient", "performer": "Practitioner", "location": "Location"},
	"AllergyIntolerance":   {"patient": "Patient", "asserter": "Practitioner"},
	"DiagnosticReport":     {"subject": "Patient", "performer": "Practitioner", "encounter": "Encounter"},
	"ServiceRequest":       {"subject": "Patient", "performer": "Practitioner", "encounter": "Encounter"},
	"Composition":          {"subject": "Patient", "author": "Practitioner", "encounter": "Encounter"},
	"CarePlan":             {"subject": "Patient", "author": "Practitioner"},
	"Appointment":          {"participant": "Patient"},
	"Endpoint":             {"managingOrganization": "Organization"},
	"Location":             {"managingOrganization": "Organization", "partOf": "Location"},
	"Specimen":             {"subject": "Patient", "collection": "Practitioner"},
	"Device":               {"owner": "Organization"},
	"Medication":           {"manufacturer": "Organization"},
	"ResearchStudy":        {"principalInvestigator": "Practitioner"},
	"Claim":                {"patient": "Patient", "provider": "Practitioner"},
	"ExplanationOfBenefit": {"patient": "Patient", "provider": "Practitioner"},
}

// GenerateCorpus produces a realistic corpus of instances for each of the
// given resource types. By default defaultCount instances are generated per
// type; overrides, keyed by resource type, take precedence when present.
// References are wired across the generated pools so the resources form a
// sensible, distributed web: dependents are spread over the available targets
// deterministically, so several resources share a common target rather than
// everything pointing at one instance.
func (g *DatasetGenerator) GenerateCorpus(ctx context.Context, resourceTypes []string, defaultCount int, overrides map[string]int) (*model.Dataset, error) {
	_ = ctx
	if g.reg == nil {
		return nil, fmt.Errorf("generator requires a registry")
	}
	if defaultCount < 1 {
		defaultCount = 1
	}
	if len(resourceTypes) == 0 {
		return nil, fmt.Errorf("no resource types to generate")
	}

	ds := &model.Dataset{Resources: make(map[string]*model.ResourceInstance), Relationships: []model.Reference{}}
	pools := make(map[string][]string) // resource type -> ordered generated local IDs

	// Expand the type set to include referenced target types (e.g. including
	// HealthcareService pulls in Organization, Practitioner, Location) so that
	// every reference resolves even when the caller requested a subset.
	resourceTypes = g.expandReferenceTargets(resourceTypes)

	// Pass 1: generate instances of each type. References are left as
	// dangling placeholders and wired in pass 2 once all pools exist. Types that
	// cannot be synthesised (e.g. abstract or unresolved profiles) are skipped.
	for _, t := range resourceTypes {
		count := defaultCount
		if c, ok := overrides[t]; ok && c > 0 {
			count = c
		}
		for i := 0; i < count; i++ {
			id := fmt.Sprintf("momus-%s-%d", sanitizeID(t), i+1)
			body, err := synthesizeResource(g.reg, t, "", id, nil, true, newRNG(id))
			if err != nil {
				// A single unsynthesizable type must not abort the corpus.
				break
			}
			if body == nil || len(body) == 0 {
				break
			}
			ds.Resources[id] = &model.ResourceInstance{LocalID: id, ResourceType: t, Profile: "", Resource: body}
			pools[t] = append(pools[t], id)
		}
	}

	// Pass 2: wire each resource's reference fields to a distributed target.
	for _, t := range resourceTypes {
		refFields := g.referenceFields(t)
		for _, id := range pools[t] {
			inst := ds.Resources[id]
			wireCorpusReferences(inst, refFields, pools)
		}
	}

	return ds, nil
}

// expandReferenceTargets grows the type set to a fixpoint: whenever an included
// type references another type available in the registry, that type is added so
// the generated corpus is self-contained and every reference resolves.
func (g *DatasetGenerator) expandReferenceTargets(resourceTypes []string) []string {
	included := make(map[string]bool, len(resourceTypes))
	for _, t := range resourceTypes {
		included[t] = true
	}
	for changed := true; changed; {
		changed = false
		for _, t := range resourceTypes {
			for _, target := range g.referenceFields(t) {
				if !included[target] && g.hasResourceType(target) {
					included[target] = true
					resourceTypes = append(resourceTypes, target)
					changed = true
				}
			}
		}
	}
	sort.Strings(resourceTypes)
	return resourceTypes
}

func (g *DatasetGenerator) hasResourceType(resourceType string) bool {
	return g.reg != nil && len(g.reg.ProfilesForResource(resourceType)) > 0
}

// referenceFields derives the reference element paths of a resource type and
// their target resource types, from the type's resolved profile, falling back
// to commonReferenceTargets for fields without a target profile.
func (g *DatasetGenerator) referenceFields(resourceType string) map[string]string {
	out := make(map[string]string)
	if profileURL := defaultProfile(g.reg, resourceType); profileURL != "" {
		if resolved, err := g.reg.ResolveProfile(profileURL); err == nil && resolved != nil && resolved.Root != nil {
			collectReferenceFields(resolved.Root, g.reg, out)
		}
	}
	if fallback, ok := commonReferenceTargets[resourceType]; ok {
		for leaf, target := range fallback {
			path := resourceType + "." + leaf
			if _, exists := out[path]; !exists {
				out[path] = target
			}
		}
	}
	return out
}

// collectReferenceFields walks an element tree and records Reference elements
// with a resolvable target resource type, keyed by canonical path.
func collectReferenceFields(node *model.ElementNode, reg *registry.Registry, out map[string]string) {
	if node == nil {
		return
	}
	if node.Definition != nil && primaryTypeCode(node.Definition) == "Reference" {
		if target := referenceTargetType(node.Definition, reg); target != "" {
			out[node.Path] = target
		}
	}
	for _, child := range node.Children {
		collectReferenceFields(child, reg, out)
	}
	for _, slice := range node.Slices {
		for _, child := range slice.Children {
			collectReferenceFields(child, reg, out)
		}
	}
}

// referenceTargetType returns the first resolvable target resource type for a
// Reference element, from its target profiles.
func referenceTargetType(def *model.ElementDefinition, reg *registry.Registry) string {
	for _, profileURL := range def.TargetProfile {
		if rt := resourceTypeOfProfile(reg, profileURL); rt != "" {
			return rt
		}
	}
	for _, et := range def.Types {
		for _, profileURL := range et.TargetProfile {
			if rt := resourceTypeOfProfile(reg, profileURL); rt != "" {
				return rt
			}
		}
	}
	return ""
}

func resourceTypeOfProfile(reg *registry.Registry, profileURL string) string {
	if reg == nil || strings.TrimSpace(profileURL) == "" {
		return ""
	}
	resolved, err := reg.ResolveProfile(normalizeCanonical(profileURL))
	if err != nil || resolved == nil {
		return ""
	}
	return resolved.ResourceType
}

// wireCorpusReferences sets each reference field of inst to a pool instance of
// the target type, chosen deterministically from the source id and path so
// references spread across the pool (sharing targets) while staying
// reproducible.
func wireCorpusReferences(inst *model.ResourceInstance, refFields map[string]string, pools map[string][]string) {
	if inst == nil || inst.Resource == nil {
		return
	}
	for path, targetType := range refFields {
		pool := pools[targetType]
		if len(pool) == 0 {
			continue
		}
		idx := int(hashCorpus(inst.LocalID+"|"+path)) % len(pool)
		setReferencePath(inst.Resource, path, refTarget{resourceType: targetType, localID: pool[idx]})
	}
}

func hashCorpus(seed string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	return h.Sum32()
}
