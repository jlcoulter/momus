package registry

import (
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"

	fhir "github.com/jlcoulter/fhir-registry"
)

func TestIsAbstractType(t *testing.T) {
	r := New()
	// Abstract: kind "resource" with abstract flag.
	r.AddStructureDefinition(&model.StructureDefinition{URL: "http://hl7.org/fhir/StructureDefinition/Parameters", Type: "Parameters", Kind: "resource", Abstract: true})
	// Concrete resource.
	r.AddStructureDefinition(&model.StructureDefinition{URL: "http://hl7.org/fhir/StructureDefinition/Patient", Type: "Patient", Kind: "resource"})

	cases := []struct {
		typ  string
		want bool
	}{
		{"Parameters", true},
		{"Patient", false},
		{"UnknownType", false}, // not indexed -> concrete
		{"", true},             // empty type is treated as abstract
	}
	for _, c := range cases {
		if got := r.IsAbstractType(c.typ); got != c.want {
			t.Errorf("IsAbstractType(%q) = %v, want %v", c.typ, got, c.want)
		}
	}
}

func TestFirstConceptAndResolveBoundCoding(t *testing.T) {
	r := New()
	cs := &model.CodeSystem{
		URL: "http://example.org/CodeSystem/cs",
		Concepts: []model.CodeSystemConcept{
			{Code: "A", Display: "Alpha"},
		},
	}
	r.AddCodeSystem(cs)

	// ValueSet with explicit compose concepts.
	vs := &model.ValueSet{
		URL: "http://example.org/ValueSet/vs",
		Compose: &model.ValueSetCompose{
			Include: []model.ValueSetInclude{
				{System: "http://example.org/CodeSystem/cs", Concept: []model.ConceptReference{{Code: "A", Display: "Alpha"}}},
			},
		},
	}
	r.AddValueSet(vs)

	got, ok := r.FirstConcept(vs)
	if !ok || got.Code != "A" || got.System != "http://example.org/CodeSystem/cs" {
		t.Fatalf("FirstConcept = %+v, %v; want A from compose", got, ok)
	}

	coding, ok := r.ResolveBoundCoding("http://example.org/ValueSet/vs")
	if !ok || coding.Code != "A" {
		t.Fatalf("ResolveBoundCoding = %+v, %v; want A", coding, ok)
	}

	// Unknown value set -> no coding.
	if _, ok := r.ResolveBoundCoding("http://example.org/missing"); ok {
		t.Fatal("ResolveBoundCoding should fail for unknown value set")
	}
}

func TestCodingDisplay(t *testing.T) {
	r := New()
	r.AddCodeSystem(&model.CodeSystem{
		URL: "http://example.org/CodeSystem/cs",
		Concepts: []model.CodeSystemConcept{
			{Code: "A", Display: "Alpha", Concepts: []model.CodeSystemConcept{{Code: "A1", Display: "Alpha One"}}},
		},
	})

	if got := r.CodingDisplay("http://example.org/CodeSystem/cs", "A1"); got != "Alpha One" {
		t.Fatalf("CodingDisplay(A1) = %q, want Alpha One", got)
	}
	if got := r.CodingDisplay("http://example.org/CodeSystem/cs", "missing"); got != "" {
		t.Fatalf("CodingDisplay(missing) = %q, want empty", got)
	}
	if got := r.CodingDisplay("", "A"); got != "" {
		t.Fatalf("CodingDisplay(empty system) = %q, want empty", got)
	}
}

func TestFirstExampleCoding(t *testing.T) {
	r := New()
	r.AddResource(&model.Resource{
		ResourceType: "Observation",
		ProfileURLs:  []string{"http://example.org/StructureDefinition/obs"},
		Raw: map[string]any{
			"resourceType": "Observation",
			"code": map[string]any{
				"coding": []any{map[string]any{"system": "http://loinc.org", "code": "8302-2", "display": "Body height"}},
			},
		},
	})

	got, ok := r.FirstExampleCoding("Observation", "code", "http://example.org/StructureDefinition/obs")
	if !ok || got.Code != "8302-2" || got.Display != "Body height" {
		t.Fatalf("FirstExampleCoding = %+v, %v; want 8302-2", got, ok)
	}
	// No resources for a type -> not found.
	if _, ok := r.FirstExampleCoding("Patient", "name", ""); ok {
		t.Fatal("FirstExampleCoding should fail for a type with no resources")
	}
}

func TestExampleCodingForExtension(t *testing.T) {
	r := New()
	r.AddResource(&model.Resource{
		ResourceType: "Patient",
		Raw: map[string]any{
			"resourceType": "Patient",
			"extension": []any{
				map[string]any{
					"url":         "http://example.org/ext/flag",
					"valueCoding": map[string]any{"system": "http://example.org/cs", "code": "X", "display": "Ex"},
				},
			},
		},
	})

	got, ok := r.ExampleCodingForExtension("http://example.org/ext/flag")
	if !ok || got.Code != "X" {
		t.Fatalf("ExampleCodingForExtension = %+v, %v; want X", got, ok)
	}
	if _, ok := r.ExampleCodingForExtension("http://example.org/missing"); ok {
		t.Fatal("ExampleCodingForExtension should fail for an unknown extension")
	}
}

func TestIsPlaceholderURL(t *testing.T) {
	r := New()
	if !r.IsPlaceholderURL("http://example.org/fhir/StructureDefinition/foo") {
		t.Fatal("expected example.org to be a placeholder URL")
	}
	if r.IsPlaceholderURL("http://hl7.org/fhir/StructureDefinition/Patient") {
		t.Fatal("did not expect hl7.org to be a placeholder URL")
	}
}

func TestFromFhirNil(t *testing.T) {
	if got := fromFhir(nil); got != nil {
		t.Fatalf("fromFhir(nil) = %v, want nil", got)
	}
}

func TestStripVersion(t *testing.T) {
	if got := stripVersion("http://x/Patient|4.0.1"); got != "http://x/Patient" {
		t.Fatalf("stripVersion(with version) = %q", got)
	}
	if got := stripVersion("http://x/Patient"); got != "http://x/Patient" {
		t.Fatalf("stripVersion(no version) = %q", got)
	}
	if got := stripVersion(""); got != "" {
		t.Fatalf("stripVersion(empty) = %q", got)
	}
}

func TestDerivationDepth(t *testing.T) {
	r := New()
	base := &model.StructureDefinition{
		URL:  "http://hl7.org/fhir/StructureDefinition/Resource",
		Type: "Resource",
	}
	parent := &model.StructureDefinition{
		URL:            "http://hl7.org/fhir/StructureDefinition/DomainResource",
		Type:           "DomainResource",
		BaseDefinition: base.URL,
	}
	child := &model.StructureDefinition{
		URL:            "http://example.org/StructureDefinition/patient",
		Type:           "Patient",
		BaseDefinition: parent.URL + "|4.0.1",
	}
	for _, sd := range []*model.StructureDefinition{base, parent, child} {
		r.AddStructureDefinition(sd)
	}

	if got := derivationDepth(r, child); got != 2 {
		t.Fatalf("derivationDepth(child) = %d, want 2", got)
	}
	if got := derivationDepth(r, parent); got != 1 {
		t.Fatalf("derivationDepth(parent) = %d, want 1", got)
	}
	if got := derivationDepth(r, base); got != 0 {
		t.Fatalf("derivationDepth(base) = %d, want 0", got)
	}
	if got := derivationDepth(r, nil); got != 0 {
		t.Fatalf("derivationDepth(nil) = %d, want 0", got)
	}
}

func TestFromFhirRoundTrip(t *testing.T) {
	f := fhir.NewStructureDefinition(
		"http://example.org/StructureDefinition/patient",
		"PatientProfile", "Patient", "resource", "",
		"",
		[]fhir.ElementDefinition{{ID: "Patient", Path: "Patient", Min: 1, Max: fhir.Max(1)}},
	)
	sd := fromFhir(f)
	if sd == nil {
		t.Fatal("fromFhir returned nil")
	}
	if sd.URL != "http://example.org/StructureDefinition/patient" || sd.Name != "PatientProfile" || sd.Type != "Patient" {
		t.Fatalf("fromFhir identity mismatch: %+v", sd)
	}
	if len(sd.Elements) != 1 || sd.Elements[0].Path != "Patient" {
		t.Fatalf("fromFhir elements = %+v, want one Patient element", sd.Elements)
	}
}
