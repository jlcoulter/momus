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
	def, ok := searchElementDefinition(req.ResourceType, elementPath, b.reg)
	if !ok || def == nil {
		return "momus-search"
	}
	switch primaryTypeCode(def) {
	case "code", "Coding", "CodeableConcept":
		if bound, ok := resolveBoundCoding(def, b.reg); ok && bound.Code != "" {
			return bound.Code
		}
		return "momus-search"
	case "boolean":
		return "true"
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

// searchValidNonMatchValue returns a syntactically valid value, appropriate to
// the search parameter type, that cannot match any provisioned resource. Such a
// value is accepted by a conformant server (200 with no results).
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
	default: // string, composite, special, and unknown
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
		if node, ok := resolved.Elements[resourceType+"."+elementPath]; ok && node != nil && node.Definition != nil {
			return node.Definition, true
		}
	}
	return nil, false
}
