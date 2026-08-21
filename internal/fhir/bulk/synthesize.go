package bulk

import (
	"fmt"
	"hash/fnv"
	"math/rand"
	"sort"
	"strconv"
	"strings"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

// optionalInclusionProbability is the chance that an optional (Min == 0)
// element is included in an exhaustive resource. Randomising presence produces
// realistic variation across instances instead of every resource carrying every
// optional field.
const optionalInclusionProbability = 0.5

// refTarget is a resolved relationship reference: the resource type and the
// local dataset ID it points at.
type refTarget struct {
	resourceType string
	localID      string
}

// newRNG returns a deterministic random source seeded from seedString, so the
// same input produces the same resource while different instances vary.
func newRNG(seedString string) *rand.Rand {
	h := fnv.New32a()
	_, _ = h.Write([]byte(seedString))
	return rand.New(rand.NewSource(int64(h.Sum32())))
}

// synthesizeResource builds a concrete resource body for the given resource
// type and profile, populating required elements with realistic sample values,
// wiring relationship references, and (when exhaustive) including a randomised
// subset of optional elements so the result is a fuller, realistic resource.
func synthesizeResource(reg *registry.Registry, resourceType, profileURL, id string, refs map[string]refTarget, exhaustive bool, rng *rand.Rand) (map[string]any, error) {
	body := map[string]any{
		"resourceType": resourceType,
		"id":           id,
	}
	if profileURL == "" {
		profileURL = defaultProfile(reg, resourceType)
	}
	if profileURL != "" {
		// Declare conformance to the profile via meta.profile so the target
		// server (e.g. HAPI) can validate the resource against it; servers
		// commonly reject resources that omit meta.profile.
		body["meta"] = map[string]any{"profile": []any{profileURL}}
		resolved, err := reg.ResolveProfile(profileURL)
		if err != nil {
			return nil, fmt.Errorf("resolve profile %s: %w", profileURL, err)
		}
		if resolved == nil || resolved.Root == nil {
			return nil, fmt.Errorf("profile %s has no element tree", profileURL)
		}
		populateChildren(body, resolved.Root, reg, refs, exhaustive, rng)
	}
	// Drop any Extension that carries neither a value[x] nor a nested sub-extension
	// (violates ext-1); simple extensions are populated with a value above.
	stripEmptyExtensions(body)
	return body, nil
}

func defaultProfile(reg *registry.Registry, resourceType string) string {
	if reg == nil || resourceType == "" {
		return ""
	}
	profiles := reg.ProfilesForResource(resourceType)
	if len(profiles) == 0 {
		return ""
	}
	// Prefer a scoped (package) profile — e.g. hcpd-organization over the base
	// FHIR Organization — so package-specific extensions (such as the suppressed
	// extension) are exercised. The registry is scoped to the selected package's
	// StructureDefinitions, so a profile whose URL is in scope is the profile the
	// user is testing against.
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

// populateChildren fills the children of a node into body. Required (Min > 0)
// children are always populated; optional children are populated only in
// exhaustive mode, and then randomly, so presence varies across instances.
// Sliced children (node.Slices) are emitted as array members of the same
// property: required slices always, optional slices randomly.
func populateChildren(body map[string]any, node *model.ElementNode, reg *registry.Registry, refs map[string]refTarget, exhaustive bool, rng *rand.Rand) {
	if body == nil || node == nil {
		return
	}
	names := make([]string, 0, len(node.Children))
	for name := range node.Children {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		child := node.Children[name]
		if child == nil {
			continue
		}
		propName := nodePropertyName(child)
		if propName == "" || propName == "id" {
			// Skip the resource/element id: ids are assigned by the generator or
			// the target server and must not be synthesised.
			continue
		}
		// A sliced child emits each slice as a member of the same property, so the
		// generic base value is not synthesised in addition to its slices.
		if len(child.Slices) > 0 {
			if members := synthesizeSliceMembers(child, reg, refs, exhaustive, rng); len(members) > 0 {
				body[propName] = members
			}
			continue
		}
		if child.Definition == nil {
			// Intermediate or choice node without its own definition. Descend
			// into it whenever a descendant is required (Min > 0) so required
			// containers are never dropped; otherwise only in exhaustive mode,
			// and then randomly so optional containers vary across instances.
			if len(child.Children) == 0 {
				continue
			}
			if !hasRequiredDescendant(child) {
				if !exhaustive {
					continue
				}
				if rng != nil && rng.Float64() > optionalInclusionProbability {
					continue
				}
			}
			if value := synthesizeNodeValue(child, reg, refs, rng); value != nil {
				body[propName] = value
			}
			continue
		}
		def := child.Definition
		optional := def.Min <= 0 && !hasRequiredSlices(child)
		if !exhaustive && optional {
			continue
		}
		if optional && rng != nil && rng.Float64() > optionalInclusionProbability {
			continue
		}
		value := synthesizeNodeValue(child, reg, refs, rng)
		if value == nil {
			continue
		}
		if isRepeatable(def) {
			body[propName] = []any{value}
		} else {
			body[propName] = value
		}
	}
}

// synthesizeSliceMembers emits the included slices of a sliced child as an array
// of members sharing the child's property name. Required slices (Min > 0) are
// always present; optional slices are present with optionalInclusionProbability
// in exhaustive mode, so a sliced extension like the HCPD suppressed extension
// appears in a fraction of instances like real data.
func synthesizeSliceMembers(node *model.ElementNode, reg *registry.Registry, refs map[string]refTarget, exhaustive bool, rng *rand.Rand) []any {
	if node == nil {
		return nil
	}
	names := make([]string, 0, len(node.Slices))
	for name := range node.Slices {
		names = append(names, name)
	}
	sort.Strings(names)
	var members []any
	for _, name := range names {
		slice := node.Slices[name]
		if slice == nil || slice.Definition == nil {
			continue
		}
		required := slice.Definition.Min > 0
		if !required {
			if !exhaustive {
				continue
			}
			if rng != nil && rng.Float64() > optionalInclusionProbability {
				continue
			}
		}
		if value := synthesizeSliceValue(slice, reg, refs, rng); value != nil {
			members = append(members, value)
		}
	}
	return members
}

// synthesizeSliceValue synthesises the value for a single slice member, using
// the slice's children (and applying nested Fixed/Pattern constraints) so a
// sliced extension such as suppressed resolves its nested coding to the profile's
// fixed value rather than a generic placeholder.
func synthesizeSliceValue(slice *model.SliceNode, reg *registry.Registry, refs map[string]refTarget, rng *rand.Rand) any {
	if slice == nil || slice.Definition == nil {
		return nil
	}
	synthetic := &model.ElementNode{
		Name:       slice.Definition.Name,
		Path:       slice.Definition.Path,
		Definition: slice.Definition,
		Children:   slice.Children,
		Slices:     make(map[string]*model.SliceNode),
	}
	value := synthesizeNodeValue(synthetic, reg, refs, rng)
	if value == nil {
		return nil
	}
	if m, ok := value.(map[string]any); ok {
		applySliceConstraints(m, slice, reg)
		ensureSimpleExtensionValue(m, slice, reg)
	}
	return value
}

// applySliceConstraints overlays a slice's Fixed/Pattern child values onto a
// synthesised slice value, recursing into nested children (e.g. a
// CodeableConcept's coding) so a Fixed value carried several levels deep (such as
// the suppressedBy sub-extension's value[x].coding) is applied rather than left
// as a generic placeholder.
func applySliceConstraints(value map[string]any, slice *model.SliceNode, reg *registry.Registry) {
	if value == nil || slice == nil {
		return
	}
	for _, name := range sortedSliceChildren(slice) {
		child := slice.Children[name]
		if child == nil || child.Definition == nil {
			continue
		}
		applySliceChildConstraints(value, child, reg)
	}
}

// applySliceChildConstraints applies one slice-child's Fixed/Pattern onto the
// value, recursing into nested children that carry no direct Fixed/Pattern.
func applySliceChildConstraints(value map[string]any, child *model.ElementNode, reg *registry.Registry) {
	if value == nil || child == nil || child.Definition == nil {
		return
	}
	def := child.Definition
	prop := nodePropertyName(child)
	if prop == "" || prop == "id" {
		return
	}
	if def.Fixed != nil {
		value[prop] = wrapBulkFixed(value[prop], def, def.Fixed)
		if prop == "coding" {
			delete(value, "text")
		}
		return
	}
	if def.Pattern != nil {
		if patternMap, ok := def.Pattern.(map[string]any); ok {
			mergeBulkSlicePattern(value, prop, patternMap)
			if prop == "coding" {
				delete(value, "text")
			}
		} else {
			value[prop] = def.Pattern
		}
		return
	}
	recurseSliceChildConstraints(value, prop, child, reg)
}

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

func applySliceNodeChildren(value map[string]any, node *model.ElementNode, reg *registry.Registry) {
	if value == nil || node == nil {
		return
	}
	for _, name := range sortedNodeChildren(node) {
		applySliceChildConstraints(value, node.Children[name], reg)
	}
}

func sortedSliceChildren(slice *model.SliceNode) []string {
	out := make([]string, 0, len(slice.Children))
	for name := range slice.Children {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func sortedNodeChildren(node *model.ElementNode) []string {
	out := make([]string, 0, len(node.Children))
	for name := range node.Children {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// wrapBulkFixed returns the Fixed value in the correct shape for the target
// property: if the target is already an array (or the element is repeatable) the
// fixed value is wrapped in a single-element array.
func wrapBulkFixed(current any, def *model.ElementDefinition, fixed any) any {
	if _, isArray := current.([]any); isArray {
		return []any{fixed}
	}
	if isRepeatable(def) {
		return []any{fixed}
	}
	return fixed
}

// mergeBulkSlicePattern merges a pattern object into value at prop, recursing
// into nested objects and arrays so a patterned Coding/CodeableConcept lands.
func mergeBulkSlicePattern(value map[string]any, prop string, pattern map[string]any) {
	current, ok := value[prop].(map[string]any)
	if !ok {
		value[prop] = cloneMap(pattern)
		return
	}
	for k, v := range pattern {
		if subMap, ok := v.(map[string]any); ok {
			if existing, ok := current[k].(map[string]any); ok {
				mergeBulkSlicePattern(existing, k, subMap)
				continue
			}
		}
		current[k] = v
	}
}

// hasRequiredSlices reports whether any slice of a node is required.
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

// hasRequiredDescendant reports whether node or any of its descendants (including
// slice members) is required (Min > 0). It is used to keep required complex
// intermediates present even in non-exhaustive mode.
func hasRequiredDescendant(node *model.ElementNode) bool {
	if node == nil {
		return false
	}
	if node.Definition != nil && node.Definition.Min > 0 {
		return true
	}
	if hasRequiredSlices(node) {
		return true
	}
	for _, child := range node.Children {
		if hasRequiredDescendant(child) {
			return true
		}
	}
	for _, slice := range node.Slices {
		for _, child := range slice.Children {
			if hasRequiredDescendant(child) {
				return true
			}
		}
	}
	return false
}

// ensureSimpleExtensionValue gives a simple Extension slice a value[x] when the
// generic generator emitted only {"url": ...}. A simple extension (Extension.extension
// Max 0) must carry a value[x] to satisfy ext-1; complex extensions (e.g. suppressed)
// already carry sub-extensions and are left alone.
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
	if v := synthesizeNodeValue(valueChild, reg, nil, nil); v != nil {
		value[nodePropertyName(valueChild)] = v
	}
}

// findSliceValueX locates the value[x] element of an extension slice, first from
// the slice's own children and then by resolving the extension profile. It only
// returns a value[x] when the extension genuinely permits one (not a complex
// extension whose value[x] is Max 0).
func findSliceValueX(slice *model.SliceNode, reg *registry.Registry) (*model.ElementNode, bool) {
	root := sliceExtensionRoot(slice, reg)
	if root == nil {
		return nil, false
	}
	if extChild, ok := root.Children["extension"]; ok && extChild != nil && extChild.Definition != nil && extChild.Definition.Max != "0" {
		return nil, false
	}
	vx, ok := root.Children["value[x]"]
	if !ok || vx == nil || vx.Definition == nil || vx.Definition.Max == "0" {
		return nil, false
	}
	return vx, true
}

// sliceExtensionRoot resolves the extension StructureDefinition a slice references,
// preferring the slice's own children when present.
func sliceExtensionRoot(slice *model.SliceNode, reg *registry.Registry) *model.ElementNode {
	if slice == nil || slice.Definition == nil || reg == nil {
		return nil
	}
	if c, ok := slice.Children["value[x]"]; ok && c != nil && c.Definition != nil {
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

// stripEmptyExtensions recursively removes any Extension object that has neither a
// value[x] nor nested sub-extensions, which violates ext-1.
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

// synthesizedCoding is a resolved code value with its system and display.
type synthesizedCoding struct {
	System  string
	Code    string
	Display string
}

// resolveBoundCoding returns a realistic code for a bound element from the
// registry's value sets and code systems, mirroring how a conformant client
// would pick a code.
func resolveBoundCoding(def *model.ElementDefinition, reg *registry.Registry) (synthesizedCoding, bool) {
	if def == nil || def.Binding == nil || reg == nil {
		return synthesizedCoding{}, false
	}
	valueSetURL := normalizeCanonical(def.Binding.ValueSet)
	if valueSetURL == "" {
		return synthesizedCoding{}, false
	}
	vs, ok := reg.ValueSet(valueSetURL)
	if !ok || vs == nil {
		return synthesizedCoding{}, false
	}
	if coding, ok := firstExpansionCoding(vs.ExpansionContains); ok {
		return coding, true
	}
	for _, include := range vs.ComposeIncludes {
		if len(include.Concepts) > 0 {
			concept := include.Concepts[0]
			return synthesizedCoding{System: include.System, Code: concept.Code, Display: concept.Display}, true
		}
		if include.System == "" {
			continue
		}
		if cs, ok := reg.CodeSystem(normalizeCanonical(include.System)); ok && cs != nil {
			if concept, ok := firstCodeSystemConcept(cs.Concepts); ok {
				return synthesizedCoding{System: include.System, Code: concept.Code, Display: concept.Display}, true
			}
		}
	}
	return synthesizedCoding{}, false
}

func firstExpansionCoding(entries []model.ValueSetExpansionContains) (synthesizedCoding, bool) {
	for _, entry := range entries {
		if entry.Code != "" {
			return synthesizedCoding{System: entry.System, Code: entry.Code, Display: entry.Display}, true
		}
		if coding, ok := firstExpansionCoding(entry.Contains); ok {
			return coding, true
		}
	}
	return synthesizedCoding{}, false
}

func firstCodeSystemConcept(concepts []model.CodeSystemConcept) (model.CodeSystemConcept, bool) {
	for _, concept := range concepts {
		if concept.Code != "" {
			return concept, true
		}
		if child, ok := firstCodeSystemConcept(concept.Concepts); ok {
			return child, true
		}
	}
	return model.CodeSystemConcept{}, false
}

func codingToMap(coding synthesizedCoding) map[string]any {
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

func normalizeCanonical(s string) string {
	return strings.TrimSpace(s)
}

// synthesizeNodeValue produces a realistic sample value for an element node,
// recursing into complex datatypes, honouring fixed/pattern/example values and
// terminology bindings, and resolving relationship references.
func synthesizeNodeValue(node *model.ElementNode, reg *registry.Registry, refs map[string]refTarget, rng *rand.Rand) any {
	if node == nil || node.Definition == nil {
		if node != nil && len(node.Children) > 0 {
			value := map[string]any{}
			populateChildren(value, node, reg, refs, true, rng)
			if len(value) == 0 {
				return nil
			}
			return value
		}
		return nil
	}
	def := node.Definition
	typeCode := primaryTypeCode(def)
	if def.Fixed != nil {
		return def.Fixed
	}
	boundCoding, hasBoundCoding := resolveBoundCoding(def, reg)
	if def.Pattern != nil {
		if merged, ok := mergePatternWithBinding(def.Pattern, typeCode, boundCoding, hasBoundCoding); ok {
			return merged
		}
		return def.Pattern
	}
	if len(def.Examples) > 0 {
		return def.Examples[0]
	}
	switch typeCode {
	case "string", "markdown":
		return realisticText(def.Path, rng)
	case "id":
		return deterministicID(def.Path, rng)
	case "uri", "url", "canonical", "oid", "uuid":
		return "http://example.org/fhir/" + leafName(def.Path)
	case "code":
		if hasBoundCoding && boundCoding.Code != "" {
			return boundCoding.Code
		}
		return defaultCodeValue(def.Path)
	case "boolean":
		return rng != nil && rng.Intn(2) == 0
	case "integer", "unsignedInt", "positiveInt":
		if rng != nil {
			return rng.Intn(1000) + 1
		}
		return 1
	case "decimal":
		if rng != nil {
			return float64(rng.Intn(1000)+1) / 10.0
		}
		return 1.5
	case "date":
		return randomDate(rng)
	case "dateTime", "instant":
		return randomDateTime(rng)
	case "time":
		return randomTime(rng)
	case "Identifier":
		value := map[string]any{
			"system": "http://example.org/fhir/identifier/" + sanitizeID(leafName(def.Path)),
			"value":  realisticIdentifier(def.Path, rng),
		}
		populateChildren(value, node, reg, refs, false, rng)
		return value
	case "HumanName":
		value := map[string]any{"family": pickString(familyNames, def.Path, rng), "given": []any{pickString(givenNames, def.Path+".given", rng)}}
		populateChildren(value, node, reg, refs, false, rng)
		return value
	case "Address":
		value := map[string]any{
			"line":    []any{fmtAddressLine(rng)},
			"city":    pickString(cities, def.Path, rng),
			"state":   pickString(states, def.Path+".state", rng),
			"country": "AU",
		}
		populateChildren(value, node, reg, refs, false, rng)
		return value
	case "ContactPoint":
		value := map[string]any{"system": "phone", "value": fmtPhone(rng), "use": "home"}
		populateChildren(value, node, reg, refs, false, rng)
		return value
	case "CodeableConcept":
		if hasBoundCoding {
			concept := map[string]any{"coding": []any{codingToMap(boundCoding)}}
			if boundCoding.Display != "" {
				concept["text"] = boundCoding.Display
			}
			return concept
		}
		return map[string]any{
			"text": realisticText(def.Path, rng),
			"coding": []any{map[string]any{
				"system":  "http://terminology.hl7.org/CodeSystem/v3-NullFlavor",
				"code":    defaultCodeValue(def.Path),
				"display": realisticText(def.Path, rng),
			}},
		}
	case "Coding":
		if hasBoundCoding {
			return codingToMap(boundCoding)
		}
		return map[string]any{"system": "http://example.org/fhir/code-system", "code": defaultCodeValue(def.Path)}
	case "Quantity":
		value := map[string]any{
			"value":  quantityValue(rng),
			"unit":   "sample",
			"system": "http://unitsofmeasure.org",
			"code":   "1",
		}
		populateChildren(value, node, reg, refs, false, rng)
		return value
	case "Period":
		return randomPeriod(rng)
	case "Reference":
		if ref, ok := refs[node.Path]; ok {
			return map[string]any{"reference": ref.resourceType + "/" + ref.localID}
		}
		return map[string]any{"reference": "Patient/unknown"}
	default:
		if value, ok := synthesizeProfileValue(def, reg, refs, rng); ok {
			return value
		}
		value := map[string]any{}
		populateChildren(value, node, reg, refs, true, rng)
		if len(value) == 0 {
			return nil
		}
		return value
	}
}

// synthesizeProfileValue produces a value for an element with a profiled
// datatype by resolving the first referenced profile.
func synthesizeProfileValue(def *model.ElementDefinition, reg *registry.Registry, refs map[string]refTarget, rng *rand.Rand) (any, bool) {
	if reg == nil || def == nil {
		return nil, false
	}
	for _, et := range def.Types {
		for _, profileURL := range et.Profile {
			resolved, err := reg.ResolveProfile(normalizeCanonical(profileURL))
			if err != nil || resolved == nil || resolved.Root == nil {
				continue
			}
			value := map[string]any{}
			populateChildren(value, resolved.Root, reg, refs, true, rng)
			if len(value) == 0 {
				continue
			}
			return value, true
		}
	}
	return nil, false
}

// mergePatternWithBinding merges a fixed pattern object with a resolved
// binding so patterned codings still carry a real code.
func mergePatternWithBinding(pattern any, typeCode string, binding synthesizedCoding, hasBinding bool) (any, bool) {
	if !hasBinding {
		return nil, false
	}
	patternMap, ok := pattern.(map[string]any)
	if !ok {
		return nil, false
	}
	switch typeCode {
	case "CodeableConcept", "Coding":
		merged := cloneMap(patternMap)
		if binding.System != "" {
			merged["system"] = binding.System
		}
		if binding.Code != "" {
			merged["code"] = binding.Code
		}
		if binding.Display != "" {
			merged["display"] = binding.Display
		}
		return merged, true
	default:
		return nil, false
	}
}

func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// nodePropertyName maps an element node to its JSON property name, handling
// choice elements (value[x] becomes valueString etc) and intermediate nodes.
func nodePropertyName(node *model.ElementNode) string {
	if node == nil {
		return ""
	}
	if node.Definition == nil {
		return strings.TrimSuffix(node.Name, "[x]")
	}
	def := node.Definition
	leaf := leafName(def.Path)
	if isChoiceName(node.Name) {
		leaf = strings.TrimSuffix(leaf, "[x]")
		if len(def.Types) > 0 {
			if tc := def.Types[0].Code; tc != "" {
				return leaf + upperFirst(tc)
			}
		}
	}
	return leaf
}

func isChoiceName(name string) bool {
	return strings.HasSuffix(name, "[x]")
}

func leafName(path string) string {
	if idx := strings.LastIndex(path, "."); idx >= 0 && idx+1 < len(path) {
		return path[idx+1:]
	}
	return path
}

func primaryTypeCode(def *model.ElementDefinition) string {
	if def == nil || len(def.Types) == 0 {
		return ""
	}
	return def.Types[0].Code
}

func isRepeatable(def *model.ElementDefinition) bool {
	if def == nil {
		return false
	}
	if def.Max == "*" {
		return true
	}
	n, err := strconv.Atoi(def.Max)
	return err == nil && n > 1
}

// defaultCodeByLeaf maps common element leaf names to realistic codes for
// elements whose terminology binding could not be resolved from the registry.
var defaultCodeByLeaf = map[string]string{
	"status":     "active",
	"gender":     "female",
	"priority":   "routine",
	"severity":   "moderate",
	"class":      "inpatient",
	"use":        "usual",
	"mode":       "production",
	"state":      "active",
	"daysOfWeek": "mon",
	"daysofweek": "mon",
}

// defaultCodeValue returns a realistic default code for an unresolvable bound
// code element, preferring a field-specific default and falling back to a
// neutral token rather than the raw leaf name. The path is used to disambiguate
// leaves that share a name across different bindings (e.g. role).
func defaultCodeValue(path string) string {
	lower := strings.ToLower(path)
	// Provenance.entity.role is bound to ProvenanceEntityRole (codes such as
	// derivation/revision/source), distinct from agent.role's binding.
	if strings.Contains(lower, ".entity.role") || strings.HasSuffix(lower, ".entity.role") {
		return "source"
	}
	leaf := strings.ToLower(leafName(path))
	if code, ok := defaultCodeByLeaf[leaf]; ok {
		return code
	}
	return "UNK"
}

func upperFirst(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// pickString deterministically selects a value from a pool for a given path.
func pickString(pool []string, path string, rng *rand.Rand) string {
	if len(pool) == 0 {
		return ""
	}
	if rng != nil {
		return pool[rng.Intn(len(pool))]
	}
	return pool[int(hashString(path))%len(pool)]
}

func hashString(s string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return h.Sum32()
}

var givenNames = []string{
	"Liam", "Olivia", "Noah", "Emma", "Oliver", "Ava", "Elijah", "Sophia",
	"Lucas", "Mia", "Henry", "Charlotte", "James", "Amelia", "Benjamin", "Harper",
}

var familyNames = []string{
	"Smith", "Johnson", "Williams", "Brown", "Jones", "Garcia", "Miller",
	"Davis", "Wilson", "Anderson", "Thomas", "Taylor", "Moore", "Jackson",
}

var cities = []string{"Sydney", "Melbourne", "Brisbane", "Perth", "Adelaide", "Canberra", "Hobart", "Darwin"}

var states = []string{"NSW", "VIC", "QLD", "WA", "SA", "ACT", "TAS", "NT"}

var textPool = []string{
	"Routine clinical documentation recorded during a standard encounter.",
	"Representative sample generated for conformance and coverage testing.",
	"Example entry illustrating a complete, well-formed resource.",
	"Documented assessment supporting the patient's clinical record.",
	"Recorded observation captured as part of routine care.",
}

func realisticText(path string, rng *rand.Rand) string {
	return pickString(textPool, path, rng)
}

func deterministicID(path string, rng *rand.Rand) string {
	return sanitizeID(leafName(path)) + "-" + pickString(familyNames, path, rng)
}

func realisticIdentifier(path string, rng *rand.Rand) string {
	return strings.ToUpper(pickString(familyNames, path, rng)) + "-" + strconv.Itoa(int(hashString(path+"value")%9000+1000))
}

func fmtAddressLine(rng *rand.Rand) string {
	n := 1
	if rng != nil {
		n = rng.Intn(900) + 100
	}
	return strconv.Itoa(n) + " " + pickString([]string{"Main", "Queen", "George", "Market", "York", "King"}, "street", rng) + " Street"
}

func fmtPhone(rng *rand.Rand) string {
	if rng != nil {
		area := rng.Intn(8) + 2
		return fmt.Sprintf("0%d%d%d %d%d%d %d%d%d%d", area, rng.Intn(10), rng.Intn(10), rng.Intn(10), rng.Intn(10), rng.Intn(10), rng.Intn(10), rng.Intn(10), rng.Intn(10), rng.Intn(10))
	}
	return "0299999999"
}

func quantityValue(rng *rand.Rand) float64 {
	if rng != nil {
		return float64(rng.Intn(500)+1) / 10.0
	}
	return 1.5
}

func randomDate(rng *rand.Rand) string {
	if rng == nil {
		return "2024-01-01"
	}
	y := 1950 + rng.Intn(70)
	m := rng.Intn(12) + 1
	d := rng.Intn(28) + 1
	return fmt.Sprintf("%04d-%02d-%02d", y, m, d)
}

func randomDateTime(rng *rand.Rand) string {
	return randomDate(rng) + "T" + randomTime(rng) + "Z"
}

func randomTime(rng *rand.Rand) string {
	if rng == nil {
		return "00:00:00"
	}
	return fmt.Sprintf("%02d:%02d:%02d", rng.Intn(24), rng.Intn(60), rng.Intn(60))
}

func randomPeriod(rng *rand.Rand) map[string]any {
	return map[string]any{"start": randomDateTime(rng), "end": randomDateTime(rng)}
}
