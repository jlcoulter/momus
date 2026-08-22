package fhircoverage

import (
	"testing"

	"github.com/jlcoulter/momus/internal/core/coverage"
	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

// interactionFixture returns a registry with a profile whose elements yield
// several accept obligations that can participate in pairwise interactions.
func interactionFixture() *registry.Registry {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/patient",
		Type: "Patient",
		Elements: []model.ElementDefinition{
			{Path: "Patient", Min: 0, Max: "*"},
			{Path: "Patient.name", Min: 1, Max: "*", Types: []model.ElementType{{Code: "HumanName"}}},
			{Path: "Patient.birthDate", Min: 1, Max: "1", Types: []model.ElementType{{Code: "date"}}},
			{
				Path:  "Patient.gender",
				Min:   0,
				Max:   "1",
				Types: []model.ElementType{{Code: "code"}},
				Binding: &model.Binding{
					Strength: "required",
					ValueSet: "http://hl7.org/fhir/ValueSet/administrative-gender",
				},
			},
		},
	})
	return r
}

// countAcceptBaseRequirements returns base (non-interaction) accept obligations.
func countAcceptBaseRequirements(plan *coverage.CoveragePlan) int {
	count := 0
	for _, req := range plan.Requirements {
		if isNonElementDomain(req.Domain) {
			continue
		}
		if req.Variant.IsReject() {
			continue
		}
		count++
	}
	return count
}

func TestDerivePlanStrengthOneHasNoInteractions(t *testing.T) {
	plan, err := DerivePlan(interactionFixture(), coverage.DeriveOptions{Strength: 1})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}
	if plan.Strength != 1 {
		t.Fatalf("plan strength = %d, want 1", plan.Strength)
	}
	if len(plan.Interactions) != 0 {
		t.Fatalf("got %d interactions at strength 1, want 0", len(plan.Interactions))
	}
	for _, req := range plan.Requirements {
		if req.Domain == coverage.CoverageDomainInteraction {
			t.Fatalf("unexpected interaction requirement at strength 1: %s", req.ID)
		}
	}
}

func TestDerivePlanStrengthTwoDerivesPairwiseInteractions(t *testing.T) {
	plan, err := DerivePlan(interactionFixture(), coverage.DeriveOptions{Strength: 2})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}
	if plan.Strength != 2 {
		t.Fatalf("plan strength = %d, want 2", plan.Strength)
	}

	baseAccept := countAcceptBaseRequirements(plan)
	if baseAccept < 2 {
		t.Fatalf("fixture produced only %d accept base obligations; need >= 2 for a pair", baseAccept)
	}
	wantPairs := baseAccept * (baseAccept - 1) / 2
	if len(plan.Interactions) != wantPairs {
		t.Fatalf("got %d interactions, want %d (C(%d,2))", len(plan.Interactions), wantPairs, baseAccept)
	}
	if plan.Summary.Interactions != len(plan.Interactions) {
		t.Fatalf("summary interactions = %d, want %d", plan.Summary.Interactions, len(plan.Interactions))
	}

	byID := make(map[string]coverage.CoverageRequirement, len(plan.Requirements))
	for _, req := range plan.Requirements {
		byID[req.ID] = req
	}
	for _, in := range plan.Interactions {
		req, ok := byID[in.ID]
		if !ok {
			t.Fatalf("interaction %s missing from requirements", in.ID)
		}
		if req.Domain != coverage.CoverageDomainInteraction || req.Variant != coverage.CoverageVariantInteractionPair {
			t.Fatalf("interaction %s has domain %q variant %q", in.ID, req.Domain, req.Variant)
		}
		if req.PairA != in.RequirementA || req.PairB != in.RequirementB {
			t.Fatalf("interaction %s pair mismatch: requirement=%s/%s, interaction=%s/%s", in.ID, req.PairA, req.PairB, in.RequirementA, in.RequirementB)
		}
		// Both sources must be accept base obligations present in the plan.
		a, aOK := byID[req.PairA]
		b, bOK := byID[req.PairB]
		if !aOK || !bOK {
			t.Fatalf("interaction %s references missing source requirements", in.ID)
		}
		if a.Domain == coverage.CoverageDomainInteraction || a.Variant.IsReject() {
			t.Fatalf("interaction %s source A is not an accept base obligation", in.ID)
		}
		if b.Domain == coverage.CoverageDomainInteraction || b.Variant.IsReject() {
			t.Fatalf("interaction %s source B is not an accept base obligation", in.ID)
		}
		if a.ProfileURL != b.ProfileURL || a.ProfileURL != req.ProfileURL {
			t.Fatalf("interaction %s sources span different profiles", in.ID)
		}
	}
}

func TestDerivePlanStrengthTwoHasUniqueInteractionIDs(t *testing.T) {
	plan, err := DerivePlan(interactionFixture(), coverage.DeriveOptions{Strength: 2})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}
	if got, unique := len(plan.Requirements), uniqueRequirementCount(plan); got != unique {
		t.Fatalf("plan has %d requirements but only %d unique IDs (%d duplicates)", got, unique, got-unique)
	}
}
