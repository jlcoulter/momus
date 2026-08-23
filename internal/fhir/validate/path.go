package validate

import (
	"strings"
	"unicode"
)

// This file provides path-based value resolution over a FHIR resource JSON
// body. It mirrors the generator's element-path conventions: a canonical path
// "Patient.name" maps to resource["name"], arrays are traversed element-wise,
// and choice-type keys ("value" -> "valueString") resolve by prefix.

// elementSegments returns the path segments after the resource-type root, e.g.
// "Patient.name.given" -> ["name", "given"]. Returns nil when the path has no
// dot (a bare type name).
func elementSegments(path string) []string {
	parts := strings.Split(path, ".")
	if len(parts) <= 1 {
		return nil
	}
	return parts[1:]
}

// choiceKeyMatches reports whether key is the concrete serialization of the
// choice element name, i.e. name followed by a capitalized datatype suffix
// (e.g. "value" matches "valueString", "valueQuantity"). FHIR choice
// properties carry no separator, so a plain prefix match on name + "." would
// miss them.
// choiceKeyMatches reports whether key is the concrete serialization of the
// choice element name, i.e. name followed by a capitalized datatype suffix
// (e.g. "value" matches "valueString", "valueQuantity"). FHIR choice
// properties carry no separator, so a plain prefix match on name + "." would
// miss them. The name may carry the FHIR choice placeholder suffix "[x]"
// (e.g. "value[x]"), which is stripped before matching so "value[x]" matches
// "valueString" and "valueQuantity" too.
func choiceKeyMatches(key, name string) bool {
	name = strings.TrimSuffix(name, "[x]")
	if len(key) <= len(name) || !strings.HasPrefix(key, name) {
		return false
	}
	return unicode.IsUpper(rune(key[len(name)]))
}

// resolveLeafKey returns the actual property key holding the leaf named name in
// parent, resolving a choice-type prefix (e.g. "value" -> "valueString"). It
// returns "" when the leaf is absent.
func resolveLeafKey(parent map[string]any, name string) string {
	if _, ok := parent[name]; ok {
		return name
	}
	for k := range parent {
		if choiceKeyMatches(k, name) {
			return k
		}
	}
	return ""
}

// resolvePath returns every value reachable at the given canonical FHIR path
// within the resource. A path that names an array element yields each array
// element's matching value; a path ending in a choice element resolves the
// concrete choice key. When no value is present at the path, present is false.
func resolvePath(resource map[string]any, path string) (values []any, present bool) {
	segments := elementSegments(path)
	if len(segments) == 0 {
		return []any{resource}, true
	}
	cur := []any{resource}
	for i, seg := range segments {
		last := i == len(segments)-1
		var next []any
		for _, c := range cur {
			m, ok := c.(map[string]any)
			if !ok {
				continue
			}
			key := seg
			if _, ok := m[seg]; ok {
				key = seg
			} else if rk := resolveLeafKey(m, seg); rk != "" {
				key = rk
			} else {
				continue
			}
			val := m[key]
			if last {
				if arr, isArr := val.([]any); isArr {
					next = append(next, arr...)
				} else {
					next = append(next, val)
				}
			} else {
				switch v := val.(type) {
				case []any:
					next = append(next, v...)
				default:
					next = append(next, v)
				}
			}
		}
		if len(next) == 0 {
			return nil, false
		}
		cur = next
	}
	return cur, true
}
