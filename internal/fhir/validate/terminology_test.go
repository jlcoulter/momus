package validate

import (
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

func buildTerminologyRegistry() *registry.Registry {
	r := registry.New()
	r.AddValueSet(&model.ValueSet{
		URL: "http://example.org/ValueSet/gender",
		ComposeIncludes: []model.ValueSetInclude{{
			System:   "http://hl7.org/fhir/administrative-gender",
			Concepts: []model.ConceptReference{{Code: "male"}, {Code: "female"}},
		}},
		ExpansionContains: []model.ValueSetExpansionContains{{Code: "other"}},
	})
	return r
}

func TestExtractCodes(t *testing.T) {
	cases := []struct {
		name string
		val  any
		want []codeRef
	}{
		{"bare string", "male", []codeRef{{Code: "male"}}},
		{"empty string", "", nil},
		{"coding map", map[string]any{"system": "http://x", "code": "c1"}, []codeRef{{System: "http://x", Code: "c1"}}},
		{"coding map empty code", map[string]any{"system": "http://x", "code": ""}, nil},
		{"codeable concept", map[string]any{"coding": []any{map[string]any{"system": "http://x", "code": "c1"}}}, []codeRef{{System: "http://x", Code: "c1"}}},
		{"array", []any{"a", "b"}, []codeRef{{Code: "a"}, {Code: "b"}}},
		{"nested array of codings", []any{map[string]any{"coding": []any{map[string]any{"code": "c"}}}}, []codeRef{{Code: "c"}}},
		{"unrelated value", float64(5), nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := extractCodes(c.val)
			if len(got) != len(c.want) {
				t.Fatalf("extractCodes(%v) = %v, want %v", c.val, got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("extractCodes(%v)[%d] = %v, want %v", c.val, i, got[i], c.want[i])
				}
			}
		})
	}
}

func TestValueSetContains(t *testing.T) {
	r := registry.New()
	r.AddValueSet(&model.ValueSet{
		URL: "http://example.org/ValueSet/gender",
		ComposeIncludes: []model.ValueSetInclude{{
			System:   "http://hl7.org/fhir/administrative-gender",
			Concepts: []model.ConceptReference{{Code: "male"}, {Code: "female"}},
		}},
		ExpansionContains: []model.ValueSetExpansionContains{{Code: "other"}},
	})
	vsp, _ := r.ValueSet("http://example.org/ValueSet/gender")

	cases := []struct {
		name         string
		system, code string
		want         bool
	}{
		{"matching code and system", "http://hl7.org/fhir/administrative-gender", "male", true},
		{"mismatched system", "http://other", "male", false},
		{"bare code", "", "male", true},
		{"expansion contains", "", "other", true},
		{"unknown code", "", "zzz", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := valueSetContains(vsp, c.system, c.code); got != c.want {
				t.Fatalf("valueSetContains(%q, %q) = %v, want %v", c.system, c.code, got, c.want)
			}
		})
	}
}

func TestIsMemberOf(t *testing.T) {
	r := buildTerminologyRegistry()
	v := New(r)

	// Resolvable value set, valid code.
	if !v.isMemberOf("http://example.org/ValueSet/gender", "male") {
		t.Fatal("isMemberOf valid code = false, want true")
	}
	// Resolvable value set, invalid code.
	if v.isMemberOf("http://example.org/ValueSet/gender", "zzz") {
		t.Fatal("isMemberOf invalid code = true, want false")
	}
	// Unresolvable value set must not over-reject.
	if !v.isMemberOf("http://example.org/ValueSet/missing", "male") {
		t.Fatal("isMemberOf unresolvable value set = false, want true")
	}
}
