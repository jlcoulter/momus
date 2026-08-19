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
	"unicode"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
	"github.com/jlcoulter/momus/internal/test/ast"
	"github.com/jlcoulter/momus/internal/test/coverage"
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
type BuildOptions struct {
	BaseURL string
	// WriteBaseURL, when set, is used for write requests (PUT/PATCH/POST/DELETE)
	// instead of BaseURL, so resource creation can target a different endpoint
	// than read/search requests. When empty, write requests use BaseURL.
	WriteBaseURL                   string
	Registry                       *registry.Registry
	PreferredProfileURLsByResource map[string][]string
	// Strength is the interaction strength used when generating. When unset (or
	// < 2) it falls back to the coverage plan's own Strength, and finally to
	// strength 1 (one test per requirement). Strength >= 2 groups compatible
	// obligations into shared payloads selected by greedy set-cover.
	Strength int
	// Exhaustive populates optional (Min == 0) elements in addition to required
	// ones, with randomised presence, so generated payloads are fuller and more
	// realistic. When false, only required and contract-driven elements are
	// populated.
	Exhaustive bool
}

// GenerateFromCoveragePlan maps coverage requirements into a concrete AST.
func GenerateFromCoveragePlan(plan *coverage.CoveragePlan, options BuildOptions) (*ast.Plan, error) {
	if plan == nil {
		return nil, errors.New("coverage plan is required")
	}

	depPlan, err := buildDependencyPlan(plan, options)
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

	root := &ast.Sequence{Steps: make([]ast.Node, 0)}
	for _, level := range depPlan.Levels {
		resourceNodes := make([]ast.Node, 0, len(level))
		for _, resourceType := range level {
			resourceSeq := &ast.Sequence{Steps: make([]ast.Node, 0)}
			deps := depPlan.Dependencies[resourceType]
			resourceProfiles := uniqueProfileURLs(byResource[resourceType])
			setupProfileURL := ""
			if len(resourceProfiles) > 0 {
				setupProfileURL = resourceProfiles[0]
			}
			setupProfiles := orderedProfilesForResource(resourceType, setupProfileURL, options.PreferredProfileURLsByResource)
			setupPrimaryProfile := firstProfileURL(setupProfiles)

			resourceSeq.Steps = append(resourceSeq.Steps,
				&ast.Request{
					Method: "PUT",
					URL:    joinInstanceURL(baseURLForMethod(options, "PUT"), resourceType, setupResourceID(resourceType)),
					Headers: map[string]string{
						"Content-Type": "application/fhir+json",
					},
					Body: buildSetupBody(resourceType, setupResourceID(resourceType), setupProfiles, setupPrimaryProfile, deps, options.Registry, options.Exhaustive),
				},
				&ast.Assert{
					Description:   "setup create seed resource",
					RequirementID: "setup:" + resourceType,
					Expression:    "status in [200,201]",
				},
				&ast.Capture{Name: resourceType + ".id", Path: "id"},
			)

			for _, caseSeq := range buildResourceCases(byResource[resourceType], plan, options, deps) {
				resourceSeq.Steps = append(resourceSeq.Steps, caseSeq)
			}

			resourceNodes = append(resourceNodes, resourceSeq)
		}

		if len(resourceNodes) == 1 {
			root.Steps = append(root.Steps, resourceNodes[0])
			continue
		}
		root.Steps = append(root.Steps, &ast.Parallel{Steps: resourceNodes})
	}

	return &ast.Plan{Version: "v1", Root: root}, nil
}

// RequirementCount returns the number of requirement-bound Assertions in a
// generated plan, excluding setup scaffolding.
func RequirementCount(plan *ast.Plan) int {
	if plan == nil || plan.Root == nil {
		return 0
	}
	count := 0
	seen := make(map[string]struct{})
	var walk func(ast.Node)
	walk = func(node ast.Node) {
		switch n := node.(type) {
		case *ast.Sequence:
			for _, step := range n.Steps {
				walk(step)
			}
		case *ast.Parallel:
			for _, step := range n.Steps {
				walk(step)
			}
		case *ast.Assert:
			if strings.HasPrefix(n.RequirementID, "setup:") {
				return
			}
			// Count each obligation once even when its execution expands to
			// multiple asserts (e.g. a CRUD sequence).
			if _, ok := seen[n.RequirementID]; ok {
				return
			}
			seen[n.RequirementID] = struct{}{}
			count++
		}
	}
	walk(plan.Root)
	return count
}

func joinURL(baseURL, resourceType string) string {
	if baseURL == "" {
		return "/" + strings.TrimPrefix(resourceType, "/")
	}
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimPrefix(resourceType, "/")
}

func joinInstanceURL(baseURL, resourceType, id string) string {
	return joinURL(baseURL, resourceType) + "/" + strings.TrimPrefix(id, "/")
}

// baseURLForMethod returns the base URL to use for a request of the given
// method: write methods (PUT/PATCH/POST/DELETE) use the write base URL when
// configured, while read/search (GET) requests use the read base URL.
func baseURLForMethod(options BuildOptions, method string) string {
	switch method {
	case "GET":
		return options.BaseURL
	default:
		if options.WriteBaseURL != "" {
			return options.WriteBaseURL
		}
		return options.BaseURL
	}
}

func buildBodyTemplate(req coverage.CoverageRequirement, id string, profileURLs []string, primaryProfileURL string, deps []string, reg *registry.Registry, exhaustive bool) map[string]any {
	body := baseBodyTemplate(req.ResourceType, id, profileURLs, deps)
	enrichBodyFromProfile(body, primaryProfileURL, reg)
	if exhaustive {
		enrichBodyExhaustive(body, primaryProfileURL, reg, newRNG(id))
	}
	normalizeGeneratedPayload(body)
	normalizeResourceSpecificPayload(body)
	applyNegativeMutation(body, req, reg)
	return body
}

func buildSetupBody(resourceType, id string, profileURLs []string, primaryProfileURL string, deps []string, reg *registry.Registry, exhaustive bool) map[string]any {
	body := baseBodyTemplate(resourceType, id, profileURLs, deps)
	enrichBodyFromProfile(body, primaryProfileURL, reg)
	if exhaustive {
		enrichBodyExhaustive(body, primaryProfileURL, reg, newRNG(id))
	}
	normalizeGeneratedPayload(body)
	normalizeResourceSpecificPayload(body)
	return body
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
		if generated, ok := generateRequiredValue(child, reg); ok {
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

func baseBodyTemplate(resourceType, id string, profileURLs, deps []string) map[string]any {
	body := map[string]any{
		"resourceType": resourceType,
		"id":           id,
	}
	if meta := buildMeta(profileURLs); meta != nil {
		body["meta"] = meta
	}

	attachDependencyReferences(body, resourceType, deps)
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
		if value, ok := generateRequiredValue(child, reg); ok {
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

func generateRequiredValue(node *model.ElementNode, reg *registry.Registry) (any, bool) {
	if node == nil || node.Definition == nil {
		return nil, false
	}
	def := node.Definition
	if elementAllowsMultiple(def) || def.Min > 1 {
		return generateRepeatedValue(node, reg)
	}
	if len(node.Slices) > 0 {
		for _, sliceName := range sortedSliceNames(node.Slices) {
			slice := node.Slices[sliceName]
			if slice == nil || slice.Definition == nil || slice.Definition.Min <= 0 {
				continue
			}
			return generateSliceValue(slice, reg)
		}
	}
	return generateSingleValue(node, reg)
}

func generateRepeatedValue(node *model.ElementNode, reg *registry.Registry) (any, bool) {
	values := make([]any, 0)
	sliceNames := make([]string, 0, len(node.Slices))
	for name := range node.Slices {
		sliceNames = append(sliceNames, name)
	}
	sort.Strings(sliceNames)
	for _, name := range sliceNames {
		slice := node.Slices[name]
		if slice == nil || slice.Definition == nil || slice.Definition.Min <= 0 {
			continue
		}
		for i := 0; i < slice.Definition.Min; i++ {
			if value, ok := generateSliceValue(slice, reg); ok {
				values = append(values, value)
			}
		}
	}
	for len(values) < max(node.Definition.Min, 1) {
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
		Children:   slice.Children,
		Slices:     make(map[string]*model.SliceNode),
	}
	if value, ok := generateDatatypeValueFromProfiles(slice.Definition.Types, reg); ok {
		if valueMap, ok := value.(map[string]any); ok {
			populateRequiredChildren(valueMap, synthetic, reg)
			applySimpleConstraints(valueMap, synthetic, reg)
		}
		return value, true
	}
	return generateSingleValue(synthetic, reg)
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
	merged := map[string]any{}
	hit := false
	for _, et := range types {
		for _, profileURL := range et.Profile {
			value, ok := generateDatatypeValueFromProfile(profileURL, reg)
			if !ok {
				continue
			}
			valueMap, ok := value.(map[string]any)
			if !ok {
				continue
			}
			for key, val := range valueMap {
				merged[key] = val
			}
			hit = true
		}
	}
	if !hit {
		return nil, false
	}
	return merged, true
}

func generateSingleValue(node *model.ElementNode, reg *registry.Registry) (any, bool) {
	if node == nil || node.Definition == nil {
		return nil, false
	}
	typeCode := primaryTypeCode(node.Definition)
	if node.Definition.Fixed != nil {
		return node.Definition.Fixed, true
	}
	boundCoding, hasBoundCoding := resolveBoundCoding(node.Definition, reg)
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
	case "date":
		return "2024-01-01", true
	case "dateTime", "instant":
		return "2024-01-01T00:00:00Z", true
	case "Identifier":
		identifier := map[string]any{}
		populateRequiredChildren(identifier, node, reg)
		applySimpleConstraints(identifier, node, reg)
		identifier = enrichGeneratedValueWithTypeProfiles(identifier, node.Definition, reg).(map[string]any)
		if _, ok := identifier["system"]; !ok {
			identifier["system"] = "http://example.org/fhir/identifier/" + sanitizeFHIRID(node.Path)
		}
		if _, ok := identifier["value"]; !ok {
			identifier["value"] = sanitizeFHIRID(node.Path) + "-001"
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
	for _, et := range def.Types {
		for _, profileURL := range et.Profile {
			resolved, err := reg.ResolveProfile(normalizeCanonical(profileURL))
			if err != nil || resolved == nil || resolved.Root == nil {
				continue
			}
			populateRequiredChildren(valueMap, resolved.Root, reg)
			applySimpleConstraints(valueMap, resolved.Root, reg)
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
					if generated, ok := generateRequiredValue(child, reg); ok {
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
	if candidate, ok := generateRequiredValue(node, reg); ok {
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
		if len(include.Concepts) > 0 {
			concept := include.Concepts[0]
			return generatedCoding{System: include.System, Code: concept.Code, Display: concept.Display}, true
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

func firstExpansionCoding(entries []model.ValueSetExpansionContains) (generatedCoding, bool) {
	for _, entry := range entries {
		if entry.Code != "" {
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
		if concept.Code != "" {
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
			if resourceType := resolveTargetResourceType(canonical, reg); resourceType != "" {
				return resourceType + "/" + setupResourceID(resourceType)
			}
		}
		for _, et := range def.Types {
			for _, canonical := range et.TargetProfile {
				if resourceType := resolveTargetResourceType(canonical, reg); resourceType != "" {
					return resourceType + "/" + setupResourceID(resourceType)
				}
			}
		}
	}
	return "Resource/" + setupResourceID("Resource")
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
	for _, raw := range codings {
		coding, ok := raw.(map[string]any)
		if !ok {
			continue
		}
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
		"reference": "Organization/" + setupResourceID("Organization"),
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
	name := sanitizeFHIRID(leaf)
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

func firstProfileURL(profileURLs []string) string {
	for _, profileURL := range profileURLs {
		profileURL = strings.TrimSpace(profileURL)
		if profileURL != "" {
			return profileURL
		}
	}
	return ""
}

func orderedProfilesForResource(resourceType, requestedProfileURL string, preferredByResource map[string][]string) []string {
	profiles := make([]string, 0)
	seen := make(map[string]struct{})
	appendProfile := func(profileURL string) {
		profileURL = strings.TrimSpace(profileURL)
		if profileURL == "" {
			return
		}
		if _, ok := seen[profileURL]; ok {
			return
		}
		seen[profileURL] = struct{}{}
		profiles = append(profiles, profileURL)
	}

	if len(preferredByResource) > 0 {
		for _, key := range []string{strings.TrimSpace(resourceType), strings.ToLower(strings.TrimSpace(resourceType))} {
			for _, profileURL := range preferredByResource[key] {
				appendProfile(profileURL)
			}
		}
	}
	appendProfile(requestedProfileURL)
	return profiles
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

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func buildMeta(profileURLs []string) map[string]any {
	profiles := make([]any, 0, len(profileURLs))
	seen := make(map[string]struct{}, len(profileURLs))
	for _, profileURL := range profileURLs {
		profileURL = strings.TrimSpace(profileURL)
		if profileURL == "" {
			continue
		}
		if _, ok := seen[profileURL]; ok {
			continue
		}
		seen[profileURL] = struct{}{}
		profiles = append(profiles, profileURL)
	}
	if len(profiles) == 0 {
		return nil
	}
	return map[string]any{"profile": profiles}
}

func uniqueProfileURLs(reqs []coverage.CoverageRequirement) []string {
	profiles := make([]string, 0, len(reqs))
	seen := make(map[string]struct{}, len(reqs))
	for _, req := range reqs {
		profileURL := strings.TrimSpace(req.ProfileURL)
		if profileURL == "" {
			continue
		}
		if _, ok := seen[profileURL]; ok {
			continue
		}
		seen[profileURL] = struct{}{}
		profiles = append(profiles, profileURL)
	}
	return profiles
}

func setupResourceID(resourceType string) string {
	return sanitizeFHIRID("momus-setup-" + resourceType)
}

func requirementResourceID(req coverage.CoverageRequirement) string {
	resourceType := strings.TrimSpace(req.ResourceType)
	if resourceType == "" {
		resourceType = "resource"
	}
	variant := strings.TrimSpace(string(req.Variant))
	if variant == "" {
		variant = "case"
	}
	return sanitizeFHIRID("momus-" + resourceType + "-" + variant + "-" + strconv.Itoa(stableChecksum(req.ID)))
}

func sanitizeFHIRID(value string) string {
	if value == "" {
		return "momus-id"
	}
	var b strings.Builder
	prevHyphen := false
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
			prevHyphen = false
		case r == '-' || r == '.':
			if !prevHyphen {
				b.WriteRune(r)
				prevHyphen = true
			}
		default:
			if !prevHyphen {
				b.WriteRune('-')
				prevHyphen = true
			}
		}
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" {
		return "momus-id"
	}
	if len(out) > 64 {
		return out[:64]
	}
	return out
}

func stableChecksum(value string) int {
	sum := 0
	for _, r := range value {
		sum = (sum*31 + int(r)) % 1000000
	}
	if sum < 0 {
		return -sum
	}
	return sum
}

func attachDependencyReferences(body map[string]any, resourceType string, deps []string) {
	for _, dep := range deps {
		switch dep {
		case "Patient":
			ref := map[string]any{"reference": dep + "/" + setupResourceID(dep)}
			switch resourceType {
			case "AllergyIntolerance", "Immunization":
				body["patient"] = ref
			case "Appointment":
				body["participant"] = []map[string]any{{"actor": ref, "status": "accepted"}}
			default:
				body["subject"] = ref
			}
		case "Encounter":
			body["encounter"] = map[string]any{"reference": dep + "/" + setupResourceID(dep)}
		case "Observation":
			body["result"] = []map[string]any{{"reference": dep + "/" + setupResourceID(dep)}}
		}
	}
}

// buildSingleRequirementCase builds the strength-1 test for a single coverage
// requirement: one request carrying that requirement's body plus its assert.
func buildSingleRequirementCase(req coverage.CoverageRequirement, options BuildOptions, deps []string) ast.Node {
	requestID := requirementResourceID(req)
	caseProfiles := orderedProfilesForResource(req.ResourceType, req.ProfileURL, options.PreferredProfileURLsByResource)
	casePrimaryProfile := firstProfileURL(caseProfiles)
	return &ast.Sequence{Steps: []ast.Node{
		&ast.Request{
			Method: "PUT",
			URL:    joinInstanceURL(baseURLForMethod(options, "PUT"), req.ResourceType, requestID),
			Headers: map[string]string{
				"Content-Type":           "application/fhir+json",
				"X-Momus-Requirement-ID": req.ID,
			},
			Body: buildBodyTemplate(req, requestID, caseProfiles, casePrimaryProfile, deps, options.Registry, options.Exhaustive),
		},
		buildRequirementAssert(req),
	}}
}

func buildRequirementAssert(req coverage.CoverageRequirement) *ast.Assert {
	description := "server accepts generated payload"
	expression := "status in [200,201]"
	expected := "accept"
	if isNegativeVariant(req.Variant) {
		description = "server rejects violating payload"
		expression = "status in [400,412,422]"
		expected = "reject"
	}
	return &ast.Assert{
		Description:   description,
		RequirementID: req.ID,
		Expression:    expression,
		Trace: &ast.Trace{
			ConstraintID: req.ConstraintID,
			ProfileURL:   req.ProfileURL,
			ResourceType: req.ResourceType,
			ElementPath:  req.ElementPath,
			Domain:       string(req.Domain),
			Variant:      string(req.Variant),
			Expected:     expected,
		},
	}
}
