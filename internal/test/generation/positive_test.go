package generation

import (
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
	"github.com/jlcoulter/momus/internal/test/ast"
	"github.com/jlcoulter/momus/internal/test/coverage"
)

func TestGenerateFromCoveragePlanBuildsPerRequirementSequence(t *testing.T) {
	plan, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{
		Requirements: []coverage.CoverageRequirement{
			{ID: "req-1", ProfileURL: "http://example.org/StructureDefinition/patient", ResourceType: "Patient", ElementPath: "Patient.name", Variant: coverage.CoverageVariantValidMin, Min: 1, Max: "*"},
			{ID: "req-2", ProfileURL: "http://example.org/StructureDefinition/patient", ResourceType: "Patient", ElementPath: "Patient.name", Variant: coverage.CoverageVariantMissingRequired, Min: 1, Max: "*"},
		},
	}, BuildOptions{BaseURL: "http://localhost:8080/fhir"})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}
	if plan.Version != "v1" {
		t.Fatalf("got version %q, want v1", plan.Version)
	}
	root := plan.Root.(*ast.Sequence)
	resourceSeq := root.Steps[0].(*ast.Sequence)
	setupReq := resourceSeq.Steps[0].(*ast.Request)
	if setupReq.Method != "PUT" {
		t.Fatalf("got method %q, want PUT", setupReq.Method)
	}
	if setupReq.URL != "http://localhost:8080/fhir/Patient/momus-setup-patient" {
		t.Fatalf("got URL %q, want setup patient URL", setupReq.URL)
	}
	setupBody := setupReq.Body.(map[string]any)
	if setupBody["id"] != "momus-setup-patient" {
		t.Fatalf("got setup id %v, want momus-setup-patient", setupBody["id"])
	}
	setupMeta := setupBody["meta"].(map[string]any)
	setupProfiles := setupMeta["profile"].([]any)
	if len(setupProfiles) != 1 || setupProfiles[0] != "http://example.org/StructureDefinition/patient" {
		t.Fatalf("got setup meta.profile %v, want patient profile", setupMeta["profile"])
	}
	case0 := resourceSeq.Steps[3].(*ast.Sequence)
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
	case1 := resourceSeq.Steps[4].(*ast.Sequence)
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
	setupReq := obsResourceSeq.Steps[0].(*ast.Request)
	body := setupReq.Body.(map[string]any)
	meta := body["meta"].(map[string]any)
	profiles := meta["profile"].([]any)
	if len(profiles) != 1 || profiles[0] != "http://example.org/StructureDefinition/observation" {
		t.Fatalf("got setup meta.profile %v, want observation profile", meta["profile"])
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
	caseSeq := resourceSeq.Steps[3].(*ast.Sequence)
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
	caseSeq := resourceSeq.Steps[3].(*ast.Sequence)
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
	caseSeq := resourceSeq.Steps[3].(*ast.Sequence)
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
	caseSeq := resourceSeq.Steps[3].(*ast.Sequence)
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
	caseSeq := resourceSeq.Steps[3].(*ast.Sequence)
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
	caseSeq := resourceSeq.Steps[3].(*ast.Sequence)
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
	caseSeq := resourceSeq.Steps[3].(*ast.Sequence)
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
	caseSeq := resourceSeq.Steps[3].(*ast.Sequence)
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
	caseSeq := resourceSeq.Steps[3].(*ast.Sequence)
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
	plan, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{
		Requirements: []coverage.CoverageRequirement{
			{ID: "d-valid", ProfileURL: "http://example.org/StructureDefinition/observation", ResourceType: "Observation", ElementPath: "Observation.value", Variant: coverage.CoverageVariantDatatypeValid},
			{ID: "d-invalid", ProfileURL: "http://example.org/StructureDefinition/observation", ResourceType: "Observation", ElementPath: "Observation.value", Variant: coverage.CoverageVariantDatatypeInvalidLexical},
		},
	}, BuildOptions{BaseURL: "http://localhost:8080/fhir"})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan returned error: %v", err)
	}
	if got := RequirementCount(plan); got != 2 {
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
	plan, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{
		Requirements: []coverage.CoverageRequirement{
			{ID: "req|obs|datatype-invalid-lexical", ConstraintID: "profile|Observation.value|datatype|date", ProfileURL: "http://example.org/StructureDefinition/observation", ResourceType: "Observation", ElementPath: "Observation.value", Domain: coverage.CoverageDomainDatatype, Variant: coverage.CoverageVariantDatatypeInvalidLexical},
		},
	}, BuildOptions{BaseURL: "http://localhost:8080/fhir"})
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
