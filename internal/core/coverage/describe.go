package coverage

import (
	"fmt"
	"strings"
)

// DescribeCoverageRequirement renders a human-readable sentence explaining what
// conformance obligation a coverage requirement tests, from its domain, variant,
// and contextual metadata (resource type, element path, search code, ...).
func DescribeCoverageRequirement(req CoverageRequirement) string {
	rt := strings.TrimSpace(req.ResourceType)
	if rt == "" {
		rt = "resource"
	}
	elem := strings.TrimSpace(req.ElementPath)
	if elem == "" {
		elem = rt
	}
	elemLabel := elem

	switch req.Variant {
	// Cardinality domain.
	case CoverageVariantValidMin:
		return fmt.Sprintf("%s: accept a resource with the required element present (min=%d)", elemLabel, req.Min)
	case CoverageVariantMissingRequired:
		return fmt.Sprintf("%s: reject a resource missing the required element (min=%d)", elemLabel, req.Min)
	case CoverageVariantMultipleValues:
		return fmt.Sprintf("%s: accept a resource with multiple values (max=%s)", elemLabel, req.Max)

	// Datatype domain.
	case CoverageVariantDatatypeValid:
		return fmt.Sprintf("%s (%s): accept a valid value", elemLabel, req.Datatype)
	case CoverageVariantDatatypeInvalidLexical:
		return fmt.Sprintf("%s (%s): reject a lexically invalid value", elemLabel, req.Datatype)
	case CoverageVariantDatatypeWrongJSONType:
		return fmt.Sprintf("%s (%s): reject a value with the wrong JSON type", elemLabel, req.Datatype)
	case CoverageVariantDatatypeNull:
		return fmt.Sprintf("%s (%s): reject a null value for a non-null element", elemLabel, req.Datatype)

	// Terminology domain.
	case CoverageVariantTerminologyValid:
		return fmt.Sprintf("%s: accept a valid code from the bound value set", elemLabel)
	case CoverageVariantTerminologyInvalid:
		return fmt.Sprintf("%s: reject an invalid code", elemLabel)
	case CoverageVariantTerminologyAbsent:
		return fmt.Sprintf("%s: reject a missing required bound code", elemLabel)

	// Structure domain.
	case CoverageVariantStructureSlicePresent:
		return fmt.Sprintf("%s: accept a resource with the required slice present", elemLabel)

	// Invariant domain.
	case CoverageVariantInvariantSatisfies:
		return fmt.Sprintf("%s: accept a resource satisfying the invariant", elemLabel)
	case CoverageVariantInvariantViolates:
		return fmt.Sprintf("%s: reject a resource violating the invariant", elemLabel)

	// Reference domain.
	case CoverageVariantReferenceValid:
		return fmt.Sprintf("%s: accept a valid reference to a permitted target", elemLabel)
	case CoverageVariantReferenceWrongTarget:
		return fmt.Sprintf("%s: reject a reference to the wrong target type", elemLabel)
	case CoverageVariantReferenceDangling:
		return fmt.Sprintf("%s: reject a dangling reference to a nonexistent resource", elemLabel)

	// Interaction domain.
	case CoverageVariantInteractionPair:
		return fmt.Sprintf("%s: accept both elements present together (pairwise)", elemLabel)

	// Search domain.
	case CoverageVariantSearchValid:
		return fmt.Sprintf("%s?%s: return results for a valid search", rt, req.SearchCode)
	case CoverageVariantSearchNoResults:
		return fmt.Sprintf("%s?%s: accept a valid search returning no results", rt, req.SearchCode)
	case CoverageVariantSearchInvalidValue:
		return fmt.Sprintf("%s?%s: handle an invalid search value", rt, req.SearchCode)
	case CoverageVariantSearchMultipleResults:
		return fmt.Sprintf("%s?%s: return multiple results for a valid search", rt, req.SearchCode)
	case CoverageVariantSearchInvalidModifier:
		return fmt.Sprintf("%s?%s: reject an invalid search modifier", rt, req.SearchCode)
	case CoverageVariantSearchCombination:
		return fmt.Sprintf("%s?%s&%s: return results for a combined search", rt, req.SearchCode, req.SearchCodeB)
	case CoverageVariantSearchInclude:
		return fmt.Sprintf("%s?_include=%s: return a bundle including %s resources", rt, req.SearchCode, req.SearchTargetType)
	case CoverageVariantSearchRevInclude:
		return fmt.Sprintf("%s?_revinclude=%s: return a bundle including %s resources", rt, req.SearchCode, req.SearchTargetType)
	case CoverageVariantSearchChaining:
		return fmt.Sprintf("%s?%s: return results for a chained search", rt, req.SearchCode)

	// Operation domain.
	case CoverageVariantOperationRead:
		return fmt.Sprintf("%s: read (GET) returns the resource", rt)
	case CoverageVariantOperationUpdate:
		return fmt.Sprintf("%s: update (PUT) modifies the resource", rt)
	case CoverageVariantOperationPatch:
		return fmt.Sprintf("%s: patch (PATCH) partially modifies the resource", rt)
	case CoverageVariantOperationDelete:
		return fmt.Sprintf("%s: delete removes the resource", rt)
	case CoverageVariantOperationHistory:
		return fmt.Sprintf("%s: history returns the version chain", rt)
	case CoverageVariantOperationCustom:
		name := req.OperationName
		if name == "" {
			name = "custom"
		}
		return fmt.Sprintf("%s: $%s custom operation succeeds", rt, name)

	// State domain.
	case CoverageVariantStateCRUDSequence:
		return fmt.Sprintf("%s: create-read-update-read-delete-read(404) sequence", rt)
	case CoverageVariantStateReadNonexistent:
		return fmt.Sprintf("%s: read a nonexistent resource returns 404", rt)
	case CoverageVariantStateDeleteNonexistent:
		return fmt.Sprintf("%s: delete a nonexistent resource returns 404/200", rt)
	}

	// Fallback for unknown variants: describe from the variant name alone.
	return fmt.Sprintf("%s: %s obligation", rt, string(req.Variant))
}

// HumanID renders a human-friendly identifier for an obligation, more readable
// than the pipe-delimited machine ID. It drops verbose segments (profile URL,
// datatype where redundant) and uses dots instead of pipes.
func HumanID(req CoverageRequirement) string {
	rt := strings.TrimSpace(req.ResourceType)
	if rt == "" {
		rt = "resource"
	}
	switch req.Domain {
	case CoverageDomainSearch:
		code := strings.TrimSpace(req.SearchCode)
		if code == "" {
			code = "search"
		}
		if req.Variant == CoverageVariantSearchCombination && req.SearchCodeB != "" {
			code = code + "+" + req.SearchCodeB
		}
		if req.SearchModifier != "" {
			code = code + ":" + req.SearchModifier
		}
		return rt + ".search." + code + "." + shortVariant(req.Variant)
	case CoverageDomainOperation:
		return rt + ".operation." + shortVariant(req.Variant)
	case CoverageDomainState:
		return rt + ".state." + shortVariant(req.Variant)
	case CoverageDomainInteraction:
		return rt + ".interaction.pair"
	default:
		elem := strings.TrimSpace(req.ElementPath)
		elem = strings.TrimPrefix(elem, rt+".")
		if elem == "" {
			elem = rt
		}
		return rt + "." + elem + "." + string(req.Domain) + "." + string(req.Variant)
	}
}

// shortVariant strips a domain prefix from a variant name where present (e.g.
// "search-valid" -> "valid", "operation-read" -> "read").
func shortVariant(v CoverageVariant) string {
	s := string(v)
	for _, prefix := range []string{"search-", "operation-", "datatype-", "terminology-", "state-", "cardinality-", "reference-", "invariant-", "structure-"} {
		if strings.HasPrefix(s, prefix) {
			return strings.TrimPrefix(s, prefix)
		}
	}
	return s
}

// DescribeAPIOperation renders a human-readable sentence for an OpenAPI operation
// obligation (a constraint.KindAPIOperation), given the HTTP method and path.
func DescribeAPIOperation(method, path string) string {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = "request"
	}
	return fmt.Sprintf("%s %s: respond correctly", method, path)
}

// DescribeAPIParameter renders a human-readable sentence for an OpenAPI parameter
// obligation (a constraint.KindAPIParameter).
func DescribeAPIParameter(method, path, in, name string) string {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = "request"
	}
	location := strings.TrimSpace(in)
	if location == "" {
		location = "request"
	}
	return fmt.Sprintf("%s %s: %s parameter %s is handled", method, path, location, name)
}

// APIOperationHumanID renders a human-friendly identifier for an OpenAPI
// operation obligation.
func APIOperationHumanID(method, path string) string {
	return strings.ToUpper(strings.TrimSpace(method)) + " " + strings.TrimSpace(path)
}

var domainDescriptions = map[CoverageDomain]string{
	CoverageDomainCardinality: "Required element presence and count constraints",
	CoverageDomainDatatype:    "Element type constraints (code, string, integer, etc.)",
	CoverageDomainTerminology: "Value set binding constraints (required/extensible/preferred)",
	CoverageDomainStructure:   "Required slice presence constraints",
	CoverageDomainInvariant:   "FHIRPath invariant constraints",
	CoverageDomainReference:   "Reference target type constraints",
	CoverageDomainInteraction: "Pairwise element co-occurrence constraints",
	CoverageDomainSearch:      "Search parameter support and behavior",
	CoverageDomainOperation:   "RESTful interaction and custom operation support",
	CoverageDomainState:       "CRUD lifecycle sequence constraints",
}

var variantDescriptions = map[CoverageVariant]string{
	CoverageVariantValidMin:               "Accept: resource with required element at minimum cardinality",
	CoverageVariantMissingRequired:        "Reject: resource missing a required element",
	CoverageVariantMultipleValues:         "Accept: resource with multiple values for a repeatable element",
	CoverageVariantDatatypeValid:          "Accept: valid value for the declared datatype",
	CoverageVariantDatatypeInvalidLexical: "Reject: lexically invalid value for the datatype",
	CoverageVariantDatatypeWrongJSONType:  "Reject: value with wrong JSON type for the datatype",
	CoverageVariantDatatypeNull:           "Reject: null value for a non-null element",
	CoverageVariantTerminologyValid:       "Accept: valid code from the bound value set",
	CoverageVariantTerminologyInvalid:     "Reject: invalid code not in the bound value set",
	CoverageVariantTerminologyAbsent:      "Reject: missing required bound code",
	CoverageVariantStructureSlicePresent:  "Accept: resource with the required slice present",
	CoverageVariantInvariantSatisfies:     "Accept: resource satisfying the invariant",
	CoverageVariantInvariantViolates:      "Reject: resource violating the invariant",
	CoverageVariantReferenceValid:         "Accept: valid reference to a permitted target",
	CoverageVariantReferenceWrongTarget:   "Reject: reference to a wrong target type",
	CoverageVariantReferenceDangling:      "Reject: dangling reference to a nonexistent resource",
	CoverageVariantInteractionPair:        "Accept: both elements present together (pairwise)",
	CoverageVariantSearchValid:            "Accept: server returns results for a valid search",
	CoverageVariantSearchNoResults:        "Accept: server handles a valid search returning no results",
	CoverageVariantSearchInvalidValue:     "Accept: server handles an invalid search value",
	CoverageVariantSearchMultipleResults:  "Accept: server returns multiple results for a valid search",
	CoverageVariantSearchInvalidModifier:  "Reject: server rejects an invalid search modifier",
	CoverageVariantSearchCombination:      "Accept: server returns results for a combined search",
	CoverageVariantSearchInclude:          "Accept: server returns a bundle including the referenced resources",
	CoverageVariantSearchRevInclude:       "Accept: server returns a bundle including the referencing resources",
	CoverageVariantSearchChaining:         "Accept: server returns results for a chained search",
	CoverageVariantOperationRead:          "Accept: read (GET) returns the resource",
	CoverageVariantOperationUpdate:        "Accept: update (PUT) modifies the resource",
	CoverageVariantOperationPatch:         "Accept: patch (PATCH) partially modifies the resource",
	CoverageVariantOperationDelete:        "Accept: delete removes the resource",
	CoverageVariantOperationHistory:       "Accept: history returns the version chain",
	CoverageVariantOperationCustom:        "Accept: custom operation ($name) succeeds",
	CoverageVariantStateCRUDSequence:      "Accept: create-read-update-read-delete-read(404) sequence",
	CoverageVariantStateReadNonexistent:   "Accept: read a nonexistent resource returns 404",
	CoverageVariantStateDeleteNonexistent: "Accept: delete a nonexistent resource returns 404/200",
}

// DomainDescriptions returns a static glossary mapping every coverage domain to
// a plain-English description of what that domain tests.
func DomainDescriptions() map[CoverageDomain]string {
	out := make(map[CoverageDomain]string, len(domainDescriptions))
	for k, v := range domainDescriptions {
		out[k] = v
	}
	return out
}

// VariantDescriptions returns a static glossary mapping every coverage variant
// to a plain-English description of what that variant tests.
func VariantDescriptions() map[CoverageVariant]string {
	out := make(map[CoverageVariant]string, len(variantDescriptions))
	for k, v := range variantDescriptions {
		out[k] = v
	}
	return out
}
