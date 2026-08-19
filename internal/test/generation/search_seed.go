package generation

import (
	"fmt"
	"strings"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
	"github.com/jlcoulter/momus/internal/test/coverage"
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
	for _, code := range codes {
		// _id is a built-in search parameter indexed under the base "Resource"
		// type, so it is resolved directly rather than via the registry lookup.
		if code == "_id" {
			if count > 1 {
				return nil
			}
			params = append(params, &model.SearchParameter{Code: "_id"})
			continue
		}
		sp, ok := options.Registry.SearchParameter(req.ResourceType, code)
		if !ok {
			return nil
		}
		params = append(params, sp)
	}

	value := searchQueryValue(req)
	resourceProfiles := uniqueProfileURLs(byResource[req.ResourceType])
	setupProfileURL := ""
	if len(resourceProfiles) > 0 {
		setupProfileURL = resourceProfiles[0]
	}
	setupProfiles := orderedProfilesForResource(req.ResourceType, setupProfileURL, options.PreferredProfileURLsByResource)
	setupPrimaryProfile := firstProfileURL(setupProfiles)

	out := make([]*model.ResourceInstance, 0, count)
	for i := 0; i < count; i++ {
		localID := searchSeedID(req, i)
		body := buildSetupBody(req.ResourceType, localID, setupProfiles, setupPrimaryProfile, nil, options.Registry, options.Exhaustive)
		matched := true
		for _, sp := range params {
			if !applySearchMatch(body, req.ResourceType, sp, value, options.Registry) {
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
	if index == 0 {
		return "momus-search-" + base
	}
	return fmt.Sprintf("momus-search-%s-%d", base, index+1)
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
	typeCode := searchLeafType(resourceType, elementPath, reg)
	switch typeCode {
	case "string", "code", "uri", "markdown", "id", "oid", "uuid", "base64Binary":
		setPathLeaf(body, elementPath, value)
		return true
	case "HumanName":
		// A string search on HumanName matches the text/family tokens; ensure the
		// value appears there.
		setNameLeaf(body, elementPath, value)
		return true
	case "Address":
		setAddressLeaf(body, elementPath, value)
		return true
	case "CodeableConcept", "Coding":
		setCodeLeaf(body, elementPath, value)
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
// points at, by looking it up in the resource's resolved profile.
func searchLeafType(resourceType, elementPath string, reg *registry.Registry) string {
	profiles := reg.ProfilesForResource(resourceType)
	for _, profile := range profiles {
		resolved, err := reg.ResolveProfile(profile.URL)
		if err != nil || resolved == nil {
			continue
		}
		node, ok := resolved.Elements[resourceType+"."+elementPath]
		if ok && node != nil && node.Definition != nil && len(node.Definition.Types) > 0 {
			return node.Definition.Types[0].Code
		}
	}
	return ""
}

// setPathLeaf sets a primitive string value at a dotted element path within the
// resource body, creating intermediate objects as needed.
func setPathLeaf(body map[string]any, path string, value string) {
	segs := strings.Split(path, ".")
	if len(segs) == 0 {
		return
	}
	cur := body
	for i := 0; i < len(segs)-1; i++ {
		next, ok := cur[segs[i]].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[segs[i]] = next
		}
		cur = next
	}
	cur[segs[len(segs)-1]] = value
}

// setNameLeaf places the search value on a HumanName (which FHIR string search
// indexes via family/text) so the search can match it.
func setNameLeaf(body map[string]any, path string, value string) {
	field := path
	if idx := strings.LastIndex(path, "."); idx >= 0 {
		field = path[idx+1:]
	}
	// Ensure the field is an array of name objects.
	arr, ok := body[field].([]any)
	if !ok || len(arr) == 0 {
		arr = []any{map[string]any{"family": value, "text": value}}
		body[field] = arr
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

// setCodeLeaf places the search value as a code on a CodeableConcept/Coding so
// token search can match it.
func setCodeLeaf(body map[string]any, path string, value string) {
	field := path
	if idx := strings.LastIndex(path, "."); idx >= 0 {
		field = path[idx+1:]
	}
	raw, ok := body[field]
	if !ok {
		body[field] = map[string]any{"coding": []any{map[string]any{"code": value}}}
		return
	}
	if codeable, ok := raw.(map[string]any); ok {
		if coding, ok := codeable["coding"].([]any); ok && len(coding) > 0 {
			if first, ok := coding[0].(map[string]any); ok {
				first["code"] = value
				return
			}
		}
		codeable["coding"] = []any{map[string]any{"code": value}}
		return
	}
	body[field] = map[string]any{"coding": []any{map[string]any{"code": value}}}
}

// setFieldLeaf sets a string sub-field on the first element of a (possibly
// array) field, creating it if needed.
func setFieldLeaf(body map[string]any, path, leaf, value string) {
	field := path
	if idx := strings.LastIndex(path, "."); idx >= 0 {
		field = path[idx+1:]
	}
	raw, ok := body[field]
	if !ok {
		body[field] = []any{map[string]any{leaf: value}}
		return
	}
	if arr, ok := raw.([]any); ok {
		if len(arr) == 0 {
			body[field] = []any{map[string]any{leaf: value}}
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
