package generation

import (
	"strings"
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
	"github.com/jlcoulter/momus/internal/test/ast"
	"github.com/jlcoulter/momus/internal/test/coverage"
)

const interactionProfile = "http://example.org/StructureDefinition/patient"

// interactionPlan builds a strength-2 plan with three accept base obligations,
// one reject obligation, and the pairwise interactions among the accepts.
func interactionPlan() *coverage.CoveragePlan {
	base := []coverage.CoverageRequirement{
		{ID: "req-a", ProfileURL: interactionProfile, ResourceType: "Patient", ElementPath: "Patient.name", Domain: coverage.CoverageDomainCardinality, Variant: coverage.CoverageVariantValidMin, Min: 1, Max: "*"},
		{ID: "req-b", ProfileURL: interactionProfile, ResourceType: "Patient", ElementPath: "Patient.name", Domain: coverage.CoverageDomainCardinality, Variant: coverage.CoverageVariantMultipleValues, Min: 1, Max: "*"},
		{ID: "req-c", ProfileURL: interactionProfile, ResourceType: "Patient", ElementPath: "Patient.birthDate", Domain: coverage.CoverageDomainDatatype, Variant: coverage.CoverageVariantDatatypeValid, Min: 1, Max: "1"},
		{ID: "req-n", ProfileURL: interactionProfile, ResourceType: "Patient", ElementPath: "Patient.name", Domain: coverage.CoverageDomainCardinality, Variant: coverage.CoverageVariantMissingRequired, Min: 1, Max: "*"},
	}
	interactions := []coverage.CoverageRequirement{
		{ID: "int-ab", ProfileURL: interactionProfile, ResourceType: "Patient", ElementPath: "Patient.name ++ Patient.name", Domain: coverage.CoverageDomainInteraction, Variant: coverage.CoverageVariantInteractionPair, PairA: "req-a", PairB: "req-b"},
		{ID: "int-ac", ProfileURL: interactionProfile, ResourceType: "Patient", ElementPath: "Patient.name ++ Patient.birthDate", Domain: coverage.CoverageDomainInteraction, Variant: coverage.CoverageVariantInteractionPair, PairA: "req-a", PairB: "req-c"},
		{ID: "int-bc", ProfileURL: interactionProfile, ResourceType: "Patient", ElementPath: "Patient.name ++ Patient.birthDate", Domain: coverage.CoverageDomainInteraction, Variant: coverage.CoverageVariantInteractionPair, PairA: "req-b", PairB: "req-c"},
	}
	reqs := append(append([]coverage.CoverageRequirement{}, base...), interactions...)
	interactionList := make([]coverage.InteractionRequirement, 0, len(interactions))
	for _, in := range interactions {
		interactionList = append(interactionList, coverage.InteractionRequirement{
			ID: in.ID, ProfileURL: in.ProfileURL, ResourceType: in.ResourceType, RequirementA: in.PairA, RequirementB: in.PairB,
		})
	}
	return &coverage.CoveragePlan{
		Requirements: reqs,
		Interactions: interactionList,
		Strength:     2,
	}
}

func TestGenerateFromCoveragePlanStrengthTwoGroupsAccepts(t *testing.T) {
	plan, err := GenerateFromCoveragePlan(interactionPlan(), BuildOptions{BaseURL: "http://localhost:8080/fhir", Strength: 2})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}

	// Requirement case count must equal the total plan obligations (4 base + 3
	// interactions), one assert per obligation.
	if got := RequirementCount(plan); got != 7 {
		t.Fatalf("RequirementCount = %d, want 7", got)
	}

	root := plan.Root.(*ast.Sequence)
	resourceSeq := root.Steps[0].(*ast.Sequence)
	if len(resourceSeq.Steps) != 5 {
		t.Fatalf("resourceSeq has %d steps, want 5 (setup req/assert/capture + group case + reject case)", len(resourceSeq.Steps))
	}

	group := resourceSeq.Steps[3].(*ast.Sequence)
	groupReq, ok := group.Steps[0].(*ast.Request)
	if !ok {
		t.Fatalf("group first step is %T, want *ast.Request", group.Steps[0])
	}
	if groupReq.URL != "http://localhost:8080/fhir/Patient/"+requirementResourceID(interactionPlan().Requirements[0]) {
		t.Fatalf("group request URL = %q", groupReq.URL)
	}
	// Group shares a single request, so its asserts start at index 1.
	if group.Steps[1].(*ast.Assert).RequirementID != "req-a" {
		t.Fatalf("group assert[1] = %s, want req-a", group.Steps[1].(*ast.Assert).RequirementID)
	}

	// Collect the requirement IDs asserted within the group case.
	groupIDs := make(map[string]bool)
	acceptCount := 0
	rejectCount := 0
	for _, step := range group.Steps[1:] {
		assert, ok := step.(*ast.Assert)
		if !ok {
			t.Fatalf("group step is %T, want *ast.Assert", step)
		}
		groupIDs[assert.RequirementID] = true
		if assert.Trace != nil && assert.Trace.Domain == string(coverage.CoverageDomainInteraction) {
			if assert.Expression != "status in [200,201]" {
				t.Fatalf("interaction assert %s expression = %q, want accept", assert.RequirementID, assert.Expression)
			}
			if assert.Trace.Expected != "accept" {
				t.Fatalf("interaction assert %s expected = %q, want accept", assert.RequirementID, assert.Trace.Expected)
			}
			acceptCount++
		} else if assert.Expression == "status in [200,201]" {
			acceptCount++
		} else if assert.Expression == "status in [400,412,422]" {
			rejectCount++
		}
	}
	for _, id := range []string{"req-a", "req-b", "req-c", "int-ab", "int-ac", "int-bc"} {
		if !groupIDs[id] {
			t.Fatalf("group case missing assert for %s", id)
		}
	}
	if acceptCount != 6 || rejectCount != 0 {
		t.Fatalf("group case has %d accept and %d reject asserts, want 6 accept / 0 reject", acceptCount, rejectCount)
	}

	reject := resourceSeq.Steps[4].(*ast.Sequence)
	if len(reject.Steps) != 2 {
		t.Fatalf("reject case has %d steps, want 2 (request + assert)", len(reject.Steps))
	}
	rejectAssert := reject.Steps[1].(*ast.Assert)
	if rejectAssert.RequirementID != "req-n" {
		t.Fatalf("reject assert = %s, want req-n", rejectAssert.RequirementID)
	}
	if rejectAssert.Expression != "status in [400,412,422]" {
		t.Fatalf("reject expression = %q, want status in [400,412,422]", rejectAssert.Expression)
	}
}

func TestGenerateFromCoveragePlanStrengthTwoHasFewerRequestsThanRequirements(t *testing.T) {
	plan, err := GenerateFromCoveragePlan(interactionPlan(), BuildOptions{BaseURL: "http://localhost:8080/fhir", Strength: 2})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}
	root := plan.Root.(*ast.Sequence)
	resourceSeq := root.Steps[0].(*ast.Sequence)
	requestCount := 0
	for _, step := range resourceSeq.Steps {
		if _, ok := step.(*ast.Request); ok {
			requestCount++
			continue
		}
		if seq, ok := step.(*ast.Sequence); ok {
			if _, ok := seq.Steps[0].(*ast.Request); ok {
				requestCount++
			}
		}
	}
	// 3 accepts share one request + 1 reject = 2 case requests (plus setup).
	if requestCount != 3 {
		t.Fatalf("request count = %d, want 3 (setup + shared accept + reject)", requestCount)
	}
}

func TestGenerateFromCoveragePlanStrengthOneKeepsPerRequirementTests(t *testing.T) {
	plan, err := GenerateFromCoveragePlan(interactionPlan(), BuildOptions{BaseURL: "http://localhost:8080/fhir", Strength: 1})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}
	root := plan.Root.(*ast.Sequence)
	resourceSeq := root.Steps[0].(*ast.Sequence)
	// Setup (3) + one case per requirement (7) = 10 steps.
	if len(resourceSeq.Steps) != 10 {
		t.Fatalf("resourceSeq has %d steps at strength 1, want 10", len(resourceSeq.Steps))
	}
	for i := 3; i < len(resourceSeq.Steps); i++ {
		seq := resourceSeq.Steps[i].(*ast.Sequence)
		if len(seq.Steps) != 2 {
			t.Fatalf("strength-1 case %d has %d steps, want 2", i, len(seq.Steps))
		}
	}
}

func TestBuildInteractionAssertTraceDomain(t *testing.T) {
	in := coverage.CoverageRequirement{
		ID: "int-x", ProfileURL: interactionProfile, ResourceType: "Patient",
		ElementPath: "Patient.a ++ Patient.b",
		Domain:      coverage.CoverageDomainInteraction, Variant: coverage.CoverageVariantInteractionPair,
		PairA: "req-x", PairB: "req-y",
	}
	assert := buildInteractionAssert(in)
	if assert.RequirementID != "int-x" {
		t.Fatalf("requirement id = %s, want int-x", assert.RequirementID)
	}
	if !strings.HasPrefix(assert.Description, "server accepts") {
		t.Fatalf("description = %q, want accept description", assert.Description)
	}
	if assert.Trace == nil {
		t.Fatal("expected interaction trace")
	}
	if assert.Trace.Domain != "interaction" || assert.Trace.Variant != "interaction-pair" || assert.Trace.Expected != "accept" {
		t.Fatalf("trace = %+v, want interaction/accept trace", assert.Trace)
	}
}

// TestGenerateFromCoveragePlanStrengthTwoEndToEnd verifies the run-summary
// invariant end to end: the number of requirement cases generated at strength 2
// equals the plan's total obligations (base + interactions), so no duplicate or
// missing requirement warning fires.
func TestGenerateFromCoveragePlanStrengthTwoEndToEnd(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  interactionProfile,
		Type: "Patient",
		Elements: []model.ElementDefinition{
			{Path: "Patient", Min: 0, Max: "*"},
			{Path: "Patient.name", Min: 1, Max: "*", Types: []model.ElementType{{Code: "HumanName"}}},
			{Path: "Patient.birthDate", Min: 1, Max: "1", Types: []model.ElementType{{Code: "date"}}},
			{Path: "Patient.gender", Min: 0, Max: "1", Types: []model.ElementType{{Code: "code"}},
				Binding: &model.Binding{Strength: "required", ValueSet: "http://hl7.org/fhir/ValueSet/administrative-gender"}},
		},
	})

	plan, err := coverage.DerivePlan(r, coverage.DeriveOptions{Strength: 2})
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}
	if len(plan.Interactions) == 0 {
		t.Fatal("expected pairwise interactions at strength 2")
	}

	astPlan, err := GenerateFromCoveragePlan(plan, BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: r, Strength: 2})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}

	expect := len(plan.Requirements)
	if got := RequirementCount(astPlan); got != expect {
		t.Fatalf("requirement cases = %d, plan total obligations = %d (base + %d interactions); run-summary warning would fire", got, expect, len(plan.Interactions))
	}
}
