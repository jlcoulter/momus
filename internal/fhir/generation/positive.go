package generation

import (
	"errors"
	"fmt"
	"hash/fnv"
	"math/rand"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/jlcoulter/momus/internal/core/coverage"
	coregen "github.com/jlcoulter/momus/internal/core/generation"
	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

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

func elementAllowsMultiple(def *model.ElementDefinition) bool {
	if def == nil {
		return false
	}
	if allowsMultiple(def.Max) {
		return true
	}
	return allowsMultiple(def.BaseMax)
}

// optionalInclusionProbability is the chance that an optional (Min == 0)
// element is included when generating exhaustive payloads, so presence varies
// realistically across requests.
const optionalInclusionProbability = 0.5

// newRNG returns a deterministic random source seeded from seedString, so the
// same input produces the same payload while different requests vary.
func newRNG(seedString string) *rand.Rand {
	h := fnv.New32a()
	_, _ = h.Write([]byte(seedString))
	return rand.New(rand.NewSource(int64(h.Sum32())))
}

// BuildOptions controls AST construction behavior.
// (defined in options.go)

// GenerateFromCoveragePlan maps coverage requirements into a concrete AST.
func buildSetupResource(resourceType string, options BuildOptions, deps []string, byResource map[string][]coverage.CoverageRequirement) *model.ResourceInstance {
	resourceProfiles := coregen.UniqueProfileURLs(byResource[resourceType])
	setupProfileURL := ""
	if len(resourceProfiles) > 0 {
		setupProfileURL = resourceProfiles[0]
	}
	setupProfiles := coregen.OrderedProfilesForResource(resourceType, setupProfileURL, options.PreferredProfileURLsByResource)
	setupPrimaryProfile := coregen.FirstProfileURL(setupProfiles)
	// Capability-gated: only seed a resource whose selected profile the server
	// declares, so we never provision something the server cannot validate.
	if options.CapabilityProfiles != nil && setupPrimaryProfile != "" {
		if _, ok := options.CapabilityProfiles[setupPrimaryProfile]; !ok {
			return nil
		}
	}
	id := coregen.SetupResourceID(resourceType)
	body := buildSetupBody(resourceType, id, setupProfiles, setupPrimaryProfile, deps, options.Registry, options.Exhaustive)
	return &model.ResourceInstance{
		LocalID:      id,
		ResourceType: resourceType,
		Profile:      setupPrimaryProfile,
		Resource:     body,
	}
}

// BuildSetupDataset builds the Dataset of seed resources that the generated
// AST provisions ahead of execution. It uses the exact same body-generation
// logic as GenerateFromCoveragePlan's setup requests, so the provisioned data
// conforms to the same profiles and is what the generated tests reference.
// Relationships are recorded so provisioning orders targets before dependents.
func BuildSetupDataset(plan *coverage.CoveragePlan, options BuildOptions) (*model.Dataset, error) {
	if plan == nil {
		return nil, errors.New("coverage plan is required")
	}
	depPlan, err := buildDependencyPlan(plan, options.CapabilityResourceTypes, options.Registry)
	if err != nil {
		return nil, err
	}
	byResource := make(map[string][]coverage.CoverageRequirement)
	for _, req := range plan.Requirements {
		if req.ResourceType == "" {
			return nil, fmt.Errorf("coverage requirement %s missing resource type", req.ID)
		}
		byResource[req.ResourceType] = append(byResource[req.ResourceType], req)
	}
	for resourceType := range byResource {
		sort.Slice(byResource[resourceType], func(i, j int) bool {
			return byResource[resourceType][i].ID < byResource[resourceType][j].ID
		})
	}

	ds := &model.Dataset{
		Resources:     make(map[string]*model.ResourceInstance),
		Relationships: make([]model.Reference, 0),
	}
	for _, level := range depPlan.Levels {
		for _, resourceType := range level {
			// Abstract base types (Resource, DomainResource, ...) cannot be
			// instantiated and must never be provisioned.
			if isAbstractResourceType(resourceType) {
				continue
			}
			// When a capability scope is set, only seed resource types the server
			// declares, so provisioning never sends an unsupported type.
			if options.CapabilityResourceTypes != nil {
				if _, ok := options.CapabilityResourceTypes[resourceType]; !ok {
					continue
				}
			}
			deps := depPlan.Dependencies[resourceType]
			inst := buildSetupResource(resourceType, options, deps, byResource)
			if inst == nil {
				// Gated out by the capability scope (unsupported type or profile).
				continue
			}
			ds.Resources[inst.LocalID] = inst
		}
	}

	// Search-accept obligations need data that matches the query to be
	// meaningful (and to satisfy multiple-results). Add those matching seeds so
	// the provisioned dataset carries the critical data each search test needs.
	for _, req := range plan.Requirements {
		appendSearchSeedResources(ds, req, options, byResource)
	}

	// Record relationships from the actual generated resource bodies so
	// provisioning orders targets before dependents. The relationship graph must
	// reflect only references that actually appear in a resource body: the
	// dependency plan lists every declared reference target, but a setup resource
	// may not populate an optional reference, so recording those edges creates
	// false dependencies and, worse, dependency cycles (e.g. Organization lists
	// Endpoint as a target while Endpoint lists Organization, even though the
	// bodies reference only one direction). A cyclic graph defeats the
	// provisioner's topological sort, which then falls back to alphabetical ID
	// order and creates dependents before their targets, failing with HAPI-1094
	// "not found". Scanning the bodies keeps the graph acyclic and correct.
	recordBodyReferences(ds)
	return ds, nil
}

// recordBodyReferences scans every resource body in ds for "reference" fields
// of the form "<Type>/<id>" and records a Relationship for each one whose target
// is a resource present in the dataset. This makes provisioning order targets
// before dependents for references the dependency plan did not model.
func recordBodyReferences(ds *model.Dataset) {
	if ds == nil {
		return
	}
	typeByLocalID := make(map[string]string, len(ds.Resources))
	for _, inst := range ds.Resources {
		if inst != nil && inst.LocalID != "" {
			typeByLocalID[inst.LocalID] = inst.ResourceType
		}
	}

	seen := make(map[string]struct{})
	// Iterate resources in deterministic LocalID order so the relationship list is
	// stable regardless of map iteration order.
	instances := make([]*model.ResourceInstance, 0, len(ds.Resources))
	for _, inst := range ds.Resources {
		instances = append(instances, inst)
	}
	sort.Slice(instances, func(i, j int) bool {
		return instances[i].LocalID < instances[j].LocalID
	})
	for _, inst := range instances {
		if inst == nil || inst.Resource == nil {
			continue
		}
		walkBodyRefs(inst, inst.Resource, typeByLocalID, ds, seen, "")
	}
}

// walkBodyRefs recursively descends a resource body, recording a
// relationship for every "reference" value of the form "<Type>/<id>" whose
// target exists in the dataset. path is the dotted path accumulated so far.
func walkBodyRefs(inst *model.ResourceInstance, node any, typeByLocalID map[string]string, ds *model.Dataset, seen map[string]struct{}, path string) {
	switch v := node.(type) {
	case map[string]any:
		for key, val := range v {
			childPath := key
			if path != "" {
				childPath = path + "." + key
			}
			if key == "reference" {
				if ref, ok := val.(string); ok {
					targetID := referenceTargetID(ref)
					if targetID != "" && targetID != inst.LocalID {
						if targetType, ok := typeByLocalID[targetID]; ok && targetType != "" {
							edge := inst.LocalID + "\x00" + targetID + "\x00" + childPath
							if _, dup := seen[edge]; !dup {
								seen[edge] = struct{}{}
								ds.Relationships = append(ds.Relationships, model.Reference{
									SourceID: inst.LocalID,
									Path:     childPath,
									TargetID: targetID,
								})
							}
						}
					}
				}
				continue
			}
			walkBodyRefs(inst, val, typeByLocalID, ds, seen, childPath)
		}
	case []any:
		for _, el := range v {
			walkBodyRefs(inst, el, typeByLocalID, ds, seen, path)
		}
	}
}

// referenceTargetID parses a FHIR reference string "<Type>/<id>" and returns
// the id portion, or "" when the reference is not an absolute local reference
// (e.g. "#fragment", "http://...", or just an id).
func referenceTargetID(ref string) string {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return ""
	}
	slash := strings.Index(trimmed, "/")
	if slash <= 0 || slash == len(trimmed)-1 {
		return ""
	}
	if strings.ContainsAny(trimmed, ":#?") {
		return ""
	}
	return trimmed[slash+1:]
}

// RequirementCount returns the number of requirement-bound Assertions in a
// generated plan, excluding setup scaffolding.
func buildBodyTemplate(req coverage.CoverageRequirement, id string, profileURLs []string, primaryProfileURL string, deps []string, reg *registry.Registry, exhaustive bool) (map[string]any, bool) {
	// A test payload is the same registry-driven body synthesis as every other
	// resource, with an optional negative mutation applied only when the test
	// expects the server to reject it. The bool reports whether the mutation
	// produced a concrete violation (false when the target element is absent).
	body := synthesizeBody(req.ResourceType, id, profileURLs, primaryProfileURL, deps, reg, exhaustive)
	applied := applyNegativeMutation(body, req, reg)
	return body, applied
}

func buildSetupBody(resourceType, id string, profileURLs []string, primaryProfileURL string, deps []string, reg *registry.Registry, exhaustive bool) map[string]any {
	return synthesizeBody(resourceType, id, profileURLs, primaryProfileURL, deps, reg, exhaustive)
}

// synthesizeBody is the single registry-driven body-data core used for all
// generated data — provisioned seed resources and test-case payloads alike. It
// depends on the registry as the source of truth: it walks the resolved profile
// to populate required (and, when exhaustive, optional) elements, resolves
// bindings to real codes, and applies resource-specific normalisation. Keeping
// one core means test data and provisioned data cannot drift apart.
func synthesizeBody(resourceType, id string, profileURLs []string, primaryProfileURL string, deps []string, reg *registry.Registry, exhaustive bool) map[string]any {
	body := baseBodyTemplate(resourceType, id, profileURLs, deps, reg, primaryProfileURL)
	enrichBodyFromProfile(body, primaryProfileURL, reg)
	if exhaustive {
		enrichBodyExhaustive(body, primaryProfileURL, reg, newRNG(id))
	}
	// A resource must never reference itself: HAPI validates referential
	// integrity at create time, so a self-reference (e.g. Location.partOf ->
	// Location/momus-setup-location on the setup Location) is rejected before
	// the resource exists. Strip any Reference object or array element whose
	// reference equals the resource's own logical reference.
	stripSelfReferences(body, resourceType+"/"+id)
	normalizeGeneratedPayload(body)
	normalizeResourceSpecificPayload(body)
	// Final pass: resolve any coding display that is missing or echoes its code
	// to the canonical CodeSystem display. This covers every generation path
	// (datatype profiles, slice patterns, bound codings), not just slice
	// constraints, so e.g. an identifier type coding with code "XX" gets the
	// canonical "Organization identifier" display instead of echoing "XX".
	normalisePayloadCodingDisplays(body, reg)
	// Remove the internal fixed-coding markers so they never reach the payload
	// that is serialised and uploaded.
	stripFixedCodingMarkers(body)
	// Drop any Extension that ended up with neither a value[x] nor a nested
	// sub-extension: such an extension violates ext-1 and is rejected by HAPI.
	// Simple extensions are populated with a value earlier; complex ones whose
	// optional sub-slices were not generated are simply omitted.
	stripEmptyExtensions(body)
	return body
}

// NormalizeGeneratedResource applies the same post-generation normalisation
// passes that synthesizeBody applies to seed data and test-case payloads, so
// resources produced by other generators (e.g. the bulk corpus) conform to the
// same profiles and pass server validation. It strips self-references, resolves
// coding displays, removes internal fixed-coding markers, applies
// resource-specific normalisation, and drops empty extensions.
func NormalizeGeneratedResource(body map[string]any, resourceType, id string, reg *registry.Registry) {
	if body == nil {
		return
	}
	stripSelfReferences(body, resourceType+"/"+id)
	normalizeGeneratedPayload(body)
	normalizeResourceSpecificPayload(body)
	normalisePayloadCodingDisplays(body, reg)
	stripFixedCodingMarkers(body)
	stripEmptyExtensions(body)
}

// stripSelfReferences removes any FHIR Reference object (a map containing a
// "reference" key) whose reference equals selfRef, deleting the enclosing
// property (for object-valued references) or the array element (for arrays of
// references). It walks the payload recursively. Optional self-referencing
// elements (e.g. Location.partOf) become absent, which is valid; required
// self-references are pathological and would be rejected by the server
// regardless.
func stripSelfReferences(v any, selfRef string) {
	if selfRef == "" {
		return
	}
	switch t := v.(type) {
	case map[string]any:
		for key, val := range t {
			if refObj, ok := val.(map[string]any); ok && referenceIs(refObj, selfRef) {
				delete(t, key)
				continue
			}
			if arr, ok := val.([]any); ok {
				t[key] = filterSelfReferences(arr, selfRef)
				stripSelfReferences(t[key], selfRef)
				continue
			}
			stripSelfReferences(val, selfRef)
		}
	case []any:
		filtered := filterSelfReferences(t, selfRef)
		for _, el := range filtered {
			stripSelfReferences(el, selfRef)
		}
	}
}

// filterSelfReferences drops array elements that are Reference objects pointing
// at selfRef, returning the filtered slice.
func filterSelfReferences(arr []any, selfRef string) []any {
	out := arr[:0]
	for _, el := range arr {
		if refObj, ok := el.(map[string]any); ok && referenceIs(refObj, selfRef) {
			continue
		}
		out = append(out, el)
	}
	return out
}

// referenceIs reports whether m is a FHIR Reference object whose "reference"
// field equals ref.
func referenceIs(m map[string]any, ref string) bool {
	if m == nil {
		return false
	}
	r, ok := m["reference"].(string)
	return ok && r == ref
}

// enrichBodyExhaustive populates optional elements of the resolved profile
// into body, with randomised presence, so generated payloads include every
// parameter with a realistic value rather than only required elements.
func enrichBodyExhaustive(body map[string]any, profileURL string, reg *registry.Registry, rng *rand.Rand) {
	if reg == nil || strings.TrimSpace(profileURL) == "" {
		return
	}
	resolved, err := reg.ResolveProfile(profileURL)
	if err != nil || resolved == nil || resolved.Root == nil {
		return
	}
	populateOptionalChildren(body, resolved.Root, reg, rng)
	applySimpleConstraints(body, resolved.Root, reg)
	normalizeRepeatableChildren(body, resolved.Root)
}

// populateOptionalChildren adds optional (Min == 0) children that are not
// already present, randomised by rng, and recurses into existing complex
// values so their optional children are populated too.
func populateOptionalChildren(value map[string]any, node *model.ElementNode, reg *registry.Registry, rng *rand.Rand) {
	if value == nil || node == nil {
		return
	}
	childNames := make([]string, 0, len(node.Children))
	for name := range node.Children {
		childNames = append(childNames, name)
	}
	sort.Strings(childNames)
	for _, name := range childNames {
		child := node.Children[name]
		if child == nil {
			continue
		}
		if child.Definition == nil {
			// Intermediate or choice node: recurse into any existing container.
			if raw, ok := value[name]; ok {
				recurseExhaustive(raw, child, reg, rng)
			}
			continue
		}
		propName := propertyNameForNode(child)
		if propName == "" || propName == "id" {
			// Skip the resource/element id: ids are assigned by the generator or
			// the target server and must not be synthesised.
			continue
		}
		optional := child.Definition.Min <= 0 && !hasRequiredSlices(child) && !hasContractSignal(child)
		if !optional {
			if raw, ok := value[propName]; ok {
				recurseExhaustive(raw, child, reg, rng)
			}
			continue
		}
		if _, exists := value[propName]; exists {
			recurseExhaustive(value[propName], child, reg, rng)
			continue
		}
		if rng != nil && rng.Float64() > optionalInclusionProbability {
			continue
		}
		if generated, ok := generateRequiredValue(child, reg, rng); ok {
			value[propName] = generated
		}
	}
}

// recurseExhaustive walks an existing value to populate optional children of
// nested complex datatypes.
func recurseExhaustive(raw any, node *model.ElementNode, reg *registry.Registry, rng *rand.Rand) {
	switch typed := raw.(type) {
	case map[string]any:
		populateOptionalChildren(typed, node, reg, rng)
	case []any:
		for _, item := range typed {
			if itemMap, ok := item.(map[string]any); ok {
				populateOptionalChildren(itemMap, node, reg, rng)
			}
		}
	}
}

func baseBodyTemplate(resourceType, id string, profileURLs, deps []string, reg *registry.Registry, primaryProfileURL string) map[string]any {
	body := map[string]any{
		"resourceType": resourceType,
		"id":           id,
	}
	if meta := coregen.BuildMeta(profileURLs); meta != nil {
		body["meta"] = meta
	}

	attachDependencyReferences(body, resourceType, primaryProfileURL, deps, reg)
	return body
}

func enrichBodyFromProfile(body map[string]any, profileURL string, reg *registry.Registry) {
	if reg == nil || strings.TrimSpace(profileURL) == "" {
		return
	}
	resolved, err := reg.ResolveProfile(profileURL)
	if err != nil || resolved == nil || resolved.Root == nil {
		return
	}
	populateRequiredChildren(body, resolved.Root, reg)
	applySimpleConstraints(body, resolved.Root, reg)
	normalizeRepeatableChildren(body, resolved.Root)
}

func normalizeRepeatableChildren(value map[string]any, node *model.ElementNode) {
	if value == nil || node == nil {
		return
	}
	childNames := make([]string, 0, len(node.Children))
	for name := range node.Children {
		childNames = append(childNames, name)
	}
	sort.Strings(childNames)
	for _, name := range childNames {
		child := node.Children[name]
		if child == nil || child.Definition == nil {
			continue
		}
		propertyName := propertyNameForNode(child)
		raw, exists := value[propertyName]
		if !exists {
			continue
		}
		if elementAllowsMultiple(child.Definition) {
			switch typed := raw.(type) {
			case []any:
				raw = typed
			case []map[string]any:
				wrapped := make([]any, 0, len(typed))
				for _, item := range typed {
					wrapped = append(wrapped, item)
				}
				value[propertyName] = wrapped
				raw = wrapped
			default:
				wrapped := []any{raw}
				value[propertyName] = wrapped
				raw = wrapped
			}
		}
		normalizeNestedRepeatables(raw, child)
	}
}

func normalizeNestedRepeatables(raw any, node *model.ElementNode) {
	switch typed := raw.(type) {
	case map[string]any:
		normalizeRepeatableChildren(typed, node)
	case []any:
		for _, item := range typed {
			if itemMap, ok := item.(map[string]any); ok {
				normalizeRepeatableChildren(itemMap, node)
			}
		}
	}
}

func populateRequiredChildren(body map[string]any, node *model.ElementNode, reg *registry.Registry) {
	if body == nil || node == nil {
		return
	}
	childNames := make([]string, 0, len(node.Children))
	for name := range node.Children {
		childNames = append(childNames, name)
	}
	sort.Strings(childNames)
	for _, name := range childNames {
		child := node.Children[name]
		if child == nil || child.Definition == nil {
			continue
		}
		propertyName := propertyNameForNode(child)
		if child.Definition.Min <= 0 && !hasRequiredSlices(child) && !hasContractSignal(child) {
			continue
		}
		if child.Definition.Min <= 0 && !hasRequiredSlices(child) && primaryTypeCode(child.Definition) == "Reference" {
			continue
		}
		if _, exists := body[propertyName]; exists && !prefersContractValue(child) {
			continue
		}
		if value, ok := generateRequiredValue(child, reg, nil); ok {
			body[propertyName] = value
		}
	}
}

func hasRequiredSlices(node *model.ElementNode) bool {
	if node == nil {
		return false
	}
	for _, slice := range node.Slices {
		if slice != nil && slice.Definition != nil && slice.Definition.Min > 0 {
			return true
		}
	}
	return false
}

func prefersContractValue(node *model.ElementNode) bool {
	if node == nil || node.Definition == nil {
		return false
	}
	return hasContractSignal(node) || hasRequiredSlices(node)
}

func hasContractSignal(node *model.ElementNode) bool {
	if node == nil || node.Definition == nil {
		return false
	}
	def := node.Definition
	return def.Fixed != nil || def.Pattern != nil || len(def.Examples) > 0 || def.Binding != nil || hasProfileTypes(def)
}

func hasProfileTypes(def *model.ElementDefinition) bool {
	if def == nil {
		return false
	}
	for _, et := range def.Types {
		if len(et.Profile) > 0 {
			return true
		}
	}
	return false
}

func propertyNameForNode(node *model.ElementNode) string {
	if node == nil {
		return ""
	}
	name := node.Name
	if !strings.HasSuffix(name, "[x]") {
		return name
	}
	prefix := strings.TrimSuffix(name, "[x]")
	typeCode := primaryTypeCode(node.Definition)
	if typeCode == "" || typeCode == "Element" {
		typeCode = choiceTypeFromSlices(node.Slices)
	}
	if typeCode == "" {
		return prefix
	}
	return prefix + upperCamelTypeName(typeCode)
}

func choiceTypeFromSlices(slices map[string]*model.SliceNode) string {
	for _, sliceName := range sortedSliceNames(slices) {
		slice := slices[sliceName]
		if slice == nil || slice.Definition == nil {
			continue
		}
		if slice.Definition.Min <= 0 && sliceName == "" {
			continue
		}
		if typeCode := primaryTypeCode(slice.Definition); typeCode != "" {
			return typeCode
		}
	}
	return ""
}

func upperCamelTypeName(typeCode string) string {
	typeCode = strings.TrimSpace(typeCode)
	if typeCode == "" {
		return ""
	}
	typeCode = strings.TrimPrefix(typeCode, "http://hl7.org/fhirpath/System.")
	if len(typeCode) == 1 {
		return strings.ToUpper(typeCode)
	}
	return strings.ToUpper(typeCode[:1]) + typeCode[1:]
}

func generateRequiredValue(node *model.ElementNode, reg *registry.Registry, rng *rand.Rand) (any, bool) {
	if node == nil || node.Definition == nil {
		return nil, false
	}
	def := node.Definition
	if elementAllowsMultiple(def) || def.Min > 1 {
		return generateRepeatedValue(node, reg, rng)
	}
	if len(node.Slices) > 0 {
		for _, sliceName := range sortedSliceNames(node.Slices) {
			slice := node.Slices[sliceName]
			if slice == nil || slice.Definition == nil {
				continue
			}
			// A required slice (Min > 0) is always present. An optional slice
			// (Min == 0) is added only some of the time to simulate real data:
			// without an RNG it stays omitted (the required/nil path), with an
			// RNG it is included with optionalInclusionProbability so presence
			// varies realistically across requests.
			if slice.Definition.Min <= 0 {
				if rng == nil || rng.Float64() > optionalInclusionProbability {
					continue
				}
			}
			return generateSliceValue(slice, reg)
		}
	}
	return generateSingleValue(node, reg)
}

func generateRepeatedValue(node *model.ElementNode, reg *registry.Registry, rng *rand.Rand) (any, bool) {
	values := make([]any, 0)
	sliceNames := make([]string, 0, len(node.Slices))
	for name := range node.Slices {
		sliceNames = append(sliceNames, name)
	}
	sort.Strings(sliceNames)
	for _, name := range sliceNames {
		slice := node.Slices[name]
		if slice == nil || slice.Definition == nil {
			continue
		}
		// Required slices (Min > 0) always appear; optional slices (Min == 0)
		// are included only some of the time so generated payloads vary like
		// real data. A nil RNG means the optional slice is omitted.
		if slice.Definition.Min <= 0 {
			if rng == nil || rng.Float64() > optionalInclusionProbability {
				continue
			}
		}
		count := slice.Definition.Min
		if count < 1 {
			count = 1
		}
		for i := 0; i < count; i++ {
			if value, ok := generateSliceValue(slice, reg); ok {
				values = append(values, value)
			}
		}
	}
	for len(values) < coregen.Max(node.Definition.Min, 1) {
		// A fallback value generated into a sliced collection must still conform to a
		// slice, or a server flags it as matching no slice (e.g. a bare phone without
		// the use=home the Practitioner.telecom:personalPhoneNumber slice requires).
		// Prefer the slice whose discriminator agrees with the generic value (e.g. the
		// phone slice for a phone ContactPoint) so the generated value stays internally
		// consistent, and fall back to the first slice otherwise. Only when no slice can
		// produce a value use the generic generator.
		if len(node.Slices) > 0 {
			if generic, ok := generateSingleValue(node, reg); ok {
				if slice := matchingSlice(node, generic); slice != nil {
					if value, ok := generateSliceValue(slice, reg); ok {
						values = append(values, value)
						continue
					}
				}
			}
			if first := firstSliceNode(node); first != nil {
				if value, ok := generateSliceValue(first, reg); ok {
					values = append(values, value)
					continue
				}
			}
		}
		if value, ok := generateSingleValue(node, reg); ok {
			values = append(values, value)
		} else {
			break
		}
	}
	if len(values) == 0 {
		return nil, false
	}
	return values, true
}

func generateSliceValue(slice *model.SliceNode, reg *registry.Registry) (any, bool) {
	if slice == nil || slice.Definition == nil {
		return nil, false
	}
	synthetic := &model.ElementNode{
		Name:       slice.Definition.Name,
		Path:       slice.Definition.Path,
		Definition: slice.Definition,
		ProfileURL: slice.ProfileURL,
		Children:   slice.Children,
		Slices:     make(map[string]*model.SliceNode),
	}
	if value, ok := generateDatatypeValueFromProfiles(slice.Definition.Types, reg); ok {
		if valueMap, ok := value.(map[string]any); ok {
			populateRequiredChildren(valueMap, synthetic, reg)
			applySimpleConstraints(valueMap, synthetic, reg)
			applySliceConstractions(valueMap, slice, reg)
			ensureSimpleExtensionValue(valueMap, slice, reg)
		}
		return value, true
	}
	value, ok := generateSingleValue(synthetic, reg)
	if valueMap, ok := value.(map[string]any); ok {
		applySliceConstractions(valueMap, slice, reg)
		ensureSimpleExtensionValue(valueMap, slice, reg)
	}
	return value, ok
}

// ensureSimpleExtensionValue gives a simple Extension slice a value[x] when the
// generic generator emitted only {"url": ...}. A simple extension (Extension.extension
// Max 0) must carry a value[x] to satisfy ext-1 ("Must have either extensions or
// value[x], not both"); emitting it with neither is rejected by HAPI. Complex
// extensions (e.g. suppressed) already carry sub-extensions and are left alone.
func ensureSimpleExtensionValue(value map[string]any, slice *model.SliceNode, reg *registry.Registry) {
	if value == nil || slice == nil || reg == nil {
		return
	}
	if _, hasURL := value["url"]; !hasURL {
		return
	}
	if _, hasExt := value["extension"]; hasExt {
		return
	}
	if hasAnyValue(value) {
		return
	}
	valueChild, ok := findSliceValueX(slice, reg)
	if !ok || valueChild == nil || valueChild.Definition == nil {
		return
	}
	if v, ok := generateSingleValue(valueChild, reg); ok {
		value[propertyNameForNode(valueChild)] = v
	}
}

// findSliceValueX locates the value[x] element of an extension slice, first from
// the slice's own children and then by resolving the extension profile the slice
// references. Extension slices sometimes carry no resolved children (the registry
// leaves them empty), so the profile lookup is the reliable path. It only returns
// the value[x] when the extension genuinely permits a value (not a complex
// extension whose value[x] is Max 0).
func findSliceValueX(slice *model.SliceNode, reg *registry.Registry) (*model.ElementNode, bool) {
	root := sliceExtensionRoot(slice, reg)
	if root == nil {
		return nil, false
	}
	// A complex extension carries sub-extension content and must not receive a
	// value[x] (its value[x] is Max 0). Only genuinely simple extensions get one.
	if extChild, ok := root.Children["extension"]; ok && extChild != nil && extChild.Definition != nil && extChild.Definition.Max != "0" {
		return nil, false
	}
	vx, ok := root.Children["value[x]"]
	if !ok || vx == nil || vx.Definition == nil || vx.Definition.Max == "0" {
		return nil, false
	}
	return vx, true
}

// sliceExtensionRoot resolves the extension StructureDefinition that a slice
// references, preferring the slice's own children when present.
func sliceExtensionRoot(slice *model.SliceNode, reg *registry.Registry) *model.ElementNode {
	if slice == nil || slice.Definition == nil || reg == nil {
		return nil
	}
	if c, ok := slice.Children["value[x]"]; ok && c != nil && c.Definition != nil {
		// Use a synthetic root if the slice already carries its value[x] child.
		return &model.ElementNode{Name: slice.Definition.Name, Path: slice.Definition.Path, Definition: slice.Definition, Children: slice.Children}
	}
	for _, et := range slice.Definition.Types {
		for _, p := range et.Profile {
			resolved, err := reg.ResolveProfile(normalizeCanonical(p))
			if err != nil || resolved == nil || resolved.Root == nil {
				continue
			}
			return resolved.Root
		}
	}
	return nil
}

// hasAnyValue reports whether a map carries a FHIR value[x] property (valueString,
// valuePeriod, ...).
func hasAnyValue(m map[string]any) bool {
	for k := range m {
		if strings.HasPrefix(k, "value") {
			return true
		}
	}
	return false
}

// stripEmptyExtensions recursively removes any Extension object that has neither
// a value[x] nor nested sub-extensions. FHIR's ext-1 constraint requires an
// extension to carry either extensions or a value[x] (not both, not neither), so
// an empty extension is always invalid and safely dropped.
func stripEmptyExtensions(v any) {
	switch t := v.(type) {
	case map[string]any:
		if arr, ok := t["extension"].([]any); ok {
			filtered := make([]any, 0, len(arr))
			for _, raw := range arr {
				ext, isExt := raw.(map[string]any)
				if isExt && isEmptyExtension(ext) {
					continue
				}
				filtered = append(filtered, raw)
			}
			if len(filtered) == 0 {
				delete(t, "extension")
			} else {
				t["extension"] = filtered
			}
		}
		for _, val := range t {
			stripEmptyExtensions(val)
		}
	case []any:
		for _, el := range t {
			stripEmptyExtensions(el)
		}
	}
}

// isEmptyExtension reports whether an Extension object carries neither a value[x]
// nor nested sub-extensions, making it invalid under ext-1.
func isEmptyExtension(m map[string]any) bool {
	if m == nil {
		return false
	}
	if _, hasURL := m["url"]; !hasURL {
		return false
	}
	if hasAnyValue(m) {
		return false
	}
	if sub, ok := m["extension"].([]any); ok && len(sub) > 0 {
		return false
	}
	return true
}

// applySliceConstractions overlays a slice's Fixed/Pattern onto a generated value
// so the element satisfies the slice's discriminator. This covers both the slice
// element's own Fixed/Pattern (e.g. Practitioner.telecom:personalPhoneNumber has
// pattern {"system":"phone","use":"home"}) and the Fixed/Pattern of its children
// (e.g. an Organization.address:physical slice constrains `type` to the pattern
// "physical"). Without applying these, a required slice is not matched and servers
// reject the resource.
//
// Codings materialised from a slice pattern are normalised so their display
// resolves to the canonical CodeSystem display rather than echoing the code
// (e.g. "XX" instead of "Organization identifier").
func applySliceConstractions(value map[string]any, slice *model.SliceNode, reg *registry.Registry) {
	if value == nil || slice == nil {
		return
	}
	applySliceElementConstraint(value, slice)
	for _, name := range sortedSliceChildren(slice) {
		child := slice.Children[name]
		if child == nil || child.Definition == nil {
			continue
		}
		applySliceChildConstraints(value, child, reg)
	}
}

// applySliceElementConstraint overlays the slice element's own Fixed/Pattern onto
// a generated value. FHIR slices commonly carry their discriminating values as a
// pattern/fixed on the slice element itself rather than as per-child constraints,
// so without applying it a generated value does not match the slice.
func applySliceElementConstraint(value map[string]any, slice *model.SliceNode) {
	if value == nil || slice == nil || slice.Definition == nil {
		return
	}
	def := slice.Definition
	if def.Fixed != nil {
		if fixedMap, ok := def.Fixed.(map[string]any); ok {
			for k, v := range fixedMap {
				if subMap, ok := v.(map[string]any); ok {
					mergeSlicePattern(value, k, subMap)
				} else {
					value[k] = v
				}
			}
		}
		return
	}
	if def.Pattern != nil {
		if patternMap, ok := def.Pattern.(map[string]any); ok {
			for k, v := range patternMap {
				if subMap, ok := v.(map[string]any); ok {
					mergeSlicePattern(value, k, subMap)
				} else {
					value[k] = v
				}
			}
		}
	}
}

// applySliceChildConstraints applies one slice-child's Fixed/Pattern onto the
// generated value, recursing into nested children (e.g. a CodeableConcept's
// coding) so a Fixed value carried several levels deep is applied. The slice's
// discriminator often constrains a nested element (for example the suppressedBy
// sub-extension's value[x].coding must be the fixed organisation-initiated
// coding); without recursion that required value is left as a generic
// placeholder and the server rejects the resource.
func applySliceChildConstraints(value map[string]any, child *model.ElementNode, reg *registry.Registry) {
	if value == nil || child == nil || child.Definition == nil {
		return
	}
	def := child.Definition
	prop := propertyNameForNode(child)
	if prop == "" || prop == "id" {
		return
	}
	if def.Fixed != nil {
		value[prop] = wrapFixedSlice(value[prop], def, def.Fixed)
		// A fixed coding may carry only system+code: HAPI rejects a display/text on
		// a fixed value that defines only system+code. Mark it so the later
		// display/text normalisation passes leave it alone.
		markFixedCoding(value[prop])
		// A slice that fixes a CodeableConcept's coding fully determines the
		// concept; a `text` synthesized by the generic fallback (e.g. "Value[x]")
		// is stale and misleading, so drop it. If the slice itself fixes text it
		// is reapplied when the text child is processed.
		if prop == "coding" {
			delete(value, "text")
		}
		return
	}
	if def.Pattern != nil {
		if patternMap, ok := def.Pattern.(map[string]any); ok {
			mergeSlicePattern(value, prop, patternMap)
			markFixedCoding(value[prop])
			if prop == "coding" {
				delete(value, "text")
			}
		} else {
			value[prop] = def.Pattern
		}
		return
	}
	// No Fixed/Pattern on this child itself: recurse into its nested children,
	// applying their Fixed/Pattern onto the corresponding nested generated value.
	recurseSliceChildConstraints(value, prop, child, reg)
}

// recurseSliceChildConstraints descends into an existing generated value at prop
// (an object or array of objects) and applies child's nested element constraints.
func recurseSliceChildConstraints(value map[string]any, prop string, child *model.ElementNode, reg *registry.Registry) {
	raw, ok := value[prop]
	if !ok {
		return
	}
	switch typed := raw.(type) {
	case map[string]any:
		applySliceNodeChildren(typed, child, reg)
	case []any:
		for _, item := range typed {
			if m, ok := item.(map[string]any); ok {
				applySliceNodeChildren(m, child, reg)
			}
		}
	}
}

// applySliceNodeChildren applies each of node's children's constraints onto the
// target value, recursing for nested children that carry no direct Fixed/Pattern.
func applySliceNodeChildren(value map[string]any, node *model.ElementNode, reg *registry.Registry) {
	if value == nil || node == nil {
		return
	}
	for _, name := range sortedNodeChildren(node) {
		applySliceChildConstraints(value, node.Children[name], reg)
	}
}

func sortedNodeChildren(node *model.ElementNode) []string {
	out := make([]string, 0, len(node.Children))
	for name := range node.Children {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// wrapFixedSlice returns the Fixed value in the correct shape for the target
// property: if the generated target is already an array (or the element is
// repeatable), the fixed value is wrapped in a single-element array so the
// existing array shape is preserved.
func wrapFixedSlice(current any, def *model.ElementDefinition, fixed any) any {
	if _, isArray := current.([]any); isArray {
		return []any{fixed}
	}
	if elementAllowsMultiple(def) {
		return []any{fixed}
	}
	return fixed
}

func sortedSliceChildren(slice *model.SliceNode) []string {
	out := make([]string, 0, len(slice.Children))
	for name := range slice.Children {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// mergeSlicePattern merges a pattern object into value at prop, recursing into
// nested objects and arrays so a patterned Coding/CodeableConcept lands.
func mergeSlicePattern(value map[string]any, prop string, pattern map[string]any) {
	current, ok := value[prop].(map[string]any)
	if !ok {
		value[prop] = cloneMap(pattern)
		return
	}
	for k, v := range pattern {
		if subMap, ok := v.(map[string]any); ok {
			if existing, ok := current[k].(map[string]any); ok {
				mergeSlicePattern(existing, k, subMap)
				continue
			}
		}
		current[k] = v
	}
}

func generateDatatypeValueFromProfile(profileURL string, reg *registry.Registry) (any, bool) {
	profileURL = normalizeCanonical(profileURL)
	if reg == nil || strings.TrimSpace(profileURL) == "" {
		return nil, false
	}
	resolved, err := reg.ResolveProfile(profileURL)
	if err != nil || resolved == nil || resolved.Root == nil {
		return nil, false
	}
	value := map[string]any{}
	populateRequiredChildren(value, resolved.Root, reg)
	applySimpleConstraints(value, resolved.Root, reg)
	if len(value) == 0 {
		return nil, false
	}
	return value, true
}

func generateDatatypeValueFromProfiles(types []model.ElementType, reg *registry.Registry) (any, bool) {
	if reg == nil {
		return nil, false
	}
	// Apply the first type profile that yields content, not a merge of all of
	// them: an element may list several profiles (e.g. all the AU Identifier
	// variants on Organization.identifier), and merging them produces a value
	// that conforms to none.
	for _, et := range types {
		for _, profileURL := range et.Profile {
			value, ok := generateDatatypeValueFromProfile(profileURL, reg)
			if ok {
				if _, ok := value.(map[string]any); ok {
					return value, true
				}
			}
		}
	}
	return nil, false
}

func generateSingleValue(node *model.ElementNode, reg *registry.Registry) (any, bool) {
	if node == nil || node.Definition == nil {
		return nil, false
	}
	typeCode := primaryTypeCode(node.Definition)
	if node.Definition.Fixed != nil {
		return node.Definition.Fixed, true
	}
	boundCoding, hasBoundCoding := resolveBoundCodingForNode(node, reg)
	if node.Definition.Pattern != nil {
		if merged, ok := mergePatternWithBinding(node.Definition.Pattern, typeCode, boundCoding, hasBoundCoding); ok {
			return merged, true
		}
		return node.Definition.Pattern, true
	}
	if len(node.Definition.Examples) > 0 {
		return node.Definition.Examples[0], true
	}
	switch typeCode {
	case "string", "markdown", "id":
		return sampleStringValue(node.Path), true
	case "uri", "url", "canonical", "oid":
		return derivedURIValue(node.Path), true
	case "code":
		if hasBoundCoding && boundCoding.Code != "" {
			return boundCoding.Code, true
		}
		return sampleCodeValue(node.Path), true
	case "boolean":
		return true, true
	case "integer", "unsignedInt", "positiveInt":
		return 1, true
	case "decimal":
		return 123.45, true
	case "date":
		return "2024-01-01", true
	case "dateTime", "instant":
		return "2024-01-01T00:00:00Z", true
	case "time":
		return "12:00:00", true
	case "Identifier":
		identifier := map[string]any{}
		populateRequiredChildren(identifier, node, reg)
		applySimpleConstraints(identifier, node, reg)
		identifier = enrichGeneratedValueWithTypeProfiles(identifier, node.Definition, reg).(map[string]any)
		if _, ok := identifier["system"]; !ok {
			identifier["system"] = "http://example.org/fhir/identifier/" + coregen.SanitizeFHIRID(node.Path)
		}
		if _, ok := identifier["value"]; !ok {
			identifier["value"] = coregen.SanitizeFHIRID(node.Path) + "-001"
		}
		if _, ok := identifier["type"]; !ok {
			identifier["type"] = map[string]any{"text": sampleStringValue(node.Path + ".type")}
		}
		normalizeGeneratedIdentifier(identifier)
		return identifier, true
	case "CodeableConcept":
		if hasBoundCoding {
			concept := map[string]any{"coding": []any{codingToMap(boundCoding)}}
			if boundCoding.Display != "" {
				concept["text"] = boundCoding.Display
			}
			return enrichGeneratedValueWithTypeProfiles(concept, node.Definition, reg), true
		}
		leaf := sampleCodeValue(node.Path)
		concept := map[string]any{
			"text": sampleStringValue(node.Path),
			"coding": []any{map[string]any{
				"system":  "http://example.org/fhir/code-system",
				"code":    leaf,
				"display": sampleStringValue(node.Path),
			}},
		}
		return enrichGeneratedValueWithTypeProfiles(concept, node.Definition, reg), true
	case "Coding":
		if hasBoundCoding {
			return enrichGeneratedValueWithTypeProfiles(codingToMap(boundCoding), node.Definition, reg), true
		}
		return enrichGeneratedValueWithTypeProfiles(map[string]any{"system": "http://example.org/fhir/system", "code": sampleCodeValue(node.Path)}, node.Definition, reg), true
	case "HumanName":
		value := map[string]any{"family": "Momus", "given": []any{"Test"}}
		populateRequiredChildren(value, node, reg)
		applySimpleConstraints(value, node, reg)
		return enrichGeneratedValueWithTypeProfiles(value, node.Definition, reg), true
	case "Address":
		value := map[string]any{"line": []any{"1 Momus Street"}, "city": "Sydney", "country": "AU"}
		populateRequiredChildren(value, node, reg)
		applySimpleConstraints(value, node, reg)
		normalizeGeneratedAddress(value)
		return enrichGeneratedValueWithTypeProfiles(value, node.Definition, reg), true
	case "ContactPoint":
		value := map[string]any{"system": "phone", "value": "0299999999"}
		populateRequiredChildren(value, node, reg)
		applySimpleConstraints(value, node, reg)
		return enrichGeneratedValueWithTypeProfiles(value, node.Definition, reg), true
	case "Reference":
		value := map[string]any{"reference": referencePlaceholder(node.Definition, reg)}
		populateRequiredChildren(value, node, reg)
		applySimpleConstraints(value, node, reg)
		normalizeReferenceType(value, node.Definition, reg)
		return enrichGeneratedValueWithTypeProfiles(value, node.Definition, reg), true
	case "Period":
		value := map[string]any{"start": "2024-01-01T00:00:00Z", "end": "2024-12-31T23:59:59Z"}
		populateRequiredChildren(value, node, reg)
		applySimpleConstraints(value, node, reg)
		return enrichGeneratedValueWithTypeProfiles(value, node.Definition, reg), true
	case "Quantity":
		value := map[string]any{
			"value":  123.45,
			"unit":   "mmol",
			"system": "http://unitsofmeasure.org",
			"code":   "mmol",
		}
		populateRequiredChildren(value, node, reg)
		applySimpleConstraints(value, node, reg)
		return enrichGeneratedValueWithTypeProfiles(value, node.Definition, reg), true
	case "Ratio":
		value := map[string]any{
			"numerator":   map[string]any{"value": 1.0},
			"denominator": map[string]any{"value": 2.0},
		}
		populateRequiredChildren(value, node, reg)
		applySimpleConstraints(value, node, reg)
		return enrichGeneratedValueWithTypeProfiles(value, node.Definition, reg), true
	case "Range":
		value := map[string]any{
			"low":  map[string]any{"value": 1.0},
			"high": map[string]any{"value": 10.0},
		}
		populateRequiredChildren(value, node, reg)
		applySimpleConstraints(value, node, reg)
		return enrichGeneratedValueWithTypeProfiles(value, node.Definition, reg), true
	case "Attachment":
		value := map[string]any{
			"contentType": "text/plain",
			"url":         "http://example.org/attachment.txt",
		}
		populateRequiredChildren(value, node, reg)
		applySimpleConstraints(value, node, reg)
		return enrichGeneratedValueWithTypeProfiles(value, node.Definition, reg), true
	default:
		if value, ok := generateDatatypeValueFromProfiles(node.Definition.Types, reg); ok {
			if valueMap, ok := value.(map[string]any); ok {
				populateRequiredChildren(valueMap, node, reg)
				applySimpleConstraints(valueMap, node, reg)
			}
			return enrichGeneratedValueWithTypeProfiles(value, node.Definition, reg), true
		}
		if len(node.Children) == 0 {
			return nil, false
		}
		value := map[string]any{}
		populateRequiredChildren(value, node, reg)
		applySimpleConstraints(value, node, reg)
		if len(value) == 0 {
			return nil, false
		}
		return enrichGeneratedValueWithTypeProfiles(value, node.Definition, reg), true
	}
}

func enrichGeneratedValueWithTypeProfiles(value any, def *model.ElementDefinition, reg *registry.Registry) any {
	if reg == nil || def == nil {
		return value
	}
	valueMap, ok := value.(map[string]any)
	if !ok {
		return value
	}
	// Enrich with the first type profile that contributes required fields. Do
	// not merge all profiles: an element that lists several (e.g. all the AU
	// Identifier variants) must be generated from one, not a Frankenstein of all.
	for _, et := range def.Types {
		for _, profileURL := range et.Profile {
			resolved, err := reg.ResolveProfile(normalizeCanonical(profileURL))
			if err != nil || resolved == nil || resolved.Root == nil {
				continue
			}
			before := len(valueMap)
			populateRequiredChildren(valueMap, resolved.Root, reg)
			applySimpleConstraints(valueMap, resolved.Root, reg)
			if len(valueMap) > before {
				return valueMap
			}
		}
	}
	return valueMap
}

var matchesConstraintPattern = regexp.MustCompile(`^([A-Za-z0-9_]+)\.matches\('([^']+)'\)$`)
var existsEitherPattern = regexp.MustCompile(`^([A-Za-z0-9_]+)\.exists\(\) or ([A-Za-z0-9_]+)\.exists\(\)$`)
var whereEmptyPattern = regexp.MustCompile(`^([A-Za-z0-9_]+)\.where\(([A-Za-z0-9_]+) = '([^']+)'\)\.empty\(\)$`)
var currentWhereEmptyPattern = regexp.MustCompile(`^where\(([A-Za-z0-9_]+) = '([^']+)'\)\.empty\(\)$`)
var collectionEitherWhereExistsPattern = regexp.MustCompile(`^([A-Za-z0-9_]+)\.where\(([A-Za-z0-9_]+)='([^']+)'\)\.exists\(\) or ([A-Za-z0-9_]+)\.where\(([A-Za-z0-9_]+)='([^']+)'\)\.exists\(\)$`)

func applySimpleConstraints(value map[string]any, node *model.ElementNode, reg *registry.Registry) {
	if value == nil || node == nil || node.Definition == nil {
		return
	}
	for _, constraint := range node.Definition.Constraints {
		expr := strings.TrimSpace(constraint.Expression)
		if matches := matchesConstraintPattern.FindStringSubmatch(expr); len(matches) == 3 {
			fieldName := matches[1]
			regex := matches[2]
			generated, ok := synthesizeRegexExample(regex)
			if !ok {
				continue
			}
			current, _ := value[fieldName].(string)
			if current == "" || !regexp.MustCompile(regex).MatchString(current) {
				value[fieldName] = generated
			}
			continue
		}
		if matches := existsEitherPattern.FindStringSubmatch(expr); len(matches) == 3 {
			left := matches[1]
			right := matches[2]
			if value[left] == nil && value[right] == nil {
				if child := node.Children[left]; child != nil {
					if generated, ok := generateRequiredValue(child, reg, nil); ok {
						value[left] = generated
					}
				}
			}
			continue
		}
		if matches := collectionEitherWhereExistsPattern.FindStringSubmatch(expr); len(matches) == 7 {
			collectionName := matches[1]
			fieldName := matches[2]
			if matches[4] != collectionName || matches[5] != fieldName {
				continue
			}
			if collectionHasFieldValue(value[collectionName], fieldName, matches[3], matches[6]) {
				continue
			}
			child := node.Children[collectionName]
			if child == nil {
				continue
			}
			candidate, ok := generateMatchingCollectionCandidate(child, fieldName, []string{matches[3], matches[6]}, reg)
			if !ok {
				continue
			}
			existing, _ := value[collectionName].([]any)
			value[collectionName] = append(existing, candidate)
			continue
		}
		if matches := whereEmptyPattern.FindStringSubmatch(expr); len(matches) == 4 {
			collectionName := matches[1]
			fieldName := matches[2]
			forbidden := matches[3]
			rawItems, ok := value[collectionName].([]any)
			if !ok {
				continue
			}
			for _, rawItem := range rawItems {
				item, ok := rawItem.(map[string]any)
				if !ok {
					continue
				}
				if current, ok := item[fieldName].(string); ok && current == forbidden {
					delete(item, fieldName)
				}
			}
			continue
		}
		if matches := currentWhereEmptyPattern.FindStringSubmatch(expr); len(matches) == 3 {
			fieldName := matches[1]
			forbidden := matches[2]
			if current, ok := value[fieldName].(string); ok && current == forbidden {
				delete(value, fieldName)
			}
		}
	}
}

func collectionHasFieldValue(raw any, fieldName string, values ...string) bool {
	items, ok := raw.([]any)
	if !ok {
		return false
	}
	allowed := make(map[string]struct{}, len(values))
	for _, value := range values {
		allowed[value] = struct{}{}
	}
	for _, rawItem := range items {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		if current, ok := item[fieldName].(string); ok {
			if _, exists := allowed[current]; exists {
				return true
			}
		}
	}
	return false
}

func generateMatchingCollectionCandidate(node *model.ElementNode, fieldName string, wanted []string, reg *registry.Registry) (any, bool) {
	if node == nil {
		return nil, false
	}
	for _, value := range wanted {
		for _, sliceName := range sortedSliceNames(node.Slices) {
			slice := node.Slices[sliceName]
			candidate, ok := generateSliceValue(slice, reg)
			if !ok {
				continue
			}
			candidateMap, ok := candidate.(map[string]any)
			if !ok {
				continue
			}
			if current, ok := candidateMap[fieldName].(string); ok && current == value {
				return candidateMap, true
			}
		}
	}
	if candidate, ok := generateRequiredValue(node, reg, nil); ok {
		if candidateMap, ok := candidate.(map[string]any); ok {
			for _, value := range wanted {
				if current, ok := candidateMap[fieldName].(string); ok && current == value {
					return candidateMap, true
				}
			}
		}
	}
	return nil, false
}

func sortedSliceNames(slices map[string]*model.SliceNode) []string {
	names := make([]string, 0, len(slices))
	for name := range slices {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// firstSliceNode returns the first slice (deterministically sorted by name) of a
// node, or nil when the node has no slices. It lets a sliced element's fallback
// value be generated through a slice so the slice's Fixed/Pattern constraints apply.
func firstSliceNode(node *model.ElementNode) *model.SliceNode {
	if node == nil || len(node.Slices) == 0 {
		return nil
	}
	names := sortedSliceNames(node.Slices)
	slice := node.Slices[names[0]]
	if slice == nil || slice.Definition == nil {
		return nil
	}
	return slice
}

// matchingSlice returns the first slice whose discriminator agrees with an already
// generated generic value, or nil. It compares each slice child's Fixed value to the
// corresponding field of the generic value, so a phone ContactPoint matches the
// phone slice (system=phone) rather than the email slice. This keeps a sliced
// element's fallback value both conformant and internally consistent (e.g. a phone
// number under system=phone rather than under system=email).
func matchingSlice(node *model.ElementNode, generic any) *model.SliceNode {
	gm, ok := generic.(map[string]any)
	if !ok || node == nil {
		return nil
	}
	for _, name := range sortedSliceNames(node.Slices) {
		slice := node.Slices[name]
		if slice == nil || slice.Definition == nil {
			continue
		}
		for _, childName := range sortedSliceChildren(slice) {
			child := slice.Children[childName]
			if child == nil || child.Definition == nil || child.Definition.Fixed == nil {
				continue
			}
			fixed, ok := child.Definition.Fixed.(string)
			if !ok {
				continue
			}
			if genericValue, ok := gm[childName].(string); ok && genericValue == fixed {
				return slice
			}
		}
	}
	return nil
}

func synthesizeRegexExample(regex string) (string, bool) {
	switch regex {
	case "^([0-9]{11})$":
		return "12345678901", true
	case "^[0-9]{11}$":
		return "12345678901", true
	default:
		return "", false
	}
}

type generatedCoding struct {
	System  string
	Code    string
	Display string
}

func resolveBoundCoding(def *model.ElementDefinition, reg *registry.Registry) (generatedCoding, bool) {
	if def == nil || def.Binding == nil || reg == nil {
		return generatedCoding{}, false
	}
	valueSetURL := normalizeCanonical(def.Binding.ValueSet)
	if valueSetURL == "" {
		return generatedCoding{}, false
	}
	vs, ok := reg.ValueSet(valueSetURL)
	if !ok || vs == nil {
		return generatedCoding{}, false
	}
	if coding, ok := firstExpansionCoding(vs.ExpansionContains); ok {
		return coding, true
	}
	for _, include := range vs.ComposeIncludes {
		for _, concept := range include.Concepts {
			if isMeaningfulCoding(concept.Code, concept.Display) {
				return generatedCoding{System: include.System, Code: concept.Code, Display: concept.Display}, true
			}
		}
		if include.System == "" {
			continue
		}
		if cs, ok := reg.CodeSystem(normalizeCanonical(include.System)); ok && cs != nil {
			if concept, ok := firstCodeSystemConcept(cs.Concepts); ok {
				return generatedCoding{System: include.System, Code: concept.Code, Display: concept.Display}, true
			}
		}
	}
	return generatedCoding{}, false
}

// resolveBoundCodingForNode resolves a bound coding for an element node, falling
// back to the package's own example instance data when the bound ValueSet or
// CodeSystem is not present in the registry (or carries no meaningful code).
//
// The registry represents the package and its dependencies in full, so example
// instances are a first-class source of conformant values. The example-driven
// fallback prefers an instance whose meta.profile matches the node's profile,
// and otherwise uses the first example of the resource type, so generation only
// emits the synthetic example.org fallback in genuinely exceptional cases.
func resolveBoundCodingForNode(node *model.ElementNode, reg *registry.Registry) (generatedCoding, bool) {
	if node == nil || node.Path == "" {
		return generatedCoding{}, false
	}
	if node.Definition != nil {
		if coding, ok := resolveBoundCoding(node.Definition, reg); ok {
			return coding, true
		}
	}
	// A CodeableConcept node may carry its required binding on its child
	// "coding" element rather than on itself (common for nested extension
	// value[x].coding, e.g. suppressedBy's responsible-party-type binding).
	if coding, ok := resolveBoundCodingFromCodingChild(node, reg); ok {
		return coding, true
	}
	return resolveBoundCodingFromExample(node, reg)
}

// resolveBoundCodingFromCodingChild resolves a bound coding from the node's
// "coding" child element when the node itself has no binding but its coding
// child does. This covers elements whose binding lives one level down, e.g.
// Extension.extension.value[x] whose CodeableConcept has a nil binding but
// whose value[x].coding carries the required value set.
func resolveBoundCodingFromCodingChild(node *model.ElementNode, reg *registry.Registry) (generatedCoding, bool) {
	if node == nil || reg == nil {
		return generatedCoding{}, false
	}
	coding, ok := node.Children["coding"]
	if !ok || coding == nil || coding.Definition == nil || coding.Definition.Binding == nil {
		return generatedCoding{}, false
	}
	return resolveBoundCoding(coding.Definition, reg)
}

// resolveBoundCodingFromExample looks for a real coding at the node's element
// path within the package's example instance resources. It prefers an instance
// whose meta.profile matches the node's profile URL, then falls back to the
// first example of the resource type (derived from the leading path segment).
//
// For an extension value[x] node (path "Extension.value[x]" whose profile is the
// extension's StructureDefinition URL), it matches by extension URL across every
// example instance — extension URLs are globally unique, so the coding is found
// wherever the extension appears (e.g. the "new-patient-availability" extension
// on a HealthcareService example).
func resolveBoundCodingFromExample(node *model.ElementNode, reg *registry.Registry) (generatedCoding, bool) {
	if node == nil || node.Path == "" || reg == nil {
		return generatedCoding{}, false
	}
	// Extension value[x] resolution by extension URL.
	if strings.HasPrefix(node.Path, "Extension.") && node.ProfileURL != "" {
		return resolveBoundCodingFromExtensionValue(node.ProfileURL, reg)
	}
	resourceType, path := splitResourcePath(node.Path)
	if resourceType == "" || path == "" {
		return generatedCoding{}, false
	}
	instances := reg.ResourcesForType(resourceType)
	if len(instances) == 0 {
		return generatedCoding{}, false
	}

	// First pass: prefer instances whose meta.profile matches the node's profile.
	profileURL := normalizeCanonical(node.ProfileURL)
	for _, inst := range instances {
		if !hasProfile(inst.ProfileURLs, profileURL) {
			continue
		}
		if coding, ok := codingAtPath(inst.Raw, path); ok {
			return coding, true
		}
	}
	// Second pass: any example of the resource type.
	for _, inst := range instances {
		if coding, ok := codingAtPath(inst.Raw, path); ok {
			return coding, true
		}
	}
	return generatedCoding{}, false
}

// resolveBoundCodingFromExtensionValue searches every example instance for an
// extension whose url equals extensionURL, and returns the first meaningful
// coding from its valueCodeableConcept (or valueCoding). The extension URL is
// globally unique, so this resolves wherever the extension appears in the
// package's examples.
func resolveBoundCodingFromExtensionValue(extensionURL string, reg *registry.Registry) (generatedCoding, bool) {
	if extensionURL == "" || reg == nil {
		return generatedCoding{}, false
	}
	extensionURL = normalizeCanonical(extensionURL)
	for _, inst := range reg.AllResources() {
		if inst == nil || inst.Raw == nil {
			continue
		}
		if coding, ok := findExtensionValueCoding(inst.Raw, extensionURL); ok {
			return coding, true
		}
	}
	return generatedCoding{}, false
}

// findExtensionValueCoding walks a resource instance's extension array (and
// nested extension arrays) looking for an extension whose url equals
// extensionURL, then extracts the first meaningful coding from its
// valueCodeableConcept or valueCoding.
func findExtensionValueCoding(raw map[string]any, extensionURL string) (generatedCoding, bool) {
	if raw == nil || extensionURL == "" {
		return generatedCoding{}, false
	}
	// The top-level "extension" array, plus recurse into any nested resource
	// values (e.g. contained resources or nested extensions) defensively.
	if coding, ok := findExtensionValueCodingInAny(raw, extensionURL); ok {
		return coding, true
	}
	return generatedCoding{}, false
}

func findExtensionValueCodingInAny(v any, extensionURL string) (generatedCoding, bool) {
	switch typed := v.(type) {
	case []any:
		for _, item := range typed {
			if coding, ok := findExtensionValueCodingInAny(item, extensionURL); ok {
				return coding, true
			}
		}
		return generatedCoding{}, false
	case map[string]any:
		// If this is an extension with the matching url, extract its value.
		if u, ok := typed["url"].(string); ok && normalizeCanonical(u) == extensionURL {
			if coding, ok := extensionValueCoding(typed); ok {
				return coding, true
			}
		}
		// Recurse into all sub-values, including nested extension arrays.
		for _, val := range typed {
			if coding, ok := findExtensionValueCodingInAny(val, extensionURL); ok {
				return coding, true
			}
		}
		return generatedCoding{}, false
	default:
		return generatedCoding{}, false
	}
}

// extensionValueCoding extracts the first meaningful coding from an extension's
// valueCodeableConcept or valueCoding.
func extensionValueCoding(ext map[string]any) (generatedCoding, bool) {
	if ext == nil {
		return generatedCoding{}, false
	}
	if vcc, ok := ext["valueCodeableConcept"]; ok {
		if coding, ok := firstCodingInValue(vcc); ok {
			return coding, true
		}
	}
	if vc, ok := ext["valueCoding"]; ok {
		if coding, ok := firstCodingInValue(vc); ok {
			return coding, true
		}
	}
	return generatedCoding{}, false
}

// splitResourcePath splits a canonical element path into its leading resource
// type and the remainder. E.g. "PractitionerRole.code" -> ("PractitionerRole",
// "code"); "Patient.communication" -> ("Patient", "communication").
func splitResourcePath(path string) (resourceType, elementPath string) {
	idx := strings.Index(path, ".")
	if idx <= 0 {
		return "", ""
	}
	return path[:idx], path[idx+1:]
}

// hasProfile reports whether profileURL (possibly versionless) matches any of
// the resource's declared profile URLs.
func hasProfile(profiles []string, profileURL string) bool {
	if profileURL == "" {
		return false
	}
	for _, p := range profiles {
		if normalizeCanonical(p) == profileURL {
			return true
		}
	}
	return false
}

// codingAtPath walks a raw resource instance to the named element path and
// returns the first meaningful coding found there. It handles both a single
// element and a repeatable element (an array of objects). path is the
// dot-separated path below the resource root, e.g. "communication" or
// "code.coding". The final element is expected to carry a "coding" array.
func codingAtPath(raw map[string]any, path string) (generatedCoding, bool) {
	if raw == nil || path == "" {
		return generatedCoding{}, false
	}
	segments := strings.Split(path, ".")
	// Descend into the element structure, handling arrays of objects at each
	// repeatable level (e.g. communication[0].coding[0]).
	var cur any = raw
	for _, seg := range segments {
		switch typed := cur.(type) {
		case map[string]any:
			next, ok := typed[seg]
			if !ok {
				return generatedCoding{}, false
			}
			cur = next
		case []any:
			// Recurse into the first element of the array that has the segment.
			found := false
			for _, item := range typed {
				if itemMap, ok := item.(map[string]any); ok {
					if next, ok := itemMap[seg]; ok {
						cur = next
						found = true
						break
					}
				}
			}
			if !found {
				return generatedCoding{}, false
			}
		default:
			return generatedCoding{}, false
		}
	}
	return firstCodingInValue(cur)
}

// firstCodingInValue extracts the first meaningful coding from a value that is
// either a CodeableConcept (map with "coding"), a Coding (map with system+code),
// or an array of those.
func firstCodingInValue(v any) (generatedCoding, bool) {
	switch typed := v.(type) {
	case []any:
		for _, item := range typed {
			if coding, ok := firstCodingInValue(item); ok {
				return coding, true
			}
		}
		return generatedCoding{}, false
	case map[string]any:
		// CodeableConcept: look at coding array.
		if codings, ok := typed["coding"].([]any); ok {
			for _, c := range codings {
				if cm, ok := c.(map[string]any); ok {
					if coding, ok := codingFromMap(cm); ok {
						return coding, true
					}
				}
			}
			return generatedCoding{}, false
		}
		// Bare Coding.
		return codingFromMap(typed)
	default:
		return generatedCoding{}, false
	}
}

// codingFromMap converts a raw coding map into a generatedCoding, returning
// false when the code is empty or not meaningful.
func codingFromMap(m map[string]any) (generatedCoding, bool) {
	code, _ := m["code"].(string)
	if !isMeaningfulCoding(code, "") {
		return generatedCoding{}, false
	}
	system, _ := m["system"].(string)
	display, _ := m["display"].(string)
	return generatedCoding{System: system, Code: code, Display: display}, true
}

// meaningfulCodingCodes are codes that represent null/placeholder values (not a
// real, meaningful code). Generation must avoid them so bound elements get a
// meaningful code from the package rather than e.g. the v2-0203 "XX" null code.
var meaningfulCodingCodes = map[string]bool{
	"XX": true, "UNK": true, "UN": true, "NULL": true, "NIL": true,
	"N/A": true, "NA": true, "NI": true, "OTH": true, "OT": true,
}

// isMeaningfulCoding reports whether a code should be used as a generated value:
// non-empty, not a placeholder/null code, and not a v3 abstract/group code
// (which begin with an underscore and are not valid instance values).
func isMeaningfulCoding(code, display string) bool {
	if strings.TrimSpace(code) == "" {
		return false
	}
	trimmed := strings.ToUpper(strings.TrimSpace(code))
	if meaningfulCodingCodes[trimmed] {
		return false
	}
	// v3 code systems mark abstract/group concepts with a leading underscore
	// (e.g. _ActAccommodationReason). They are not valid instance values.
	if strings.HasPrefix(trimmed, "_") {
		return false
	}
	return true
}

func firstExpansionCoding(entries []model.ValueSetExpansionContains) (generatedCoding, bool) {
	for _, entry := range entries {
		if entry.Code != "" && isMeaningfulCoding(entry.Code, entry.Display) {
			return generatedCoding{System: entry.System, Code: entry.Code, Display: entry.Display}, true
		}
		if coding, ok := firstExpansionCoding(entry.Contains); ok {
			return coding, true
		}
	}
	return generatedCoding{}, false
}

func firstCodeSystemConcept(concepts []model.CodeSystemConcept) (model.CodeSystemConcept, bool) {
	for _, concept := range concepts {
		if concept.Code != "" && isMeaningfulCoding(concept.Code, concept.Display) {
			return concept, true
		}
		if child, ok := firstCodeSystemConcept(concept.Concepts); ok {
			return child, true
		}
	}
	return model.CodeSystemConcept{}, false
}

func codingToMap(coding generatedCoding) map[string]any {
	out := map[string]any{}
	if coding.System != "" {
		out["system"] = coding.System
	}
	if coding.Code != "" {
		out["code"] = coding.Code
	}
	if coding.Display != "" {
		out["display"] = coding.Display
	}
	return out
}

// fixedCodingKey marks a coding map that was materialised from a profile's
// Fixed/Pattern value. HCPD profiles fix codings with only system+code, so HAPI
// rejects any extra display/text on such a coding. The marker lets the
// display/text normalisation passes skip these codings; it is stripped before a
// payload is serialised (see stripFixedCodingMarkers). The key is prefixed so it
// cannot collide with a real FHIR element name.
const fixedCodingKey = "__momus_fixed_coding"

// markFixedCoding marks v (a Coding map, a CodeableConcept map, or an array of
// either) as derived from a Fixed/Pattern value and strips display/text from it,
// since a fixed coding may carry only system+code.
func markFixedCoding(v any) {
	switch t := v.(type) {
	case map[string]any:
		delete(t, "display")
		delete(t, "text")
		if codings, ok := t["coding"].([]any); ok {
			for _, c := range codings {
				markFixedCoding(c)
			}
		} else {
			t[fixedCodingKey] = true
		}
	case []any:
		for _, el := range t {
			markFixedCoding(el)
		}
	}
}

// isFixedCoding reports whether a coding map was materialised from a
// Fixed/Pattern value and therefore must not gain a display/text.
func isFixedCoding(m map[string]any) bool {
	if m == nil {
		return false
	}
	marked, _ := m[fixedCodingKey].(bool)
	return marked
}

// stripFixedCodingMarkers recursively removes fixedCodingKey markers from a
// generated payload so they never reach the serialised output.
func stripFixedCodingMarkers(v any) {
	switch t := v.(type) {
	case map[string]any:
		delete(t, fixedCodingKey)
		for _, val := range t {
			stripFixedCodingMarkers(val)
		}
	case []any:
		for _, el := range t {
			stripFixedCodingMarkers(el)
		}
	}
}

// normaliseCodingDisplay resolves a coding's display to the canonical CodeSystem
// display so a pattern/fixed that only carries system+code does not echo the
// code as the display (e.g. "XX" instead of "Organization identifier"). It
// operates on a value that is either a Coding/CodeableConcept map (with a
// "coding" array) or an array of codings.
func normaliseCodingDisplay(v any, reg *registry.Registry) {
	switch t := v.(type) {
	case map[string]any:
		if codings, ok := t["coding"].([]any); ok {
			for _, c := range codings {
				normaliseCoding(c, reg)
			}
		}
	case []any:
		for _, c := range t {
			normaliseCoding(c, reg)
		}
	}
}

// normalisePayloadCodingDisplays recursively walks a generated payload and
// normalises every coding's display to the canonical CodeSystem display. It is
// the final safeguard applied after all generation paths, so a coding that was
// materialised from a datatype profile or a slice pattern with only system+code
// never ships with a display that merely echoes its code.
func normalisePayloadCodingDisplays(v any, reg *registry.Registry) {
	if reg == nil {
		return
	}
	switch t := v.(type) {
	case map[string]any:
		normaliseCodingDisplay(t, reg)
		for _, val := range t {
			normalisePayloadCodingDisplays(val, reg)
		}
	case []any:
		for _, el := range t {
			normalisePayloadCodingDisplays(el, reg)
		}
	}
}

// normaliseCoding fixes the display on a single coding map. If a canonical
// display is known it is set; if the display merely echoes the code and no
// canonical display is known, the display is dropped rather than echoed.
func normaliseCoding(c any, reg *registry.Registry) {
	m, ok := c.(map[string]any)
	if !ok {
		return
	}
	system, _ := m["system"].(string)
	code, _ := m["code"].(string)
	if system == "" || code == "" {
		return
	}
	if isFixedCoding(m) {
		// A coding fixed by the profile may carry only system+code; HAPI rejects a
		// display on a fixed value that defines only system+code.
		delete(m, "display")
		return
	}
	current, _ := m["display"].(string)
	if current != "" && current != code {
		// An intentional, non-echoed display is preserved.
		return
	}
	resolved := resolveCodingDisplay(reg, system, code)
	switch {
	case resolved != "":
		m["display"] = resolved
	case current == code:
		// Never echo the code as the display when the canonical display is not
		// known.
		delete(m, "display")
	}
}

// resolveCodingDisplay returns the canonical display for code in the CodeSystem
// at system, or "" when the CodeSystem or concept is not indexed.
func resolveCodingDisplay(reg *registry.Registry, system, code string) string {
	if reg == nil || system == "" || code == "" {
		return ""
	}
	cs, ok := reg.CodeSystem(system)
	if !ok || cs == nil {
		return ""
	}
	if c := findCodeSystemConceptByCode(cs.Concepts, code); c != nil {
		return c.Display
	}
	return ""
}

// findCodeSystemConceptByCode returns the first CodeSystemConcept matching code,
// walking nested Concepts, or nil when not found.
func findCodeSystemConceptByCode(concepts []model.CodeSystemConcept, code string) *model.CodeSystemConcept {
	for i := range concepts {
		if concepts[i].Code == code {
			return &concepts[i]
		}
		if child := findCodeSystemConceptByCode(concepts[i].Concepts, code); child != nil {
			return child
		}
	}
	return nil
}

func mergePatternWithBinding(pattern any, typeCode string, binding generatedCoding, hasBinding bool) (any, bool) {
	if !hasBinding {
		return nil, false
	}
	switch typeCode {
	case "CodeableConcept":
		patternMap, ok := pattern.(map[string]any)
		if !ok {
			return nil, false
		}
		merged := cloneMap(patternMap)
		codings := mergeCodingArray(patternMap["coding"], binding)
		if len(codings) > 0 {
			merged["coding"] = codings
		}
		if _, exists := merged["text"]; !exists && binding.Display != "" {
			merged["text"] = binding.Display
		}
		return merged, true
	case "Coding":
		patternMap, ok := pattern.(map[string]any)
		if !ok {
			return nil, false
		}
		merged := cloneMap(patternMap)
		if _, exists := merged["system"]; !exists && binding.System != "" {
			merged["system"] = binding.System
		}
		if _, exists := merged["code"]; !exists && binding.Code != "" {
			merged["code"] = binding.Code
		}
		if _, exists := merged["display"]; !exists && binding.Display != "" {
			merged["display"] = binding.Display
		}
		return merged, true
	default:
		return nil, false
	}
}

func mergeCodingArray(existing any, binding generatedCoding) []any {
	if existingCodings, ok := existing.([]any); ok {
		merged := make([]any, 0, len(existingCodings))
		for _, raw := range existingCodings {
			codingMap, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			merged = append(merged, mergeCodingMap(cloneMap(codingMap), binding))
		}
		if len(merged) > 0 {
			return merged
		}
	}
	bindingMap := codingToMap(binding)
	if len(bindingMap) > 0 {
		return []any{bindingMap}
	}
	return nil
}

func mergeCodingMap(existing map[string]any, binding generatedCoding) map[string]any {
	if existing == nil {
		return codingToMap(binding)
	}
	if _, exists := existing["system"]; !exists && binding.System != "" {
		existing["system"] = binding.System
	}
	if _, exists := existing["code"]; !exists && binding.Code != "" {
		existing["code"] = binding.Code
	}
	if _, exists := existing["display"]; !exists && binding.Display != "" {
		existing["display"] = binding.Display
	}
	return existing
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func primaryTypeCode(def *model.ElementDefinition) string {
	if def == nil || len(def.Types) == 0 {
		return ""
	}
	return strings.TrimSpace(def.Types[0].Code)
}

func referencePlaceholder(def *model.ElementDefinition, reg *registry.Registry) string {
	if def != nil {
		for _, canonical := range def.TargetProfile {
			if resourceType := resolveTargetResourceType(canonical, reg); resourceType != "" && !isAbstractResourceType(resourceType) {
				return resourceType + "/" + coregen.SetupResourceID(resourceType)
			}
		}
		for _, et := range def.Types {
			for _, canonical := range et.TargetProfile {
				if resourceType := resolveTargetResourceType(canonical, reg); resourceType != "" && !isAbstractResourceType(resourceType) {
					return resourceType + "/" + coregen.SetupResourceID(resourceType)
				}
			}
		}
	}
	// No concrete target profile: reference a representative provisioned type
	// rather than the abstract base "Resource" (which is never provisioned). A
	// Reference to a real provisioned resource resolves on the server; pointing
	// at the abstract Resource type produces a dangling reference.
	return "Organization/" + coregen.SetupResourceID("Organization")
}

func normalizeReferenceType(value map[string]any, def *model.ElementDefinition, reg *registry.Registry) {
	if value == nil || def == nil {
		return
	}
	reference, _ := value["reference"].(string)
	targetType := referenceResourceType(reference)
	if targetType == "" {
		targetType = firstTargetResourceType(def, reg)
	}
	if targetType == "" {
		return
	}
	// Reference.type must identify a valid target resource type for this reference.
	value["type"] = targetType
}

func normalizeGeneratedIdentifier(identifier map[string]any) {
	if identifier == nil {
		return
	}
	system, _ := identifier["system"].(string)
	system = strings.TrimSpace(system)
	if system == "http://ns.electronichealth.net.au/id/hi/hpio/1.0" {
		identifier["value"] = generateHPIONumber()
		return
	}
	if system == "http://ns.electronichealth.net.au/id/hi/hpii/1.0" {
		identifier["value"] = generateHPIINumber()
		return
	}
	if system == "http://hl7.org.au/id/abn" {
		identifier["value"] = generateABN()
		return
	}
	if system == "http://hl7.org.au/id/acn" {
		identifier["value"] = generateACN()
		return
	}
	if system == "http://hl7.org.au/id/ahpra-registration-number" {
		identifier["value"] = generateAHPRA()
	}
}

func normalizeGeneratedAddress(address map[string]any) {
	if address == nil {
		return
	}
	country, _ := address["country"].(string)
	if strings.EqualFold(strings.TrimSpace(country), "AU") {
		delete(address, "state")
	}
}

func normalizeGeneratedPayload(value any) {
	switch typed := value.(type) {
	case map[string]any:
		normalizeCodeableConceptMap(typed)
		if _, hasLine := typed["line"]; hasLine {
			if _, hasCity := typed["city"]; hasCity {
				normalizeGeneratedAddress(typed)
			}
		}
		if _, hasSystem := typed["system"]; hasSystem {
			if _, hasValue := typed["value"]; hasValue {
				normalizeGeneratedIdentifier(typed)
			}
		}
		for _, child := range typed {
			normalizeGeneratedPayload(child)
		}
	case []any:
		for _, child := range typed {
			normalizeGeneratedPayload(child)
		}
	}
}

func normalizeCodeableConceptMap(value map[string]any) {
	if value == nil {
		return
	}
	rawCoding, hasCoding := value["coding"]
	if !hasCoding {
		return
	}
	codings, ok := rawCoding.([]any)
	if !ok || len(codings) == 0 {
		return
	}
	firstLabel := ""
	allFixed := true
	for _, raw := range codings {
		coding, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if isFixedCoding(coding) {
			// A fixed coding may carry only system+code; never add a display/text
			// to it, and never add a text to the concept derived from it.
			delete(coding, "display")
			continue
		}
		allFixed = false
		code, _ := coding["code"].(string)
		display, _ := coding["display"].(string)
		if strings.TrimSpace(display) == "" && strings.TrimSpace(code) != "" {
			display = sampleStringValue(code)
			coding["display"] = display
		}
		if firstLabel == "" {
			if strings.TrimSpace(display) != "" {
				firstLabel = display
			} else if strings.TrimSpace(code) != "" {
				firstLabel = sampleStringValue(code)
			}
		}
	}
	if allFixed {
		delete(value, "text")
		return
	}
	if _, hasText := value["text"]; !hasText && firstLabel != "" {
		value["text"] = firstLabel
	}
}

func generateHPIONumber() string {
	base := "800362123456789"
	return appendLuhnCheckDigit(base)
}

func generateHPIINumber() string {
	base := "800361123456789"
	return appendLuhnCheckDigit(base)
}

func appendLuhnCheckDigit(number string) string {
	if len(number) == 0 {
		return ""
	}
	sum := 0
	parity := (len(number) + 1) % 2
	for idx, r := range number {
		digit := int(r - '0')
		if digit < 0 || digit > 9 {
			return number
		}
		if idx%2 == parity {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}
		sum += digit
	}
	checkDigit := (10 - (sum % 10)) % 10
	return number + strconv.Itoa(checkDigit)
}

// generateABN returns a valid 11-digit Australian Business Number. ABNs satisfy
// a mod-89 check digit: subtract 1 from the first digit, weight the 11 digits by
// [10,1,3,5,7,9,11,13,15,17,19], and the sum must be divisible by 89.
func generateABN() string {
	seed := uint64(coregen.StableChecksum("abn"))
	weights := []int{10, 1, 3, 5, 7, 9, 11, 13, 15, 17, 19}
	for i := uint64(0); i < 100000; i++ {
		n := 1000000000 + (seed+i)%9000000000 // 10 digits, first digit 1-9
		prefix := fmt.Sprintf("%010d", n)
		if full, ok := appendMod89Check(prefix, weights, true); ok {
			return full
		}
	}
	return "51824753556"
}

// generateAHPRA returns a syntactically valid Ahpra registration number: three
// uppercase letters followed by ten digits (per the au-ahpraregistrationnumber
// inv-ahpra-0 invariant).
func generateAHPRA() string {
	digits := coregen.StableChecksum("ahpra") % 10000000000
	return "MED" + fmt.Sprintf("%010d", digits)
}

// generateACN returns a valid 9-digit Australian Company Number (mod-89 check
// digit, weights [10,1,3,5,7,9,11,13,15]).
func generateACN() string {
	seed := uint64(coregen.StableChecksum("acn"))
	weights := []int{10, 1, 3, 5, 7, 9, 11, 13, 15}
	for i := uint64(0); i < 100000; i++ {
		n := 10000000 + (seed+i)%90000000 // 8 digits, first digit 1-9
		prefix := fmt.Sprintf("%08d", n)
		if full, ok := appendMod89Check(prefix, weights, false); ok {
			return full
		}
	}
	return "0050043679"
}

// appendMod89Check appends a check digit (0-9) to prefix so the full number
// satisfies the ABN/ACN mod-89 weighting scheme. weights covers every digit
// (the prefix digits plus the appended check digit). When subtractFirst is true
// (ABN), 1 is subtracted from the first digit before weighting. It returns
// (full, true) when a valid check digit exists, otherwise ("", false).
func appendMod89Check(prefix string, weights []int, subtractFirst bool) (string, bool) {
	for c := 0; c <= 9; c++ {
		full := prefix + strconv.Itoa(c)
		if mod89Valid(full, weights, subtractFirst) {
			return full, true
		}
	}
	return "", false
}

func mod89Valid(number string, weights []int, subtractFirst bool) bool {
	if len(number) != len(weights) {
		return false
	}
	sum := 0
	for i := 0; i < len(number); i++ {
		d := int(number[i] - '0')
		if d < 0 || d > 9 {
			return false
		}
		if i == 0 && subtractFirst {
			d -= 1
		}
		sum += d * weights[i]
	}
	return sum%89 == 0
}

func normalizeResourceSpecificPayload(body map[string]any) {
	if body == nil {
		return
	}
	resourceType, _ := body["resourceType"].(string)
	switch resourceType {
	case "HealthcareService":
		normalizeHealthcareServiceTypeCoding(body)
		ensureHealthcareServiceKnownIdentifier(body)
	case "PractitionerRole":
		ensurePractitionerRoleKnownIdentifier(body)
	case "Endpoint":
		ensureEndpointManagingOrganization(body)
		ensureEndpointKnownIdentifier(body)
	case "Practitioner":
		normalizePractitionerFields(body)
		ensureRecordedSexOrGenderValue(body)
	}
}

// recordedSexOrGenderExtensionURL is the canonical URL of the
// individual-recordedSexOrGender extension that hcpd-practitioner requires on
// Practitioner.extension.
const recordedSexOrGenderExtensionURL = "http://hl7.org/fhir/StructureDefinition/individual-recordedSexOrGender"

// ensureRecordedSexOrGenderValue ensures the Practitioner's required
// recordedSexOrGender extension carries its required nested "value"
// sub-extension. The extension is required (Min 1) on hcpd-practitioner and its
// "value" slice is also required (Min 1); momus's slice generation emits other
// optional sub-extensions (e.g. genderElementQualifier) but not the required
// "value" slice, so without this the server rejects the resource with
// "Slice 'Extension.extension:value': a matching slice is required, but not
// found".
func ensureRecordedSexOrGenderValue(body map[string]any) {
	rawExt, ok := body["extension"].([]any)
	if !ok {
		return
	}
	for _, raw := range rawExt {
		ext, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if url, _ := ext["url"].(string); url != recordedSexOrGenderExtensionURL {
			continue
		}
		rawSub, _ := ext["extension"].([]any)
		for _, s := range rawSub {
			if sub, ok := s.(map[string]any); ok {
				if u, _ := sub["url"].(string); u == "value" {
					return // required value slice already present
				}
			}
		}
		ext["extension"] = append(rawSub, map[string]any{
			"url": "value",
			"valueCodeableConcept": map[string]any{
				"coding": []any{map[string]any{
					"system": "http://hl7.org/fhir/administrative-gender",
					"code":   "male",
				}},
			},
		})
		return
	}
}

func normalizePractitionerFields(body map[string]any) {
	// AU PD profiles require Practitioner.active and both official/usual name slices.
	if _, exists := body["active"]; !exists {
		body["active"] = true
	}
	rawName, ok := body["name"].([]any)
	if !ok || len(rawName) == 0 {
		body["name"] = []any{
			map[string]any{"use": "official", "family": "Practitioner", "given": []any{"Setup"}},
			map[string]any{"use": "usual", "family": "Practitioner", "given": []any{"Setup"}},
		}
		return
	}

	hasOfficial := false
	hasUsual := false
	for _, raw := range rawName {
		name, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		use, _ := name["use"].(string)
		use = strings.TrimSpace(use)
		if use == "official" {
			hasOfficial = true
		}
		if use == "usual" {
			hasUsual = true
		}
	}

	firstName, ok := rawName[0].(map[string]any)
	if !ok {
		return
	}
	if !hasOfficial {
		officialCopy := cloneMap(firstName)
		officialCopy["use"] = "official"
		rawName = append(rawName, officialCopy)
	}
	if !hasUsual {
		usualCopy := cloneMap(firstName)
		usualCopy["use"] = "usual"
		rawName = append(rawName, usualCopy)
	}
	body["name"] = rawName
}

func normalizeHealthcareServiceTypeCoding(body map[string]any) {
	values, ok := body["type"].([]any)
	if !ok {
		return
	}
	for _, raw := range values {
		cc, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if _, hasCoding := cc["coding"]; hasCoding {
			continue
		}
		code := "service-type"
		if text, _ := cc["text"].(string); strings.TrimSpace(text) != "" {
			code = sampleCodeValue(text)
		}
		cc["coding"] = []any{map[string]any{
			"system": "http://example.org/fhir/service-type",
			"code":   code,
		}}
	}
}

func ensurePractitionerRoleKnownIdentifier(body map[string]any) {
	raw, ok := body["identifier"]
	if !ok {
		body["identifier"] = []any{practitionerRoleKnownIdentifier()}
		return
	}
	identifiers, ok := raw.([]any)
	if !ok {
		body["identifier"] = []any{practitionerRoleKnownIdentifier()}
		return
	}
	for _, rawIdentifier := range identifiers {
		identifier, ok := rawIdentifier.(map[string]any)
		if !ok {
			continue
		}
		if identifierMatchesPractitionerRoleKnownType(identifier) {
			return
		}
	}
	body["identifier"] = append(identifiers, practitionerRoleKnownIdentifier())
}

func identifierMatchesPractitionerRoleKnownType(identifier map[string]any) bool {
	rawType, ok := identifier["type"].(map[string]any)
	if !ok {
		return false
	}
	rawCoding, ok := rawType["coding"].([]any)
	if !ok {
		return false
	}
	for _, raw := range rawCoding {
		coding, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		system, _ := coding["system"].(string)
		code, _ := coding["code"].(string)
		system = strings.TrimSpace(system)
		code = strings.TrimSpace(code)
		if system == "http://terminology.hl7.org.au/CodeSystem/v2-0203" {
			switch code {
			case "UPIN", "NPIO", "VDI", "AHPRA", "PRN":
				return true
			}
		}
	}
	return false
}

func practitionerRoleKnownIdentifier() map[string]any {
	return map[string]any{
		"system": "http://ns.electronichealth.net.au/id/medicare-provider-number",
		"value":  "UPIN-123456",
		"use":    "usual",
		"type": map[string]any{
			"coding": []any{map[string]any{
				"system":  "http://terminology.hl7.org.au/CodeSystem/v2-0203",
				"code":    "UPIN",
				"display": "Medicare Provider Number",
			}},
			"text": "Medicare Provider Number",
		},
	}
}

func ensureHealthcareServiceKnownIdentifier(body map[string]any) {
	// AU PD requires at least one known HealthcareService identifier slice (au-pd-hs-01).
	raw, ok := body["identifier"]
	if !ok {
		body["identifier"] = []any{healthcareServiceKnownIdentifier()}
		return
	}
	identifiers, ok := raw.([]any)
	if !ok {
		body["identifier"] = []any{healthcareServiceKnownIdentifier()}
		return
	}
	for _, rawIdentifier := range identifiers {
		identifier, ok := rawIdentifier.(map[string]any)
		if !ok {
			continue
		}
		if identifierMatchesHealthcareServiceKnownType(identifier) {
			if system, _ := identifier["system"].(string); strings.TrimSpace(system) == "http://ns.electronichealth.net.au/id/hi/hpio/1.0" {
				normalizeGeneratedIdentifier(identifier)
			}
			return
		}
	}
	body["identifier"] = append(identifiers, healthcareServiceKnownIdentifier())
}

func identifierMatchesHealthcareServiceKnownType(identifier map[string]any) bool {
	rawType, ok := identifier["type"].(map[string]any)
	if !ok {
		return false
	}
	rawCoding, ok := rawType["coding"].([]any)
	if !ok {
		return false
	}
	for _, raw := range rawCoding {
		coding, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		system, _ := coding["system"].(string)
		code, _ := coding["code"].(string)
		system = strings.TrimSpace(system)
		code = strings.TrimSpace(code)
		if system == "http://terminology.hl7.org.au/CodeSystem/v2-0203" {
			switch code {
			case "NOI", "VDI":
				return true
			}
		}
	}
	return false
}

func healthcareServiceKnownIdentifier() map[string]any {
	return map[string]any{
		"system": "http://ns.electronichealth.net.au/id/hi/hpio/1.0",
		"value":  generateHPIONumber(),
		"use":    "usual",
		"type": map[string]any{
			"coding": []any{map[string]any{
				"system":  "http://terminology.hl7.org.au/CodeSystem/v2-0203",
				"code":    "NOI",
				"display": "National provider at organisation",
			}},
			"text": "National provider at organisation",
		},
	}
}

func ensureEndpointManagingOrganization(body map[string]any) {
	if _, exists := body["managingOrganization"]; exists {
		return
	}
	body["managingOrganization"] = map[string]any{
		"reference": "Organization/" + coregen.SetupResourceID("Organization"),
		"type":      "Organization",
		"display":   "Organization",
	}
}

func ensureEndpointKnownIdentifier(body map[string]any) {
	raw, ok := body["identifier"]
	if !ok {
		body["identifier"] = []any{endpointKnownIdentifier()}
		return
	}
	identifiers, ok := raw.([]any)
	if !ok {
		body["identifier"] = []any{endpointKnownIdentifier()}
		return
	}
	for _, rawIdentifier := range identifiers {
		identifier, ok := rawIdentifier.(map[string]any)
		if !ok {
			continue
		}
		system, _ := identifier["system"].(string)
		if strings.TrimSpace(system) == "http://ns.electronichealth.net.au/smd/target" {
			return
		}
	}
	body["identifier"] = append(identifiers, endpointKnownIdentifier())
}

func endpointKnownIdentifier() map[string]any {
	return map[string]any{
		"system": "http://ns.electronichealth.net.au/smd/target",
		"value":  "smd-target-001",
		"use":    "usual",
	}
}

func referenceResourceType(reference string) string {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return ""
	}
	if i := strings.Index(reference, "/"); i > 0 {
		return strings.TrimSpace(reference[:i])
	}
	return ""
}

func firstTargetResourceType(def *model.ElementDefinition, reg *registry.Registry) string {
	if def == nil {
		return ""
	}
	for _, canonical := range def.TargetProfile {
		if resourceType := resolveTargetResourceType(canonical, reg); resourceType != "" {
			return resourceType
		}
	}
	for _, et := range def.Types {
		for _, canonical := range et.TargetProfile {
			if resourceType := resolveTargetResourceType(canonical, reg); resourceType != "" {
				return resourceType
			}
		}
	}
	return ""
}

func sampleStringValue(path string) string {
	name := pathLeaf(path)
	if name == "" {
		name = "value"
	}
	words := strings.Fields(strings.ReplaceAll(name, "-", " "))
	for i, w := range words {
		if w == "" {
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

func sampleCodeValue(path string) string {
	leaf := pathLeaf(path)
	name := coregen.SanitizeFHIRID(leaf)
	if name == "" {
		return "momus-code"
	}
	return name
}

func derivedURIValue(path string) string {
	return "urn:uuid:" + deterministicUUID(path)
}

func deterministicUUID(seed string) string {
	if seed == "" {
		seed = "momus"
	}
	hash := stableHex(seed)
	if len(hash) < 32 {
		hash = hash + strings.Repeat("0", 32-len(hash))
	}
	hash = hash[:32]
	// Version 4, variant 10xx.
	hash = hash[:12] + "4" + hash[13:16] + "a" + hash[17:]
	return hash[0:8] + "-" + hash[8:12] + "-" + hash[12:16] + "-" + hash[16:20] + "-" + hash[20:32]
}

func stableHex(seed string) string {
	const hexChars = "0123456789abcdef"
	parts := [4]uint64{}
	for idx, r := range seed {
		parts[idx%4] = (parts[idx%4]*131 + uint64(r)) & 0xffffffffffffffff
	}
	var b strings.Builder
	for _, part := range parts {
		for shift := 60; shift >= 0; shift -= 4 {
			b.WriteByte(hexChars[(part>>shift)&0xf])
		}
	}
	return b.String()
}

func pathLeaf(path string) string {
	if idx := strings.LastIndex(path, "."); idx >= 0 && idx+1 < len(path) {
		return path[idx+1:]
	}
	return path
}

func coverageCanonicalToResourceType(canonical string) string {
	v := strings.TrimSpace(canonical)
	if v == "" {
		return ""
	}
	if i := strings.Index(v, "|"); i >= 0 {
		v = v[:i]
	}
	if i := strings.LastIndex(v, "/"); i >= 0 && i+1 < len(v) {
		return v[i+1:]
	}
	return v
}

func resolveTargetResourceType(canonical string, reg *registry.Registry) string {
	v := normalizeCanonical(canonical)
	if v == "" {
		return ""
	}
	if reg != nil {
		if sd, ok := reg.StructureDefinition(v); ok && sd != nil && strings.TrimSpace(sd.Type) != "" {
			return sd.Type
		}
	}
	return coverageCanonicalToResourceType(v)
}

func normalizeCanonical(canonical string) string {
	v := strings.TrimSpace(canonical)
	if i := strings.Index(v, "|"); i >= 0 {
		v = v[:i]
	}
	if i := strings.Index(v, "#"); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

func attachDependencyReferences(body map[string]any, resourceType, primaryProfileURL string, deps []string, reg *registry.Registry) {
	for _, dep := range deps {
		switch dep {
		case "Patient":
			ref := map[string]any{"reference": dep + "/" + coregen.SetupResourceID(dep)}
			switch resourceType {
			case "AllergyIntolerance", "Immunization":
				body["patient"] = ref
			case "Appointment":
				body["participant"] = []map[string]any{{"actor": ref, "status": "accepted"}}
			default:
				// Resolve the element that actually references the dependency from the
				// profile instead of hard-coding "subject": resources such as
				// Provenance reference patients via "target" (and have no "subject"
				// element), so writing "subject" would be an undeclared property.
				name := ""
				if reg != nil {
					name = dependencyReferenceElementName(resourceType, primaryProfileURL, dep, reg)
				}
				if name == "" && reg == nil {
					// No registry (legacy call path): preserve the historical
					// "subject" placement rather than dropping the reference.
					name = "subject"
				}
				// With a registry present, only emit the reference under an element
				// the profile actually declares. If no element references the
				// dependency, omit it rather than writing an undeclared property.
				if name != "" {
					body[name] = ref
				}
			}
		case "Encounter":
			body["encounter"] = map[string]any{"reference": dep + "/" + coregen.SetupResourceID(dep)}
		case "Observation":
			body["result"] = []map[string]any{{"reference": dep + "/" + coregen.SetupResourceID(dep)}}
		}
	}
}

// dependencyReferenceElementName returns the JSON property name of the resource
// element that references the given dependency resource type, resolved from the
// primary profile's element tree. It returns "" when no top-level Reference
// element targets the dependency type (callers then omit the reference rather
// than emitting it under an undeclared property).
func dependencyReferenceElementName(resourceType, primaryProfileURL, dependency string, reg *registry.Registry) string {
	if reg == nil || dependency == "" {
		return ""
	}
	profileURL := primaryProfileURL
	if profileURL == "" {
		profileURL = "http://hl7.org/fhir/StructureDefinition/" + resourceType
	}
	resolved, err := reg.ResolveProfile(normalizeCanonical(profileURL))
	if err != nil || resolved == nil || resolved.Root == nil {
		return ""
	}
	for _, child := range resolved.Root.Children {
		if child == nil || child.Definition == nil {
			continue
		}
		if primaryTypeCode(child.Definition) != "Reference" {
			continue
		}
		if firstTargetResourceType(child.Definition, reg) == dependency {
			if name := propertyNameForNode(child); name != "" {
				return name
			}
		}
	}
	return ""
}

// buildSingleRequirementCase builds the strength-1 test for a single coverage
// requirement: one request carrying that requirement's body plus its assert. It
// returns nil when a negative requirement's target element is absent from the
// synthesized payload (so no concrete violation could be constructed); the
// caller skips such cases rather than emitting a reject test a conformant
// server would accept.
