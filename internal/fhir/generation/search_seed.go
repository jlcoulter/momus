package generation

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jlcoulter/momus/internal/core/coverage"
	coregen "github.com/jlcoulter/momus/internal/core/generation"
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
func appendSearchSeedResources(
	ds *model.Dataset,
	req coverage.CoverageRequirement,
	options BuildOptions,
	byResource map[string][]coverage.CoverageRequirement,
) {
	if req.Variant == coverage.CoverageVariantSearchChaining {
		// A chaining search needs both the primary resource (referencing the
		// target) and the target resource (carrying the terminal value), so the
		// returned Bundle contains a primary result and the server can resolve
		// the chain through the reference.
		for _, inst := range buildChainSeedInstances(req, options, byResource) {
			if inst == nil || inst.LocalID == "" {
				continue
			}
			ds.Resources[inst.LocalID] = inst
		}
		return
	}
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

// buildChainSeedInstances builds the seed resources a chaining search needs: a
// primary resource of req.ResourceType that references a target resource of
// req.SearchTargetType whose terminal search parameter (req.SearchTargetCode)
// equals the query value. Returns the primary and target instances, or nil when
// the chain cannot be seeded (so the test stays status-only).
func buildChainSeedInstances(
	req coverage.CoverageRequirement,
	options BuildOptions,
	byResource map[string][]coverage.CoverageRequirement,
) []*model.ResourceInstance {
	if options.Registry == nil || req.SearchTargetType == "" || req.SearchTargetCode == "" {
		return nil
	}
	// The first segment of the chain is a reference search parameter on the
	// primary type; resolve it so the primary seed can reference the target.
	firstSeg := strings.SplitN(req.SearchCode, ".", 2)[0]
	refParam, ok := options.Registry.SearchParameter(req.ResourceType, firstSeg)
	if !ok {
		return nil
	}
	// The terminal value is the query value for the target parameter on the
	// target type.
	targetReq := req
	targetReq.ResourceType = req.SearchTargetType
	builder := NewBuilder(options.Registry, options.Exhaustive)
	value := SearchQueryValue(targetReq, req.SearchTargetCode, builder)
	if value == "" {
		return nil
	}

	// Build the target resource carrying the terminal value.
	targetParams := byResource[req.SearchTargetType]
	targetProfiles := coregen.UniqueProfileURLs(targetParams)
	targetProfileURL := ""
	if len(targetProfiles) > 0 {
		targetProfileURL = targetProfiles[0]
	}
	targetSetupProfiles := coregen.OrderedProfilesForResource(
		req.SearchTargetType,
		targetProfileURL,
		options.PreferredProfileURLsByResource,
	)
	targetPrimaryProfile := coregen.FirstProfileURL(targetSetupProfiles)
	targetID := coregen.SetupResourceID(req.SearchTargetType)
	targetBody := buildSetupBody(
		req.SearchTargetType,
		targetID,
		targetSetupProfiles,
		targetPrimaryProfile,
		nil,
		options.Registry,
		options.Exhaustive,
	)
	targetSP, ok := options.Registry.SearchParameter(req.SearchTargetType, req.SearchTargetCode)
	if !ok {
		return nil
	}
	if !applySearchMatch(targetBody, req.SearchTargetType, targetSP, value, options.Registry) {
		return nil
	}
	normalisePayloadCodingDisplays(targetBody, options.Registry)

	// Build the primary resource referencing the target.
	primaryParams := byResource[req.ResourceType]
	primaryProfiles := coregen.UniqueProfileURLs(primaryParams)
	primaryProfileURL := ""
	if len(primaryProfiles) > 0 {
		primaryProfileURL = primaryProfiles[0]
	}
	primarySetupProfiles := coregen.OrderedProfilesForResource(
		req.ResourceType,
		primaryProfileURL,
		options.PreferredProfileURLsByResource,
	)
	primaryPrimaryProfile := coregen.FirstProfileURL(primarySetupProfiles)
	primaryID := searchSeedID(req, 0)
	primaryBody := buildSetupBody(
		req.ResourceType,
		primaryID,
		primarySetupProfiles,
		primaryPrimaryProfile,
		nil,
		options.Registry,
		options.Exhaustive,
	)
	// Place a reference on the primary resource's reference element pointing at
	// the target resource.
	refTarget := req.SearchTargetType + "/" + targetID
	if !applySearchMatch(primaryBody, req.ResourceType, refParam, refTarget, options.Registry) {
		return nil
	}
	normalisePayloadCodingDisplays(primaryBody, options.Registry)

	return []*model.ResourceInstance{
		{
			LocalID:      targetID,
			ResourceType: req.SearchTargetType,
			Profile:      targetPrimaryProfile,
			Resource:     targetBody,
		},
		{
			LocalID:      primaryID,
			ResourceType: req.ResourceType,
			Profile:      primaryPrimaryProfile,
			Resource:     primaryBody,
		},
	}
}

// buildSearchSeedInstances generates up to count matching seed resources for a
// search-accept requirement. It returns nil when the search cannot be matched
// with a valid value (so no seed is added and the test remains status-only).
func buildSearchSeedInstances(
	req coverage.CoverageRequirement,
	count int,
	options BuildOptions,
	byResource map[string][]coverage.CoverageRequirement,
) []*model.ResourceInstance {
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
	builder := NewBuilder(options.Registry, options.Exhaustive)
	for _, code := range codes {
		values[code] = SearchQueryValue(req, code, builder)
	}
	resourceProfiles := coregen.UniqueProfileURLs(byResource[req.ResourceType])
	setupProfileURL := ""
	if len(resourceProfiles) > 0 {
		setupProfileURL = resourceProfiles[0]
	}
	setupProfiles := coregen.OrderedProfilesForResource(
		req.ResourceType,
		setupProfileURL,
		options.PreferredProfileURLsByResource,
	)
	setupPrimaryProfile := coregen.FirstProfileURL(setupProfiles)

	out := make([]*model.ResourceInstance, 0, count)
	for i := 0; i < count; i++ {
		// For an _id search the resource id itself must equal the search value
		// (so the URL and body ids agree and _id=<value> matches).
		localID := values[codes[0]]
		if !idSearch {
			localID = searchSeedID(req, i)
		}
		body := buildSetupBody(
			req.ResourceType,
			localID,
			setupProfiles,
			setupPrimaryProfile,
			nil,
			options.Registry,
			options.Exhaustive,
		)
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
		// Re-resolve coding displays: overwriting a search value resets the coding
		// (dropping its display), but a profile may require a display on the very
		// element the search writes (e.g. HealthcareService.type.coding.display).
		// Normalising after the match restores the canonical display so the seed
		// still validates.
		normalisePayloadCodingDisplays(body, options.Registry)
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
	base := coregen.SanitizeFHIRID(req.ID)
	if base == "" {
		base = coregen.SanitizeFHIRID(req.ResourceType)
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
func applySearchMatch(
	body map[string]any,
	resourceType string,
	sp *model.SearchParameter,
	value string,
	reg *registry.Registry,
) bool {
	if sp.Code == "_id" {
		body["id"] = value
		return true
	}
	elementPath := searchElementPath(sp.Expression, resourceType)
	if elementPath == "" {
		return false
	}
	// A composite search combines two component values into one query value
	// "part1$part2". Split the search value on '$' and place each component on
	// the corresponding element the expression names (split on '|').
	if strings.ToLower(sp.Type) == "composite" {
		return applyCompositeMatch(body, sp.Expression, resourceType, value, reg)
	}
	typeCode, repeatable := searchLeafType(resourceType, elementPath, reg)
	// A special search (e.g. near) matches geographic coordinates, not a single
	// leaf's primitive value. Set the Location position's lat/long from the
	// "lat|long" search value, independent of the leaf element's own type.
	if strings.ToLower(sp.Type) == "special" {
		setSpecialLeaf(body, elementPath, value)
		return true
	}
	// A date search targets a date/dateTime value regardless of the element's
	// first choice type (e.g. Provenance.occurred[x] is Period|dateTime). Place
	// the date value on the concrete dateTime choice member.
	switch strings.ToLower(sp.Type) {
	case "date", "dateTime", "instant", "time":
		setDateLeaf(body, elementPath, value, reg, resourceType)
		return true
	}
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
	case "Identifier":
		// A token search on an Identifier matches its `value` member (and a
		// type/system search may match those). Force the value onto the first
		// identifier so the search seed carries the query value.
		setFieldLeafForce(body, elementPath, "value", value)
		return true
	case "ContactPoint":
		// A token search on a ContactPoint matches its `value` (telecom number/
		// address) and `system`. Force the value onto the first contact point.
		setFieldLeafForce(body, elementPath, "value", value)
		return true
	case "Address":
		setAddressLeaf(body, elementPath, value)
		return true
	case "boolean":
		setPathLeafBoolean(body, elementPath, value)
		return true
	case "date", "dateTime", "instant", "time":
		// A date search matches the element's date value. Use the search value
		// (a valid date) so the provisioned seed is matched by the query. A
		// choice-type element (e.g. Provenance.occurred[x]) serialises under the
		// concrete choice key (occurredDateTime), so set that rather than the
		// bare choice name.
		setDateLeaf(body, elementPath, value, reg, resourceType)
		return true
	case "integer", "unsignedInt", "positiveInt", "decimal", "number":
		// A number search matches a numeric element value. The search value is
		// type-valid, so place it on the leaf.
		setPathLeaf(body, elementPath, value)
		return true
	case "Reference":
		// A reference search matches the reference string ("Type/id") held in the
		// Reference object's `reference` member.
		setReferenceLeaf(body, elementPath, value)
		return true
	case "Quantity":
		// A quantity search matches value/system/code of a Quantity element. Set
		// the first three members so the query "value|system|code" can match.
		setQuantityLeaf(body, elementPath, value)
		return true
	default:
		// composite/unknown: no single leaf type can be seeded; the caller
		// returns nil so the obligation remains status-only.
		return false
	}
}

// searchElementPath extracts a simple element path (relative to the resource)
// from a FHIRPath SearchParameter expression, e.g. "Patient.name" -> "name",
// "Observation.code" -> "code", "Patient.name.family" -> "name.family". For a
// union of alternatives it selects the branch rooted at the resource type, so a
// Practitioner search expressed as "Patient.gender | ... | Practitioner.gender"
// resolves to "gender" rather than the first (wrong) branch. It returns "" for
// expressions that cannot be reduced to a plain path.
func searchElementPath(expression, resourceType string) string {
	expr := strings.TrimSpace(expression)
	if expr == "" {
		return ""
	}
	// Prefer a union branch whose first segment is the resource type. Otherwise
	// fall back to the first branch that is a plain path.
	candidates := splitUnion(expr)
	var firstPlain string
	for _, cand := range candidates {
		p := plainSearchPath(cand, resourceType)
		if p == "" {
			continue
		}
		if firstPlain == "" {
			firstPlain = p
		}
		// Prefer a branch rooted at the resource type itself.
		root := strings.TrimSpace(cand)
		if root != "" && root[0] == '(' {
			root = strings.TrimSpace(strings.TrimPrefix(root, "("))
		}
		if i := strings.IndexByte(root, '.'); i >= 0 {
			root = root[:i]
		}
		if root == resourceType || root == "Resource" || root == "DomainResource" {
			return p
		}
	}
	return firstPlain
}

// splitUnion splits a FHIRPath expression on top-level '|' union operators,
// respecting parentheses.
func splitUnion(expr string) []string {
	var parts []string
	depth := 0
	start := 0
	for i, r := range expr {
		switch r {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case '|':
			if depth == 0 {
				parts = append(parts, expr[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, expr[start:])
	return parts
}

// plainSearchPath reduces a single candidate to a plain dotted element path,
// returning "" when it contains a function call, filter, or other non-path
// construct.
// plainSearchPath reduces a single candidate to a plain dotted element path,
// returning "" when it contains a function call, filter, or other non-path
// construct. A trailing FHIRPath type cast such as "x as dateTime" is stripped.
func plainSearchPath(candidate, resourceType string) string {
	expr := strings.TrimSpace(candidate)
	if expr == "" {
		return ""
	}
	// Drop any leading "(".
	expr = strings.TrimPrefix(expr, "(")
	expr = strings.TrimSpace(expr)
	// Strip a trailing type cast "... as Type".
	if i := strings.Index(expr, " as "); i >= 0 {
		expr = strings.TrimSpace(expr[:i])
	}
	// Truncate at a function call. A trailing ".name(" (e.g. ".where(",
	// ".exists(") is a method call, not a field, so drop the incomplete segment.
	if i := strings.IndexByte(expr, '('); i >= 0 {
		head := expr[:i]
		head = strings.TrimRight(head, " ")
		if lastDot := strings.LastIndexByte(head, '.'); lastDot >= 0 {
			method := head[lastDot+1:]
			if method == "" || isFunctionName(method) {
				// Strip the trailing method segment (the part after the last dot).
				expr = head[:lastDot]
			} else {
				expr = head
			}
		} else {
			expr = head
		}
	}
	expr = strings.TrimSpace(expr)
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

// isBaseResourceTypeToken reports whether token is a leading FHIR resource or
// base type name in a path (the resource type itself, or Resource/DomainResource
// and kin). It is used to strip the leading type from a search element path.
func isBaseResourceTypeToken(token, resourceType string) bool {
	token = strings.TrimSpace(token)
	return token == resourceType ||
		token == "Resource" || token == "DomainResource" ||
		token == "CanonicalResource" || token == "MetadataResource"
}

// isFunctionName reports whether name looks like a FHIRPath function call,
// i.e. a bare identifier immediately followed by "(" in a path expression. It
// helps strip a trailing method (e.g. ".where(") from a search element path so
// the remaining dotted prefix is the plain element path.
func isFunctionName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

// searchLeafType resolves the FHIR type code of the element a search expression
// points at (and whether it is repeatable). It first looks the full path up in
// the resource's resolved profile (with the choice "[x]" form), then falls back
// to walking nested datatypes (e.g. Provenance.signature.type resolves through
// the Signature datatype's own definition).
func searchLeafType(
	resourceType, elementPath string,
	reg *registry.Registry,
) (typeCode string, repeatable bool) {
	profiles := reg.ProfilesForResource(resourceType)
	for _, profile := range profiles {
		resolved, err := reg.ResolveProfile(profile.URL)
		if err != nil || resolved == nil {
			continue
		}
		// Try the exact key, then the choice-type "[x]" form (e.g. "occurred" ->
		// "occurred[x]") when the element is a choice.
		keys := []string{resourceType + "." + elementPath}
		if !strings.HasSuffix(elementPath, "[x]") {
			keys = append(keys, resourceType+"."+elementPath+"[x]")
		}
		for _, key := range keys {
			node, ok := resolved.Elements[key]
			if ok && node != nil && node.Definition != nil && len(node.Definition.Types) > 0 {
				return node.Definition.Types[0].Code, node.Definition.Max == "*"
			}
		}
		// Nested datatype path: walk segments, resolving each container's type,
		// then look up the leaf in the datatype's own definition.
		if tc, rep, found := resolveNestedLeafType(
			resolved,
			resourceType,
			elementPath,
			reg,
		); found {
			return tc, rep
		}
	}
	return "", false
}

// resolveNestedLeafType walks a dotted element path whose container is a complex
// datatype, resolving the leaf's type from the datatype's own StructureDefinition.
func resolveNestedLeafType(
	resolved *model.ResolvedProfile,
	resourceType, elementPath string,
	reg *registry.Registry,
) (string, bool, bool) {
	segments := strings.Split(elementPath, ".")
	if len(segments) < 2 {
		return "", false, false
	}
	// Resolve the top-level container (the first segment) to its datatype.
	container, ok := resolved.Elements[resourceType+"."+segments[0]]
	if !ok || container == nil || container.Definition == nil ||
		len(container.Definition.Types) == 0 {
		return "", false, false
	}
	containerType := container.Definition.Types[0].Code
	sub, err := reg.ResolveProfile("http://hl7.org/fhir/StructureDefinition/" + containerType)
	if err != nil || sub == nil {
		return "", false, false
	}
	// Walk the remaining segments within the datatype definition.
	cur := sub
	for i := 1; i < len(segments); i++ {
		key := containerType + "." + strings.Join(segments[1:i+1], ".")
		node, ok := cur.Elements[key]
		if !ok || node == nil || node.Definition == nil {
			return "", false, false
		}
		if i == len(segments)-1 {
			if len(node.Definition.Types) > 0 {
				return node.Definition.Types[0].Code, node.Definition.Max == "*", true
			}
			return "", false, false
		}
		// Descend into the next datatype if it is complex.
		if len(node.Definition.Types) > 0 {
			containerType = node.Definition.Types[0].Code
			sub, err = reg.ResolveProfile(
				"http://hl7.org/fhir/StructureDefinition/" + containerType,
			)
			if err != nil || sub == nil {
				return "", false, false
			}
			cur = sub
		}
	}
	return "", false, false
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

// setDateLeaf places a date search value on a date element. A Period element
// (e.g. PractitionerRole.period) receives its date on the `start` member; a
// choice element (e.g. Provenance.occurred[x]) receives it on the concrete
// dateTime choice member so the value lands on the serialised member.
func setDateLeaf(
	body map[string]any,
	path, value string,
	reg *registry.Registry,
	resourceType string,
) {
	segments := strings.Split(path, ".")
	leaf := segments[len(segments)-1]
	// If the leaf is a choice element (e.g. "occurred" typed Period|dateTime),
	// prefer the dateTime choice member so the primitive date value is valid.
	if def, ok := searchElementDefinition(resourceType, path, reg); ok && def != nil {
		hasPeriod := false
		for _, et := range def.Types {
			if et.Code == "dateTime" || et.Code == "date" || et.Code == "instant" ||
				et.Code == "time" {
				leaf = leaf + upperCamelTypeName(et.Code)
				hasPeriod = false
				break
			}
			if et.Code == "Period" {
				hasPeriod = true
			}
		}
		// A pure Period element (no dateTime choice) is a map; set its `start`
		// so the seed carries the date value rather than a bare scalar.
		if hasPeriod {
			segments = append(segments, "start")
			leaf = "start"
		}
	}
	segments[len(segments)-1] = leaf
	setPathLeaf(body, strings.Join(segments, "."), value)
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
	// Force the search value so the seed always matches the query, even when the
	// generated payload already carried a name.
	first["family"] = value
	first["text"] = value
}

// setAddressLeaf places the search value on an Address's text so string search
// can match it. The value is forced so the seed always matches the query.
func setAddressLeaf(body map[string]any, path string, value string) {
	setFieldLeafForce(body, path, "text", value)
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
func setSearchCodeValue(
	body map[string]any,
	path string,
	value string,
	typeCode string,
	repeatable bool,
	system string,
) {
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
func resetCodingForSearchValue(
	coding map[string]any,
	owner map[string]any,
	value string,
	system string,
) {
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

// setFieldLeafForce sets a string leaf property on the first element of a field,
// overwriting an existing value. Search seeds call this so the query value
// always appears on the element the search filters, even when the generated
// payload already populated the field with a different value.
func setFieldLeafForce(body map[string]any, path, leaf, value string) {
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
		first[leaf] = value
		return
	}
	if m, ok := raw.(map[string]any); ok {
		m[leaf] = value
	}
}

// setPathLeafBoolean sets a boolean value at a dotted element path, parsing the
// string "true"/"false" into a Go bool.
func setPathLeafBoolean(body map[string]any, path, value string) {
	cur, leaf := containerForPath(body, path)
	switch value {
	case "true":
		cur[leaf] = true
	case "false":
		cur[leaf] = false
	default:
		cur[leaf] = value
	}
}

// setReferenceLeaf places the search value ("Type/id") on a Reference object's
// `reference` member.
func setReferenceLeaf(body map[string]any, path, value string) {
	cur, field := containerForPath(body, path)
	raw, ok := cur[field]
	if !ok {
		cur[field] = map[string]any{"reference": value}
		return
	}
	if arr, ok := raw.([]any); ok {
		if len(arr) > 0 {
			if m, ok := arr[0].(map[string]any); ok {
				m["reference"] = value
				return
			}
			arr[0] = map[string]any{"reference": value}
			return
		}
		cur[field] = []any{map[string]any{"reference": value}}
		return
	}
	if m, ok := raw.(map[string]any); ok {
		m["reference"] = value
		return
	}
	cur[field] = map[string]any{"reference": value}
}

// setQuantityLeaf places a search value on a Quantity element so a quantity
// search can match it. The value is "number|system|code"; only the number and
// code parts that are present are applied.
func setQuantityLeaf(body map[string]any, path, value string) {
	cur, field := containerForPath(body, path)
	raw, ok := cur[field]
	if !ok {
		cur[field] = map[string]any{"value": firstNumericPart(value)}
		return
	}
	q, ok := raw.(map[string]any)
	if !ok {
		cur[field] = map[string]any{"value": firstNumericPart(value)}
		return
	}
	if _, hasValue := q["value"]; !hasValue {
		q["value"] = firstNumericPart(value)
	}
}

// firstNumericPart returns the leading numeric portion of a "number|system|code"
// search value, or "0" if none is present.
func firstNumericPart(value string) any {
	if i := strings.IndexByte(value, '|'); i >= 0 {
		value = value[:i]
	}
	if f, err := strconv.ParseFloat(value, 64); err == nil {
		return f
	}
	return value
}

// applyCompositeMatch places each component of a composite search value
// "part1$part2" on the corresponding element of the composite expression
// "pathA | pathB".
func applyCompositeMatch(
	body map[string]any,
	expression, resourceType, value string,
	reg *registry.Registry,
) bool {
	parts := strings.Split(value, "$")
	if len(parts) < 2 {
		return false
	}
	paths := compositePaths(expression)
	if len(paths) < len(parts) {
		// Pad with the last path so extra parts still have a target.
		for len(paths) < len(parts) {
			paths = append(paths, paths[len(paths)-1])
		}
	}
	for i, part := range parts {
		if i >= len(paths) || paths[i] == "" {
			return false
		}
		path := paths[i]
		typeCode, _ := searchLeafType(resourceType, path, reg)
		switch typeCode {
		case "code", "Coding", "CodeableConcept":
			system := boundCodingSystem(resourceType, path, reg)
			setSearchCodeValue(body, path, part, typeCode, false, system)
		case "Quantity":
			setQuantityLeaf(body, path, part)
		case "boolean":
			setPathLeafBoolean(body, path, part)
		default:
			setPathLeaf(body, path, part)
		}
	}
	return true
}

// compositePaths extracts the ordered element paths from a composite search
// expression "pathA | pathB", stripping a leading resource-type token.
func compositePaths(expression string) []string {
	expr := strings.ReplaceAll(expression, ",", "|")
	var paths []string
	for _, p := range strings.Split(expr, "|") {
		p = strings.TrimSpace(p)
		segs := strings.Split(p, ".")
		if len(segs) > 1 && isBaseResourceTypeToken(segs[0], "") {
			segs = segs[1:]
		}
		if len(segs) > 0 {
			paths = append(paths, strings.Join(segs, "."))
		}
	}
	return paths
}

// setSpecialLeaf places geographic coordinates from a special (near) search
// value "lat|long" (or "lat|long|distance") on the Location's position. The
// element path resolves to the longitude/latitude leaves; we set both from the
// value's leading two coordinate parts.
func setSpecialLeaf(body map[string]any, path, value string) {
	parts := strings.Split(value, "|")
	lat, lng := "", ""
	if len(parts) > 1 {
		lng = strings.TrimSpace(parts[1])
	}
	if len(parts) > 0 {
		lat = strings.TrimSpace(parts[0])
	}
	// The expression is "position.longitude | position.latitude" (or vice
	// versa). We resolve the parent container of the longitude path and set both
	// latitude and longitude on it.
	cur, _ := containerForPath(body, path)
	if lng != "" {
		if f, err := strconv.ParseFloat(lng, 64); err == nil {
			cur["longitude"] = f
		}
	}
	if lat != "" {
		if f, err := strconv.ParseFloat(lat, 64); err == nil {
			cur["latitude"] = f
		}
	}
}
