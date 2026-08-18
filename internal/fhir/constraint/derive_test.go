package constraint

import (
	"reflect"
	"strings"
	"testing"

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
			}},
		}},
	})
	return reg
}

func requireConstraint(t *testing.T, constraints []Constraint, kind Kind, id string) Constraint {
	t.Helper()
	for _, c := range constraints {
		if c.Kind == kind && c.ID == id {
			return c
		}
	}
	t.Fatalf("expected constraint kind=%s id=%s, got:\n%s", kind, id, formatConstraints(constraints))
	return Constraint{}
}

func formatConstraints(constraints []Constraint) string {
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

	card := requireConstraint(t, derived, KindCardinality, ID(profileURL, "Observation.status", string(KindCardinality)))
	if card.Min != 1 || card.Max != "1" {
		t.Fatalf("cardinality = %d..%s, want 1..1", card.Min, card.Max)
	}
	if card.ProfileURL != profileURL || card.ResourceType != "Observation" {
		t.Fatalf("unexpected card context: %+v", card)
	}

	requireConstraint(t, derived, KindDatatype, ID(profileURL, "Observation.status", string(KindDatatype), "code"))
	requireConstraint(t, derived, KindDatatype, ID(profileURL, "Observation.birthDate", string(KindDatatype), "date"))

	term := requireConstraint(t, derived, KindTerminology, ID(profileURL, "Observation.status", string(KindTerminology)))
	if term.BindingStrength != "required" || term.ValueSet != "http://hl7.org/fhir/ValueSet/observation-status" {
		t.Fatalf("unexpected terminology constraint: %+v", term)
	}

	inv := requireConstraint(t, derived, KindInvariant, ID(profileURL, "Observation.status", string(KindInvariant), "obs-1"))
	if inv.Expression != "status.exists()" || inv.Severity != "error" {
		t.Fatalf("unexpected invariant constraint: %+v", inv)
	}

	ref := requireConstraint(t, derived, KindReference, ID(profileURL, "Observation.subject", string(KindReference)))
	if len(ref.TargetProfiles) != 1 || ref.TargetProfiles[0] != "http://example.org/StructureDefinition/patient" {
		t.Fatalf("unexpected reference constraint: %+v", ref)
	}

	pat := requireConstraint(t, derived, KindPattern, ID(profileURL, "Observation.birthDate", string(KindPattern)))
	if pat.Value != "2024-01-01" {
		t.Fatalf("unexpected pattern constraint: %+v", pat)
	}

	fixed := requireConstraint(t, derived, KindFixed, ID(profileURL, "Observation.code", string(KindFixed)))
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
	rootID := ID(profileURL, "Observation", string(KindCardinality))
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
	sp := requireConstraint(t, derived, KindSearch, ID("http://hl7.org/fhir/SearchParameter/Observation-code", string(KindSearch), "Observation", "code"))
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
	requireConstraint(t, derived, KindInteraction, ID("http://example.org/CapabilityStatement/server", string(KindInteraction), "Observation", "create"))
	requireConstraint(t, derived, KindInteraction, ID("http://example.org/CapabilityStatement/server", string(KindInteraction), "Observation", "read"))
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

func TestIDDropsEmptyParts(t *testing.T) {
	if got := ID("", string(KindSearch), "Observation", "code"); got != "search|Observation|code" {
		t.Fatalf("got %q", got)
	}
	if got := ID(profileURL, "Observation.status", string(KindCardinality)); got != profileURL+"|Observation.status|cardinality" {
		t.Fatalf("got %q", got)
	}
}
