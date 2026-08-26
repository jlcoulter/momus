// Package search provides programmatic access to the FHIR search features
// that are implicit in the specification rather than declared as concrete
// SearchParameter resources in a package: the universal parameters, the
// per-type search modifiers, and the type→modifier mapping.
//
// FHIR defines a fixed set of universal search parameters (those with a base
// of "Resource" or "DomainResource") that a conformant server must support for
// every resource type. Some are shipped as SearchParameter resources in the
// R4 core package (_id, _lastUpdated, _content, _text, _filter, ...), but many
// are only referenced from the CapabilityStatement (_include, _revinclude,
// _has, _sort, _count, _summary, ...) and have no SearchParameter resource of
// their own. This package synthesizes the complete, authoritative set so
// callers never have to special-case the two sources.
package search

import (
	"sort"
	"strings"

	"github.com/jlcoulter/momus/internal/fhir/model"
)

// modifierMapping defines, per FHIR search parameter type, the modifiers that
// are valid for that type. This is derived from the FHIR search specification:
// modifiers are a function of the parameter's type, not something stored per
// SearchParameter resource.
var modifierMapping = map[string][]string{
	"string":    {"exact", "contains"},
	"token":     {"text", "not", "in", "not-in", "above", "below", "identifier"},
	"reference": {"type", "identifier"},
	"uri":       {"above", "below"},
	"quantity":  {"not"},
	"date":      {},
	"number":    {},
	"boolean":   {},
	"special":   {},
	"composite": {},
}

// ModifiersForType returns the FHIR search modifiers valid for a search
// parameter type, in the order defined by the specification, or nil when the
// type has no modifiers (or is unknown). The type argument is case-insensitive
// and is trimmed.
func ModifiersForType(paramType string) []string {
	mods, ok := modifierMapping[normalizeType(paramType)]
	if !ok || len(mods) == 0 {
		return nil
	}
	return append([]string(nil), mods...)
}

// TypeModifiers returns the full type→modifiers mapping. The returned map and
// its slices are copies and are safe to modify.
func TypeModifiers() map[string][]string {
	out := make(map[string][]string, len(modifierMapping))
	for t, mods := range modifierMapping {
		out[t] = append([]string(nil), mods...)
	}
	return out
}

// normalizeType lowercases and trims a search parameter type so lookups are
// robust against case differences in package data.
func normalizeType(t string) string {
	return strings.ToLower(strings.TrimSpace(t))
}

// declaredUniversal lists the universal search parameters that the FHIR R4 core
// package ships as concrete SearchParameter resources (base Resource or
// DomainResource). Their metadata is distilled to the canonical form Momus
// needs; the type/base values match the R4 definitions.
var declaredUniversal = []model.SearchParameter{
	{
		URL:        "http://hl7.org/fhir/SearchParameter/Resource-id",
		Name:       "_id",
		Code:       "_id",
		Base:       []string{"Resource"},
		Type:       "token",
		Expression: "Resource.id",
	},
	{
		URL:        "http://hl7.org/fhir/SearchParameter/Resource-lastUpdated",
		Name:       "_lastUpdated",
		Code:       "_lastUpdated",
		Base:       []string{"Resource"},
		Type:       "date",
		Expression: "Resource.meta.lastUpdated",
	},
	{
		URL:        "http://hl7.org/fhir/SearchParameter/Resource-profile",
		Name:       "_profile",
		Code:       "_profile",
		Base:       []string{"Resource"},
		Type:       "uri",
		Expression: "Resource.meta.profile",
	},
	{
		URL:        "http://hl7.org/fhir/SearchParameter/Resource-security",
		Name:       "_security",
		Code:       "_security",
		Base:       []string{"Resource"},
		Type:       "token",
		Expression: "Resource.meta.security",
	},
	{
		URL:        "http://hl7.org/fhir/SearchParameter/Resource-source",
		Name:       "_source",
		Code:       "_source",
		Base:       []string{"Resource"},
		Type:       "uri",
		Expression: "Resource.meta.source",
	},
	{
		URL:        "http://hl7.org/fhir/SearchParameter/Resource-tag",
		Name:       "_tag",
		Code:       "_tag",
		Base:       []string{"Resource"},
		Type:       "token",
		Expression: "Resource.meta.tag",
	},
	{
		URL:        "http://hl7.org/fhir/SearchParameter/Resource-content",
		Name:       "_content",
		Code:       "_content",
		Base:       []string{"Resource"},
		Type:       "string",
		Expression: "Resource",
	},
	{
		URL:        "http://hl7.org/fhir/SearchParameter/DomainResource-text",
		Name:       "_text",
		Code:       "_text",
		Base:       []string{"DomainResource"},
		Type:       "string",
		Expression: "DomainResource.text",
	},
	{
		URL:        "http://hl7.org/fhir/SearchParameter/filter",
		Name:       "_filter",
		Code:       "_filter",
		Base:       []string{"Resource"},
		Type:       "special",
		Expression: "",
	},
}

// implicitUniversal lists the universal search parameters that exist only by
// specification reference (they appear in the CapabilityStatement's system-level
// searchParam entries but have no SearchParameter resource file in the R4 core
// package). They are synthesized here so callers get a complete, authoritative
// set regardless of which source the data came from.
var implicitUniversal = []model.SearchParameter{
	{
		URL:        "http://hl7.org/fhir/SearchParameter/Resource-include",
		Name:       "_include",
		Code:       "_include",
		Base:       []string{"Resource"},
		Type:       "token",
		Expression: "",
	},
	{
		URL:        "http://hl7.org/fhir/SearchParameter/Resource-revinclude",
		Name:       "_revinclude",
		Code:       "_revinclude",
		Base:       []string{"Resource"},
		Type:       "token",
		Expression: "",
	},
	{
		URL:        "http://hl7.org/fhir/SearchParameter/Resource-has",
		Name:       "_has",
		Code:       "_has",
		Base:       []string{"Resource"},
		Type:       "composite",
		Expression: "",
	},
	{
		URL:        "http://hl7.org/fhir/SearchParameter/Resource-type",
		Name:       "_type",
		Code:       "_type",
		Base:       []string{"Resource"},
		Type:       "token",
		Expression: "",
	},
	{
		URL:        "http://hl7.org/fhir/SearchParameter/Resource-sort",
		Name:       "_sort",
		Code:       "_sort",
		Base:       []string{"Resource"},
		Type:       "token",
		Expression: "",
	},
	{
		URL:        "http://hl7.org/fhir/SearchParameter/Resource-count",
		Name:       "_count",
		Code:       "_count",
		Base:       []string{"Resource"},
		Type:       "number",
		Expression: "",
	},
	{
		URL:        "http://hl7.org/fhir/SearchParameter/Resource-summary",
		Name:       "_summary",
		Code:       "_summary",
		Base:       []string{"Resource"},
		Type:       "token",
		Expression: "",
	},
	{
		URL:        "http://hl7.org/fhir/SearchParameter/Resource-elements",
		Name:       "_elements",
		Code:       "_elements",
		Base:       []string{"Resource"},
		Type:       "string",
		Expression: "",
	},
	{
		URL:        "http://hl7.org/fhir/SearchParameter/Resource-contained",
		Name:       "_contained",
		Code:       "_contained",
		Base:       []string{"Resource"},
		Type:       "token",
		Expression: "",
	},
	{
		URL:        "http://hl7.org/fhir/SearchParameter/Resource-containedType",
		Name:       "_containedType",
		Code:       "_containedType",
		Base:       []string{"Resource"},
		Type:       "token",
		Expression: "",
	},
	{
		URL:        "http://hl7.org/fhir/SearchParameter/Resource-list",
		Name:       "_list",
		Code:       "_list",
		Base:       []string{"Resource"},
		Type:       "token",
		Expression: "",
	},
	{
		URL:        "http://hl7.org/fhir/SearchParameter/Resource-query",
		Name:       "_query",
		Code:       "_query",
		Base:       []string{"Resource"},
		Type:       "token",
		Expression: "",
	},
}

// UniversalParameters returns the complete set of universal search parameters
// that apply to every resource type. This merges the parameters that the FHIR
// R4 core package declares as SearchParameter resources (declaredUniversal)
// with the implicit ones that exist only by spec reference (implicitUniversal).
// The returned slice is a fresh copy, sorted by code; it is safe to modify.
func UniversalParameters() []model.SearchParameter {
	out := make([]model.SearchParameter, 0, len(declaredUniversal)+len(implicitUniversal))
	out = append(out, declaredUniversal...)
	out = append(out, implicitUniversal...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

// DeclaredUniversal returns the universal parameters that the FHIR package
// ships as concrete SearchParameter resources (Base "Resource" or
// "DomainResource"). Use this when you want to filter out parameters the
// package already defines explicitly. The returned slice is a copy.
func DeclaredUniversal() []model.SearchParameter {
	out := make([]model.SearchParameter, len(declaredUniversal))
	copy(out, declaredUniversal)
	return out
}

// ImplicitUniversal returns the universal parameters that exist only by
// specification reference (no SearchParameter resource in the package). The
// returned slice is a copy.
func ImplicitUniversal() []model.SearchParameter {
	out := make([]model.SearchParameter, len(implicitUniversal))
	copy(out, implicitUniversal)
	return out
}
