package generation

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/jlcoulter/momus/internal/core/coverage"
	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

// isNegativeVariant reports whether a requirement variant's generated test must
// be rejected by a conformant server (i.e. it asserts a constraint violation).
func isNegativeVariant(variant coverage.CoverageVariant) bool {
	return variant.IsReject()
}

// applyNegativeMutation mutates an otherwise-valid payload so that it violates
// exactly the one constraint identified by the requirement variant. Positive
// variants are left unchanged. It returns whether the mutation produced a
// concrete violation: false when the requirement's element is not present in the
// payload, in which case the caller should skip the negative test rather than
// emit a reject assertion a conformant server would accept.
func applyNegativeMutation(body map[string]any, req coverage.CoverageRequirement, reg *registry.Registry) bool {
	switch req.Variant {
	case coverage.CoverageVariantMissingRequired,
		coverage.CoverageVariantTerminologyAbsent:
		return deletePath(body, req.ElementPath)
	case coverage.CoverageVariantDatatypeNull:
		return setPath(body, req.ElementPath, nil)
	case coverage.CoverageVariantDatatypeInvalidLexical,
		coverage.CoverageVariantDatatypeWrongJSONType:
		return setPath(body, req.ElementPath, wrongDatatypeValue(req, reg))
	case coverage.CoverageVariantTerminologyInvalid:
		return setBogusCode(body, req.ElementPath)
	case coverage.CoverageVariantReferenceWrongTarget:
		return mutateReferenceType(body, req.ElementPath)
	case coverage.CoverageVariantReferenceDangling:
		return mutateReferenceDangling(body, req.ElementPath)
	default:
		// Positive variants require no mutation.
		return true
	}
}

// elementSegments returns the property segments of a canonical element path,
// dropping the leading resource type (e.g. "Patient.name.given" -> [name given]).
func elementSegments(path string) []string {
	parts := strings.Split(path, ".")
	if len(parts) <= 1 {
		return nil
	}
	return parts[1:]
}

// choiceKeyMatches reports whether key is the concrete serialization of the
// choice element name, i.e. name followed by a capitalized datatype suffix (e.g.
// "value" matches "valueString", "valueQuantity"). FHIR choice properties carry
// no separator, so a plain prefix match on name + "." would miss them.
func choiceKeyMatches(key, name string) bool {
	if len(key) <= len(name) || !strings.HasPrefix(key, name) {
		return false
	}
	return unicode.IsUpper(rune(key[len(name)]))
}

// lookupChild finds a property by exact name, falling back to a choice-type key
// (e.g. "value" -> "valueString").
func lookupChild(m map[string]any, name string) (any, string, bool) {
	if v, ok := m[name]; ok {
		return v, name, true
	}
	for k, v := range m {
		if choiceKeyMatches(k, name) {
			return v, k, true
		}
	}
	return nil, "", false
}

// resolveLeafKey returns the actual property key holding the leaf named name in
// parent, resolving a choice-type prefix (e.g. "value" -> "valueString"). It
// returns "" when the leaf is absent. A FHIR payload carries at most one choice
// variant, so the sorted-first match is a deterministic single candidate.
func resolveLeafKey(parent map[string]any, name string) string {
	if _, ok := parent[name]; ok {
		return name
	}
	matches := make([]string, 0, 1)
	for k := range parent {
		if choiceKeyMatches(k, name) {
			matches = append(matches, k)
		}
	}
	if len(matches) == 0 {
		return ""
	}
	sort.Strings(matches)
	return matches[0]
}

// descendParent walks to the container holding the leaf, descending into the
// first element of repeatable (array) fields.
func descendParent(body map[string]any, segments []string) (map[string]any, string, bool) {
	if len(segments) == 0 {
		return nil, "", false
	}
	cur := body
	for i := 0; i < len(segments)-1; i++ {
		child, _, ok := lookupChild(cur, segments[i])
		if !ok {
			return nil, "", false
		}
		switch v := child.(type) {
		case map[string]any:
			cur = v
		case []any:
			if len(v) == 0 {
				return nil, "", false
			}
			m, ok := v[0].(map[string]any)
			if !ok {
				return nil, "", false
			}
			cur = m
		default:
			return nil, "", false
		}
	}
	return cur, segments[len(segments)-1], true
}

// resolveLeafContainer returns the container map and the actual property key for
// the leaf of path, resolving a choice-type prefix (e.g. "value" ->
// "valueString") on the leaf. found is false when the leaf is absent, so callers
// no-op instead of creating an illegal bare choice key.
func resolveLeafContainer(body map[string]any, path string) (map[string]any, string, bool) {
	segments := elementSegments(path)
	if len(segments) == 0 {
		return nil, "", false
	}
	if len(segments) == 1 {
		if key := resolveLeafKey(body, segments[0]); key != "" {
			return body, key, true
		}
		return nil, "", false
	}
	parent, key, ok := descendParent(body, segments)
	if !ok {
		return nil, "", false
	}
	if leaf := resolveLeafKey(parent, key); leaf != "" {
		return parent, leaf, true
	}
	return nil, "", false
}

// deletePath deletes the leaf at path, returning whether a leaf was present and
// removed (false when the element is absent from the payload, in which case no
// violation is created by deletion).
func deletePath(body map[string]any, path string) bool {
	parent, key, ok := resolveLeafContainer(body, path)
	if !ok {
		return false
	}
	delete(parent, key)
	return true
}

// setPath sets the leaf at path to value, reporting whether a leaf was present
// and modified (false when the element is absent, so no invalid value could be
// placed).
func setPath(body map[string]any, path string, value any) bool {
	parent, key, ok := resolveLeafContainer(body, path)
	if !ok {
		return false
	}
	parent[key] = value
	return true
}

// wrongDatatypeValue produces a value that violates the element's datatype in
// one of two ways: an invalid lexical form, or a JSON value of the wrong type.
func wrongDatatypeValue(req coverage.CoverageRequirement, reg *registry.Registry) any {
	typeCode := elementTypeOf(reg, req.ProfileURL, req.ElementPath)
	if req.Variant == coverage.CoverageVariantDatatypeInvalidLexical {
		switch typeCode {
		case "date", "dateTime", "instant", "time":
			return "not-a-date"
		case "integer", "positiveInt", "unsignedInt", "decimal":
			return "12abc"
		case "uri", "canonical", "url", "oid":
			return "not a uri"
		default:
			if typeCode == "" {
				return "not-a-valid-value"
			}
			return "not-a-" + typeCode
		}
	}
	// Wrong JSON type: supply a value whose Go/JSON type contradicts the element.
	switch typeCode {
	case "string", "uri", "code", "id", "markdown", "canonical", "url", "oid", "uuid":
		return 42
	case "integer", "positiveInt", "unsignedInt", "decimal":
		return "not-a-number"
	case "boolean":
		return "not-a-boolean"
	case "date", "dateTime", "instant", "time":
		return 42
	default:
		return true
	}
}

// setBogusCode replaces the code of a coded (code/Coding/CodeableConcept) field
// with a value that does not exist in any real value set. It reports whether a
// coded leaf was found and mutated.
func setBogusCode(body map[string]any, path string) bool {
	parent, key, ok := resolveLeafContainer(body, path)
	if !ok {
		return false
	}
	switch v := parent[key].(type) {
	case map[string]any:
		bogusCodedValue(v)
		return true
	case []any:
		if len(v) > 0 {
			if m, ok := v[0].(map[string]any); ok {
				bogusCodedValue(m)
				return true
			}
		}
		return false
	default:
		parent[key] = "not-a-real-code"
		return true
	}
}

func bogusCodedValue(m map[string]any) {
	if coding, ok := m["coding"].([]any); ok && len(coding) > 0 {
		if c, ok := coding[0].(map[string]any); ok {
			c["code"] = "not-a-real-code"
			return
		}
	}
	m["code"] = "not-a-real-code"
}

// referenceMapsAt returns the Reference object(s) at an element path.
func referenceMapsAt(body map[string]any, path string) []map[string]any {
	parent, key, ok := resolveLeafContainer(body, path)
	if !ok {
		return nil
	}
	var out []map[string]any
	switch v := parent[key].(type) {
	case map[string]any:
		out = append(out, v)
	case []any:
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
	}
	return out
}

// mutateReferenceType retargets references to a different resource type,
// reporting whether any reference was retargeted.
func mutateReferenceType(body map[string]any, path string) bool {
	refs := referenceMapsAt(body, path)
	if len(refs) == 0 {
		return false
	}
	for _, ref := range refs {
		id := referenceID(ref["reference"])
		ref["type"] = "Organization"
		ref["reference"] = "Organization/" + id
	}
	return true
}

// mutateReferenceDangling retargets references to a nonexistent resource,
// reporting whether any reference was retargeted.
func mutateReferenceDangling(body map[string]any, path string) bool {
	refs := referenceMapsAt(body, path)
	if len(refs) == 0 {
		return false
	}
	for _, ref := range refs {
		ref["reference"] = "Patient/momus-does-not-exist"
		ref["type"] = "Patient"
	}
	return true
}

func referenceID(ref any) string {
	s := fmt.Sprint(ref)
	if idx := strings.LastIndex(s, "/"); idx >= 0 && idx+1 < len(s) {
		return s[idx+1:]
	}
	return "momus-wrong"
}

func elementTypeOf(reg *registry.Registry, profileURL, path string) string {
	def := elementDefinitionOf(reg, profileURL, path)
	if def == nil || len(def.Types) == 0 {
		return ""
	}
	return def.Types[0].Code
}

func elementDefinitionOf(reg *registry.Registry, profileURL, path string) *model.ElementDefinition {
	if reg == nil || strings.TrimSpace(profileURL) == "" {
		return nil
	}
	resolved, err := reg.ResolveProfile(profileURL)
	if err != nil || resolved == nil {
		return nil
	}
	node := resolved.Elements[path]
	if node == nil || node.Definition == nil {
		return nil
	}
	return node.Definition
}
