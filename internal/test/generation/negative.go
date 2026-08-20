package generation

import (
	"fmt"
	"strings"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
	"github.com/jlcoulter/momus/internal/test/coverage"
)

// isNegativeVariant reports whether a requirement variant's generated test must
// be rejected by a conformant server (i.e. it asserts a constraint violation).
func isNegativeVariant(variant coverage.CoverageVariant) bool {
	return variant.IsReject()
}

// applyNegativeMutation mutates an otherwise-valid payload so that it violates
// exactly the one constraint identified by the requirement variant. Positive
// variants are left unchanged.
func applyNegativeMutation(body map[string]any, req coverage.CoverageRequirement, reg *registry.Registry) {
	switch req.Variant {
	case coverage.CoverageVariantMissingRequired,
		coverage.CoverageVariantTerminologyAbsent:
		deletePath(body, req.ElementPath)
	case coverage.CoverageVariantDatatypeNull:
		setPath(body, req.ElementPath, nil)
	case coverage.CoverageVariantDatatypeInvalidLexical,
		coverage.CoverageVariantDatatypeWrongJSONType:
		setPath(body, req.ElementPath, wrongDatatypeValue(req, reg))
	case coverage.CoverageVariantTerminologyInvalid:
		setBogusCode(body, req.ElementPath)
	case coverage.CoverageVariantReferenceWrongTarget:
		mutateReferenceType(body, req.ElementPath)
	case coverage.CoverageVariantReferenceDangling:
		mutateReferenceDangling(body, req.ElementPath)
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

// lookupChild finds a property by exact name, falling back to a choice-type key
// that starts with "name." (e.g. "value" -> "valueString").
func lookupChild(m map[string]any, name string) (any, string, bool) {
	if v, ok := m[name]; ok {
		return v, name, true
	}
	prefix := name + "."
	for k, v := range m {
		if strings.HasPrefix(k, prefix) {
			return v, k, true
		}
	}
	return nil, "", false
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

func deletePath(body map[string]any, path string) {
	segments := elementSegments(path)
	if len(segments) == 0 {
		return
	}
	if len(segments) == 1 {
		delete(body, segments[0])
		return
	}
	parent, key, ok := descendParent(body, segments)
	if ok {
		delete(parent, key)
	}
}

func setPath(body map[string]any, path string, value any) {
	segments := elementSegments(path)
	if len(segments) == 0 {
		return
	}
	if len(segments) == 1 {
		body[segments[0]] = value
		return
	}
	parent, key, ok := descendParent(body, segments)
	if ok {
		parent[key] = value
	}
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
// with a value that does not exist in any real value set.
func setBogusCode(body map[string]any, path string) {
	segments := elementSegments(path)
	if len(segments) == 0 {
		return
	}
	var parent map[string]any
	var key string
	if len(segments) == 1 {
		parent, key = body, segments[0]
	} else {
		var ok bool
		parent, key, ok = descendParent(body, segments)
		if !ok {
			return
		}
	}
	switch v := parent[key].(type) {
	case map[string]any:
		bogusCodedValue(v)
	case []any:
		if len(v) > 0 {
			if m, ok := v[0].(map[string]any); ok {
				bogusCodedValue(m)
			}
		}
	default:
		parent[key] = "not-a-real-code"
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
	segments := elementSegments(path)
	if len(segments) == 0 {
		return nil
	}
	var parent map[string]any
	var key string
	if len(segments) == 1 {
		parent, key = body, segments[0]
	} else {
		var ok bool
		parent, key, ok = descendParent(body, segments)
		if !ok {
			return nil
		}
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

// mutateReferenceType retargets references to a different resource type.
func mutateReferenceType(body map[string]any, path string) {
	for _, ref := range referenceMapsAt(body, path) {
		id := referenceID(ref["reference"])
		ref["type"] = "Organization"
		ref["reference"] = "Organization/" + id
	}
}

// mutateReferenceDangling retargets references to a nonexistent resource.
func mutateReferenceDangling(body map[string]any, path string) {
	for _, ref := range referenceMapsAt(body, path) {
		ref["reference"] = "Patient/momus-does-not-exist"
		ref["type"] = "Patient"
	}
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
