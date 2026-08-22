package constraintderive

import (
	"reflect"
	"strings"
	"testing"

	"github.com/jlcoulter/momus/internal/core/constraint"
	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

const profileURL = "http://example.org/StructureDefinition/observation"

func testRegistry() *registry.Registry {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{
		URL:  profileURL,
		Type: "Observation",
		Name: "ObservationProfile",
		Elements: []model.ElementDefinition{
			{Path: "Observation", Min: 0, Max: "*"},
			{
				Path:  "Observation.status",
				Name:  "status",
				Min:   1,
				Max:   "1",
				Types: []model.ElementType{{Code: "code"}},
				Binding: &model.Binding{
					Strength: "required",
					ValueSet: "http://hl7.org/fhir/ValueSet/observation-status",
				},
				Constraints: []model.ElementConstraint{{
					Key:        "obs-1",
					Severity:   "error",
					Expression: "status.exists()",
					Human:      "status is required",
				}},
			},
			{
				Path:    "Observation.birthDate",
				Name:    "birthDate",
				Min:     0,
				Max:     "1",
				Types:   []model.ElementType{{Code: "date"}},
				Pattern: "2024-01-01",
			},
			{
				Path:          "Observation.subject",
				Name:          "subject",
				Min:           0,
				Max:           "1",
				Types:         []model.ElementType{{Code: "Reference"}},
				TargetProfile: []string{"http://example.org/StructureDefinition/patient"},
			},
			{
				Path:  "Observation.code",
				Name:  "code",
				Min:   1,
				Max:   "*",
				Types: []model.ElementType{{Code: "CodeableConcept"}},
				Fixed: "value-placeholder",
			},
		},
	})
	reg.AddSearchParameter(&model.SearchParameter{
		URL:        "http://hl7.org/fhir/SearchParameter/Observation-code",
		Name:       "code",
		Code:       "code",
		Base:       []string{"Observation"},
		Type:       "token",
		Expression: "Observation.code",
	})
	reg.AddCapabilityStatement(&model.CapabilityStatement{
		URL: "http://example.org/CapabilityStatement/server",
		Rest: []model.CapabilityStatementRest{{
			Mode: "server",
			Resource: []model.CapabilityStatementRestResource{{
				Type: "Observation",
				Interaction: []model.CapabilityStatementInteraction{
					{Code: "create"},
					{Code: "read"},
				},
				Operation: []model.CapabilityStatementOperation{
					{Name: "$everything", Definition: "http://example.org/OperationDefinition/Observation-everything"},
				},
			}},
		}},
	})
	return reg
}

func requireConstraint(t *testing.T, constraints []constraint.Constraint, kind constraint.Kind, id string) constraint.Constraint {
	t.Helper()
	for _, c := range constraints {
		if c.Kind == kind && c.ID == id {
			return c
		}
	}
	t.Fatalf("expected constraint kind=%s id=%s, got:\n%s", kind, id, formatConstraints(constraints))
	return constraint.Constraint{}
}

func formatConstraints(constraints []constraint.Constraint) string {
	var b strings.Builder
	for _, c := range constraints {
		b.WriteString("  - ")
		b.WriteString(c.ID)
		b.WriteByte('\n')
	}
	return b.String()
}

func TestDeriveElementConstraints(t *testing.T) {
	derived, err := Derive(testRegistry())
	if err != nil {
		t.Fatal(err)
	}

	card := requireConstraint(t, derived, constraint.KindCardinality, constraint.ID(profileURL, "Observation.status", string(constraint.KindCardinality)))
	if card.Min != 1 || card.Max != "1" {
		t.Fatalf("cardinality = %d..%s, want 1..1", card.Min, card.Max)
	}
	if card.ProfileURL != profileURL || card.ResourceType != "Observation" {
		t.Fatalf("unexpected card context: %+v", card)
	}

	requireConstraint(t, derived, constraint.KindDatatype, constraint.ID(profileURL, "Observation.status", string(constraint.KindDatatype), "code"))
	requireConstraint(t, derived, constraint.KindDatatype, constraint.ID(profileURL, "Observation.birthDate", string(constraint.KindDatatype), "date"))

	term := requireConstraint(t, derived, constraint.KindTerminology, constraint.ID(profileURL, "Observation.status", string(constraint.KindTerminology)))
	if term.BindingStrength != "required" || term.ValueSet != "http://hl7.org/fhir/ValueSet/observation-status" {
		t.Fatalf("unexpected terminology constraint: %+v", term)
	}

	inv := requireConstraint(t, derived, constraint.KindInvariant, constraint.ID(profileURL, "Observation.status", string(constraint.KindInvariant), "obs-1"))
	if inv.Expression != "status.exists()" || inv.Severity != "error" {
		t.Fatalf("unexpected invariant constraint: %+v", inv)
	}

	ref := requireConstraint(t, derived, constraint.KindReference, constraint.ID(profileURL, "Observation.subject", string(constraint.KindReference)))
	if len(ref.TargetProfiles) != 1 || ref.TargetProfiles[0] != "http://example.org/StructureDefinition/patient" {
		t.Fatalf("unexpected reference constraint: %+v", ref)
	}

	pat := requireConstraint(t, derived, constraint.KindPattern, constraint.ID(profileURL, "Observation.birthDate", string(constraint.KindPattern)))
	if pat.Value != "2024-01-01" {
		t.Fatalf("unexpected pattern constraint: %+v", pat)
	}

	fixed := requireConstraint(t, derived, constraint.KindFixed, constraint.ID(profileURL, "Observation.code", string(constraint.KindFixed)))
	if fixed.Value != "value-placeholder" {
		t.Fatalf("unexpected fixed constraint: %+v", fixed)
	}
}

func TestDeriveSkipsRootElement(t *testing.T) {
	derived, err := Derive(testRegistry())
	if err != nil {
		t.Fatal(err)
	}
	// The root element "Observation" must not produce a cardinality constraint.
	rootID := constraint.ID(profileURL, "Observation", string(constraint.KindCardinality))
	for _, c := range derived {
		if c.ID == rootID {
			t.Fatalf("root element produced constraint %+v", c)
		}
	}
}

func TestDeriveSearchConstraints(t *testing.T) {
	derived, err := Derive(testRegistry())
	if err != nil {
		t.Fatal(err)
	}
	sp := requireConstraint(t, derived, constraint.KindSearch, constraint.ID("http://hl7.org/fhir/SearchParameter/Observation-code", string(constraint.KindSearch), "Observation", "code"))
	if sp.SearchCode != "code" || sp.SearchType != "token" || sp.ResourceType != "Observation" {
		t.Fatalf("unexpected search constraint: %+v", sp)
	}
	if sp.SearchExpression != "Observation.code" {
		t.Fatalf("unexpected search expression: %+v", sp)
	}
}

func TestDeriveCapabilityConstraints(t *testing.T) {
	derived, err := Derive(testRegistry())
	if err != nil {
		t.Fatal(err)
	}
	requireConstraint(t, derived, constraint.KindInteraction, constraint.ID("http://example.org/CapabilityStatement/server", string(constraint.KindInteraction), "Observation", "create"))
	requireConstraint(t, derived, constraint.KindInteraction, constraint.ID("http://example.org/CapabilityStatement/server", string(constraint.KindInteraction), "Observation", "read"))

	op := requireConstraint(t, derived, constraint.KindOperation, constraint.ID("http://example.org/CapabilityStatement/server", string(constraint.KindOperation), "Observation", "everything"))
	if op.OperationName != "everything" {
		t.Fatalf("operation name = %q, want everything", op.OperationName)
	}
}

func TestDeriveIsDeterministicAndSorted(t *testing.T) {
	first, err := Derive(testRegistry())
	if err != nil {
		t.Fatal(err)
	}
	second, err := Derive(testRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != len(second) {
		t.Fatalf("derivation not stable across runs: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if !reflect.DeepEqual(first[i], second[i]) {
			t.Fatalf("derivation not deterministic at index %d:\n%+v\n%+v", i, first[i], second[i])
		}
		if i > 0 && first[i].ID < first[i-1].ID {
			t.Fatalf("constraints not sorted: %q before %q", first[i-1].ID, first[i].ID)
		}
	}
}

func TestDeriveRequiresRegistry(t *testing.T) {
	if _, err := Derive(nil); err == nil {
		t.Fatal("expected error for nil registry")
	}
}

func TestDeriveScopedRequiresRegistry(t *testing.T) {
	if _, err := DeriveScoped(nil); err == nil {
		t.Fatal("expected error for nil registry")
	}
}

// TestDeriveScopedMergesParentChain verifies that a differential-only subject
// profile inherits its parent's elements/constraints through the registry, and
// that those inherited constraints are attributed to the subject (child) profile
// URL rather than the parent. It also confirms Derive (the unscoped dump) is
// unchanged and still attributes the same constraint to the parent.
func TestDeriveScopedMergesParentChain(t *testing.T) {
	parentURL := "http://example.org/StructureDefinition/base-patient"
	childURL := "http://example.org/StructureDefinition/child-patient"

	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  parentURL,
		Type: "Patient",
		Kind: "resource",
		Elements: []model.ElementDefinition{
			{Path: "Patient", Min: 0, Max: "*"},
			{Path: "Patient.name", Min: 1, Max: "*", Types: []model.ElementType{{Code: "HumanName"}}},
		},
	})
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:            childURL,
		Type:           "Patient",
		Kind:           "resource",
		BaseDefinition: parentURL,
		// Differential-only: no Patient.name and no root Patient element, so the
		// parent must supply them through the registry.
		Elements: []model.ElementDefinition{
			{Path: "Patient.identifier", Min: 1, Max: "1"},
		},
	})

	r.SetScope([]string{childURL})

	derived, err := DeriveScoped(r)
	if err != nil {
		t.Fatal(err)
	}

	// Inherited cardinality attributed to the child (subject) profile.
	inheritedCard := requireConstraint(t, derived, constraint.KindCardinality, constraint.ID(childURL, "Patient.name", string(constraint.KindCardinality)))
	if inheritedCard.Min != 1 || inheritedCard.Max != "*" {
		t.Fatalf("inherited cardinality = %d..%s, want 1..*", inheritedCard.Min, inheritedCard.Max)
	}
	if inheritedCard.ProfileURL != childURL {
		t.Fatalf("inherited cardinality ProfileURL = %q, want child %q", inheritedCard.ProfileURL, childURL)
	}

	// Inherited datatype constraint, also attributed to the child.
	inheritedType := requireConstraint(t, derived, constraint.KindDatatype, constraint.ID(childURL, "Patient.name", string(constraint.KindDatatype), "HumanName"))
	if inheritedType.ResourceType != "Patient" || inheritedType.Datatype != "HumanName" {
		t.Fatalf("unexpected inherited datatype constraint: %+v", inheritedType)
	}

	// The child's own differential element also produces constraints.
	childCard := requireConstraint(t, derived, constraint.KindCardinality, constraint.ID(childURL, "Patient.identifier", string(constraint.KindCardinality)))
	if childCard.Min != 1 || childCard.Max != "1" {
		t.Fatalf("child cardinality = %d..%s, want 1..1", childCard.Min, childCard.Max)
	}

	// The parent must NOT be attributed constraints under the scoped derivation.
	for _, c := range derived {
		if c.ProfileURL == parentURL {
			t.Fatalf("DeriveScoped attributed constraint to out-of-scope parent: %+v", c)
		}
	}

	// Derive (the unscoped dump) must remain unchanged: it still attributes the
	// inherited constraint to the parent profile.
	unscoped, err := Derive(r)
	if err != nil {
		t.Fatal(err)
	}
	parentCard := requireConstraint(t, unscoped, constraint.KindCardinality, constraint.ID(parentURL, "Patient.name", string(constraint.KindCardinality)))
	if parentCard.ProfileURL != parentURL {
		t.Fatalf("Derive parent cardinality ProfileURL = %q, want %q", parentCard.ProfileURL, parentURL)
	}
}

func TestIDDropsEmptyParts(t *testing.T) {
	if got := constraint.ID("", string(constraint.KindSearch), "Observation", "code"); got != "search|Observation|code" {
		t.Fatalf("got %q", got)
	}
	if got := constraint.ID(profileURL, "Observation.status", string(constraint.KindCardinality)); got != profileURL+"|Observation.status|cardinality" {
		t.Fatalf("got %q", got)
	}
}
