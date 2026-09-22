package generation

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jlcoulter/momus/internal/core/coverage"
	coregen "github.com/jlcoulter/momus/internal/core/generation"
	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"

	fhir "github.com/jlcoulter/fhir-registry"
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
		// Search writes may have created repeatable elements (e.g.
		// Provenance.signature, PractitionerRole.availableTime) as a bare map via
		// descendContainer, but the IG requires them as arrays. Re-normalise
		// repeatable children so the seed keeps the correct shape.
		reNormalizeRepeatables(body, setupPrimaryProfile, options.Registry)
		// Display resolution for search-written codings happens inline when the
		// coding is placed (see codingForSearchValue/resetCodingForSearchValue),
		// so no whole-body re-normalisation is needed here. Running a whole-body
		// display pass over the payload after SynthesizeBody has already stripped
		// the fixed-coding markers would re-add a display to codings derived from
		// a Fixed/Pattern value, which a conformant server rejects ("not allowed
		// in the applicable fixed value").
		out = append(out, &model.ResourceInstance{
			LocalID:      localID,
			ResourceType: req.ResourceType,
			Profile:      setupPrimaryProfile,
			Resource:     body,
		})
	}
	return out
}

// searchSeedID returns a deterministic FHIR id for a search seed resource.
// reNormalizeRepeatables re-applies array-wrapping for repeatable (Max > 1)
// elements after search-match writes create nested containers as bare maps (via
// descendContainer). Without this, a seed element the IG requires as an array
// (e.g. Provenance.signature, PractitionerRole.availableTime) is left as a bare
// object and fails validation. It is a no-op when the profile root cannot be
// resolved.
func reNormalizeRepeatables(body map[string]any, profileURL string, reg *registry.Registry) {
	if profileURL == "" || reg == nil {
		return
	}
	resolved, err := reg.ResolveProfile(profileURL)
	if err != nil || resolved == nil || resolved.Root == nil {
		return
	}
	normalizeRepeatableChildren(body, resolved.Root)
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
	// A search write that descends into a complex datatype container (e.g.
	// Provenance.signature.type descends into Signature) creates the container
	// from scratch, but the datatype's required children (e.g. Signature.when,
	// Signature.who) are not inlined into the resource profile's element tree,
	// so they cannot be populated afterwards. Rather than shipping a seed that
	// fails validation, decline it so the obligation degrades to a status-only
	// search.
	if containerDatatypeRequiresChildren(resourceType, elementPath, reg) {
		return false
	}
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
		setPathLeafRepeatable(body, elementPath, value, repeatable)
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
		//
		// A placeholder value can never carry the display a profile requires on
		// a coding (e.g. hcpd-healthcareservice.type.coding.display min=1 with an
		// external value set the registry cannot resolve). A system-less
		// placeholder is an opaque token the validator skips, but a required
		// display cannot be fabricated. Rather than shipping a seed that fails
		// validation, decline the seed so the obligation degrades to a status-only
		// search.
		if isSearchPlaceholder(value) && codingRequiresDisplay(resourceType, elementPath, reg) {
			return false
		}
		system := boundCodingSystem(resourceType, elementPath, reg)
		setSearchCodeValue(body, elementPath, value, typeCode, repeatable, system, reg)
		return true
	case "HumanName":
		// A string search on HumanName matches the text/family tokens; ensure the
		// value appears there.
		setNameLeaf(body, elementPath, value)
		return true
	case "Identifier":
		// A token search on an Identifier matches its `value` member (and a
		// type/system search may match those). Force the value onto the first
		// identifier so the search seed carries the query value, then ensure the
		// value is format-valid for the identifier's own system (e.g. an ABN
		// identifier must carry an 11-digit mod-89 value, not a 16-digit HPI).
		setFieldLeafForce(body, elementPath, "value", value)
		normalizeFirstIdentifierValue(body, elementPath)
		return true
	case "ContactPoint":
		// A token search on a ContactPoint matches its `value` (telecom number/
		// address) and `system`. Force the value onto the first contact point,
		// ensuring a system is present so the seed satisfies cpt-2 (a system is
		// required whenever a value is provided).
		setContactPointValue(body, elementPath, value)
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
		setReferenceLeaf(body, elementPath, value, repeatable)
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
				return node.Definition.Types[0].Code, node.Definition.Max == fhir.MaxUnbounded
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
				return node.Definition.Types[0].Code, node.Definition.Max == fhir.MaxUnbounded, true
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

// setPathLeafRepeatable sets a primitive string value at a dotted element path,
// wrapping it in an array when the target element is repeatable (Max unbounded),
// so e.g. name.given (0..*) is set to ["momus-search"] rather than a scalar.
func setPathLeafRepeatable(body map[string]any, path, value string, repeatable bool) {
	if !repeatable {
		setPathLeaf(body, path, value)
		return
	}
	segs := strings.Split(path, ".")
	if len(segs) == 0 {
		return
	}
	cur := body
	for i := 0; i < len(segs)-1; i++ {
		cur = descendContainer(cur, segs[i])
	}
	leaf := segs[len(segs)-1]
	cur[leaf] = []any{value}
}

// setDateLeaf places a date search value on a date element. A Period element
// (e.g. PractitionerRole.period) receives its date on the `start` member; a
// choice element (e.g. Provenance.occurred[x]) receives it on the concrete
// dateTime choice member so the value lands on the serialised member. A plain
// date element that is not a choice (e.g. Provenance.recorded, typed instant)
// keeps its bare key and is never type-suffixed.
func setDateLeaf(
	body map[string]any,
	path, value string,
	reg *registry.Registry,
	resourceType string,
) {
	segments := strings.Split(path, ".")
	leaf := segments[len(segments)-1]
	base := strings.TrimSuffix(leaf, "[x]")
	def, ok := searchElementDefinition(resourceType, path, reg)
	isChoice := ok && def != nil && strings.HasSuffix(def.Path, "[x]")

	hasPeriod := false
	isInstant := false
	if def != nil {
		for _, et := range def.Types {
			if et.Code == "dateTime" || et.Code == "date" || et.Code == "instant" ||
				et.Code == "time" {
				// A choice element (path "[x]") serialises under the type-suffixed
				// member (e.g. occurred -> occurredDateTime); a plain date/instant
				// element (e.g. recorded) keeps its bare key.
				if isChoice {
					leaf = leaf + upperCamelTypeName(et.Code)
				}
				isInstant = et.Code == "instant"
				hasPeriod = false
				break
			}
			if et.Code == "Period" {
				hasPeriod = true
			}
		}
	}
	// A Period element (choice or pure) is a map; set its `start` member so the
	// seed carries the date value rather than a bare scalar. The generator may
	// already have populated `end` with an earlier date (its fakePeriod draws a
	// random start/end), so ensure the interval stays ordered (per-1) after the
	// search date overwrites `start`.
	if hasPeriod {
		segments = append(segments, "start")
		leaf = "start"
	}
	// An `instant` element requires a full timestamp; a bare date search value
	// is padded so the stored value is a valid instant.
	if isInstant {
		value = normalizeInstantValue(value)
	}
	segments[len(segments)-1] = leaf
	setPathLeaf(body, strings.Join(segments, "."), value)
	// A Period element's `start` was just set to the search date. If a
	// previously-generated `end` is now on or before `start`, the interval
	// violates per-1 ("start SHALL have a lower value than end"). Move `end`
	// just after `start` to keep the seed conformant.
	if hasPeriod {
		ensurePeriodOrdered(body, strings.Join(segments[:len(segments)-1], "."))
	}
	// Writing one choice branch must not leave a sibling choice member behind
	// (e.g. a generated occurredPeriod alongside the seed's occurredDateTime).
	if isChoice && !hasPeriod {
		clearSiblingChoiceMembers(body, segments, base, leaf)
	}
}

// ensurePeriodOrdered enforces the FHIR per-1 invariant (start < end) on a
// Period at the given dotted path. When end is missing or not strictly after
// start, end is pushed to one day after start so a conformant server accepts
// the interval.
//
// It never uses the wall clock: a fixed reference instant keeps the seed
// deterministic across runs so generation is reproducible for any IG.
func ensurePeriodOrdered(body map[string]any, path string) {
	cur, field := containerForPath(body, path)
	period, ok := cur[field].(map[string]any)
	if !ok {
		return
	}
	startStr, _ := period["start"].(string)
	endStr, _ := period["end"].(string)
	if startStr != "" && endStr != "" {
		s, serr := parsePeriodInstant(startStr)
		e, eerr := parsePeriodInstant(endStr)
		if serr == nil && eerr == nil && s.Before(e) {
			return
		}
	}
	// start missing/unparseable or end not after start: leave start as-is and
	// set end to one day after start (or a fixed reference when start is
	// absent). A fixed reference keeps the seed deterministic across runs.
	base := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if s, serr := parsePeriodInstant(startStr); serr == nil {
		base = s
	}
	period["end"] = base.AddDate(0, 0, 1).Format(time.RFC3339)
}

// parsePeriodInstant parses a Period start/end value, which may be either a
// full FHIR instant (RFC3339) or a bare date (YYYY-MM-DD). It returns the
// parsed time and nil, or a zero time and an error when neither form matches.
func parsePeriodInstant(v string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", v); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("unparseable period value %q", v)
}

// normalizeInstantValue pads a bare date search value into a valid FHIR
// instant (RFC3339 with a time and timezone), leaving an already-qualified
// value untouched.
func normalizeInstantValue(value string) string {
	if value == "" {
		return value
	}
	if strings.Contains(value, "T") || strings.Contains(value, " ") {
		return value
	}
	// A bare date like "2024-01-01" becomes "2024-01-01T00:00:00Z".
	return value + "T00:00:00Z"
}

// clearSiblingChoiceMembers removes the other concrete members of a choice
// element (keys sharing the base prefix) from the container that holds the
// just-written member, so only the seeded branch remains.
func clearSiblingChoiceMembers(body map[string]any, segments []string, base, written string) {
	cur := body
	for i := 0; i < len(segments)-1; i++ {
		cur = descendContainer(cur, segments[i])
	}
	for key := range cur {
		if key == written {
			continue
		}
		if strings.HasPrefix(key, base) {
			delete(cur, key)
		}
	}
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

// codingRequiresDisplay reports whether the element a token search targets has a
// required (Min >= 1) display on its coding child. When a profile constrains
// e.g. HealthcareService.type.coding.display to min=1, a search seed that can
// only carry a system-less placeholder (the bound value set is unresolvable,
// e.g. an external terminology server) cannot fabricate a valid display, so the
// seed must be declined rather than shipped invalid.
func codingRequiresDisplay(resourceType, elementPath string, reg *registry.Registry) bool {
	if reg == nil {
		return false
	}
	def, ok := searchElementDefinition(resourceType, elementPath, reg)
	if !ok || def == nil {
		return false
	}
	// Find the "coding" child (CodeableConcept.coding or Coding itself), then
	// check whether its "display" child is required.
	coding := codingChildOf(def)
	if coding == nil {
		return false
	}
	for _, child := range coding.Children {
		if child != nil && child.Path != "" &&
			strings.HasSuffix(child.Path, ".display") && child.Min >= 1 {
			return true
		}
	}
	return false
}

// codingChildOf returns the "coding" child ElementDefinition of a CodeableConcept
// element, or the element itself when it is already a Coding, or nil otherwise.
func codingChildOf(def *model.ElementDefinition) *model.ElementDefinition {
	if def == nil {
		return nil
	}
	if len(def.Types) > 0 && def.Types[0].Code == "Coding" {
		return def
	}
	for _, child := range def.Children {
		if child != nil && child.Path != "" && strings.HasSuffix(child.Path, ".coding") {
			return child
		}
	}
	return nil
}

// containerDatatypeRequiresChildren reports whether a search expression path
// descends into a complex datatype whose definition requires (Min >= 1) child
// elements beyond the single leaf the search write populates. The resource
// profile's element tree does not inline datatype children (e.g.
// Provenance.signature.type resolves the Signature datatype separately), so a
// search write that creates the datatype container from scratch would ship a
// resource missing those required children. When true, the caller declines the
// seed (the obligation stays status-only) rather than emitting an invalid one.
func containerDatatypeRequiresChildren(resourceType, elementPath string, reg *registry.Registry) bool {
	if reg == nil {
		return false
	}
	segments := strings.Split(elementPath, ".")
	// A datatype container is only at risk when the path has at least two
	// segments (e.g. "signature.type": the "signature" segment is the datatype
	// container, "type" is the leaf inside it).
	if len(segments) < 2 {
		return false
	}
	def, ok := searchElementDefinition(resourceType, elementPath, reg)
	if !ok || def == nil || len(def.Types) == 0 {
		return false
	}
	// The leaf itself being a complex datatype (e.g. Signature.type is Coding,
	// which is fine) is not the concern; the concern is the *container* datatype
	// (e.g. Signature) carrying required children other than the one written.
	containerType := ""
	for _, profile := range reg.ProfilesForResource(resourceType) {
		resolved, err := reg.ResolveProfile(profile.URL)
		if err != nil || resolved == nil {
			continue
		}
		node, ok := resolved.Elements[resourceType+"."+segments[0]]
		if !ok || node == nil || node.Definition == nil || len(node.Definition.Types) == 0 {
			continue
		}
		containerType = node.Definition.Types[0].Code
		break
	}
	if containerType == "" {
		return false
	}
	datatype, err := reg.ResolveProfile("http://hl7.org/fhir/StructureDefinition/" + containerType)
	if err != nil || datatype == nil || datatype.Root == nil {
		return false
	}
	// The leaf we will populate is the last path segment under the container.
	leafKey := containerType + "." + strings.Join(segments[1:], ".")
	leafMin := 0
	if node, ok := datatype.Elements[leafKey]; ok && node != nil && node.Definition != nil {
		leafMin = node.Definition.Min
	}
	for key, node := range datatype.Elements {
		if node == nil || node.Definition == nil {
			continue
		}
		// Only direct children of the container (one segment below) and not the
		// leaf we populate.
		if !strings.HasPrefix(key, containerType+".") {
			continue
		}
		rest := strings.TrimPrefix(key, containerType+".")
		if strings.Contains(rest, ".") {
			continue // a grandchild, not a direct child
		}
		if key == leafKey && leafMin > 0 {
			// The leaf is required and the search write populates it.
			continue
		}
		if node.Definition.Min >= 1 && key != leafKey {
			return true
		}
	}
	return false
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
	reg *registry.Registry,
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
			single := map[string]any{"coding": []any{codingForSearchValue(value, system, reg)}}
			if repeatable {
				cur[field] = []any{single}
			} else {
				cur[field] = single
			}
		case "Coding":
			// A repeatable Coding (e.g. Signature.type, max="*") must be an array
			// of coding objects, never a bare object (servers reject an object
			// where an array is required).
			coding := codingForSearchValue(value, system, reg)
			if repeatable {
				cur[field] = []any{coding}
			} else {
				cur[field] = coding
			}
		default:
			// A primitive code: set the scalar.
			cur[field] = value
		}
		return
	}
	switch v := raw.(type) {
	case map[string]any:
		// A CodeableConcept (even a text-only one) must carry a `coding` array,
		// never a bare `code` member. If the map lacks a coding array but is a
		// CodeableConcept (has a "text"), wrap the search coding in one.
		if _, hasCode := v["code"]; hasCode {
			resetCodingForSearchValue(v, nil, value, system, reg)
			return
		}
		if coding, ok := v["coding"].([]any); ok && len(coding) > 0 {
			if first, ok := coding[0].(map[string]any); ok {
				resetCodingForSearchValue(first, v, value, system, reg)
				return
			}
			coding[0] = codingForSearchValue(value, system, reg)
			return
		}
		if _, isTextOnly := v["text"]; isTextOnly {
			v["coding"] = []any{codingForSearchValue(value, system, reg)}
			return
		}
		resetCodingForSearchValue(v, nil, value, system, reg)
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
			resetCodingForSearchValue(first, nil, value, system, reg)
			return
		}
		if coding, ok := first["coding"].([]any); ok && len(coding) > 0 {
			if c, ok := coding[0].(map[string]any); ok {
				resetCodingForSearchValue(c, first, value, system, reg)
				return
			}
			coding[0] = codingForSearchValue(value, system, reg)
			return
		}
		if _, isTextOnly := first["text"]; isTextOnly {
			first["coding"] = []any{codingForSearchValue(value, system, reg)}
			return
		}
		resetCodingForSearchValue(first, nil, value, system, reg)
	case string:
		cur[field] = value
	default:
		cur[field] = map[string]any{"code": value}
	}
}

// codingForSearchValue builds a coding map for a token search value, carrying the
// resolved system (when known) and the canonical display (when resolvable) so a
// profile that requires a coding display stays valid. A system is only attached
// when the search value is a real code; attaching a system to a synthetic
// placeholder (e.g. "momus-search") turns it into a checkable unknown code and
// fails a required binding, whereas a system-less placeholder is treated as an
// opaque token.
func codingForSearchValue(value, system string, reg *registry.Registry) map[string]any {
	coding := map[string]any{"code": value}
	if system == "" || isSearchPlaceholder(value) {
		return coding
	}
	coding["system"] = system
	if display := resolveCodingDisplay(reg, system, value); display != "" {
		coding["display"] = display
	}
	return coding
}

// isSearchPlaceholder reports whether value is a synthetic search placeholder
// rather than a real data value. Placeholders must never be paired with a real
// code system, or a conformant server rejects them as unknown codes.
func isSearchPlaceholder(value string) bool {
	v := strings.TrimSpace(value)
	if v == "" {
		return true
	}
	return strings.HasPrefix(v, "momus-search") || strings.HasPrefix(v, "momus-no-match")
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
	reg *registry.Registry,
) {
	coding["code"] = value
	delete(coding, "display")
	// Only align the system when the value is a real code; a placeholder (e.g.
	// "momus-search") must stay system-less so it is not rejected as an unknown
	// code of a real CodeSystem.
	if system != "" && !isSearchPlaceholder(value) {
		coding["system"] = system
		if display := resolveCodingDisplay(reg, system, value); display != "" {
			coding["display"] = display
		}
	} else {
		delete(coding, "system")
	}
	if owner != nil {
		delete(owner, "text")
	}
}

// normalizeFirstIdentifierValue rewrites the value of the first identifier at
// path to a format-valid value for its own system, so a token search seed that
// forces a value onto a format-checked AU identifier (e.g. ABN, HPI-I) does not
// ship a value that fails the identifier's invariants.
func normalizeFirstIdentifierValue(body map[string]any, path string) {
	cur, field := containerForPath(body, path)
	raw, ok := cur[field]
	if !ok {
		return
	}
	ids, ok := raw.([]any)
	if !ok || len(ids) == 0 {
		return
	}
	id, ok := ids[0].(map[string]any)
	if !ok {
		return
	}
	normalizeGeneratedIdentifierSeeded(id, path)
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

// setContactPointValue writes a token search value onto the first ContactPoint
// element, ensuring the contact point carries a `system` so it satisfies the
// cpt-2 invariant ("a system is required if a value is provided"). A newly
// created contact point defaults to system "phone"; an existing one is given a
// system only when it lacks one.
func setContactPointValue(body map[string]any, path, value string) {
	cur, field := containerForPath(body, path)
	raw, ok := cur[field]
	if !ok {
		cur[field] = []any{map[string]any{"system": "phone", "value": value}}
		return
	}
	switch typed := raw.(type) {
	case []any:
		if len(typed) == 0 {
			cur[field] = []any{map[string]any{"system": "phone", "value": value}}
			return
		}
		first, ok := typed[0].(map[string]any)
		if !ok {
			typed[0] = map[string]any{"system": "phone", "value": value}
			return
		}
		first["value"] = value
		if _, hasSystem := first["system"]; !hasSystem {
			first["system"] = "phone"
		}
	case map[string]any:
		typed["value"] = value
		if _, hasSystem := typed["system"]; !hasSystem {
			typed["system"] = "phone"
		}
	default:
		cur[field] = map[string]any{"system": "phone", "value": value}
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
// `reference` member. When the target element is repeatable (Max unbounded, e.g.
// HealthcareService.endpoint 0..*), the reference is stored as a single-element
// array so the seed keeps the correct shape.
func setReferenceLeaf(body map[string]any, path, value string, repeatable bool) {
	cur, field := containerForPath(body, path)
	ref := map[string]any{"reference": value}
	raw, ok := cur[field]
	if !ok {
		if repeatable {
			cur[field] = []any{ref}
		} else {
			cur[field] = ref
		}
		return
	}
	if arr, ok := raw.([]any); ok {
		if len(arr) > 0 {
			if m, ok := arr[0].(map[string]any); ok {
				m["reference"] = value
				return
			}
			arr[0] = ref
			return
		}
		cur[field] = []any{ref}
		return
	}
	if m, ok := raw.(map[string]any); ok {
		m["reference"] = value
		return
	}
	if repeatable {
		cur[field] = []any{ref}
	} else {
		cur[field] = ref
	}
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
			setSearchCodeValue(body, path, part, typeCode, false, system, reg)
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
	// The expression is the element itself (e.g. Location.position), and the
	// coordinates go on that element's map — not on its parent. When the path is
	// a single segment the element is the coordinate container: create it as a
	// map so lat/long never leak to the resource root. When the path is nested
	// (e.g. position.longitude) the container is the parent map, which holds both
	// the longitude and latitude members.
	cur, leaf := containerForPath(body, path)
	target := cur
	segs := strings.Split(path, ".")
	if len(segs) == 1 {
		// The path names the element itself; ensure it is a map container.
		switch t := cur[leaf].(type) {
		case map[string]any:
			target = t
		case []any:
			if len(t) > 0 {
				if el, ok := t[0].(map[string]any); ok {
					target = el
				}
			}
		default:
			el := map[string]any{}
			cur[leaf] = el
			target = el
		}
	} else if el, ok := cur[leaf].(map[string]any); ok {
		// Nested path: the leaf is the last member of the container map.
		target = el
	}
	if lng != "" {
		if f, err := strconv.ParseFloat(lng, 64); err == nil {
			target["longitude"] = f
		}
	}
	if lat != "" {
		if f, err := strconv.ParseFloat(lat, 64); err == nil {
			target["latitude"] = f
		}
	}
}
