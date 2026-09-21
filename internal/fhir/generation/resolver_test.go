package generation

import (
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"

	fhir "github.com/jlcoulter/fhir-registry"
)

func TestElementCodingChild(t *testing.T) {
	// Element with a coding child whose path matches.
	elem := &fhir.ElementDefinition{
		Path: "CodeableConcept",
		Children: []*fhir.ElementDefinition{
			{ID: "CodeableConcept.coding", Path: "CodeableConcept.coding"},
		},
	}
	child, ok := elementCodingChild(elem)
	if !ok || child == nil || child.Path != "CodeableConcept.coding" {
		t.Fatalf("elementCodingChild = %v, %v; want coding child", child, ok)
	}

	// Element with no matching child.
	elem2 := &fhir.ElementDefinition{Path: "Something", Children: []*fhir.ElementDefinition{{Path: "Something.other"}}}
	if _, ok := elementCodingChild(elem2); ok {
		t.Fatal("expected no coding child for unmatched paths")
	}

	// Nil / no children.
	if _, ok := elementCodingChild(nil); ok {
		t.Fatal("expected no coding child for nil element")
	}
	if _, ok := elementCodingChild(&fhir.ElementDefinition{Path: "X"}); ok {
		t.Fatal("expected no coding child when Children is nil")
	}
}

func TestResolveDisplay(t *testing.T) {
	r := &registryDisplayResolver{reg: nil}
	if _, ok := r.ResolveDisplay("http://cs", "C"); ok {
		t.Fatal("ResolveDisplay with nil registry should fail")
	}

	reg := registry.New()
	reg.AddCodeSystem(&model.CodeSystem{URL: "http://cs", Concepts: []model.CodeSystemConcept{{Code: "C", Display: "Canonical"}}})
	r = &registryDisplayResolver{reg: reg}
	if got, ok := r.ResolveDisplay("http://cs", "C"); !ok || got != "Canonical" {
		t.Fatalf("ResolveDisplay = %q, %v; want Canonical", got, ok)
	}
	if _, ok := r.ResolveDisplay("http://cs", "missing"); ok {
		t.Fatal("ResolveDisplay for unknown code should fail")
	}
}

func TestResolveBindingFromValueSet(t *testing.T) {
	reg := registry.New()
	reg.AddValueSet(&model.ValueSet{
		URL: "http://example.org/ValueSet/gender",
		Compose: &model.ValueSetCompose{Include: []model.ValueSetInclude{{
			System:  "http://hl7.org/fhir/administrative-gender",
			Concept: []model.ConceptReference{{Code: "male"}},
		}}},
	})
	r := &registryBindingResolver{reg: reg}

	elem := &fhir.ElementDefinition{
		Path:    "Patient.gender",
		Binding: &fhir.Binding{Strength: "required", ValueSet: "http://example.org/ValueSet/gender"},
	}
	got, ok := r.ResolveBinding(elem, nil)
	if !ok || got.Code != "male" || got.System != "http://hl7.org/fhir/administrative-gender" {
		t.Fatalf("ResolveBinding = %+v, %v; want male", got, ok)
	}

	// Nil resolver / nil element.
	if _, ok := (&registryBindingResolver{reg: nil}).ResolveBinding(elem, nil); ok {
		t.Fatal("ResolveBinding with nil registry should fail")
	}
	if _, ok := r.ResolveBinding(nil, nil); ok {
		t.Fatal("ResolveBinding with nil element should fail")
	}
}

func TestResolveBindingFromCodingChild(t *testing.T) {
	reg := registry.New()
	reg.AddValueSet(&model.ValueSet{
		URL: "http://example.org/ValueSet/code",
		Compose: &model.ValueSetCompose{Include: []model.ValueSetInclude{{
			System:  "http://cs",
			Concept: []model.ConceptReference{{Code: "X"}},
		}}},
	})
	// The binding lives on the coding child, not the concept itself.
	concept := &fhir.ElementDefinition{Path: "Extension.value[x]"}
	concept.Children = []*fhir.ElementDefinition{{
		Path:    "Extension.value[x].coding",
		Binding: &fhir.Binding{Strength: "required", ValueSet: "http://example.org/ValueSet/code"},
	}}

	r := &registryBindingResolver{reg: reg}
	got, ok := r.ResolveBinding(concept, nil)
	if !ok || got.Code != "X" {
		t.Fatalf("ResolveBinding(coding child) = %+v, %v; want X", got, ok)
	}
}

func TestFixedIdentifierSystem(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/au-hpii",
		Type: "Identifier",
		Elements: []model.ElementDefinition{
			{Path: "Identifier", Min: 0, Max: fhir.MaxUnbounded},
			{Path: "Identifier.system", Min: 1, Max: 1, Types: []model.ElementType{{Code: "uri"}}, Fixed: "http://ns.electronichealth.net.au/id/hi/hpii/1.0"},
		},
	})

	if got := fixedIdentifierSystem("http://example.org/au-hpii", reg); got != "http://ns.electronichealth.net.au/id/hi/hpii/1.0" {
		t.Fatalf("fixedIdentifierSystem = %q", got)
	}
	// Unresolvable profile -> "".
	if got := fixedIdentifierSystem("http://example.org/missing", reg); got != "" {
		t.Fatalf("fixedIdentifierSystem(missing) = %q", got)
	}
	// Nil guards.
	if got := fixedIdentifierSystem("", reg); got != "" {
		t.Fatalf("fixedIdentifierSystem(empty) = %q", got)
	}
	if got := fixedIdentifierSystem("http://example.org/au-hpii", nil); got != "" {
		t.Fatalf("fixedIdentifierSystem(nil reg) = %q", got)
	}
}

func TestValidIdentifierSearchValue(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/au-hpii",
		Type: "Identifier",
		Elements: []model.ElementDefinition{
			{Path: "Identifier", Min: 0, Max: fhir.MaxUnbounded},
			{Path: "Identifier.system", Min: 1, Max: 1, Types: []model.ElementType{{Code: "uri"}}, Fixed: "http://ns.electronichealth.net.au/id/hi/hpii/1.0"},
		},
	})

	def := &model.ElementDefinition{
		Path:  "Organization.identifier",
		Types: []model.ElementType{{Profiles: []string{"http://example.org/au-hpii"}}},
	}
	got := validIdentifierSearchValue(def, reg)
	if got == "" || len(got) != 16 {
		t.Fatalf("validIdentifierSearchValue = %q, want a 16-digit HPI-I", got)
	}

	// A profile that does not fix a known system -> "".
	def2 := &model.ElementDefinition{Path: "X.id", Types: []model.ElementType{{Profiles: []string{"http://example.org/missing"}}}}
	if got := validIdentifierSearchValue(def2, reg); got != "" {
		t.Fatalf("validIdentifierSearchValue(missing) = %q", got)
	}
	// Nil guards.
	if got := validIdentifierSearchValue(nil, reg); got != "" {
		t.Fatalf("validIdentifierSearchValue(nil) = %q", got)
	}
	if got := validIdentifierSearchValue(def, nil); got != "" {
		t.Fatalf("validIdentifierSearchValue(nil reg) = %q", got)
	}
}
