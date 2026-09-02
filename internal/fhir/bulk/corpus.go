package bulk

import (
	"context"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"

	"github.com/jlcoulter/momus/internal/fhir/generation"
	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

// CorpusGenerator produces a realistic corpus of random resources for bulk
// `$export` data. It is a distinct bulk-export generator: unlike the
// coverage-driven data pipeline (internal/test/generation), it synthesises
// random, realistic instances per resource type and links references across
// pools, rather than generating data required by coverage obligations.
type CorpusGenerator struct {
	reg        *registry.Registry
	exhaustive bool
}

// refTarget is a resolved relationship reference: the resource type and the
// local dataset ID it points at.
type refTarget struct {
	resourceType string
	localID      string
}

var abstractResourceTypes = map[string]bool{
	"Resource":          true,
	"DomainResource":    true,
	"CanonicalResource": true,
	"MetadataResource":  true,
}

// NewCorpusGenerator returns a CorpusGenerator backed by reg.
func NewCorpusGenerator(reg *registry.Registry, exhaustive bool) *CorpusGenerator {
	return &CorpusGenerator{reg: reg, exhaustive: exhaustive}
}

// defaultProfile selects the profile used to synthesise a resource type in the
// corpus. It prefers a scoped (package) profile — e.g. hcpd-organization over the
// base FHIR Organization — so package-specific extensions (such as the suppressed
// extension) are exercised. The registry is scoped to the selected package's
// StructureDefinitions, so a profile whose URL is in scope is the profile the
// user is testing against.
func defaultProfile(reg *registry.Registry, resourceType string) string {
	if reg == nil || resourceType == "" {
		return ""
	}
	profiles := reg.ProfilesForResource(resourceType)
	if len(profiles) == 0 {
		return ""
	}
	inScope := make(map[string]bool)
	for _, sd := range reg.ScopedStructureDefinitions() {
		if sd != nil && sd.URL != "" {
			inScope[normalizeCanonical(sd.URL)] = true
		}
	}
	for _, p := range profiles {
		if p == nil || strings.TrimSpace(p.URL) == "" {
			continue
		}
		if inScope[normalizeCanonical(p.URL)] {
			return p.URL
		}
	}
	// Fall back to the first non-empty profile.
	for _, p := range profiles {
		if p != nil && strings.TrimSpace(p.URL) != "" {
			return p.URL
		}
	}
	return ""
}

// normalizeCanonical trims surrounding whitespace from a canonical URL.
func normalizeCanonical(s string) string {
	return strings.TrimSpace(s)
}

// primaryTypeCode returns the first declared datatype code of an element
// definition, or "" when none is declared.
func primaryTypeCode(def *model.ElementDefinition) string {
	if def == nil || len(def.Types) == 0 {
		return ""
	}
	return def.Types[0].Code
}

// GenerateCorpus produces a realistic corpus of instances for each of the
// given resource types. By default defaultCount instances are generated per
// type; overrides, keyed by resource type, take precedence when present.
// References are wired across the generated pools so the resources form a
// sensible, distributed web: dependents are spread over the available targets
// deterministically, so several resources share a common target rather than
// everything pointing at one instance.
func (g *CorpusGenerator) GenerateCorpus(ctx context.Context, resourceTypes []string, defaultCount int, overrides map[string]int) (*model.Dataset, error) {
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
	if len(resourceTypes) == 0 {
		return nil, fmt.Errorf("no concrete resource types to generate")
	}

	// Pass 1: generate instances of each type. Types that cannot be synthesised
	// (e.g. abstract or unresolved profiles) are skipped. Each type is generated
	// in its own goroutine so the whole corpus is synthesised in parallel, and
	// results are fanned-in through a channel and reordered deterministically by
	// the original type index so the dataset (and Pass 2's reference wiring)
	// stays reproducible. The registry is safe for concurrent reads.
	type corpusResult struct {
		index     int
		instances []*model.ResourceInstance
		err       error
	}
	resultCh := make(chan corpusResult, len(resourceTypes))
	for idx, t := range resourceTypes {
		go func(idx int, t string) {
			count := defaultCount
			if c, ok := overrides[t]; ok && c > 0 {
				count = c
			}
			instances := make([]*model.ResourceInstance, 0, count)
			var synthErr error
			for i := 0; i < count; i++ {
				// Embed a hash of the raw resource type so types that sanitize to the
				// same segment (e.g. "A/B" vs "A-B") never collide on local ids.
				id := fmt.Sprintf("momus-%s-%x-%d", sanitizeID(t), hashCorpus(t), i+1)
				// Body synthesis is delegated entirely to the shared generation core
				// (generation.SynthesizeBody), the single source of truth for FHIR
				// resource bodies. The bulk corpus passes no dependency references:
				// references are wired across the generated pools in Pass 2 below.
				profileURL := defaultProfile(g.reg, t)
				var profileURLs []string
				if profileURL != "" {
					profileURLs = []string{profileURL}
					// A profile that resolves to no element tree cannot be synthesised;
					// surface it as an error rather than emitting a bare resource.
					if resolved, err := g.reg.ResolveProfile(profileURL); err != nil || resolved == nil || resolved.Root == nil {
						synthErr = fmt.Errorf("synthesize %s: profile %s has no element tree", t, profileURL)
						break
					}
				}
				body := generation.SynthesizeBody(t, id, profileURLs, profileURL, nil, g.reg, g.exhaustive)
				if len(body) == 0 {
					synthErr = fmt.Errorf("synthesize %s: produced no resource body", t)
					break
				}
				instances = append(instances, &model.ResourceInstance{LocalID: id, ResourceType: t, Profile: profileURL, Resource: body})
			}
			resultCh <- corpusResult{index: idx, instances: instances, err: synthErr}
		}(idx, t)
	}

	// Fan-in the per-type results, then merge them in the original type order so
	// the dataset and reference wiring are deterministic. Cancellation is honoured
	// here: if the context is done we stop waiting on the synthesis goroutines and
	// surface the cancellation rather than blocking on the fan-in.
	results := make([]corpusResult, len(resourceTypes))
	for range resourceTypes {
		select {
		case r := <-resultCh:
			results[r.index] = r
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	var synthErrs []string
	for _, res := range results {
		if res.err != nil {
			synthErrs = append(synthErrs, res.err.Error())
		}
		for _, inst := range res.instances {
			if _, exists := ds.Resources[inst.LocalID]; exists {
				return nil, fmt.Errorf("bulk: duplicate generated id %q", inst.LocalID)
			}
			ds.Resources[inst.LocalID] = inst
			pools[inst.ResourceType] = append(pools[inst.ResourceType], inst.LocalID)
		}
	}
	if len(synthErrs) > 0 {
		return nil, fmt.Errorf("bulk: failed to synthesize some resource types: %s", strings.Join(synthErrs, "; "))
	}

	// Pass 2: wire each resource's reference fields to a distributed target.
	for _, t := range resourceTypes {
		refFields := g.referenceFields(t)
		for _, id := range pools[t] {
			inst := ds.Resources[id]
			ds.Relationships = append(ds.Relationships, wireCorpusReferences(inst, refFields, pools)...)
		}
	}

	return ds, nil
}

// expandReferenceTargets grows the type set to a fixpoint: whenever an included
// type references another type available in the registry, that type is added so
// the generated corpus is self-contained and every reference resolves.
func (g *CorpusGenerator) expandReferenceTargets(resourceTypes []string) []string {
	included := make(map[string]bool, len(resourceTypes))
	concrete := make([]string, 0, len(resourceTypes))
	for _, t := range resourceTypes {
		if !g.hasResourceType(t) {
			continue
		}
		included[t] = true
		concrete = append(concrete, t)
	}
	resourceTypes = concrete
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

func (g *CorpusGenerator) hasResourceType(resourceType string) bool {
	return isConcreteResourceType(resourceType) && g.reg != nil && len(g.reg.ProfilesForResource(resourceType)) > 0
}

func isConcreteResourceType(resourceType string) bool {
	return strings.TrimSpace(resourceType) != "" && !abstractResourceTypes[resourceType]
}

// referenceFields derives the reference element paths of a resource type and
// their target resource types, from the type's resolved profile, falling back
// to example-instance data for fields without a target profile.
func (g *CorpusGenerator) referenceFields(resourceType string) map[string]string {
	out := make(map[string]string)
	if profileURL := defaultProfile(g.reg, resourceType); profileURL != "" {
		if resolved, err := g.reg.ResolveProfile(profileURL); err == nil && resolved != nil && resolved.Root != nil {
			collectReferenceFields(resolved.Root, g.reg, out)
		}
	}
	// Derive reference targets from the package's own example instance data.
	// This is authoritative and self-maintaining — it reflects the real
	// reference relationships of the IG. Only add targets for fields the
	// profile itself did not resolve.
	for path, target := range exampleReferenceTargets(g.reg, resourceType) {
		if _, exists := out[path]; !exists {
			out[path] = target
		}
	}
	return out
}

// exampleReferenceTargets derives reference element paths and their target
// resource types for a resource type by scanning the package's own example
// instance data. Every Reference object in an example of the type contributes
// `resourceType.<leaf> -> targetType`. This keeps the corpus's reference wiring
// aligned with the actual IG rather than a static table.
func exampleReferenceTargets(reg *registry.Registry, resourceType string) map[string]string {
	if reg == nil {
		return nil
	}
	out := make(map[string]string)
	for _, inst := range reg.ResourcesForType(resourceType) {
		if inst == nil || inst.Raw == nil {
			continue
		}
		collectExampleReferenceTargets(inst.Raw, resourceType, out)
	}
	return out
}

// collectExampleReferenceTargets walks a raw example resource, recording every
// Reference object as `resourceType.<leafElement> -> targetType`. The leaf
// element is the final path segment (e.g. "subject", "performer",
// "generalPractitioner"), matching how reference fields are keyed elsewhere.
func collectExampleReferenceTargets(raw map[string]any, resourceType string, out map[string]string) {
	if raw == nil {
		return
	}
	var walk func(v any, leaf string)
	walk = func(v any, leaf string) {
		switch typed := v.(type) {
		case map[string]any:
			// A Reference object.
			if ref, ok := typed["reference"].(string); ok {
				if target, id := splitReference(ref); target != "" && id != "" {
					key := resourceType + "." + leaf
					if _, exists := out[key]; !exists {
						out[key] = target
					}
					// Don't recurse into the reference internals.
					return
				}
			}
			for k, val := range typed {
				walk(val, k)
			}
		case []any:
			for _, item := range typed {
				walk(item, leaf)
			}
		}
	}
	walk(raw, "")
}

// splitReference splits a FHIR reference string into its resource type and id
// (e.g. "Patient/abc" -> ("Patient", "abc")). Returns empty strings for
// non-Type/id references.
func splitReference(ref string) (string, string) {
	parts := strings.SplitN(ref, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", ""
	}
	return parts[0], parts[1]
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
	// A target profile canonical may carry a version suffix (e.g.
	// "…/Organization|4.0.1"); the registry keys profiles by their versionless
	// URL, so strip the suffix before resolving.
	canonical := stripVersion(profileURL)
	resolved, err := reg.ResolveProfile(canonical)
	if err != nil || resolved == nil {
		return ""
	}
	return resolved.ResourceType
}

// stripVersion removes a FHIR canonical version suffix ("|…") from a canonical
// URL.
func stripVersion(canonical string) string {
	if i := strings.Index(canonical, "|"); i >= 0 {
		return canonical[:i]
	}
	return canonical
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

// wireCorpusReferences sets each reference field of inst to a pool instance of
// the target type, chosen deterministically from the source id and path so
// references spread across the pool (sharing targets) while staying
// reproducible.
func wireCorpusReferences(inst *model.ResourceInstance, refFields map[string]string, pools map[string][]string) []model.Reference {
	if inst == nil || inst.Resource == nil {
		return nil
	}
	refs := make([]model.Reference, 0, len(refFields))
	for path, targetType := range refFields {
		pool := pools[targetType]
		if len(pool) == 0 {
			continue
		}
		idx := int(hashCorpus(inst.LocalID+"|"+path)) % len(pool)
		targetID := pool[idx]
		setReferencePath(inst.Resource, path, refTarget{resourceType: targetType, localID: targetID})
		refs = append(refs, model.Reference{SourceID: inst.LocalID, Path: path, TargetID: targetID})
	}
	return refs
}

func hashCorpus(seed string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	return h.Sum32()
}

// setReferencePath places a FHIR reference at an element path, creating
// intermediate containers as needed, so relationship targets are always wired
// into the body even when the element is optional. It walks through both scalar
// containers and repeatable (array) containers without clobbering them: a
// repeatable intermediate is descended into at its first element, and a
// repeatable reference field receives the reference at its first element rather
// than being replaced by a single object (which would produce invalid FHIR).
func setReferencePath(body map[string]any, path string, target refTarget) {
	parts := strings.Split(path, ".")
	if len(parts) <= 1 {
		return
	}
	cur := body
	for i := 1; i < len(parts)-1; i++ {
		cur = descendForReference(cur, parts[i])
		if cur == nil {
			return
		}
	}
	setReferenceLeaf(cur, parts[len(parts)-1], target)
}

// descendForReference returns the map to continue wiring into for the given
// segment, descending through a scalar map or into the first element of a
// repeatable (array) container instead of overwriting it.
func descendForReference(parent map[string]any, key string) map[string]any {
	switch v := parent[key].(type) {
	case map[string]any:
		return v
	case []any:
		if len(v) == 0 {
			child := map[string]any{}
			parent[key] = []any{child}
			return child
		}
		if child, ok := v[0].(map[string]any); ok {
			return child
		}
		child := map[string]any{}
		v[0] = child
		return child
	default:
		child := map[string]any{}
		parent[key] = child
		return child
	}
}

// setReferenceLeaf sets the reference value at the leaf of a path, preserving
// an existing scalar object or descending into the first element of a repeatable
// (array) reference field.
func setReferenceLeaf(obj map[string]any, key string, target refTarget) {
	ref := target.resourceType + "/" + target.localID
	switch v := obj[key].(type) {
	case map[string]any:
		v["reference"] = ref
	case []any:
		if len(v) == 0 {
			obj[key] = []any{map[string]any{"reference": ref}}
			return
		}
		if el, ok := v[0].(map[string]any); ok {
			el["reference"] = ref
			return
		}
		v[0] = map[string]any{"reference": ref}
	default:
		obj[key] = map[string]any{"reference": ref}
	}
}
