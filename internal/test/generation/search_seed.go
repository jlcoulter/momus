package generation

import (
	"fmt"
	"strings"

	"github.com/jlcoulter/momus/internal/core/coverage"
	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

// appendSearchSeedResources adds seed resources that let search-accept
// obligations actually return results. Search coverage is only meaningful when
// a server with matching data answers the request, so for each search-valid,
// search-combination, or search-multiple-results obligation a matching resource
// (two for multiple-results) is added to the dataset. Only obligations whose
// search parameter can be matched with a valid value (string/_id, or a token on
// a code field) get a matching seed; the remaining search types are status-only
// and need no seed data.
func appendSearchSeedResources(ds *model.Dataset, req coverage.CoverageRequirement, options BuildOptions, byResource map[string][]coverage.CoverageRequirement) {
	var count int
	switch req.Variant {
	case coverage.CoverageVariantSearchValid, coverage.CoverageVariantSearchCombination:
		count = 1
	case coverage.CoverageVariantSearchMultipleResults:
		count = 2
	default:
		return
	}
	for _, inst := range buildSearchSeedInstances(req, count, options, byResource) {
		if inst == nil || inst.LocalID == "" {
			continue
		}
		ds.Resources[inst.LocalID] = inst
	}
}

// buildSearchSeedInstances generates up to count matching seed resources for a
// search-accept requirement. It returns nil when the search cannot be matched
// with a valid value (so no seed is added and the test remains status-only).
func buildSearchSeedInstances(req coverage.CoverageRequirement, count int, options BuildOptions, byResource map[string][]coverage.CoverageRequirement) []*model.ResourceInstance {
	if options.Registry == nil || count < 1 {
		return nil
	}
	codes := []string{req.SearchCode}
	if req.SearchCodeB != "" && req.SearchCodeB != req.SearchCode {
		codes = append(codes, req.SearchCodeB)
	}
	// Resolve every search parameter up front. A multi-code search (combination)
	// needs all codes to be matchable; if any is not, adding a partial match
	// could skew the result, so bail out for the whole obligation.
	params := make([]*model.SearchParameter, 0, len(codes))
	idSearch := false
	for _, code := range codes {
		// _id is a built-in search parameter indexed under the base "Resource"
		// type, so it is resolved directly rather than via the registry lookup.
		if code == "_id" {
			if count > 1 {
				return nil
			}
			idSearch = true
			params = append(params, &model.SearchParameter{Code: "_id"})
			continue
		}
		sp, ok := options.Registry.SearchParameter(req.ResourceType, code)
		if !ok {
			return nil
		}
		params = append(params, sp)
	}

	// Compute a value per search code so a combination with mixed value types
	// (e.g. a string `name` and a boolean `active`) gets a type-appropriate value
	// for each parameter instead of reusing one value for both.
	values := make(map[string]string, len(codes))
	for _, code := range codes {
		values[code] = searchQueryValue(req, code, options)
	}
	resourceProfiles := uniqueProfileURLs(byResource[req.ResourceType])
	setupProfileURL := ""
	if len(resourceProfiles) > 0 {
		setupProfileURL = resourceProfiles[0]
	}
	setupProfiles := orderedProfilesForResource(req.ResourceType, setupProfileURL, options.PreferredProfileURLsByResource)
	setupPrimaryProfile := firstProfileURL(setupProfiles)

	out := make([]*model.ResourceInstance, 0, count)
	for i := 0; i < count; i++ {
		// For an _id search the resource id itself must equal the search value
		// (so the URL and body ids agree and _id=<value> matches).
		localID := values[codes[0]]
		if !idSearch {
			localID = searchSeedID(req, i)
		}
		body := buildSetupBody(req.ResourceType, localID, setupProfiles, setupPrimaryProfile, nil, options.Registry, options.Exhaustive)
		matched := true
		for _, sp := range params {
			if !applySearchMatch(body, req.ResourceType, sp, values[sp.Code], options.Registry) {
				matched = false
				break
			}
		}
		if !matched {
			return nil
		}
		out = append(out, &model.ResourceInstance{
			LocalID:      localID,
			ResourceType: req.ResourceType,
			Profile:      setupPrimaryProfile,
			Resource:     body,
		})
	}
	return out
}

func searchSeedID(req coverage.CoverageRequirement, index int) string {
	base := sanitizeFHIRID(req.ID)
	if base == "" {
		base = sanitizeFHIRID(req.ResourceType)
	}
	const prefix = "momus-search-"
	suffix := ""
	if index > 0 {
		suffix = fmt.Sprintf("-%d", index+1)
	}
	// FHIR ids are at most 64 characters. Budget the prefix and the index suffix
	// so the full id stays within the limit even for long requirement ids.
	maxBase := 64 - len(prefix) - len(suffix)
	if len(base) > maxBase {
		base = base[:maxBase]
	}
	base = strings.TrimRight(base, "-.")
	if base == "" {
		base = "seed"
	}
	return prefix + base + suffix
}

// applySearchMatch sets the search value on the element(s) the search parameter
// points to in body, returning false when the value cannot be placed validly.
func applySearchMatch(body map[string]any, resourceType string, sp *model.SearchParameter, value string, reg *registry.Registry) bool {
	if sp.Code == "_id" {
		body["id"] = value
		return true
	}
	elementPath := searchElementPath(sp.Expression, resourceType)
	if elementPath == "" {
		return false
	}
	typeCode, repeatable := searchLeafType(resourceType, elementPath, reg)
	switch typeCode {
	case "string", "markdown", "uri", "url", "id", "oid", "uuid", "base64Binary":
		setPathLeaf(body, elementPath, value)
		return true
	case "code", "Coding", "CodeableConcept":
		// A token search matches the code: for a primitive code it is the scalar,
		// for a Coding it is the object's `code` member, and for a CodeableConcept
		// it is the first coding's `code`. Set the appropriate member without
		// ever adding an illegal property (e.g. `coding` on a Coding) and without
		// collapsing a repeatable element's array to an object. When the element
		// is bound to a value set, keep the coding's system aligned with the code
		// so a required binding is satisfied rather than shipping a system-less
		// coding the server rejects.
		system := boundCodingSystem(resourceType, elementPath, reg)
		setSearchCodeValue(body, elementPath, value, typeCode, repeatable, system)
		return true
	case "HumanName":
		// A string search on HumanName matches the text/family tokens; ensure the
		// value appears there.
		setNameLeaf(body, elementPath, value)
		return true
	case "Address":
		setAddressLeaf(body, elementPath, value)
		return true
	default:
		// boolean/date/number/quantity/reference/choice: "momus-search" is not a
		// valid value, so no matching seed is produced.
		return false
	}
}

// searchElementPath extracts the first simple element path (relative to the
// resource) from a FHIRPath SearchParameter expression, e.g. "Patient.name" ->
// "name", "Observation.code" -> "code", "Patient.name.family" -> "name.family".
// It returns "" for expressions that cannot be reduced to a plain path.
func searchElementPath(expression, resourceType string) string {
	expr := strings.TrimSpace(expression)
	if expr == "" {
		return ""
	}
	// Drop union alternatives and any FHIRPath function call.
	if i := strings.IndexAny(expr, "|("); i >= 0 {
		expr = expr[:i]
	}
	expr = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(expr), "("))
	if expr == "" {
		return ""
	}
	segs := strings.Split(expr, ".")
	if len(segs) == 0 {
		return ""
	}
	// Drop a leading resource/base type token.
	if isBaseResourceTypeToken(segs[0], resourceType) {
		segs = segs[1:]
	}
	if len(segs) == 0 {
		return ""
	}
	for _, s := range segs {
		if s == "" || strings.Contains(s, "[") || strings.Contains(s, ")") {
			return ""
		}
	}
	return strings.Join(segs, ".")
}

func isBaseResourceTypeToken(token, resourceType string) bool {
	token = strings.TrimSpace(token)
	return token == resourceType ||
		token == "Resource" || token == "DomainResource" ||
		token == "CanonicalResource" || token == "MetadataResource"
}

// searchLeafType resolves the FHIR type code of the element a search expression
// points at (and whether it is repeatable), by looking it up in the resource's
// resolved profile.
func searchLeafType(resourceType, elementPath string, reg *registry.Registry) (typeCode string, repeatable bool) {
	profiles := reg.ProfilesForResource(resourceType)
	for _, profile := range profiles {
		resolved, err := reg.ResolveProfile(profile.URL)
		if err != nil || resolved == nil {
			continue
		}
		node, ok := resolved.Elements[resourceType+"."+elementPath]
		if ok && node != nil && node.Definition != nil && len(node.Definition.Types) > 0 {
			return node.Definition.Types[0].Code, node.Definition.Max == "*"
		}
	}
	return "", false
}

// setPathLeaf sets a primitive string value at a dotted element path within the
// resource body, creating intermediate objects as needed. Intermediate
// containers that are repeatable arrays are descended into (their first
// element) rather than being replaced with an object, so e.g. address.city sets
// the city of the first address in the array.
func setPathLeaf(body map[string]any, path string, value string) {
	segs := strings.Split(path, ".")
	if len(segs) == 0 {
		return
	}
	cur := body
	for i := 0; i < len(segs)-1; i++ {
		cur = descendContainer(cur, segs[i])
	}
	cur[segs[len(segs)-1]] = value
}

// descendContainer moves cur into the child named by key, handling a map
// directly and descending into the first element of a repeatable array. It
// creates the child as a map when absent.
func descendContainer(cur map[string]any, key string) map[string]any {
	switch v := cur[key].(type) {
	case map[string]any:
		return v
	case []any:
		if len(v) == 0 {
			el := map[string]any{}
			cur[key] = []any{el}
			return el
		}
		if el, ok := v[0].(map[string]any); ok {
			return el
		}
		el := map[string]any{}
		v[0] = el
		return el
	default:
		el := map[string]any{}
		cur[key] = el
		return el
	}
}

// containerForPath descends into the intermediate containers of a dotted path
// (handling repeatable arrays via descendContainer) and returns the parent map
// plus the leaf key, so nested search values land on the right element.
func containerForPath(body map[string]any, path string) (map[string]any, string) {
	segs := strings.Split(path, ".")
	if len(segs) == 0 {
		return body, ""
	}
	leaf := segs[len(segs)-1]
	cur := body
	for i := 0; i < len(segs)-1; i++ {
		cur = descendContainer(cur, segs[i])
	}
	return cur, leaf
}

// setNameLeaf places the search value on a HumanName (which FHIR string search
// indexes via family/text) so the search can match it.
func setNameLeaf(body map[string]any, path string, value string) {
	cur, field := containerForPath(body, path)
	// Ensure the field is an array of name objects.
	arr, ok := cur[field].([]any)
	if !ok || len(arr) == 0 {
		arr = []any{map[string]any{"family": value, "text": value}}
		cur[field] = arr
		return
	}
	first, ok := arr[0].(map[string]any)
	if !ok {
		arr[0] = map[string]any{"family": value, "text": value}
		return
	}
	if _, hasFamily := first["family"]; !hasFamily {
		first["family"] = value
	}
	if _, hasText := first["text"]; !hasText {
		first["text"] = value
	}
}

// setAddressLeaf places the search value on an Address's text so string search
// can match it.
func setAddressLeaf(body map[string]any, path string, value string) {
	setFieldLeaf(body, path, "text", value)
}

// boundCodingSystem returns the code-system URL for a token-search element that
// is bound to a value set, or "" when it cannot be resolved. It lets the search
// seed place a coding whose system matches the search value's code so a required
// binding validates.
func boundCodingSystem(resourceType, elementPath string, reg *registry.Registry) string {
	if reg == nil {
		return ""
	}
	def, ok := searchElementDefinition(resourceType, elementPath, reg)
	if !ok || def == nil {
		return ""
	}
	if bound, ok := resolveBoundCoding(def, reg); ok {
		return bound.System
	}
	return ""
}

// setSearchCodeValue places a code value that a token search can match on the
// target element, handling a primitive code (scalar), a Coding (its `code`
// member), and a CodeableConcept (its first coding's `code`). It never adds an
// illegal property such as `coding` on a Coding, and it keeps repeatable
// elements as arrays. system (when non-empty) is applied to the coding so a
// required-bound element keeps a valid system/code pair.
func setSearchCodeValue(body map[string]any, path string, value string, typeCode string, repeatable bool, system string) {
	cur, field := containerForPath(body, path)
	if typeCode == "code" {
		// A primitive code holds scalar strings. A repeatable code is an array of
		// strings; set its first element to the string value, never an object
		// (servers reject an object where a simple value is required).
		raw, ok := cur[field]
		if !ok {
			if repeatable {
				cur[field] = []any{value}
			} else {
				cur[field] = value
			}
			return
		}
		if arr, ok := raw.([]any); ok {
			if len(arr) == 0 {
				cur[field] = []any{value}
				return
			}
			arr[0] = value
			return
		}
		cur[field] = value
		return
	}
	raw, ok := cur[field]
	if !ok {
		switch typeCode {
		case "CodeableConcept":
			single := map[string]any{"coding": []any{codingForSearchValue(value, system)}}
			if repeatable {
				cur[field] = []any{single}
			} else {
				cur[field] = single
			}
		case "Coding":
			cur[field] = codingForSearchValue(value, system)
		default:
			// A primitive code: set the scalar.
			cur[field] = value
		}
		return
	}
	switch v := raw.(type) {
	case map[string]any:
		if _, hasCode := v["code"]; hasCode {
			resetCodingForSearchValue(v, nil, value, system)
			return
		}
		if coding, ok := v["coding"].([]any); ok && len(coding) > 0 {
			if first, ok := coding[0].(map[string]any); ok {
				resetCodingForSearchValue(first, v, value, system)
				return
			}
			coding[0] = codingForSearchValue(value, system)
			return
		}
		resetCodingForSearchValue(v, nil, value, system)
	case []any:
		if len(v) == 0 {
			cur[field] = []any{map[string]any{"code": value}}
			return
		}
		first, ok := v[0].(map[string]any)
		if !ok {
			v[0] = map[string]any{"code": value}
			return
		}
		if _, hasCode := first["code"]; hasCode {
			resetCodingForSearchValue(first, nil, value, system)
			return
		}
		if coding, ok := first["coding"].([]any); ok && len(coding) > 0 {
			if c, ok := coding[0].(map[string]any); ok {
				resetCodingForSearchValue(c, first, value, system)
				return
			}
			coding[0] = codingForSearchValue(value, system)
			return
		}
		resetCodingForSearchValue(first, nil, value, system)
	case string:
		cur[field] = value
	default:
		cur[field] = map[string]any{"code": value}
	}
}

// codingForSearchValue builds a coding map for a token search value, carrying the
// resolved system (when known) so a required-bound element stays valid.
func codingForSearchValue(value, system string) map[string]any {
	coding := map[string]any{"code": value}
	if system != "" {
		coding["system"] = system
	}
	return coding
}

// resetCodingForSearchValue overwrites a coding's code with the search value and
// aligns its system with the value's resolved code system (when known), dropping
// a stale display (and the enclosing CodeableConcept text) that belonged to the
// previously-generated concept. Keeping a system-less coding was replaced by
// aligning the system so a required value-set binding validates: e.g. a search
// value overwriting only the code once left a stale system+display from a
// different concept (connectionType "dicom-wado-rs" with the smd-interfaces
// system), which servers reject as an unknown code.
func resetCodingForSearchValue(coding map[string]any, owner map[string]any, value string, system string) {
	coding["code"] = value
	delete(coding, "display")
	if system != "" {
		coding["system"] = system
	} else {
		delete(coding, "system")
	}
	if owner != nil {
		delete(owner, "text")
	}
}

// setFieldLeaf sets a string sub-field on the first element of a (possibly
// array) field, creating it if needed.
func setFieldLeaf(body map[string]any, path, leaf, value string) {
	cur, field := containerForPath(body, path)
	raw, ok := cur[field]
	if !ok {
		cur[field] = []any{map[string]any{leaf: value}}
		return
	}
	if arr, ok := raw.([]any); ok {
		if len(arr) == 0 {
			cur[field] = []any{map[string]any{leaf: value}}
			return
		}
		first, ok := arr[0].(map[string]any)
		if !ok {
			arr[0] = map[string]any{leaf: value}
			return
		}
		if _, exists := first[leaf]; !exists {
			first[leaf] = value
		}
		return
	}
	if m, ok := raw.(map[string]any); ok {
		if _, exists := m[leaf]; !exists {
			m[leaf] = value
		}
	}
}
