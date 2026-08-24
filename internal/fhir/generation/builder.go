package generation

import (
	"strings"

	"github.com/jlcoulter/momus/internal/core/ast"
	"github.com/jlcoulter/momus/internal/core/coverage"
	coregen "github.com/jlcoulter/momus/internal/core/generation"
	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

// fhirBuilder implements core/generation.PayloadBuilder for the FHIR domain. It
// wraps the registry and the FHIR-specific payload/search synthesis so the
// generic generation framework never sees FHIR types.
type fhirBuilder struct {
	reg        *registry.Registry
	exhaustive bool
}

// NewBuilder returns a PayloadBuilder that synthesizes FHIR payloads and search
// values from the registry.
func NewBuilder(reg *registry.Registry, exhaustive bool) *fhirBuilder {
	return &fhirBuilder{reg: reg, exhaustive: exhaustive}
}

// DependencyPlan computes the resource dependency order for execution.
func (b *fhirBuilder) DependencyPlan(plan *coverage.CoveragePlan, capabilityResourceTypes map[string]struct{}) (*coverage.DependencyPlan, error) {
	return buildDependencyPlan(plan, capabilityResourceTypes, b.reg)
}

// BuildResourceCases turns a resource type's obligations into a list of test
// case nodes, delegating to the FHIR case-construction logic.
func (b *fhirBuilder) BuildResourceCases(reqs []coverage.CoverageRequirement, plan *coverage.CoveragePlan, options coregen.BuildOptions, deps []string, progress func()) []ast.Node {
	return buildResourceCases(reqs, plan, options, deps, progress)
}

// BuildBody returns a FHIR test payload for a requirement.
func (b *fhirBuilder) BuildBody(req coverage.CoverageRequirement, id string, profileURLs []string, primaryProfileURL string, deps []string, exhaustive bool) (map[string]any, bool) {
	return buildBodyTemplate(req, id, profileURLs, primaryProfileURL, deps, b.reg, exhaustive)
}

// SearchParamType resolves the FHIR search parameter type for a code.
func (b *fhirBuilder) SearchParamType(req coverage.CoverageRequirement, code string) string {
	if b.reg == nil || code == "" {
		return ""
	}
	if sp, ok := b.reg.SearchParameter(req.ResourceType, code); ok {
		return strings.ToLower(sp.Type)
	}
	return ""
}

// SearchAcceptValue returns a value for an accept search obligation that both
// makes the query meaningful and validates on the provisioned seed.
func (b *fhirBuilder) SearchAcceptValue(req coverage.CoverageRequirement, code string) string {
	if b.reg == nil || code == "" || code == "_id" {
		return "momus-search"
	}
	sp, ok := b.reg.SearchParameter(req.ResourceType, code)
	if !ok {
		return "momus-search"
	}
	elementPath := searchElementPath(sp.Expression, req.ResourceType)
	if elementPath == "" {
		return "momus-search"
	}
	// For a special (near) search the query value is coordinates, not the
	// element's own datatype. Check the search parameter type first.
	switch strings.ToLower(sp.Type) {
	case "special":
		return nearSearchValue(b, req, code)
	case "composite":
		return compositeAcceptValue(b, req, code)
	case "date", "dateTime", "instant", "time":
		return "2024-01-01"
	}
	def, ok := searchElementDefinition(req.ResourceType, elementPath, b.reg)
	if !ok || def == nil {
		return "momus-search"
	}
	if strings.ToLower(sp.Type) == "token" && primaryTypeCode(def) == "Identifier" {
		return identifierTokenSearchValue(req.ResourceType)
	}
	if req.ResourceType == "Provenance" && code == "target" {
		return "Organization/" + coregen.SetupResourceID("Organization")
	}
	if req.ResourceType == "HealthcareService" && code == "hsbilling" {
		return "NFE"
	}
	switch primaryTypeCode(def) {
	case "code", "Coding", "CodeableConcept":
		if bound, ok := resolveBoundCoding(def, b.reg); ok && bound.Code != "" {
			return bound.Code
		}
		return "momus-search"
	case "boolean":
		return "true"
	case "date", "dateTime", "instant", "time":
		return "2024-01-01"
	case "integer", "unsignedInt", "positiveInt", "decimal":
		return "123.45"
	case "Quantity":
		// A quantity search value is "number|system|code"; return a value that
		// matches the provisioned seed's quantity.
		return "123.45|http://unitsofmeasure.org|mmol"
	case "Reference":
		// A reference search value is "Type/id"; match the provisioned seed.
		if t := firstTargetResourceType(def, b.reg); t != "" {
			return t + "/" + coregen.SetupResourceID(t)
		}
		return "momus-search"
	default:
		return "momus-search"
	}
}

// identifierTokenSearchValue returns a profile-valid identifier value for
// resource types with strict AU/HCPD identifier constraints.
func identifierTokenSearchValue(resourceType string) string {
	switch resourceType {
	case "Organization":
		return generateABN()
	case "Practitioner":
		return generateHPIINumber()
	case "PractitionerRole":
		return "UPIN-123456"
	case "HealthcareService":
		return generateHPIONumber()
	case "Endpoint":
		return "smd-target-001"
	default:
		return "momus-search"
	}
}

// SearchInvalidValue returns a search value for the invalid-value obligation and
// whether a conformant server is expected to reject it.
func (b *fhirBuilder) SearchInvalidValue(req coverage.CoverageRequirement, code string) (string, bool) {
	paramType := b.SearchParamType(req, code)
	switch paramType {
	case "boolean":
		return "notabool", true
	case "number":
		// Multiple decimal points violate FHIR's number lexical form.
		return "12.3.4", true
	case "date", "dateTime", "instant":
		return "not-a-date", true
	default:
		return searchValidNonMatchValue(paramType), false
	}
}

// nearSearchValue returns a coordinate value for a near (special) search. It
// resolves to a fixed Sydney coordinate so the provisioned seed matches.
func nearSearchValue(b *fhirBuilder, req coverage.CoverageRequirement, code string) string {
	return "-33.8688|151.2093"
}

// compositeAcceptValue returns a "part1$part2" composite search value that
// matches the provisioned seed. It synthesises a value for each component of
// the composite expression, joining them with '$'.
func compositeAcceptValue(b *fhirBuilder, req coverage.CoverageRequirement, code string) string {
	sp, ok := b.reg.SearchParameter(req.ResourceType, code)
	if !ok {
		return "momus-search$momus-search"
	}
	paths := compositePaths(sp.Expression)
	if len(paths) < 2 {
		return "momus-search$momus-search"
	}
	parts := make([]string, 0, len(paths))
	for _, path := range paths {
		def, ok := searchElementDefinition(req.ResourceType, path, b.reg)
		if !ok || def == nil {
			parts = append(parts, "momus-search")
			continue
		}
		parts = append(parts, elementSearchValue(def, b.reg))
	}
	return strings.Join(parts, "$")
}

// elementSearchValue returns a query value for a single composite component
// element, matching how that element's search type is seeded.
func elementSearchValue(def *model.ElementDefinition, reg *registry.Registry) string {
	switch primaryTypeCode(def) {
	case "code", "Coding", "CodeableConcept":
		if bound, ok := resolveBoundCoding(def, reg); ok && bound.Code != "" {
			return bound.Code
		}
		return "momus-search"
	case "boolean":
		return "true"
	case "date", "dateTime", "instant", "time":
		return "2024-01-01"
	case "integer", "unsignedInt", "positiveInt", "decimal":
		return "123.45"
	case "Quantity":
		return "123.45|http://unitsofmeasure.org|mmol"
	default:
		return "momus-search"
	}
}

func searchValidNonMatchValue(paramType string) string {
	switch paramType {
	case "token":
		return "momus|nomatch"
	case "reference":
		return "Patient/momus-nomatch"
	case "uri":
		return "http://example.org/momus-nomatch"
	case "quantity":
		return "999|http://example.org/sys|nomatch"
	case "date", "dateTime", "instant":
		return "1900-01-01"
	case "number":
		return "123456.789"
	case "boolean":
		return "true"
	case "special":
		// A special (near) search uses coordinates; return a far-away location
		// so it is lexically valid but matches nothing.
		return "90.0|0.0"
	case "composite":
		// A composite value is "part1$part2"; return a syntactically valid pair
		// that cannot match any provisioned resource.
		return "momus-nomatch$momus-nomatch"
	default: // string and unknown
		return "momus-invalid-zzz"
	}
}

// searchElementDefinition resolves the element a search expression points at,
// returning its ElementDefinition from the registry.
func searchElementDefinition(resourceType, elementPath string, reg *registry.Registry) (*model.ElementDefinition, bool) {
	if reg == nil {
		return nil, false
	}
	for _, profile := range reg.ProfilesForResource(resourceType) {
		resolved, err := reg.ResolveProfile(profile.URL)
		if err != nil || resolved == nil {
			continue
		}
		// Try the exact key, then the choice-type "[x]" form (e.g. "occurred" ->
		// "occurred[x]").
		keys := []string{resourceType + "." + elementPath}
		if !strings.HasSuffix(elementPath, "[x]") {
			keys = append(keys, resourceType+"."+elementPath+"[x]")
		}
		for _, key := range keys {
			if node, ok := resolved.Elements[key]; ok && node != nil && node.Definition != nil {
				return node.Definition, true
			}
		}
	}
	return nil, false
}
