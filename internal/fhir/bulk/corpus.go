package bulk

import (
	"context"
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"sync"

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

// refFieldInfo describes a reference element: its target resource type, whether
// it is repeatable (Max > 1), and whether it is required (Min >= 1). repeatable
// controls emitting the reference as an array; required controls whether a
// forward reference may be stripped (optional) or must be resolved later.
type refFieldInfo struct {
	targetType string
	repeatable bool
	required   bool
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

// elementAllowsMultiple reports whether an element may appear more than once
// (its cardinality max is "*" or greater than 1), so repeatable reference
// fields are emitted as arrays.
func elementAllowsMultiple(def *model.ElementDefinition) bool {
	if def == nil {
		return false
	}
	return allowsMultiple(def.Max) || allowsMultiple(def.BaseMax)
}

func allowsMultiple(maxValue string) bool {
	if maxValue == "*" {
		return true
	}
	n, err := strconv.Atoi(maxValue)
	if err != nil {
		return false
	}
	return n > 1
}

// elementRequired reports whether an element must appear at least once (Min >= 1).
func elementRequired(def *model.ElementDefinition) bool {
	if def == nil {
		return false
	}
	return def.Min >= 1
}

// GenerateCorpus produces a realistic corpus of instances for each of the
// given resource types. By default defaultCount instances are generated per
// type; overrides, keyed by resource type, take precedence when present.
// References are wired across the generated pools so the resources form a
// sensible, distributed web: dependents are spread over the available targets
// deterministically, so several resources share a common target rather than
// everything pointing at one instance.
//
// It is a convenience wrapper over GenerateCorpusBatched that accumulates every
// emitted batch into a single in-memory Dataset. For large corpora prefer
// GenerateCorpusBatched, which streams each type's batch to a callback so
// provisioning can begin before the whole corpus is generated.
func (g *CorpusGenerator) GenerateCorpus(ctx context.Context, resourceTypes []string, defaultCount int, overrides map[string]int) (*model.Dataset, error) {
	ds := &model.Dataset{Resources: make(map[string]*model.ResourceInstance), Relationships: []model.Reference{}}
	err := g.GenerateCorpusBatched(ctx, resourceTypes, defaultCount, overrides, func(b CorpusBatch) error {
		for _, inst := range b.Instances {
			if _, exists := ds.Resources[inst.LocalID]; exists {
				return fmt.Errorf("bulk: duplicate generated id %q", inst.LocalID)
			}
			ds.Resources[inst.LocalID] = inst
		}
		ds.Relationships = append(ds.Relationships, b.Relationships...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ds, nil
}

// CorpusBatch is a batch of generated instances of a single resource type,
// ready to be provisioned. Every reference points to a resource already emitted
// (an earlier batch), so the batch can be provisioned immediately.
//
// Finalize is true for batches from the finalization pass: these re-emit
// instances that carried a required forward reference, now re-wired against the
// complete pool. The caller should re-provision them (a PUT updates the server)
// but not re-write them to NDJSON, since they were already written.
type CorpusBatch struct {
	ResourceType  string
	Instances     []*model.ResourceInstance
	Relationships []model.Reference
	Finalize      bool
}

// GenerateCorpusBatched generates the corpus and invokes fn for each resource
// type's batch in topological order (targets before dependents), so the caller
// can provision each batch as it is ready rather than holding the whole corpus
// in memory. Memory use is bounded by the largest single type's batch plus the
// pool of already-emitted local IDs (not the full resource bodies).
//
// Reference wiring is incremental: a resource's references only point to types
// already emitted, or to earlier instances of its own type, so every reference
// resolves to a resource that will already exist on the server when the batch
// is provisioned. Required (Min >= 1) forward references — which only arise
// inside reference cycles — are preserved as dangling placeholders in the first
// batch, then re-wired against the complete pool and re-emitted in a finalization
// pass so the caller can re-provision them once their targets exist.
func (g *CorpusGenerator) GenerateCorpusBatched(ctx context.Context, resourceTypes []string, defaultCount int, overrides map[string]int, fn func(CorpusBatch) error) error {
	if g.reg == nil {
		return fmt.Errorf("generator requires a registry")
	}
	if defaultCount < 1 {
		defaultCount = 1
	}
	if len(resourceTypes) == 0 {
		return fmt.Errorf("no resource types to generate")
	}
	if fn == nil {
		return fmt.Errorf("bulk: nil batch callback")
	}

	// Expand the type set to include referenced target types (e.g. including
	// HealthcareService pulls in Organization, Practitioner, Location) so that
	// every reference resolves even when the caller requested a subset.
	resourceTypes = g.expandReferenceTargets(resourceTypes)
	if len(resourceTypes) == 0 {
		return fmt.Errorf("no concrete resource types to generate")
	}

	// availablePools maps resource type -> ordered local IDs already emitted.
	// It grows as batches are emitted, so later batches can only reference
	// resources that have already been provisioned.
	availablePools := make(map[string][]string)
	// requiredDangling tracks instances that still carry a required forward
	// reference (a dangling placeholder) after their first batch, keyed by the
	// target type they need. They are re-wired and re-emitted in the
	// finalization pass once that target type has been emitted.
	requiredDangling := make(map[string][]*model.ResourceInstance)

	for _, t := range resourceTypes {
		instances, err := g.synthesizeType(ctx, t, defaultCount, overrides)
		if err != nil {
			return err
		}
		refFields := g.referenceFields(t)
		batch := CorpusBatch{ResourceType: t, Instances: instances}
		for _, inst := range instances {
			// Per-instance available pools: cross-type refs may target any
			// instance of an already-created type; same-type refs may only
			// target instances of this type already emitted (earlier in the
			// stream), so a resource never references a not-yet-emitted peer.
			instPools := make(map[string][]string, len(availablePools)+1)
			for k, v := range availablePools {
				instPools[k] = v
			}
			instPools[t] = availablePools[t]
			refs := wireCorpusReferences(inst, refFields, instPools)
			batch.Relationships = append(batch.Relationships, refs...)
			// Strip optional dangling references; preserve required ones for
			// the finalization pass.
			if stripDanglingReferences(inst.Resource, refFields) {
				requiredDangling[t] = append(requiredDangling[t], inst)
			}
			availablePools[t] = append(availablePools[t], inst.LocalID)
		}
		if err := fn(batch); err != nil {
			return err
		}
	}

	// Finalization pass: re-wire instances that carried a required forward
	// reference now that every type has been emitted, and emit them again so
	// the caller can re-provision them against their now-existing targets.
	for _, t := range resourceTypes {
		instances := requiredDangling[t]
		if len(instances) == 0 {
			continue
		}
		refFields := g.referenceFields(t)
		batch := CorpusBatch{ResourceType: t, Instances: instances, Finalize: true}
		for _, inst := range instances {
			// The full pool is now available, so every reference resolves.
			refs := wireCorpusReferences(inst, refFields, availablePools)
			batch.Relationships = append(batch.Relationships, refs...)
			// Any reference still dangling after the full pool is available is
			// genuinely unresolvable (e.g. a self-reference with no peer); strip
			// it. Required fields that cannot resolve are left as-is rather than
			// silently dropped, so the caller can decide how to handle them.
			stripDanglingReferences(inst.Resource, nil)
		}
		if err := fn(batch); err != nil {
			return err
		}
	}

	return nil
}

// synthesizeType generates count instances of a single resource type in
// parallel, returning them in deterministic order. Un-synthesizable types
// (abstract or unresolved profiles) surface an error. Cancellation is honoured.
func (g *CorpusGenerator) synthesizeType(ctx context.Context, t string, defaultCount int, overrides map[string]int) ([]*model.ResourceInstance, error) {
	count := defaultCount
	if c, ok := overrides[t]; ok && c > 0 {
		count = c
	}
	instances := make([]*model.ResourceInstance, count)
	errs := make([]error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			select {
			case <-ctx.Done():
				errs[i] = ctx.Err()
				return
			default:
			}
			// Embed a hash of the raw resource type so types that sanitize to the
			// same segment (e.g. "A/B" vs "A-B") never collide on local ids.
			id := fmt.Sprintf("momus-%s-%x-%d", sanitizeID(t), hashCorpus(t), i+1)
			// Body synthesis is delegated entirely to the shared generation core
			// (generation.SynthesizeBody), the single source of truth for FHIR
			// resource bodies. The bulk corpus passes no dependency references:
			// references are wired across the generated pools by the caller.
			profileURL := defaultProfile(g.reg, t)
			var profileURLs []string
			if profileURL != "" {
				profileURLs = []string{profileURL}
				// A profile that resolves to no element tree cannot be synthesised;
				// surface it as an error rather than emitting a bare resource.
				if resolved, err := g.reg.ResolveProfile(profileURL); err != nil || resolved == nil || resolved.Root == nil {
					errs[i] = fmt.Errorf("synthesize %s: profile %s has no element tree", t, profileURL)
					return
				}
			}
			body := generation.SynthesizeBody(t, id, profileURLs, profileURL, nil, g.reg, g.exhaustive)
			if len(body) == 0 {
				errs[i] = fmt.Errorf("synthesize %s: produced no resource body", t)
				return
			}
			instances[i] = &model.ResourceInstance{LocalID: id, ResourceType: t, Profile: profileURL, Resource: body}
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return instances, nil
}

// expandReferenceTargets grows the type set to a fixpoint: whenever an included
// type references another type available in the registry, that type is added so
// the generated corpus is self-contained and every reference resolves. The
// result is ordered topologically (targets before dependents) so that when
// references are wired incrementally, every wired reference points to a type
// that has already been created. Cycles (e.g. Organization.partOf → Organization)
// are broken deterministically: same-type forward references are stripped during
// wiring, so cycle members may be emitted in any stable order.
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
			for _, info := range g.referenceFields(t) {
				if !included[info.targetType] && g.hasResourceType(info.targetType) {
					included[info.targetType] = true
					resourceTypes = append(resourceTypes, info.targetType)
					changed = true
				}
			}
		}
	}
	return topologicalTypeOrder(resourceTypes, g)
}

// topologicalTypeOrder orders resource types so that a type appears after every
// type it references (its targets). This lets reference wiring only point at
// already-created types. Self-references and mutual cycles are broken by falling
// back to a stable (sorted) order for the remaining cycle members.
func topologicalTypeOrder(resourceTypes []string, g *CorpusGenerator) []string {
	included := make(map[string]bool, len(resourceTypes))
	for _, t := range resourceTypes {
		included[t] = true
	}
	// deps[t] = set of types t references (that are in the corpus).
	deps := make(map[string]map[string]bool, len(resourceTypes))
	for _, t := range resourceTypes {
		deps[t] = make(map[string]bool)
		for _, info := range g.referenceFields(t) {
			if info.targetType == t {
				// A self-reference (e.g. Location.partOf → Location) is not a
				// dependency on another type: same-type forward references are
				// resolved by wiring to earlier instances and stripping danglers.
				// Counting them as dependencies left the type permanently unready,
				// deferring it to the cycle breaker and emitting dependents
				// (HealthcareService) before their targets (Organization, Location).
				continue
			}
			if included[info.targetType] {
				deps[t][info.targetType] = true
			}
		}
	}

	// Kahn's algorithm: emit types with no remaining dependencies first.
	remaining := make(map[string]bool, len(resourceTypes))
	for _, t := range resourceTypes {
		remaining[t] = true
	}
	var order []string
	for len(remaining) > 0 {
		ready := make([]string, 0)
		for t := range remaining {
			if len(deps[t]) == 0 {
				ready = append(ready, t)
			}
		}
		if len(ready) == 0 {
			// Cycle: emit the smallest remaining type to break it deterministically.
			smallest := ""
			for t := range remaining {
				if smallest == "" || t < smallest {
					smallest = t
				}
			}
			ready = []string{smallest}
		}
		sort.Strings(ready)
		for _, t := range ready {
			order = append(order, t)
			delete(remaining, t)
			for s := range remaining {
				delete(deps[s], t)
			}
		}
	}
	return order
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
func (g *CorpusGenerator) referenceFields(resourceType string) map[string]refFieldInfo {
	out := make(map[string]refFieldInfo)
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
			out[path] = refFieldInfo{targetType: target}
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
// Reference object as `resourceType.<fullPath> -> targetType`. The path is the
// full dot-separated element path from the resource root (e.g.
// "Provenance.entity.what", "Observation.performer"), matching how reference
// fields are keyed and wired elsewhere, so nested references are placed inside
// their backbone elements rather than leaking to the resource root.
func collectExampleReferenceTargets(raw map[string]any, resourceType string, out map[string]string) {
	if raw == nil {
		return
	}
	var walk func(v any, path string)
	walk = func(v any, path string) {
		switch typed := v.(type) {
		case map[string]any:
			// A Reference object.
			if ref, ok := typed["reference"].(string); ok {
				if target, id := splitReference(ref); target != "" && id != "" {
					key := resourceType + "." + path
					if _, exists := out[key]; !exists {
						out[key] = target
					}
					// Don't recurse into the reference internals.
					return
				}
			}
			for k, val := range typed {
				next := path
				if next == "" {
					next = k
				} else {
					next = next + "." + k
				}
				walk(val, next)
			}
		case []any:
			for _, item := range typed {
				walk(item, path)
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
func collectReferenceFields(node *model.ElementNode, reg *registry.Registry, out map[string]refFieldInfo) {
	if node == nil {
		return
	}
	if node.Definition != nil && primaryTypeCode(node.Definition) == "Reference" {
		if target := referenceTargetType(node.Definition, reg); target != "" {
			out[node.Path] = refFieldInfo{
				targetType: target,
				repeatable: elementAllowsMultiple(node.Definition),
				required:   elementRequired(node.Definition),
			}
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
func wireCorpusReferences(inst *model.ResourceInstance, refFields map[string]refFieldInfo, pools map[string][]string) []model.Reference {
	if inst == nil || inst.Resource == nil {
		return nil
	}
	refs := make([]model.Reference, 0, len(refFields))
	for path, info := range refFields {
		pool := pools[info.targetType]
		if len(pool) == 0 {
			continue
		}
		// A same-type reference (e.g. Location.partOf → Location, an element of
		// the same resource type) is wired to the root of that type's emitted
		// pool rather than a hash-spread peer. Spreading same-type references
		// across the pool tends to build deep chains (Location-N → Location-(N-1)
		// → … → Location-1), so a single failure at any link cascades HAPI-1094
		// "not found" to every later member. Pointing every member at the root
		// keeps the same-type dependency graph shallow and resilient: the whole
		// sub-tree is provisioned as soon as the root exists, and a failure only
		// affects the members that reference the failed root.
		var idx int
		if info.targetType == inst.ResourceType {
			idx = 0
		} else {
			idx = int(hashCorpus(inst.LocalID+"|"+path)) % len(pool)
		}
		targetID := pool[idx]
		setReferencePath(inst.Resource, path, refTarget{resourceType: info.targetType, localID: targetID}, info.repeatable)
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
func setReferencePath(body map[string]any, path string, target refTarget, repeatable bool) {
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
	setReferenceLeaf(cur, parts[len(parts)-1], target, repeatable)
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
// (array) reference field. When the field is absent and it is repeatable
// (Max > 1), a single-element array is created to satisfy cardinality; when it
// is singular, a scalar object is created.
func setReferenceLeaf(obj map[string]any, key string, target refTarget, repeatable bool) {
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
		if repeatable {
			obj[key] = []any{map[string]any{"reference": ref}}
		} else {
			obj[key] = map[string]any{"reference": ref}
		}
	}
}

// stripDanglingReferences removes dangling reference placeholders (Type/unknown
// or Type/momus-setup-*), which arise from forward references to types not yet
// created. Optional (Min=0) dangling references are removed; required (Min>=1)
// ones are preserved for later resolution (see GenerateCorpusBatched's
// finalization pass). It returns true if any required reference was preserved.
// When refFields is nil, all dangling references are stripped.
//
// Repeatable reference fields are filtered element-wise: a dangling placeholder
// is removed from its array (or the whole array removed when no real reference
// remains) rather than the array surviving intact.
func stripDanglingReferences(value any, refFields map[string]refFieldInfo) bool {
	preserved := false
	// Seed the element path with the resource type so it matches the full
	// dot-separated paths used to key refFields (e.g. "Organization.endpoint").
	root := ""
	if m, ok := value.(map[string]any); ok {
		if rt, ok := m["resourceType"].(string); ok {
			root = rt
		}
	}
	stripDanglingAt(value, root, refFields, &preserved)
	return preserved
}

// stripDanglingAt walks value, removing dangling reference placeholders at or
// below path. Optional (Min=0) placeholders are deleted; required (Min>=1) ones
// are kept and reported through preserved. Reference arrays are filtered
// element-wise in the map case, where the parent map is available for
// reassignment (filtering a slice in place would not update the parent's slice
// header).
func stripDanglingAt(value any, path string, refFields map[string]refFieldInfo, preserved *bool) {
	switch typed := value.(type) {
	case map[string]any:
		for k, v := range typed {
			childPath := k
			if path != "" {
				childPath = path + "." + k
			}
			if arr, ok := v.([]any); ok {
				// Repeatable reference field: drop dangling placeholder elements,
				// keep real references, and drop the key entirely when nothing real
				// remains. Optional (Min=0) placeholders are removed; required
				// (Min>=1) ones are preserved for later resolution.
				kept := make([]any, 0, len(arr))
				for _, item := range arr {
					if isDanglingReferenceValue(item) {
						if refFields != nil && refFields[childPath].required {
							*preserved = true
							kept = append(kept, item)
						}
						continue
					}
					kept = append(kept, item)
				}
				if len(kept) != len(arr) {
					if len(kept) == 0 {
						delete(typed, k)
					} else {
						typed[k] = kept
					}
				}
				for _, item := range kept {
					stripDanglingAt(item, childPath, refFields, preserved)
				}
				continue
			}
			if isDanglingReferenceValue(v) {
				if refFields != nil && refFields[childPath].required {
					// Preserve a required forward reference for later resolution.
					*preserved = true
					continue
				}
				delete(typed, k)
				continue
			}
			stripDanglingAt(v, childPath, refFields, preserved)
		}
	case []any:
		for _, item := range typed {
			stripDanglingAt(item, path, refFields, preserved)
		}
	}
}

// isDanglingReferenceValue reports whether a value is a reference object (or a
// single-element array of one) whose reference string is still a dangling
// placeholder.
func isDanglingReferenceValue(v any) bool {
	switch t := v.(type) {
	case map[string]any:
		ref, _ := t["reference"].(string)
		return danglingRef.MatchString(ref)
	case []any:
		if len(t) == 1 {
			if m, ok := t[0].(map[string]any); ok {
				ref, _ := m["reference"].(string)
				return danglingRef.MatchString(ref)
			}
		}
	}
	return false
}
