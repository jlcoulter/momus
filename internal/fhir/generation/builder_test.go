package generation

import (
	"testing"

	"github.com/jlcoulter/momus/internal/core/coverage"
	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

func buildBuilderRegistry() *registry.Registry {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/patient",
		Type: "Patient",
		Elements: []model.ElementDefinition{
			{Path: "Patient", Min: 0, Max: "*"},
			{Path: "Patient.active", Min: 0, Max: "1", Types: []model.ElementType{{Code: "boolean"}}},
			{Path: "Patient.gender", Min: 0, Max: "1", Types: []model.ElementType{{Code: "code"}}, Binding: &model.Binding{Strength: "required", ValueSet: "http://example.org/ValueSet/gender"}},
			{Path: "Patient.birthDate", Min: 0, Max: "1", Types: []model.ElementType{{Code: "date"}}},
			{Path: "Patient.name", Min: 0, Max: "*", Types: []model.ElementType{{Code: "string"}}},
			{Path: "Patient.photo", Min: 0, Max: "1", Types: []model.ElementType{{Code: "Attachment"}}},
		},
	})
	reg.AddValueSet(&model.ValueSet{
		URL: "http://example.org/ValueSet/gender",
		ComposeIncludes: []model.ValueSetInclude{{
			System:   "http://hl7.org/fhir/administrative-gender",
			Concepts: []model.ConceptReference{{Code: "male"}},
		}},
	})
	reg.AddSearchParameter(&model.SearchParameter{Code: "active", Base: []string{"Patient"}, Type: "boolean", Expression: "Patient.active"})
	reg.AddSearchParameter(&model.SearchParameter{Code: "gender", Base: []string{"Patient"}, Type: "token", Expression: "Patient.gender"})
	reg.AddSearchParameter(&model.SearchParameter{Code: "birthdate", Base: []string{"Patient"}, Type: "date", Expression: "Patient.birthDate"})
	reg.AddSearchParameter(&model.SearchParameter{Code: "name", Base: []string{"Patient"}, Type: "string", Expression: "Patient.name"})
	reg.AddSearchParameter(&model.SearchParameter{Code: "near", Base: []string{"Patient"}, Type: "special", Expression: "position.longitude | position.latitude"})
	reg.AddSearchParameter(&model.SearchParameter{Code: "general-practitioner", Base: []string{"Patient"}, Type: "reference", Expression: "Patient.generalPractitioner"})
	reg.AddSearchParameter(&model.SearchParameter{Code: "code-value-quantity", Base: []string{"Observation"}, Type: "composite", Expression: "Observation.code | Observation.value"})
	return reg
}

func TestSearchInvalidValue(t *testing.T) {
	reg := buildBuilderRegistry()
	b := NewBuilder(reg, false)

	cases := []struct {
		code       string
		wantReject bool
		wantValue  string
	}{
		{"active", true, "notabool"},         // boolean
		{"birthdate", true, "not-a-date"},    // date
		{"name", false, "momus-invalid-zzz"}, // string -> valid non-match
	}
	for _, c := range cases {
		req := coverage.CoverageRequirement{ID: "r", ResourceType: "Patient", SearchCode: c.code}
		val, reject := b.SearchInvalidValue(req, c.code)
		if reject != c.wantReject {
			t.Errorf("%s: reject = %v, want %v", c.code, reject, c.wantReject)
		}
		if val != c.wantValue {
			t.Errorf("%s: value = %q, want %q", c.code, val, c.wantValue)
		}
	}

	// A parameter absent from the registry defaults to a non-matching string.
	req := coverage.CoverageRequirement{ID: "r", ResourceType: "Patient"}
	val, reject := b.SearchInvalidValue(req, "unknown")
	if reject || val != "momus-invalid-zzz" {
		t.Errorf("unknown param: reject=%v val=%q, want false and mus-invalid-zzz", reject, val)
	}
}

func TestSearchAcceptValue(t *testing.T) {
	reg := buildBuilderRegistry()
	b := NewBuilder(reg, false)

	cases := []struct {
		code string
		want string
	}{
		{"active", "true"},
		{"birthdate", "2024-01-01"},
		{"gender", "male"}, // bound coding resolves to the value set's code
		{"near", "-33.8688|151.2093"},
	}
	for _, c := range cases {
		req := coverage.CoverageRequirement{ID: "r", ResourceType: "Patient", Domain: coverage.CoverageDomainSearch, Variant: coverage.CoverageVariantSearchValid, SearchCode: c.code}
		if got := b.SearchAcceptValue(req, c.code); got != c.want {
			t.Errorf("SearchAcceptValue(%s) = %q, want %q", c.code, got, c.want)
		}
	}
}

func TestSearchAcceptValueFallbacks(t *testing.T) {
	reg := buildBuilderRegistry()
	b := NewBuilder(reg, false)
	req := coverage.CoverageRequirement{ID: "r", ResourceType: "Patient", Domain: coverage.CoverageDomainSearch, Variant: coverage.CoverageVariantSearchValid, SearchCode: "_id"}

	// _id is excluded -> returns the default.
	if got := b.SearchAcceptValue(req, "_id"); got != "momus-search" {
		t.Fatalf("_id value = %q, want mus-invalid-zzz", got)
	}

	// A code with no registered search parameter.
	if got := b.SearchAcceptValue(req, "unknown"); got != "momus-search" {
		t.Fatalf("unknown value = %q, want mus-invalid-zzz", got)
	}

	// A nil-registry builder.
	nilBuilder := NewBuilder(nil, false)
	if got := nilBuilder.SearchAcceptValue(req, "name"); got != "momus-search" {
		t.Fatalf("nil registry value = %q, want mus-invalid-zzz", got)
	}
}

func TestCompositeAcceptValue(t *testing.T) {
	reg := buildBuilderRegistry()
	b := NewBuilder(reg, false)
	req := coverage.CoverageRequirement{ID: "r", ResourceType: "Observation", Domain: coverage.CoverageDomainSearch, Variant: coverage.CoverageVariantSearchValid, SearchCode: "code-value-quantity"}

	got := b.SearchAcceptValue(req, "code-value-quantity")
	if got == "" {
		t.Fatal("composite accept value is empty")
	}
}

func TestElementSearchValue(t *testing.T) {
	reg := buildBuilderRegistry()
	// A boolean element.
	def := &model.ElementDefinition{Path: "Patient.active", Types: []model.ElementType{{Code: "boolean"}}}
	if got := elementSearchValue(def, reg); got != "true" {
		t.Fatalf("boolean elementSearchValue = %q, want true", got)
	}
	// A date element.
	def = &model.ElementDefinition{Path: "Patient.birthDate", Types: []model.ElementType{{Code: "date"}}}
	if got := elementSearchValue(def, reg); got != "2024-01-01" {
		t.Fatalf("date elementSearchValue = %q, want 2024-01-01", got)
	}
	// A Quantity element.
	def = &model.ElementDefinition{Path: "Observation.value", Types: []model.ElementType{{Code: "Quantity"}}}
	if got := elementSearchValue(def, reg); got != "123.45|http://unitsofmeasure.org|mmol" {
		t.Fatalf("Quantity elementSearchValue = %q", got)
	}
	// An unknown type falls back.
	def = &model.ElementDefinition{Path: "x.y", Types: []model.ElementType{{Code: "Attachment"}}}
	if got := elementSearchValue(def, reg); got != "momus-search" {
		t.Fatalf("unknown elementSearchValue = %q", got)
	}
}

func TestSearchValidNonMatchValue(t *testing.T) {
	cases := []struct {
		paramType string
		want      string
	}{
		{"token", "momus|nomatch"},
		{"reference", "Patient/momus-nomatch"},
		{"uri", "http://example.org/momus-nomatch"},
		{"quantity", "999|http://example.org/sys|nomatch"},
		{"date", "1900-01-01"},
		{"number", "123456.789"},
		{"boolean", "true"},
		{"special", "90.0|0.0"},
		{"composite", "momus-nomatch$momus-nomatch"},
		{"string", "momus-invalid-zzz"},
		{"unknown", "momus-invalid-zzz"},
	}
	for _, c := range cases {
		if got := searchValidNonMatchValue(c.paramType); got != c.want {
			t.Errorf("searchValidNonMatchValue(%q) = %q, want %q", c.paramType, got, c.want)
		}
	}
}

func TestBuilderSearchParamType(t *testing.T) {
	reg := buildBuilderRegistry()
	b := NewBuilder(reg, false)
	req := coverage.CoverageRequirement{ID: "r", ResourceType: "Patient"}
	if got := b.SearchParamType(req, "active"); got != "boolean" {
		t.Fatalf("SearchParamType(active) = %q, want boolean", got)
	}
	if got := b.SearchParamType(req, "unknown"); got != "" {
		t.Fatalf("SearchParamType(unknown) = %q, want empty", got)
	}
	nilBuilder := NewBuilder(nil, false)
	if got := nilBuilder.SearchParamType(req, "active"); got != "" {
		t.Fatalf("nil registry SearchParamType = %q, want empty", got)
	}
}
