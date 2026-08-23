package validate

import (
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
)

func TestMatchesDatatype(t *testing.T) {
	cases := []struct {
		name  string
		val   any
		types []model.ElementType
		want  bool
	}{
		{"string match", "abc", []model.ElementType{{Code: "string"}}, true},
		{"string mismatch", float64(5), []model.ElementType{{Code: "string"}}, false},
		{"integer match", float64(5), []model.ElementType{{Code: "integer"}}, true},
		{"integer mismatch", "5", []model.ElementType{{Code: "integer"}}, false},
		{"boolean match", true, []model.ElementType{{Code: "boolean"}}, true},
		{"date match", "2024-01-01", []model.ElementType{{Code: "date"}}, true},
		{"date mismatch", "not-a-date", []model.ElementType{{Code: "date"}}, false},
		{"Reference match", map[string]any{}, []model.ElementType{{Code: "Reference"}}, true},
		{"unknown code accepts", map[string]any{}, []model.ElementType{{Code: "BackboneElement"}}, true},
		{"any matching type", "x", []model.ElementType{{Code: "integer"}, {Code: "string"}}, true},
		{"no matching known type", 5, []model.ElementType{{Code: "string"}}, false},
		{"empty types unknown", 5, nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := matchesDatatype(c.val, c.types); got != c.want {
				t.Fatalf("matchesDatatype(%v, %v) = %v, want %v", c.val, c.types, got, c.want)
			}
		})
	}
}

func TestIsKnownDatatype(t *testing.T) {
	known := []string{"string", "code", "id", "uri", "url", "canonical", "oid", "uuid",
		"integer", "integer64", "positiveInt", "unsignedInt", "decimal",
		"boolean", "date", "dateTime", "instant", "time", "base64Binary", "markdown", "xhtml",
		"Reference", "Coding", "CodeableConcept", "Quantity", "HumanName", "Address",
		"Identifier", "ContactPoint", "Period", "Extension", "Ratio", "Range",
		"Annotation", "Timing", "Age", "Count", "Distance", "Duration", "Money",
		"SampledData", "Meta", "Signature"}
	for _, code := range known {
		if !isKnownDatatype(code) {
			t.Errorf("isKnownDatatype(%q) = false, want true", code)
		}
	}
	unknown := []string{"BackboneElement", "Resource", "unknown", ""}
	for _, code := range unknown {
		if isKnownDatatype(code) {
			t.Errorf("isKnownDatatype(%q) = true, want false", code)
		}
	}
}

func TestJSONTypeMatches(t *testing.T) {
	cases := []struct {
		name string
		val  any
		code string
		want bool
	}{
		{"string code string val", "x", "string", true},
		{"string code non-string", 5, "string", false},
		{"code", "x", "code", true},
		{"uri", "http://x", "uri", true},
		{"integer float", float64(5), "integer", true},
		{"integer int", 5, "integer", true},
		{"integer int64", int64(5), "integer", true},
		{"integer string", "5", "integer", false},
		{"decimal", float64(1.5), "decimal", true},
		{"boolean bool", true, "boolean", true},
		{"boolean non-bool", 1, "boolean", false},
		{"date valid", "2024-01-01", "date", true},
		{"date invalid", "x", "date", false},
		{"dateTime valid", "2024-01-01T10:00:00", "dateTime", true},
		{"instant fractional", "2024-01-01T10:00:00.123Z", "instant", true},
		{"time valid", "10:00:00", "time", true},
		{"Reference map", map[string]any{}, "Reference", true},
		{"Reference string", "x", "Reference", false},
		{"CodeableConcept map", map[string]any{}, "CodeableConcept", true},
		{"complex unknown type accepts", map[string]any{}, "BackboneElement", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := jsonTypeMatches(c.val, c.code); got != c.want {
				t.Fatalf("jsonTypeMatches(%v, %q) = %v, want %v", c.val, c.code, got, c.want)
			}
		})
	}
}

func TestValidDateShape(t *testing.T) {
	valid := []string{
		"2024", "2024-01", "2024-01-01", "10:00:00",
		"2024-01-01T10:00:00",
		"2024-01-01T10:00:00.123Z",
	}
	for _, s := range valid {
		if !validDateShape(s) {
			t.Errorf("validDateShape(%q) = false, want true", s)
		}
	}
	invalid := []string{"", "not-a-date", "2024/01/01", "10:00", "2024-01-01T"}
	for _, s := range invalid {
		if validDateShape(s) {
			t.Errorf("validDateShape(%q) = true, want false", s)
		}
	}
}

func TestNormalizeTypeName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"string", "string"},
		{"  string  ", "string"},
		{"", ""},
		{"valueString", "valueString"},
	}
	for _, c := range cases {
		if got := normalizeTypeName(c.in); got != c.want {
			t.Errorf("normalizeTypeName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
