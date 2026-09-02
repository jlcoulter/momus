package generation

import (
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/jlcoulter/momus/internal/core/ast"
	"github.com/jlcoulter/momus/internal/core/coverage"
	coregen "github.com/jlcoulter/momus/internal/core/generation"
	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

// TestResolveBoundCodingFallsBackToExample verifies that when the bound
// ValueSet is not present in the registry, generation falls back to a real
// coding from the package's example instance data — preferring an instance
// whose meta.profile matches the node's profile.
func TestResolveBoundCodingFallsBackToExample(t *testing.T) {
	reg := registry.New()
	reg.AddResource(&model.Resource{
		ResourceType: "Patient",
		ProfileURLs:  []string{"http://digitalhealth.gov.au/fhir/hcpd/StructureDefinition/hcpd-practitioner"},
		Raw: map[string]any{
			"resourceType": "Patient",
			"communication": []any{
				map[string]any{
					"coding": []any{map[string]any{"system": "urn:ietf:bcp:47", "code": "it", "display": "Italian"}},
				},
			},
		},
	})
	// A second Patient example with a different profile and a different code.
	reg.AddResource(&model.Resource{
		ResourceType: "Patient",
		ProfileURLs:  []string{"http://example.org/StructureDefinition/other"},
		Raw: map[string]any{
			"communication": []any{
				map[string]any{
					"coding": []any{map[string]any{"system": "urn:ietf:bcp:47", "code": "en"}},
				},
			},
		},
	})

	// No bound ValueSet, so resolveBoundCoding fails and the example fallback
	// must pick the profile-matched instance's code ("it").
	node := &model.ElementNode{
		Path:       "Patient.communication",
		ProfileURL: "http://digitalhealth.gov.au/fhir/hcpd/StructureDefinition/hcpd-practitioner",
	}
	c, ok := resolveBoundCodingForNode(node, reg)
	if !ok || c.Code != "it" {
		t.Fatalf("resolveBoundCodingForNode=%+v ok=%v, want the profile-matched code \"it\"", c, ok)
	}

	// A node with no matching profile falls back to the first example of the
	// resource type.
	node2 := &model.ElementNode{Path: "Patient.communication", ProfileURL: "http://example.org/StructureDefinition/none"}
	c2, ok2 := resolveBoundCodingForNode(node2, reg)
	if !ok2 || c2.Code == "" {
		t.Fatalf("resolveBoundCodingForNode(no profile match)=%+v ok=%v, want any example code", c2, ok2)
	}
}

// TestResolveBoundCodingFromCodingChild verifies that a CodeableConcept node
// whose own binding is nil but whose "coding" child carries a required binding
// resolves a real code (common for nested extension value[x].coding).
func TestResolveBoundCodingFromCodingChild(t *testing.T) {
	reg := registry.New()
	reg.AddValueSet(&model.ValueSet{URL: "http://example.org/ValueSet/responsible-party", ComposeIncludes: []model.ValueSetInclude{{
		System: "http://example.org/CodeSystem/responsible-party",
		Concepts: []model.ConceptReference{
			{Code: "practitioner-initiated", Display: "Practitioner initiated"},
		},
	}}})
	reg.AddCodeSystem(&model.CodeSystem{URL: "http://example.org/CodeSystem/responsible-party", Concepts: []model.CodeSystemConcept{{Code: "practitioner-initiated", Display: "Practitioner initiated"}}})

	// The node is a CodeableConcept with no binding of its own; the binding is
	// on its "coding" child.
	node := &model.ElementNode{
		Path:       "Extension.extension.value[x]",
		ProfileURL: "http://example.org/StructureDefinition/suppressed",
		Children: map[string]*model.ElementNode{
			"coding": {
				Path: "Extension.extension.value[x].coding",
				Definition: &model.ElementDefinition{
					Binding: &model.Binding{Strength: "required", ValueSet: "http://example.org/ValueSet/responsible-party"},
				},
			},
		},
	}
	c, ok := resolveBoundCodingForNode(node, reg)
	if !ok || c.Code != "practitioner-initiated" {
		t.Fatalf("resolveBoundCodingForNode=%+v ok=%v, want the coding-child bound code", c, ok)
	}
}

// TestResolveBoundCodingFromExtensionValue verifies that an extension value[x]
// node resolves a real coding from example instance data by matching the
// extension URL, even when the bound ValueSet is not in the registry.
func TestResolveBoundCodingFromExtensionValue(t *testing.T) {
	reg := registry.New()
	reg.AddResource(&model.Resource{
		ResourceType: "HealthcareService",
		Raw: map[string]any{
			"resourceType": "HealthcareService",
			"extension": []any{
				map[string]any{
					"url": "http://digitalhealth.gov.au/fhir/cc/StructureDefinition/new-patient-availability",
					"valueCodeableConcept": map[string]any{
						"coding": []any{map[string]any{
							"system":  "https://www.healthterminologies.gov.au/integration/R4/fhir/CodeSystem/new-patient-availability-1",
							"code":    "accepting",
							"display": "Accepting new patients",
						}},
					},
				},
			},
		},
	})

	node := &model.ElementNode{
		Path:       "Extension.value[x]",
		ProfileURL: "http://digitalhealth.gov.au/fhir/cc/StructureDefinition/new-patient-availability",
	}
	c, ok := resolveBoundCodingForNode(node, reg)
	if !ok || c.Code != "accepting" {
		t.Fatalf("resolveBoundCodingForNode=%+v ok=%v, want the extension code \"accepting\"", c, ok)
	}
}

// TestResolveBoundCodingSkipsPlaceholders verifies that binding resolution skips
// placeholder/null codes (e.g. v2-0203 "XX") and returns a meaningful code from
// the package, so generated CodeableConcepts don't carry a null placeholder.
func TestResolveBoundCodingSkipsPlaceholders(t *testing.T) {
	reg := registry.New()
	reg.AddValueSet(&model.ValueSet{URL: "http://example.org/vs", ComposeIncludes: []model.ValueSetInclude{{System: "http://example.org/cs", Concepts: []model.ConceptReference{{Code: "XX", Display: "Null"}, {Code: "RI", Display: "Resource identifier"}}}}})
	reg.AddCodeSystem(&model.CodeSystem{URL: "http://example.org/cs", Concepts: []model.CodeSystemConcept{{Code: "XX", Display: "Null"}, {Code: "RI", Display: "Resource identifier"}}})
	def := &model.ElementDefinition{Binding: &model.Binding{Strength: "required", ValueSet: "http://example.org/vs"}}
	c, ok := resolveBoundCoding(def, reg)
	if !ok || c.Code != "RI" {
		t.Fatalf("resolveBoundCoding=%+v ok=%v, want a meaningful code (RI), not the XX placeholder", c, ok)
	}
}

// TestResolveCodingDisplayFillsCanonicalFromCodeSystem verifies that a coding
// whose display is missing or echoes the code is normalised to the canonical
// CodeSystem display, and that an unknown code does not echo the code as the
// display.
func TestResolveCodingDisplayFillsCanonicalFromCodeSystem(t *testing.T) {
	reg := registry.New()
	reg.AddCodeSystem(&model.CodeSystem{URL: "http://terminology.hl7.org/CodeSystem/v2-0203", Concepts: []model.CodeSystemConcept{
		{Code: "XX", Display: "Organization identifier"},
		{Code: "RI", Display: "Resource identifier"},
	}})

	// Display missing: fill the canonical display.
	missing := map[string]any{"system": "http://terminology.hl7.org/CodeSystem/v2-0203", "code": "XX"}
	normaliseCoding(missing, reg)
	if missing["display"] != "Organization identifier" {
		t.Fatalf("missing-display got %q, want Organization identifier", missing["display"])
	}

	// Display echoes the code: replace with the canonical.
	echored := map[string]any{"system": "http://terminology.hl7.org/CodeSystem/v2-0203", "code": "XX", "display": "XX"}
	normaliseCoding(echored, reg)
	if echored["display"] != "Organization identifier" {
		t.Fatalf("echoed-display got %q, want Organization identifier", echored["display"])
	}

	// Unknown code, display echoes the code: drop the display, never echo it.
	unknown := map[string]any{"system": "http://terminology.hl7.org/CodeSystem/v2-0203", "code": "not-a-code", "display": "not-a-code"}
	normaliseCoding(unknown, reg)
	if _, ok := unknown["display"]; ok {
		t.Fatalf("unknown-code display should be dropped, got %v", unknown["display"])
	}

	// A display that differs from the code but differs from the canonical
	// CodeSystem display is overwritten with the canonical display: the CodeSystem
	// is authoritative and servers validate the display text against it.
	// "Organisation Initiated" (title-cased from the code) must become the
	// canonical "Organisation initiated".
	reg2 := registry.New()
	reg2.AddCodeSystem(&model.CodeSystem{URL: "http://example.org/cs/responsible-party-type", Concepts: []model.CodeSystemConcept{
		{Code: "organisation-initiated", Display: "Organisation initiated"},
	}})
	overridden := map[string]any{"system": "http://example.org/cs/responsible-party-type", "code": "organisation-initiated", "display": "Organisation Initiated"}
	normaliseCoding(overridden, reg2)
	if overridden["display"] != "Organisation initiated" {
		t.Fatalf("generated display was not replaced by the canonical display: %v", overridden["display"])
	}
}

// TestNormaliseCodingPreservesDisplayWhenCodeSystemUnknown verifies that when the
// CodeSystem has no canonical display for a code, a deliberate, non-echoing
// display is preserved rather than overwritten or dropped.
func TestNormaliseCodingPreservesDisplayWhenCodeSystemUnknown(t *testing.T) {
	reg := registry.New()
	intentional := map[string]any{"system": "http://example.org/cs", "code": "ABC", "display": "Deliberately different"}
	normaliseCoding(intentional, reg)
	if intentional["display"] != "Deliberately different" {
		t.Fatalf("intentional display was overwritten/dropped: %v", intentional["display"])
	}
}

// TestIsMeaningfulCodingRejectsV3AbstractCodes verifies that v3 abstract/group
// codes (leading underscore) are not selected as generated values, while real
// codes remain acceptable.
func TestIsMeaningfulCodingRejectsV3AbstractCodes(t *testing.T) {
	if isMeaningfulCoding("_ActAccommodationReason", "") {
		t.Fatal("v3 abstract code _ActAccommodationReason must not be meaningful")
	}
	if !isMeaningfulCoding("RI", "Resource identifier") {
		t.Fatal("RI must remain a meaningful code")
	}
	if isMeaningfulCoding("XX", "") {
		t.Fatal("XX placeholder must remain non-meaningful")
	}
}

// TestGeneratedPractitionerRecordedSexOrGenderHasValueSlice verifies that the
// generated Practitioner's required recordedSexOrGender extension carries its
// required nested "value" sub-extension, so the server does not reject it with
// "Slice 'Extension.extension:value': a matching slice is required, but not
// found".
func TestGeneratedPractitionerRecordedSexOrGenderHasValueSlice(t *testing.T) {
	body := map[string]any{
		"resourceType": "Practitioner",
		"extension": []any{
			map[string]any{
				"url":       recordedSexOrGenderExtensionURL,
				"extension": []any{map[string]any{"url": "genderElementQualifier", "valueBoolean": true}},
			},
		},
	}

	ensureRecordedSexOrGenderValue(body)

	rawExt, ok := body["extension"].([]any)
	if !ok || len(rawExt) == 0 {
		t.Fatalf("expected Practitioner extension array, got %#v", body["extension"])
	}
	ext, ok := rawExt[0].(map[string]any)
	if !ok {
		t.Fatalf("expected extension map, got %T", rawExt[0])
	}
	subExt, ok := ext["extension"].([]any)
	if !ok {
		t.Fatalf("expected nested extension array, got %#v", ext["extension"])
	}
	var foundValue bool
	for _, s := range subExt {
		if sub, ok := s.(map[string]any); ok {
			if u, _ := sub["url"].(string); u == "value" {
				foundValue = true
				if _, ok := sub["valueCodeableConcept"]; !ok {
					t.Fatalf("value slice missing valueCodeableConcept: %#v", sub)
				}
			}
		}
	}
	if !foundValue {
		t.Fatalf("recordedSexOrGender extension missing required value slice: %#v", ext["extension"])
	}
}

// TestGeneratedProvenanceHasNoSubjectField verifies that a generated Provenance
// references a Patient dependency via its declared "target" element (the R4
// Provenance has no "subject" property) rather than an undeclared "subject".
func TestGeneratedProvenanceHasNoSubjectField(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/patient", Type: "Patient", Kind: "resource", Elements: []model.ElementDefinition{
		{Path: "Patient", Min: 0, Max: "*"},
		{Path: "Patient.name", Min: 1, Max: "*", Types: []model.ElementType{{Code: "HumanName"}}},
	}})
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/provenance", Type: "Provenance", Kind: "resource", Elements: []model.ElementDefinition{
		{Path: "Provenance", Min: 0, Max: "*"},
		{Path: "Provenance.target", Min: 1, Max: "*", Types: []model.ElementType{{Code: "Reference", TargetProfile: []string{"http://example.org/StructureDefinition/patient"}}}},
		{Path: "Provenance.recorded", Min: 1, Max: "1", Types: []model.ElementType{{Code: "instant"}}},
	}})

	plan, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{Requirements: []coverage.CoverageRequirement{
		{ID: "prov-1", ProfileURL: "http://example.org/StructureDefinition/provenance", ResourceType: "Provenance", ElementPath: "Provenance.target", DependencyTargets: []string{"Patient"}, Variant: coverage.CoverageVariantValidMin},
	}}, BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: reg})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}

	body := firstRequestBody(t, plan)
	if _, ok := body["subject"]; ok {
		t.Fatalf("Provenance must not carry a subject element, got %v", sortedBodyKeys(body))
	}
	target, ok := body["target"].([]any)
	if !ok || len(target) == 0 {
		t.Fatalf("expected Provenance target reference element, got %v", sortedBodyKeys(body))
	}
	targetRef, ok := target[0].(map[string]any)
	if !ok || targetRef["reference"] != "Patient/momus-setup-patient" {
		t.Fatalf("got target reference %v, want Patient/momus-setup-patient", targetRef)
	}
}

// (satisfying the AU mod-89 check digit), so identifiers conform to the
// au-australianbusinessnumber/au-australiancompanynumber profiles and their
// slices resolve on the server.
func TestGenerateValidABNAndACN(t *testing.T) {
	abnWeights := []int{10, 1, 3, 5, 7, 9, 11, 13, 15, 17, 19}
	acnWeights := []int{10, 1, 3, 5, 7, 9, 11, 13, 15}
	abn := generateABN()
	if len(abn) != 11 || !mod89Valid(abn, abnWeights, true) {
		t.Fatalf("generateABN()=%q is not a valid ABN", abn)
	}
	acn := generateACN()
	if len(acn) != 9 || !mod89Valid(acn, acnWeights, false) {
		t.Fatalf("generateACN()=%q is not a valid ACN", acn)
	}
}

// TestGenerateFromCoveragePlanExhaustiveAddsAndVariesOptionals verifies that
func TestGenerateFromCoveragePlanExhaustiveAddsAndVariesOptionals(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/patient", Type: "Patient", Elements: []model.ElementDefinition{
		{Path: "Patient", Min: 0, Max: "*"},
		{Path: "Patient.name", Min: 1, Max: "*", Types: []model.ElementType{{Code: "HumanName"}}},
		{Path: "Patient.birthDate", Min: 0, Max: "1", Types: []model.ElementType{{Code: "date"}}},
		{Path: "Patient.gender", Min: 0, Max: "1", Types: []model.ElementType{{Code: "code"}}},
	}})
	basePlan := &coverage.CoveragePlan{Requirements: []coverage.CoverageRequirement{
		{ID: "req-1", ProfileURL: "http://example.org/StructureDefinition/patient", ResourceType: "Patient", ElementPath: "Patient.name", Variant: coverage.CoverageVariantValidMin},
	}}

	// Default (non-exhaustive) payloads carry only the required name element.
	defaultPlan, err := GenerateFromCoveragePlan(basePlan, BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: reg})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}
	defaultBody := firstRequestBody(t, defaultPlan)
	if defaultBody["birthDate"] != nil || defaultBody["gender"] != nil {
		t.Fatalf("default payload unexpectedly exhaustive: %v", sortedBodyKeys(defaultBody))
	}

	// Exhaustive payloads add optional elements and vary their presence across
	// requests seeded from distinct requirement IDs.
	seen := make(map[string]bool)
	varied := false
	sawOptional := false
	for i := 0; i < 12; i++ {
		plan := &coverage.CoveragePlan{Requirements: []coverage.CoverageRequirement{
			{ID: "req-1", ProfileURL: "http://example.org/StructureDefinition/patient", ResourceType: "Patient", ElementPath: "Patient.name", Variant: coverage.CoverageVariantValidMin},
		}}
		// Ensure a distinct body seed per iteration by varying the requirement id.
		plan.Requirements[0].ID = "req-" + strconv.Itoa(i)
		p, err := GenerateFromCoveragePlan(plan, BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: reg, Exhaustive: true})
		if err != nil {
			t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
		}
		body := firstRequestBody(t, p)
		if body["name"] == nil {
			t.Fatal("exhaustive payload missing required name element")
		}
		if body["birthDate"] != nil || body["gender"] != nil {
			sawOptional = true
		}
		keySet := strings.Join(sortedBodyKeys(body), ",")
		if !seen[keySet] && len(seen) > 0 {
			varied = true
		}
		seen[keySet] = true
	}
	if !sawOptional {
		t.Fatal("exhaustive payloads never included an optional element")
	}
	if !varied {
		t.Fatal("expected exhaustive payload presence to vary across requests")
	}
}

// TestGenerateDatatypeFromProfilesPicksOneNotMerged verifies that when an
// element lists several type profiles (e.g. all the AU Identifier variants on
// Organization.identifier), generation uses a single profile rather than merging
// all of them into a value that conforms to none.
func TestGenerateDatatypeFromProfilesPicksOneNotMerged(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/profA", Type: "Identifier", Elements: []model.ElementDefinition{
		{Path: "Identifier", Min: 0, Max: "*"},
		{Path: "Identifier.system", Min: 1, Max: "1", Types: []model.ElementType{{Code: "uri"}}, Fixed: "http://example.org/system-a"},
		{Path: "Identifier.value", Min: 1, Max: "1", Types: []model.ElementType{{Code: "string"}}},
	}})
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/profB", Type: "Identifier", Elements: []model.ElementDefinition{
		{Path: "Identifier", Min: 0, Max: "*"},
		{Path: "Identifier.system", Min: 1, Max: "1", Types: []model.ElementType{{Code: "uri"}}, Fixed: "http://example.org/system-b"},
		{Path: "Identifier.value", Min: 1, Max: "1", Types: []model.ElementType{{Code: "string"}}},
	}})
	v, ok := generateDatatypeValueFromProfiles([]model.ElementType{{Profile: []string{"http://example.org/profA", "http://example.org/profB"}}}, reg)
	if !ok {
		t.Fatal("expected a generated value")
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("value = %T, want a map", v)
	}
	if m["system"] != "http://example.org/system-a" {
		t.Fatalf("system = %v, want the first profile's fixed system (profiles must not be merged)", m["system"])
	}
}

// TestGenerateFromCoveragePlanOmitsProvisioning verifies that the generated AST
// contains only test cases: provisioning is a separate stage (BuildSetupDataset +
// provisioner), so no per-resource setup create requests or setup-bound
// assertions appear in the AST.
func TestGenerateFromCoveragePlanOmitsProvisioning(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/patient", Type: "Patient", Elements: []model.ElementDefinition{
		{Path: "Patient", Min: 0, Max: "*"},
		{Path: "Patient.name", Min: 1, Max: "*", Types: []model.ElementType{{Code: "HumanName"}}},
	}})
	plan := &coverage.CoveragePlan{Requirements: []coverage.CoverageRequirement{
		{ID: "req-1", ProfileURL: "http://example.org/StructureDefinition/patient", ResourceType: "Patient", ElementPath: "Patient.name", Variant: coverage.CoverageVariantValidMin},
	}}

	astPlan, err := GenerateFromCoveragePlan(plan, BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: reg})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}
	if hasSetupStep(astPlan.Root) {
		t.Fatal("expected no provisioning steps in generated AST; provisioning is a separate stage")
	}
	if coregen.RequirementCount(astPlan) != 1 {
		t.Fatalf("RequirementCount = %d, want 1", coregen.RequirementCount(astPlan))
	}
}

// hasSetupStep reports whether a plan contains any setup create request or
// setup-bound assertion.
func hasSetupStep(node ast.Node) bool {
	found := false
	var walk func(ast.Node)
	walk = func(n ast.Node) {
		if found {
			return
		}
		switch typed := n.(type) {
		case *ast.Sequence:
			for _, step := range typed.Steps {
				walk(step)
			}
		case *ast.Parallel:
			for _, step := range typed.Steps {
				walk(step)
			}
		case *ast.Assert:
			if strings.HasPrefix(typed.RequirementID, "setup:") {
				found = true
			}
		case *ast.Request:
			if strings.Contains(typed.URL, "momus-setup-") {
				found = true
			}
		}
	}
	walk(node)
	return found
}

func firstRequestBody(t *testing.T, plan *ast.Plan) map[string]any {
	t.Helper()
	var body map[string]any
	var walk func(ast.Node)
	walk = func(node ast.Node) {
		if body != nil {
			return
		}
		switch n := node.(type) {
		case *ast.Sequence:
			for _, step := range n.Steps {
				walk(step)
			}
		case *ast.Parallel:
			for _, step := range n.Steps {
				walk(step)
			}
		case *ast.Request:
			// Skip the per-resource setup request; take the first requirement case.
			if _, isRequirement := n.Headers["X-Momus-Requirement-ID"]; isRequirement {
				if m, ok := n.Body.(map[string]any); ok {
					body = m
				}
			}
		}
	}
	walk(plan.Root)
	if body == nil {
		t.Fatal("no requirement case request body found in plan")
	}
	return body
}

func sortedBodyKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestGenerateFromCoveragePlanBuildsPerRequirementSequence(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/patient",
		Type: "Patient",
		Elements: []model.ElementDefinition{
			{Path: "Patient", Min: 0, Max: "*"},
			{Path: "Patient.name", Min: 1, Max: "*", Types: []model.ElementType{{Code: "HumanName"}}},
		},
	})
	plan, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{
		Requirements: []coverage.CoverageRequirement{
			{ID: "req-1", ProfileURL: "http://example.org/StructureDefinition/patient", ResourceType: "Patient", ElementPath: "Patient.name", Variant: coverage.CoverageVariantValidMin, Min: 1, Max: "*"},
			{ID: "req-2", ProfileURL: "http://example.org/StructureDefinition/patient", ResourceType: "Patient", ElementPath: "Patient.name", Variant: coverage.CoverageVariantMissingRequired, Min: 1, Max: "*"},
		},
	}, BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: reg})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}
	if plan.Version != "v1" {
		t.Fatalf("got version %q, want v1", plan.Version)
	}
	root := plan.Root.(*ast.Sequence)
	resourceSeq := root.Steps[0].(*ast.Sequence)
	case0 := resourceSeq.Steps[0].(*ast.Sequence)
	caseReq := case0.Steps[0].(*ast.Request)
	caseBody := caseReq.Body.(map[string]any)
	if _, ok := caseBody["_momus"]; ok {
		t.Fatal("did not expect _momus field in generated request body")
	}
	caseMeta := caseBody["meta"].(map[string]any)
	caseProfiles := caseMeta["profile"].([]any)
	if len(caseProfiles) != 1 || caseProfiles[0] != "http://example.org/StructureDefinition/patient" {
		t.Fatalf("got case meta.profile %v, want patient profile", caseMeta["profile"])
	}
	assert1 := case0.Steps[1].(*ast.Assert)
	if assert1.Expression != "status in [200,201]" {
		t.Fatalf("got expression %q, want status in [200,201]", assert1.Expression)
	}
	case1 := resourceSeq.Steps[1].(*ast.Sequence)
	assert2 := case1.Steps[1].(*ast.Assert)
	if assert2.Expression != "status in [400,412,422]" {
		t.Fatalf("got expression %q, want status in [400,412,422]", assert2.Expression)
	}
}

func TestGenerateFromCoveragePlanUsesDependencyTemplate(t *testing.T) {
	plan, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{
		Requirements: []coverage.CoverageRequirement{
			{ID: "p-1", ProfileURL: "http://example.org/StructureDefinition/patient", ResourceType: "Patient", ElementPath: "Patient.name", Variant: coverage.CoverageVariantValidMin},
			{ID: "o-1", ProfileURL: "http://example.org/StructureDefinition/observation", ResourceType: "Observation", ElementPath: "Observation.subject", DependencyTargets: []string{"Patient"}, Variant: coverage.CoverageVariantValidMin},
		},
	}, BuildOptions{BaseURL: "http://localhost:8080/fhir"})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}
	root := plan.Root.(*ast.Sequence)
	obsResourceSeq := root.Steps[1].(*ast.Sequence)
	caseSeq := obsResourceSeq.Steps[0].(*ast.Sequence)
	req := caseSeq.Steps[0].(*ast.Request)
	body := req.Body.(map[string]any)
	meta := body["meta"].(map[string]any)
	profiles := meta["profile"].([]any)
	if len(profiles) != 1 || profiles[0] != "http://example.org/StructureDefinition/observation" {
		t.Fatalf("got case meta.profile %v, want observation profile", meta["profile"])
	}
	subject := body["subject"].(map[string]any)
	if subject["reference"] != "Patient/momus-setup-patient" {
		t.Fatalf("got subject reference %v, want Patient/momus-setup-patient", subject["reference"])
	}
}

func TestNormalizeGeneratedIdentifierProducesValidHPIO(t *testing.T) {
	identifier := map[string]any{
		"system": "http://ns.electronichealth.net.au/id/hi/hpio/1.0",
		"value":  "123456",
	}

	normalizeGeneratedIdentifier(identifier)

	value, _ := identifier["value"].(string)
	if len(value) != 16 {
		t.Fatalf("got HPI-O length %d, want 16", len(value))
	}
	if value[:6] != "800362" {
		t.Fatalf("got HPI-O prefix %q, want 800362", value[:6])
	}
	if !isValidLuhn(value) {
		t.Fatalf("generated HPI-O %q failed Luhn check", value)
	}
}

func TestNormalizeGeneratedIdentifierProducesValidHPII(t *testing.T) {
	identifier := map[string]any{
		"system": "http://ns.electronichealth.net.au/id/hi/hpii/1.0",
		"value":  "123456",
	}

	normalizeGeneratedIdentifier(identifier)

	value, _ := identifier["value"].(string)
	if len(value) != 16 {
		t.Fatalf("got HPI-I length %d, want 16", len(value))
	}
	if value[:6] != "800361" {
		t.Fatalf("got HPI-I prefix %q, want 800361", value[:6])
	}
	if !isValidLuhn(value) {
		t.Fatalf("generated HPI-I %q failed Luhn check", value)
	}
}

func TestNormalizeGeneratedAddressDropsAUStateForPortableValidation(t *testing.T) {
	address := map[string]any{"country": "AU", "state": "State"}

	normalizeGeneratedAddress(address)

	if _, ok := address["state"]; ok {
		t.Fatalf("expected AU state to be removed, got %v", address["state"])
	}
}

func TestNormalizeHealthcareServiceTypeCodingAddsCoding(t *testing.T) {
	body := map[string]any{
		"resourceType": "HealthcareService",
		"type":         []any{map[string]any{"text": "Type"}},
	}

	normalizeResourceSpecificPayload(body)

	types := body["type"].([]any)
	first := types[0].(map[string]any)
	coding, ok := first["coding"].([]any)
	if !ok || len(coding) == 0 {
		t.Fatalf("expected type coding to be populated, got %+v", first)
	}
}

func TestEnsurePractitionerRoleKnownIdentifierAddsKnownIdentifier(t *testing.T) {
	body := map[string]any{
		"resourceType": "PractitionerRole",
		"identifier":   []any{map[string]any{"system": "http://example.org/id", "value": "x"}},
	}

	normalizeResourceSpecificPayload(body)

	identifiers := body["identifier"].([]any)
	found := false
	for _, raw := range identifiers {
		identifier, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		rawType, _ := identifier["type"].(map[string]any)
		if rawType == nil {
			continue
		}
		coding, ok := rawType["coding"].([]any)
		if !ok || len(coding) == 0 {
			continue
		}
		firstCoding, _ := coding[0].(map[string]any)
		if firstCoding["system"] != "http://terminology.hl7.org.au/CodeSystem/v2-0203" || firstCoding["code"] != "UPIN" {
			continue
		}
		if identifier["value"] == "" {
			t.Fatalf("expected practitioner role known identifier value, got %+v", identifier)
		}
		found = true
	}
	if !found {
		t.Fatalf("expected synthesized practitioner role known identifier, got %+v", identifiers)
	}
}

func TestNormalizeCodeableConceptMapFillsCodingDisplayAndText(t *testing.T) {
	value := map[string]any{
		"coding": []any{map[string]any{
			"system": "http://example.org/system",
			"code":   "test-code",
		}},
	}

	normalizeCodeableConceptMap(value)

	coding := value["coding"].([]any)[0].(map[string]any)
	if coding["display"] == "" {
		t.Fatalf("expected coding.display to be populated, got %+v", coding)
	}
	if value["text"] == "" {
		t.Fatalf("expected CodeableConcept.text to be populated, got %+v", value)
	}
}

func TestNormalizePractitionerFieldsAddsActiveAndRequiredNameUses(t *testing.T) {
	body := map[string]any{
		"resourceType": "Practitioner",
		"name": []any{map[string]any{
			"family": "Momus",
		}},
	}

	normalizeResourceSpecificPayload(body)

	if body["active"] != true {
		t.Fatalf("expected practitioner active=true, got %v", body["active"])
	}
	names := body["name"].([]any)
	hasOfficial := false
	hasUsual := false
	for _, raw := range names {
		name, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if name["use"] == "official" {
			hasOfficial = true
		}
		if name["use"] == "usual" {
			hasUsual = true
		}
	}
	if !hasOfficial || !hasUsual {
		t.Fatalf("expected practitioner names to include official and usual uses, got %+v", names)
	}
}

func TestEnsureHealthcareServiceKnownIdentifierAdded(t *testing.T) {
	body := map[string]any{
		"resourceType": "HealthcareService",
		"identifier":   []any{map[string]any{"system": "http://example.org/id", "value": "x"}},
	}

	normalizeResourceSpecificPayload(body)

	identifiers := body["identifier"].([]any)
	found := false
	for _, raw := range identifiers {
		identifier, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		system, _ := identifier["system"].(string)
		if system != "http://ns.electronichealth.net.au/id/hi/hpio/1.0" {
			continue
		}
		rawType, _ := identifier["type"].(map[string]any)
		coding := rawType["coding"].([]any)
		firstCoding := coding[0].(map[string]any)
		if firstCoding["system"] != "http://terminology.hl7.org.au/CodeSystem/v2-0203" || firstCoding["code"] != "NOI" {
			t.Fatalf("unexpected HealthcareService known identifier coding: %+v", firstCoding)
		}
		found = true
	}
	if !found {
		t.Fatalf("expected synthesized HealthcareService known identifier, got %+v", identifiers)
	}
}

func TestEnsureEndpointManagingOrganizationAdded(t *testing.T) {
	body := map[string]any{"resourceType": "Endpoint"}

	normalizeResourceSpecificPayload(body)

	managingOrganization, ok := body["managingOrganization"].(map[string]any)
	if !ok {
		t.Fatalf("expected managingOrganization reference, got %T", body["managingOrganization"])
	}
	if managingOrganization["reference"] != "Organization/momus-setup-organization" {
		t.Fatalf("got managingOrganization reference %v, want Organization/momus-setup-organization", managingOrganization["reference"])
	}
	identifiers := body["identifier"].([]any)
	found := false
	for _, raw := range identifiers {
		identifier, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		system, _ := identifier["system"].(string)
		if system == "http://ns.electronichealth.net.au/smd/target" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected endpoint smd target identifier, got %+v", identifiers)
	}
}

func isValidLuhn(number string) bool {
	if number == "" {
		return false
	}
	sum := 0
	double := false
	for i := len(number) - 1; i >= 0; i-- {
		r := number[i]
		if r < '0' || r > '9' {
			return false
		}
		digit := int(r - '0')
		if double {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}
		sum += digit
		double = !double
	}
	return sum%10 == 0
}

func TestEncodePlanIncludesTypeTags(t *testing.T) {
	plan := &ast.Plan{Version: "v1", Root: &ast.Sequence{Steps: []ast.Node{&ast.Request{Method: "GET", URL: "/Patient"}, &ast.Assert{Description: "ok", RequirementID: "r-1", Expression: "status == 200"}}}}
	encoded, err := ast.EncodePlan(plan)
	if err != nil {
		t.Fatalf("EncodePlan returned error: %v", err)
	}
	root := encoded["root"].(map[string]any)
	if root["type"] != "sequence" {
		t.Fatalf("got root type %v, want sequence", root["type"])
	}
	steps := root["steps"].([]any)
	step0 := steps[0].(map[string]any)
	if step0["type"] != "request" {
		t.Fatalf("got first step type %v, want request", step0["type"])
	}
}

func TestGenerateFromCoveragePlanPopulatesRequiredIdentifierSlicesFromProfile(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/hcpd-source-identifier", Type: "Identifier", Elements: []model.ElementDefinition{{Path: "Identifier", Min: 0, Max: "*"}, {Path: "Identifier.type", Min: 1, Max: "1", Types: []model.ElementType{{Code: "CodeableConcept"}}}, {Path: "Identifier.system", Min: 1, Max: "1", Types: []model.ElementType{{Code: "uri"}}}, {Path: "Identifier.value", Min: 1, Max: "1", Types: []model.ElementType{{Code: "string"}}}}})
	r.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/hcpd-local-identifier", Type: "Identifier", Elements: []model.ElementDefinition{{Path: "Identifier", Min: 0, Max: "*"}, {Path: "Identifier.type", Min: 1, Max: "1", Types: []model.ElementType{{Code: "CodeableConcept"}}}, {Path: "Identifier.system", Min: 1, Max: "1", Types: []model.ElementType{{Code: "uri"}}}, {Path: "Identifier.value", Min: 1, Max: "1", Types: []model.ElementType{{Code: "string"}}}}})
	r.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/location", Type: "Location", Elements: []model.ElementDefinition{{Path: "Location", Min: 0, Max: "*"}, {Path: "Location.identifier", Min: 2, Max: "*", Types: []model.ElementType{{Code: "Identifier"}}}, {Path: "Location.identifier", Min: 1, Max: "1", SliceName: "Source", Types: []model.ElementType{{Code: "Identifier", Profile: []string{"http://example.org/StructureDefinition/hcpd-source-identifier"}}}}, {Path: "Location.identifier", Min: 1, Max: "1", SliceName: "Local", Types: []model.ElementType{{Code: "Identifier", Profile: []string{"http://example.org/StructureDefinition/hcpd-local-identifier"}}}}}})
	plan, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{Requirements: []coverage.CoverageRequirement{{ID: "req-location", ProfileURL: "http://example.org/StructureDefinition/location", ResourceType: "Location", ElementPath: "Location.identifier", Variant: coverage.CoverageVariantValidMin}}}, BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: r})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}
	root := plan.Root.(*ast.Sequence)
	resourceSeq := root.Steps[0].(*ast.Sequence)
	caseSeq := resourceSeq.Steps[0].(*ast.Sequence)
	req := caseSeq.Steps[0].(*ast.Request)
	body := req.Body.(map[string]any)
	identifiers := body["identifier"].([]any)
	if len(identifiers) != 2 {
		t.Fatalf("got %d identifiers, want 2", len(identifiers))
	}
	for i, raw := range identifiers {
		identifier := raw.(map[string]any)
		if identifier["system"] == nil || identifier["value"] == nil || identifier["type"] == nil {
			t.Fatalf("identifier %d missing required fields: %+v", i, identifier)
		}
	}
}

func TestGenerateFromCoveragePlanMergesPatternAndBindingForCodeableConcept(t *testing.T) {
	r := registry.New()
	r.AddValueSet(&model.ValueSet{URL: "http://example.org/ValueSet/identifier-type", ComposeIncludes: []model.ValueSetInclude{{System: "http://example.org/system/identifier-type", Concepts: []model.ConceptReference{{Code: "bound-code", Display: "Bound Code"}}}}})
	r.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/identifier-profile", Type: "Identifier", Elements: []model.ElementDefinition{{Path: "Identifier", Min: 0, Max: "*"}, {Path: "Identifier.type", Min: 1, Max: "1", Types: []model.ElementType{{Code: "CodeableConcept"}}, Binding: &model.Binding{Strength: "required", ValueSet: "http://example.org/ValueSet/identifier-type"}, Pattern: map[string]any{"coding": []any{map[string]any{"system": "http://pattern.example/system", "code": "pattern-code"}}}}, {Path: "Identifier.system", Min: 1, Max: "1", Types: []model.ElementType{{Code: "uri"}}}, {Path: "Identifier.value", Min: 1, Max: "1", Types: []model.ElementType{{Code: "string"}}}}})
	r.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/location-profile", Type: "Location", Elements: []model.ElementDefinition{{Path: "Location", Min: 0, Max: "*"}, {Path: "Location.identifier", Min: 1, Max: "*", Types: []model.ElementType{{Code: "Identifier", Profile: []string{"http://example.org/StructureDefinition/identifier-profile"}}}}}})
	plan, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{Requirements: []coverage.CoverageRequirement{{ID: "req-location-binding", ProfileURL: "http://example.org/StructureDefinition/location-profile", ResourceType: "Location", ElementPath: "Location.identifier", Variant: coverage.CoverageVariantValidMin}}}, BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: r})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}
	root := plan.Root.(*ast.Sequence)
	resourceSeq := root.Steps[0].(*ast.Sequence)
	caseSeq := resourceSeq.Steps[0].(*ast.Sequence)
	req := caseSeq.Steps[0].(*ast.Request)
	body := req.Body.(map[string]any)
	identifiers := body["identifier"].([]any)
	identifier := identifiers[0].(map[string]any)
	typeConcept := identifier["type"].(map[string]any)
	codings := typeConcept["coding"].([]any)
	if len(codings) != 1 {
		t.Fatalf("got %d codings, want 1", len(codings))
	}
	first := codings[0].(map[string]any)
	if first["system"] != "http://pattern.example/system" || first["code"] != "pattern-code" {
		t.Fatalf("got coding %+v, want pattern coding", first)
	}
	if first["display"] != "Bound Code" || typeConcept["text"] != "Bound Code" {
		t.Fatalf("expected binding display/text fill, got %+v", typeConcept)
	}
}

func TestGenerateFromCoveragePlanFillsMissingPatternCodingFieldsFromBinding(t *testing.T) {
	r := registry.New()
	r.AddValueSet(&model.ValueSet{URL: "http://example.org/ValueSet/fill-coding", ComposeIncludes: []model.ValueSetInclude{{System: "http://example.org/system/fill-coding", Concepts: []model.ConceptReference{{Code: "bound-code", Display: "Bound Display"}}}}})
	r.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/test-profile", Type: "Observation", Elements: []model.ElementDefinition{{Path: "Observation", Min: 0, Max: "*"}, {Path: "Observation.code", Min: 1, Max: "1", Types: []model.ElementType{{Code: "CodeableConcept"}}, Binding: &model.Binding{Strength: "required", ValueSet: "http://example.org/ValueSet/fill-coding"}, Pattern: map[string]any{"coding": []any{map[string]any{"system": "http://pattern.example/system"}}}}}})
	plan, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{Requirements: []coverage.CoverageRequirement{{ID: "req-observation-binding", ProfileURL: "http://example.org/StructureDefinition/test-profile", ResourceType: "Observation", ElementPath: "Observation.code", Variant: coverage.CoverageVariantValidMin}}}, BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: r})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}
	root := plan.Root.(*ast.Sequence)
	resourceSeq := root.Steps[0].(*ast.Sequence)
	caseSeq := resourceSeq.Steps[0].(*ast.Sequence)
	req := caseSeq.Steps[0].(*ast.Request)
	body := req.Body.(map[string]any)
	codeConcept := body["code"].(map[string]any)
	codings := codeConcept["coding"].([]any)
	if len(codings) != 1 {
		t.Fatalf("got %d codings, want 1", len(codings))
	}
	coding := codings[0].(map[string]any)
	if coding["system"] != "http://pattern.example/system" || coding["code"] != "bound-code" || coding["display"] != "Bound Display" {
		t.Fatalf("got coding %+v, want merged pattern/binding coding", coding)
	}
}

func TestGenerateFromCoveragePlanIncludesOptionalContainerWithRequiredSlices(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/location-sliced-extension", Type: "Location", Elements: []model.ElementDefinition{{Path: "Location", Min: 0, Max: "*"}, {Path: "Location.extension", Min: 0, Max: "*", Types: []model.ElementType{{Code: "Extension"}}}, {Path: "Location.extension", Min: 1, Max: "1", SliceName: "required-ext", Types: []model.ElementType{{Code: "Extension"}}}, {Path: "Location.extension.url", Min: 1, Max: "1", Types: []model.ElementType{{Code: "uri"}}, Fixed: "http://example.org/StructureDefinition/required-ext"}, {Path: "Location.extension.valueString", Min: 1, Max: "1", Types: []model.ElementType{{Code: "string"}}, Examples: []any{"example-value"}}}})
	plan, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{Requirements: []coverage.CoverageRequirement{{ID: "req-location-ext", ProfileURL: "http://example.org/StructureDefinition/location-sliced-extension", ResourceType: "Location", ElementPath: "Location.extension.url", Variant: coverage.CoverageVariantValidMin}}}, BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: r})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}
	root := plan.Root.(*ast.Sequence)
	resourceSeq := root.Steps[0].(*ast.Sequence)
	caseSeq := resourceSeq.Steps[0].(*ast.Sequence)
	req := caseSeq.Steps[0].(*ast.Request)
	body := req.Body.(map[string]any)
	extensions := body["extension"].([]any)
	ext := extensions[0].(map[string]any)
	if ext["url"] != "http://example.org/StructureDefinition/required-ext" || ext["valueString"] != "example-value" {
		t.Fatalf("got extension %+v, want fixed url and example value", ext)
	}
}

func TestGenerateFromCoveragePlanPopulatesSliceChildPatternFields(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/org-address-slice", Type: "Organization", Elements: []model.ElementDefinition{{Path: "Organization", Name: "Organization"}, {Path: "Organization.address", Name: "address", Min: 1, Max: "1", Types: []model.ElementType{{Code: "Address"}}}, {Path: "Organization.address", Name: "address", SliceName: "physical", Min: 1, Max: "1", Types: []model.ElementType{{Code: "Address"}}}, {ID: "Organization.address:physical.type", Path: "Organization.address.type", Name: "type", Min: 1, Max: "1", Types: []model.ElementType{{Code: "code"}}, Pattern: "physical"}, {ID: "Organization.address:physical.line", Path: "Organization.address.line", Name: "line", Min: 1, Max: "*", Types: []model.ElementType{{Code: "string"}}, Examples: []any{"1 Example Street"}}}})
	plan, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{Requirements: []coverage.CoverageRequirement{{ID: "req-org-address", ProfileURL: "http://example.org/StructureDefinition/org-address-slice", ResourceType: "Organization", ElementPath: "Organization.address", Variant: coverage.CoverageVariantValidMin}}}, BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: r})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}
	root := plan.Root.(*ast.Sequence)
	resourceSeq := root.Steps[0].(*ast.Sequence)
	caseSeq := resourceSeq.Steps[0].(*ast.Sequence)
	req := caseSeq.Steps[0].(*ast.Request)
	body := req.Body.(map[string]any)
	address := body["address"].(map[string]any)
	if address["type"] != "physical" {
		t.Fatalf("got address type %v, want physical", address["type"])
	}
	lines := address["line"].([]any)
	if len(lines) != 1 || lines[0] != "1 Example Street" {
		t.Fatalf("got address lines %v, want [1 Example Street]", lines)
	}
}

func TestGenerateFromCoveragePlanUsesTypedChoicePropertyNames(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/ext-profile", Type: "Extension", Elements: []model.ElementDefinition{{Path: "Extension", Name: "Extension"}, {Path: "Extension.url", Name: "url", Min: 1, Max: "1", Types: []model.ElementType{{Code: "uri"}}, Fixed: "http://example.org/ext"}, {Path: "Extension.value[x]", Name: "value[x]", Min: 1, Max: "1", Types: []model.ElementType{{Code: "Coding"}}, Pattern: map[string]any{"system": "http://example.org/system", "code": "seed"}}}})
	r.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/location-choice", Type: "Location", Elements: []model.ElementDefinition{{Path: "Location", Name: "Location"}, {Path: "Location.extension", Name: "extension", Min: 1, Max: "*", Types: []model.ElementType{{Code: "Extension", Profile: []string{"http://example.org/StructureDefinition/ext-profile"}}}}}})
	plan, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{Requirements: []coverage.CoverageRequirement{{ID: "req-location-choice", ProfileURL: "http://example.org/StructureDefinition/location-choice", ResourceType: "Location", ElementPath: "Location.extension", Variant: coverage.CoverageVariantValidMin}}}, BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: r})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}
	root := plan.Root.(*ast.Sequence)
	resourceSeq := root.Steps[0].(*ast.Sequence)
	caseSeq := resourceSeq.Steps[0].(*ast.Sequence)
	req := caseSeq.Steps[0].(*ast.Request)
	body := req.Body.(map[string]any)
	ext := body["extension"].([]any)[0].(map[string]any)
	if _, ok := ext["value[x]"]; ok {
		t.Fatal("did not expect literal value[x] key in generated extension")
	}
	valueCoding := ext["valueCoding"].(map[string]any)
	if valueCoding["system"] != "http://example.org/system" || valueCoding["code"] != "seed" {
		t.Fatalf("got valueCoding %+v, want pattern coding", valueCoding)
	}
}

func TestGenerateFromCoveragePlanUsesSingleObjectForSingularSlicedChoice(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/ext-sliced-choice",
		Type: "Extension",
		Elements: []model.ElementDefinition{
			{Path: "Extension", Name: "Extension"},
			{Path: "Extension.url", Name: "url", Min: 1, Max: "1", Types: []model.ElementType{{Code: "uri"}}, Fixed: "http://example.org/ext-sliced"},
			{Path: "Extension.value[x]", Name: "value[x]", Min: 1, Max: "1", Types: []model.ElementType{{Code: "Element"}}},
			{Path: "Extension.value[x]", Name: "value[x]", SliceName: "valueCoding", Min: 1, Max: "1", Types: []model.ElementType{{Code: "Coding"}}, Pattern: map[string]any{"system": "http://example.org/system", "code": "seed"}},
		},
	})
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/location-choice-slice",
		Type: "Location",
		Elements: []model.ElementDefinition{
			{Path: "Location", Name: "Location"},
			{Path: "Location.extension", Name: "extension", Min: 1, Max: "*", Types: []model.ElementType{{Code: "Extension", Profile: []string{"http://example.org/StructureDefinition/ext-sliced-choice"}}}},
		},
	})

	plan, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{Requirements: []coverage.CoverageRequirement{{ID: "req-location-choice-slice", ProfileURL: "http://example.org/StructureDefinition/location-choice-slice", ResourceType: "Location", ElementPath: "Location.extension", Variant: coverage.CoverageVariantValidMin}}}, BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: r})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}

	root := plan.Root.(*ast.Sequence)
	resourceSeq := root.Steps[0].(*ast.Sequence)
	caseSeq := resourceSeq.Steps[0].(*ast.Sequence)
	req := caseSeq.Steps[0].(*ast.Request)
	body := req.Body.(map[string]any)
	ext := body["extension"].([]any)[0].(map[string]any)
	if _, ok := ext["valueCoding"].([]any); ok {
		t.Fatal("did not expect valueCoding to be an array for singular sliced choice")
	}
	valueCoding := ext["valueCoding"].(map[string]any)
	if valueCoding["system"] != "http://example.org/system" || valueCoding["code"] != "seed" {
		t.Fatalf("got valueCoding %+v, want pattern coding", valueCoding)
	}
}

func TestGenerateFromCoveragePlanWrapsRepeatableComplexFieldsAsArrays(t *testing.T) {
	r := registry.New()
	r.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/org-repeatable-address",
		Type: "Organization",
		Elements: []model.ElementDefinition{
			{Path: "Organization", Name: "Organization"},
			{Path: "Organization.address", Name: "address", Min: 1, Max: "1", BaseMax: "*", Types: []model.ElementType{{Code: "Address"}}},
			{Path: "Organization.address.line", Name: "line", Min: 1, Max: "*", Types: []model.ElementType{{Code: "string"}}, Examples: []any{"1 Example Street"}},
		},
	})

	plan, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{Requirements: []coverage.CoverageRequirement{{
		ID:           "req-org-address-array",
		ProfileURL:   "http://example.org/StructureDefinition/org-repeatable-address",
		ResourceType: "Organization",
		ElementPath:  "Organization.address",
		Variant:      coverage.CoverageVariantValidMin,
	}}}, BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: r})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}

	root := plan.Root.(*ast.Sequence)
	resourceSeq := root.Steps[0].(*ast.Sequence)
	caseSeq := resourceSeq.Steps[0].(*ast.Sequence)
	req := caseSeq.Steps[0].(*ast.Request)
	body := req.Body.(map[string]any)
	addresses, ok := body["address"].([]any)
	if !ok {
		t.Fatalf("expected Organization.address as []any, got %T", body["address"])
	}
	if len(addresses) != 1 {
		t.Fatalf("expected one address entry, got %d", len(addresses))
	}
	address, ok := addresses[0].(map[string]any)
	if !ok {
		t.Fatalf("expected address entry as map, got %T", addresses[0])
	}
	if address["line"] == nil {
		t.Fatalf("expected address line to be populated, got %+v", address)
	}
}

func TestGenerateFromCoveragePlanPrefersCapabilityProfileInMeta(t *testing.T) {
	plan, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{
		Requirements: []coverage.CoverageRequirement{
			{
				ID:           "req-pr-role",
				ProfileURL:   "http://hl7.org.au/fhir/StructureDefinition/au-practitionerrole",
				ResourceType: "PractitionerRole",
				ElementPath:  "PractitionerRole.code",
				Variant:      coverage.CoverageVariantValidMin,
			},
		},
	}, BuildOptions{
		BaseURL: "http://localhost:8080/fhir",
		PreferredProfileURLsByResource: map[string][]string{
			"PractitionerRole": {
				"http://digitalhealth.gov.au/fhir/hcpd/StructureDefinition/hcpd-practitionerrole",
			},
		},
	})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}
	root := plan.Root.(*ast.Sequence)
	resourceSeq := root.Steps[0].(*ast.Sequence)
	caseSeq := resourceSeq.Steps[0].(*ast.Sequence)
	req := caseSeq.Steps[0].(*ast.Request)
	body := req.Body.(map[string]any)
	meta := body["meta"].(map[string]any)
	profiles := meta["profile"].([]any)
	if len(profiles) < 2 {
		t.Fatalf("expected at least two declared profiles, got %v", profiles)
	}
	if profiles[0] != "http://digitalhealth.gov.au/fhir/hcpd/StructureDefinition/hcpd-practitionerrole" {
		t.Fatalf("got primary profile %v, want hcpd-practitionerrole", profiles[0])
	}
	if profiles[1] != "http://hl7.org.au/fhir/StructureDefinition/au-practitionerrole" {
		t.Fatalf("got secondary profile %v, want au-practitionerrole", profiles[1])
	}
}

func TestNormalizeReferenceTypeUsesTargetResourceType(t *testing.T) {
	def := &model.ElementDefinition{
		Types: []model.ElementType{{Code: "Reference", TargetProfile: []string{"http://example.org/StructureDefinition/Organization"}}},
	}
	value := map[string]any{
		"reference": "Organization/momus-setup-organization",
		"type":      "urn:uuid:bad-type",
	}

	normalizeReferenceType(value, def, nil)

	if value["type"] != "Organization" {
		t.Fatalf("got reference type %v, want Organization", value["type"])
	}
}

func TestGenerateFromCoveragePlanGeneratesNegativeVariants(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/observation",
		Type: "Observation",
		Elements: []model.ElementDefinition{
			{Path: "Observation", Min: 0, Max: "*"},
			{Path: "Observation.value", Min: 1, Max: "1", Types: []model.ElementType{{Code: "string"}}},
		},
	})
	plan, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{
		Requirements: []coverage.CoverageRequirement{
			{ID: "d-valid", ProfileURL: "http://example.org/StructureDefinition/observation", ResourceType: "Observation", ElementPath: "Observation.value", Variant: coverage.CoverageVariantDatatypeValid},
			{ID: "d-invalid", ProfileURL: "http://example.org/StructureDefinition/observation", ResourceType: "Observation", ElementPath: "Observation.value", Variant: coverage.CoverageVariantDatatypeInvalidLexical},
		},
	}, BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: reg})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}
	if got := coregen.RequirementCount(plan); got != 2 {
		t.Fatalf("got %d generated cases, want 2 (positive + negative)", got)
	}
	expressions := map[string]bool{}
	collectAssertExpressions(plan.Root, expressions)
	if !expressions["status in [200,201]"] {
		t.Fatal("expected a positive (accept) assertion")
	}
	if !expressions["status in [400,412,422]"] {
		t.Fatal("expected a negative (reject) assertion")
	}
}

func collectAssertExpressions(node ast.Node, into map[string]bool) {
	switch n := node.(type) {
	case *ast.Sequence:
		for _, step := range n.Steps {
			collectAssertExpressions(step, into)
		}
	case *ast.Parallel:
		for _, step := range n.Steps {
			collectAssertExpressions(step, into)
		}
	case *ast.Assert:
		into[n.Expression] = true
	}
}

func TestGenerateFromCoveragePlanCarriesTraceToAssertions(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/observation",
		Type: "Observation",
		Elements: []model.ElementDefinition{
			{Path: "Observation", Min: 0, Max: "*"},
			{Path: "Observation.value", Min: 1, Max: "1", Types: []model.ElementType{{Code: "date"}}},
		},
	})
	plan, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{
		Requirements: []coverage.CoverageRequirement{
			{ID: "req|obs|datatype-invalid-lexical", ConstraintID: "profile|Observation.value|datatype|date", ProfileURL: "http://example.org/StructureDefinition/observation", ResourceType: "Observation", ElementPath: "Observation.value", Domain: coverage.CoverageDomainDatatype, Variant: coverage.CoverageVariantDatatypeInvalidLexical},
		},
	}, BuildOptions{BaseURL: "http://localhost:8080/fhir", Registry: reg})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}

	asserts := collectAsserts(plan.Root)
	if len(asserts) != 1 {
		t.Fatalf("got %d requirement asserts, want 1", len(asserts))
	}
	a := asserts[0]
	if a.Trace == nil {
		t.Fatal("expected assertion trace to be populated")
	}
	if a.Trace.ConstraintID != "profile|Observation.value|datatype|date" {
		t.Fatalf("got constraint id %q", a.Trace.ConstraintID)
	}
	if a.Trace.Domain != string(coverage.CoverageDomainDatatype) || a.Trace.Variant != string(coverage.CoverageVariantDatatypeInvalidLexical) {
		t.Fatalf("unexpected trace domain/variant: %+v", a.Trace)
	}
	if a.Trace.Expected != "reject" {
		t.Fatalf("got expected %q, want reject", a.Trace.Expected)
	}
}

func collectAsserts(node ast.Node) []*ast.Assert {
	var out []*ast.Assert
	var walk func(ast.Node)
	walk = func(n ast.Node) {
		switch v := n.(type) {
		case *ast.Sequence:
			for _, s := range v.Steps {
				walk(s)
			}
		case *ast.Parallel:
			for _, s := range v.Steps {
				walk(s)
			}
		case *ast.Assert:
			if v.Trace != nil {
				out = append(out, v)
			}
		}
	}
	walk(node)
	return out
}

// TestOptionalSliceIncludedRandomly verifies that an optional (Min == 0) slice
// is included only some of the time — with optionalInclusionProbability when an
// RNG is supplied, and never when the RNG is nil (the required/nil path) — while
// a required slice (Min > 0) is always present. This models how real data
// varies: optional extension slices such as the HCPD "suppressed" extension
// appear in a fraction of payloads rather than never (or always).
func TestOptionalSliceIncludedRandomly(t *testing.T) {
	// A node with one required slice and one optional slice, both on a repeatable
	// element so generateRepeatedValue is exercised.
	node := &model.ElementNode{
		Name:       "extension",
		Path:       "Organization.extension",
		Definition: &model.ElementDefinition{Path: "Organization.extension", Min: 0, Max: "*"},
		Slices: map[string]*model.SliceNode{
			"required": {Name: "required", Definition: &model.ElementDefinition{Path: "Organization.extension", SliceName: "required", Min: 1, Max: "1"}, Children: map[string]*model.ElementNode{
				"url": {Name: "url", Path: "Organization.extension.url", Definition: &model.ElementDefinition{Path: "Organization.extension.url", Min: 1, Max: "1", Fixed: "http://example.org/required"}},
			}},
			"optional": {Name: "optional", Definition: &model.ElementDefinition{Path: "Organization.extension", SliceName: "optional", Min: 0, Max: "1"}, Children: map[string]*model.ElementNode{
				"url": {Name: "url", Path: "Organization.extension.url", Definition: &model.ElementDefinition{Path: "Organization.extension.url", Min: 1, Max: "1", Fixed: "http://example.org/optional"}},
			}},
		},
	}
	reg := registry.New()

	// nil RNG: the required slice is present, the optional slice is never emitted.
	val, ok := generateRepeatedValue(node, reg, nil)
	if !ok {
		t.Fatal("generateRepeatedValue returned false with nil RNG")
	}
	arr := val.([]any)
	urls := sliceURLs(arr)
	if !contains(urls, "http://example.org/required") {
		t.Fatalf("required slice missing in nil-RNG output: %v", urls)
	}
	if contains(urls, "http://example.org/optional") {
		t.Fatalf("optional slice must be omitted with nil RNG, got %v", urls)
	}

	// With an RNG, the optional slice must appear in a non-empty subset across
	// many seeds (never always-absent and never always-present), matching
	// optionalInclusionProbability.
	var everPresent, everAbsent int
	const trials = 200
	for i := 0; i < trials; i++ {
		rng := newRNG("seed-" + strconv.Itoa(i))
		val, ok := generateRepeatedValue(node, reg, rng)
		if !ok {
			t.Fatalf("generateRepeatedValue returned nil with RNG seed %d", i)
		}
		urls := sliceURLs(val.([]any))
		if contains(urls, "http://example.org/required") {
			// required slice always present
		}
		if contains(urls, "http://example.org/optional") {
			everPresent++
		} else {
			everAbsent++
		}
	}
	if everPresent == 0 || everAbsent == 0 {
		t.Fatalf("optional slice inclusion is not random across seeds (present=%d absent=%d)", everPresent, everAbsent)
	}
}

func sliceURLs(arr []any) []string {
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if u, ok := m["url"].(string); ok {
			out = append(out, u)
		}
	}
	return out
}

// TestSliceFallbackAppliesSliceConstraints verifies that a value generated into a
// sliced repeatable element honors the slice's Fixed/Pattern constraints, both when
// the slice is required (generated via the slice loop) and when it is optional and
// reached through the sliced fallback path. Without this, a bare value (e.g. a phone
// lacking the use the personalPhoneNumber slice requires) matches no slice and a
// conformant server rejects the resource.
func TestSliceFallbackAppliesSliceConstraints(t *testing.T) {
	makeSlice := func(min int) map[string]*model.SliceNode {
		return map[string]*model.SliceNode{
			"personalPhoneNumber": {
				Name: "personalPhoneNumber",
				Definition: &model.ElementDefinition{
					Path:      "Practitioner.telecom",
					SliceName: "personalPhoneNumber",
					Min:       min,
					Max:       "1",
					Types:     []model.ElementType{{Code: "ContactPoint"}},
				},
				Children: map[string]*model.ElementNode{
					"system": {Name: "system", Path: "Practitioner.telecom.system", Definition: &model.ElementDefinition{Path: "Practitioner.telecom.system", Min: 0, Max: "1", Fixed: "phone"}},
					"use":    {Name: "use", Path: "Practitioner.telecom.use", Definition: &model.ElementDefinition{Path: "Practitioner.telecom.use", Min: 0, Max: "1", Fixed: "home"}},
				},
			},
		}
	}
	reg := registry.New()
	assertPhone := func(t *testing.T, arr any) {
		t.Helper()
		cp, ok := arr.([]any)[0].(map[string]any)
		if !ok {
			t.Fatalf("expected a ContactPoint map, got %T", arr)
		}
		if cp["use"] != "home" {
			t.Fatalf("slice constraint use=home not applied, got %+v", cp)
		}
		if cp["system"] != "phone" {
			t.Fatalf("slice constraint system=phone not applied, got %+v", cp)
		}
	}

	// Required slice: generated through the slice loop.
	required := &model.ElementNode{
		Name:       "telecom",
		Path:       "Practitioner.telecom",
		Definition: &model.ElementDefinition{Path: "Practitioner.telecom", Min: 0, Max: "*"},
		Slices:     makeSlice(1),
	}
	val, ok := generateRepeatedValue(required, reg, nil)
	if !ok {
		t.Fatal("generateRepeatedValue returned false for required slice")
	}
	assertPhone(t, val)

	// Optional slice with nil RNG: reached through the sliced fallback path.
	optional := &model.ElementNode{
		Name:       "telecom",
		Path:       "Practitioner.telecom",
		Definition: &model.ElementDefinition{Path: "Practitioner.telecom", Min: 0, Max: "*"},
		Slices:     makeSlice(0),
	}
	val, ok = generateRepeatedValue(optional, reg, nil)
	if !ok {
		t.Fatal("generateRepeatedValue returned false for optional slice")
	}
	assertPhone(t, val)
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// TestRepeatedValueRespectsParentMax verifies that generateRepeatedValue never
// emits more values than the parent element's Max cardinality allows, even when
// several optional slices could each contribute a value. The HCPD
// Practitioner.qualification.identifier element has Max "1" with two optional
// slices (ahpraregistrationnumber, peakbodyregistrationnumber); generating both
// violates the parent's max and a conformant server rejects the resource
// ("max allowed = 1, but found 2").
func TestRepeatedValueRespectsParentMax(t *testing.T) {
	makeSlice := func(name string) *model.SliceNode {
		return &model.SliceNode{
			Name: name,
			Definition: &model.ElementDefinition{
				Path:      "Practitioner.qualification.identifier",
				SliceName: name,
				Min:       0,
				Max:       "1",
				Types:     []model.ElementType{{Code: "Identifier"}},
			},
			Children: map[string]*model.ElementNode{
				"system": {Name: "system", Path: "Practitioner.qualification.identifier.system", Definition: &model.ElementDefinition{Path: "Practitioner.qualification.identifier.system", Min: 1, Max: "1", Fixed: "http://example.org/" + name}},
			},
		}
	}
	// Parent element with Max "1" and two optional slices.
	node := &model.ElementNode{
		Name:       "identifier",
		Path:       "Practitioner.qualification.identifier",
		Definition: &model.ElementDefinition{Path: "Practitioner.qualification.identifier", Min: 0, Max: "1"},
		Slices: map[string]*model.SliceNode{
			"ahpraregistrationnumber":    makeSlice("ahpra"),
			"peakbodyregistrationnumber": makeSlice("pbprn"),
		},
	}
	reg := registry.New()
	// With an RNG, both optional slices may be selected; the total must never
	// exceed the parent's Max of 1.
	for i := 0; i < 200; i++ {
		rng := newRNG("seed-" + strconv.Itoa(i))
		val, ok := generateRepeatedValue(node, reg, rng)
		if !ok {
			continue
		}
		arr := val.([]any)
		if len(arr) > 1 {
			t.Fatalf("generateRepeatedValue emitted %d values for a Max=1 element: %v", len(arr), arr)
		}
	}
}

// TestGenerateSingleValueMissingDatatypes verifies that the previously
// fall-through datatypes (decimal, time, Quantity, Ratio, Range, Attachment)
// are now explicitly synthesized.
func TestGenerateSingleValueMissingDatatypes(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/observation", Type: "Observation", Elements: []model.ElementDefinition{
		{Path: "Observation", Min: 0, Max: "*"},
		{Path: "Observation.valueDecimal", Min: 0, Max: "1", Types: []model.ElementType{{Code: "decimal"}}},
		{Path: "Observation.effectiveTime", Min: 0, Max: "1", Types: []model.ElementType{{Code: "time"}}},
		{Path: "Observation.valueQuantity", Min: 0, Max: "1", Types: []model.ElementType{{Code: "Quantity"}}},
		{Path: "Observation.valueRatio", Min: 0, Max: "1", Types: []model.ElementType{{Code: "Ratio"}}},
		{Path: "Observation.valueRange", Min: 0, Max: "1", Types: []model.ElementType{{Code: "Range"}}},
		{Path: "Observation.attachment", Min: 0, Max: "1", Types: []model.ElementType{{Code: "Attachment"}}},
	}})

	cases := []struct {
		path string
		want func(any) bool
	}{
		{"Observation.valueDecimal", func(v any) bool { _, ok := v.(float64); return ok }},
		{"Observation.effectiveTime", func(v any) bool { _, ok := v.(string); return ok }},
		{"Observation.valueQuantity", func(v any) bool { _, ok := v.(map[string]any); return ok }},
		{"Observation.valueRatio", func(v any) bool { _, ok := v.(map[string]any); return ok }},
		{"Observation.valueRange", func(v any) bool { _, ok := v.(map[string]any); return ok }},
		{"Observation.attachment", func(v any) bool { _, ok := v.(map[string]any); return ok }},
	}
	for _, c := range cases {
		node := &model.ElementNode{Path: c.path, Definition: &model.ElementDefinition{Path: c.path, Types: []model.ElementType{{Code: leafTypeOf(c.path)}}}}
		val, ok := generateSingleValue(node, reg)
		if !ok || !c.want(val) {
			t.Errorf("generateSingleValue(%s) = %v ok=%v, want synthesized %T", c.path, val, ok, val)
		}
	}
}

func leafTypeOf(path string) string {
	switch {
	case strings.HasSuffix(path, "valueDecimal"):
		return "decimal"
	case strings.HasSuffix(path, "effectiveTime"):
		return "time"
	case strings.HasSuffix(path, "valueQuantity"):
		return "Quantity"
	case strings.HasSuffix(path, "valueRatio"):
		return "Ratio"
	case strings.HasSuffix(path, "valueRange"):
		return "Range"
	case strings.HasSuffix(path, "attachment"):
		return "Attachment"
	}
	return ""
}
