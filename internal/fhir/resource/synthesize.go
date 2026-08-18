package resource

import (
	"strconv"
	"strings"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

// refTarget is a resolved relationship reference: the resource type and the
// local dataset ID it points at.
type refTarget struct {
	resourceType string
	localID      string
}

// synthesizeResource builds a concrete resource body for the given resource
// type and profile, populating required elements with sample values and
// wiring relationship references.
func synthesizeResource(reg *registry.Registry, resourceType, profileURL, id string, refs map[string]refTarget) (map[string]any, error) {
	body := map[string]any{
		"resourceType": resourceType,
		"id":           id,
	}
	if profileURL == "" {
		profileURL = defaultProfile(reg, resourceType)
	}
	if profileURL != "" {
		resolved, err := reg.ResolveProfile(profileURL)
		if err == nil && resolved != nil && resolved.Root != nil {
			populateChildren(body, resolved.Root, reg, refs)
		}
	}
	return body, nil
}

func defaultProfile(reg *registry.Registry, resourceType string) string {
	if reg == nil || resourceType == "" {
		return ""
	}
	profiles := reg.ProfilesForResource(resourceType)
	for _, p := range profiles {
		if p != nil && strings.TrimSpace(p.URL) != "" {
			return p.URL
		}
	}
	return ""
}

// populateChildren fills every required (Min > 0) child of a node into body.
func populateChildren(body map[string]any, node *model.ElementNode, reg *registry.Registry, refs map[string]refTarget) {
	if node == nil {
		return
	}
	for _, child := range node.Children {
		if child == nil || child.Definition == nil {
			continue
		}
		def := child.Definition
		if def.Path == "" || !strings.Contains(def.Path, ".") {
			continue
		}
		if def.Min <= 0 {
			continue
		}
		propName := propertyName(def)
		if propName == "" {
			continue
		}
		value := synthesizeNodeValue(child, reg, refs)
		if value == nil {
			continue
		}
		if isRepeatable(def) {
			body[propName] = []any{value}
		} else {
			body[propName] = value
		}
	}
}

// synthesizeNodeValue produces a sample value for an element node, recursing
// into complex datatypes and resolving relationship references.
func synthesizeNodeValue(node *model.ElementNode, reg *registry.Registry, refs map[string]refTarget) any {
	if node == nil || node.Definition == nil {
		return nil
	}
	def := node.Definition
	switch primaryTypeCode(def) {
	case "string", "markdown", "id", "code":
		return "sample-" + strings.ReplaceAll(leafName(def.Path), ".", "-")
	case "uri", "url", "canonical", "oid", "uuid":
		return "http://example.org/fhir/" + leafName(def.Path)
	case "boolean":
		return true
	case "integer", "unsignedInt", "positiveInt":
		return 1
	case "decimal":
		return 1.0
	case "date":
		return "2024-01-01"
	case "dateTime", "instant":
		return "2024-01-01T00:00:00Z"
	case "time":
		return "00:00:00"
	case "Identifier":
		value := map[string]any{}
		populateChildren(value, node, reg, refs)
		if _, ok := value["value"]; !ok {
			value["value"] = "sample-identifier"
		}
		return value
	case "HumanName":
		value := map[string]any{"family": "Sample", "given": []any{"Momus"}}
		populateChildren(value, node, reg, refs)
		return value
	case "CodeableConcept":
		return map[string]any{
			"coding": []any{map[string]any{"system": "http://example.org/fhir/code-system", "code": "sample-code"}},
		}
	case "Coding":
		return map[string]any{"system": "http://example.org/fhir/code-system", "code": "sample-code"}
	case "Quantity":
		value := map[string]any{"value": 1.0, "unit": "sample", "system": "http://unitsofmeasure.org", "code": "1"}
		populateChildren(value, node, reg, refs)
		return value
	case "Reference":
		if ref, ok := refs[node.Path]; ok {
			return map[string]any{"reference": ref.resourceType + "/" + ref.localID}
		}
		return map[string]any{"reference": "Patient/unknown"}
	default:
		value := map[string]any{}
		populateChildren(value, node, reg, refs)
		if len(value) == 0 {
			return nil
		}
		return value
	}
}

// propertyName maps an element to its JSON property name, handling choice
// elements (value[x] becomes valueString etc).
func propertyName(def *model.ElementDefinition) string {
	leaf := leafName(def.Path)
	leaf = strings.TrimSuffix(leaf, "[x]")
	if len(def.Types) != 1 {
		if tc := def.Types[0].Code; tc != "" {
			return leaf + upperFirst(tc)
		}
	}
	return leaf
}

func leafName(path string) string {
	if idx := strings.LastIndex(path, "."); idx >= 0 && idx+1 < len(path) {
		return path[idx+1:]
	}
	return path
}

func primaryTypeCode(def *model.ElementDefinition) string {
	if def == nil || len(def.Types) == 0 {
		return ""
	}
	return def.Types[0].Code
}

func isRepeatable(def *model.ElementDefinition) bool {
	if def == nil {
		return false
	}
	if def.Max == "*" {
		return true
	}
	n, err := strconv.Atoi(def.Max)
	return err == nil && n > 1
}

func upperFirst(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
