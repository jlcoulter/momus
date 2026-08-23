package generation

import (
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

func TestReferenceResourceType(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Patient/p1", "Patient"},
		{"  Organization/o1  ", "Organization"},
		{"practitioner/p2", "practitioner"},
		{"", ""},
		{"no-slash", ""},
		{"/leading", ""},
	}
	for _, c := range cases {
		if got := referenceResourceType(c.in); got != c.want {
			t.Errorf("referenceResourceType(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSampleStringValue(t *testing.T) {
	if got := sampleStringValue("Patient.name"); got != "Name" {
		t.Fatalf("sampleStringValue = %q, want Name", got)
	}
	if got := sampleStringValue("patient-name"); got != "Patient Name" {
		t.Fatalf("sampleStringValue = %q, want Patient Name", got)
	}
	if got := sampleStringValue(""); got != "Value" {
		t.Fatalf("sampleStringValue(empty) = %q, want Value", got)
	}
}

func TestSampleCodeValue(t *testing.T) {
	if got := sampleCodeValue("Patient.status"); got != "status" {
		t.Fatalf("sampleCodeValue = %q, want status", got)
	}
	if got := sampleCodeValue(""); got != "momus-id" {
		t.Fatalf("sampleCodeValue(empty) = %q, want mus-id", got)
	}
}

func TestDeterministicUUID(t *testing.T) {
	u := deterministicUUID("seed")
	if len(u) != 36 || u[8] != '-' || u[13] != '-' || u[18] != '-' || u[23] != '-' {
		t.Fatalf("deterministicUUID = %q, not a UUID shape", u)
	}
	// Deterministic.
	if deterministicUUID("seed") != u {
		t.Fatal("deterministicUUID is not deterministic")
	}
	if deterministicUUID("") == "" {
		t.Fatal("deterministicUUID(empty) should not be empty")
	}
}

func TestNormalizeCanonical(t *testing.T) {
	if got := normalizeCanonical("http://x/profile|4.0.1"); got != "http://x/profile" {
		t.Fatalf("normalizeCanonical = %q", got)
	}
	if got := normalizeCanonical("http://x/profile#frag"); got != "http://x/profile" {
		t.Fatalf("normalizeCanonical fragment = %q", got)
	}
	if got := normalizeCanonical("  http://x  "); got != "http://x" {
		t.Fatalf("normalizeCanonical trim = %q", got)
	}
}

func TestResolveTargetResourceType(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/Organization", Type: "Organization"})
	if got := resolveTargetResourceType("http://example.org/StructureDefinition/Organization|4.0.1", reg); got != "Organization" {
		t.Fatalf("resolveTargetResourceType = %q, want Organization", got)
	}
	// No registry -> falls back to canonical parsing.
	if got := resolveTargetResourceType("http://x/StructureDefinition/Patient", nil); got != "Patient" {
		t.Fatalf("resolveTargetResourceType(nil reg) = %q, want Patient", got)
	}
}

func TestSplitResourcePath(t *testing.T) {
	rt, ep := splitResourcePath("PractitionerRole.code")
	if rt != "PractitionerRole" || ep != "code" {
		t.Fatalf("splitResourcePath = %q, %q", rt, ep)
	}
	if rt, ep := splitResourcePath("no-dot"); rt != "" || ep != "" {
		t.Fatalf("splitResourcePath(no-dot) = %q, %q", rt, ep)
	}
}

func TestHasProfile(t *testing.T) {
	profiles := []string{"http://a/profile", "http://b/profile|2.0"}
	if !hasProfile(profiles, "http://a/profile") {
		t.Fatal("hasProfile should match exact")
	}
	if !hasProfile(profiles, "http://b/profile") {
		t.Fatal("hasProfile should strip version and match")
	}
	if hasProfile(profiles, "") || hasProfile(profiles, "http://missing") {
		t.Fatal("hasProfile should reject empty/missing")
	}
}

func TestCloneMap(t *testing.T) {
	if cloneMap(nil) != nil {
		t.Fatal("cloneMap(nil) should be nil")
	}
	m := map[string]any{"a": "x"}
	c := cloneMap(m)
	if c["a"] != "x" {
		t.Fatalf("cloneMap = %v", c)
	}
	m["a"] = "changed"
	if c["a"] != "x" {
		t.Fatal("cloneMap must be a deep-ish copy (shallow top-level)")
	}
}

func TestPrimaryTypeCode(t *testing.T) {
	if got := primaryTypeCode(&model.ElementDefinition{Types: []model.ElementType{{Code: "string"}}}); got != "string" {
		t.Fatalf("primaryTypeCode = %q", got)
	}
	if got := primaryTypeCode(&model.ElementDefinition{}); got != "" {
		t.Fatalf("primaryTypeCode(empty) = %q", got)
	}
	if got := primaryTypeCode(nil); got != "" {
		t.Fatalf("primaryTypeCode(nil) = %q", got)
	}
}

func TestReferencePlaceholder(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/Patient", Type: "Patient"})
	def := &model.ElementDefinition{TargetProfile: []string{"http://example.org/StructureDefinition/Patient"}}
	if got := referencePlaceholder(def, reg); got != "Patient/momus-setup-patient" {
		t.Fatalf("referencePlaceholder = %q", got)
	}
	// No concrete target -> Organization fallback.
	if got := referencePlaceholder(nil, reg); got != "Organization/momus-setup-organization" {
		t.Fatalf("referencePlaceholder(nil) = %q", got)
	}
}

func TestFirstTargetResourceType(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/Organization", Type: "Organization"})
	def := &model.ElementDefinition{Types: []model.ElementType{{Code: "Reference", TargetProfile: []string{"http://example.org/StructureDefinition/Organization"}}}}
	if got := firstTargetResourceType(def, reg); got != "Organization" {
		t.Fatalf("firstTargetResourceType = %q", got)
	}
	if got := firstTargetResourceType(nil, reg); got != "" {
		t.Fatalf("firstTargetResourceType(nil) = %q", got)
	}
}

func TestSortedSliceNames(t *testing.T) {
	names := sortedSliceNames(map[string]*model.SliceNode{"b": {}, "a": {}, "c": {}})
	if len(names) != 3 || names[0] != "a" || names[2] != "c" {
		t.Fatalf("sortedSliceNames = %v", names)
	}
}

func TestMergePatternWithBindingCoverage(t *testing.T) {
	binding := generatedCoding{System: "http://sys", Code: "c", Display: "D"}
	// Coding merge with missing system/code/display.
	out, ok := mergePatternWithBinding(map[string]any{}, "Coding", binding, true)
	if !ok {
		t.Fatal("expected merged Coding")
	}
	m := out.(map[string]any)
	if m["system"] != "http://sys" || m["code"] != "c" || m["display"] != "D" {
		t.Fatalf("merged Coding = %v", m)
	}
	// Existing fields preserved.
	out, ok = mergePatternWithBinding(map[string]any{"system": "http://old"}, "Coding", binding, true)
	if out.(map[string]any)["system"] != "http://old" {
		t.Fatal("existing system should be preserved")
	}
}

func TestAttachDependencyReferences(t *testing.T) {
	body := map[string]any{}
	attachDependencyReferences(body, "Appointment", "http://x", []string{"Patient"}, nil)
	if _, ok := body["participant"]; !ok {
		t.Fatalf("Appointment should get participant: %v", body)
	}

	body = map[string]any{}
	attachDependencyReferences(body, "AllergyIntolerance", "http://x", []string{"Patient"}, nil)
	if body["patient"] == nil {
		t.Fatalf("AllergyIntolerance should get patient: %v", body)
	}

	body = map[string]any{}
	attachDependencyReferences(body, "Immunization", "http://x", []string{"Encounter", "Observation"}, nil)
	if body["encounter"] == nil || body["result"] == nil {
		t.Fatalf("expected encounter and result refs: %v", body)
	}
}

func TestExtensionValueCoding(t *testing.T) {
	if _, ok := extensionValueCoding(nil); ok {
		t.Fatal("extensionValueCoding(nil) should be false")
	}
	ext := map[string]any{"valueCodeableConcept": map[string]any{"coding": []any{map[string]any{"code": "x"}}}}
	c, ok := extensionValueCoding(ext)
	if !ok || c.Code != "x" {
		t.Fatalf("extensionValueCoding = %+v ok=%v", c, ok)
	}
	ext = map[string]any{"valueCoding": map[string]any{"code": "y"}}
	c, ok = extensionValueCoding(ext)
	if !ok || c.Code != "y" {
		t.Fatalf("extensionValueCoding(valueCoding) = %+v ok=%v", c, ok)
	}
	if _, ok := extensionValueCoding(map[string]any{}); ok {
		t.Fatal("extensionValueCoding(empty) should be false")
	}
}

func TestFindCodeSystemConceptByCode(t *testing.T) {
	concepts := []model.CodeSystemConcept{
		{Code: "parent", Concepts: []model.CodeSystemConcept{{Code: "child"}}},
	}
	if got := findCodeSystemConceptByCode(concepts, "child"); got == nil || got.Code != "child" {
		t.Fatal("should find nested concept")
	}
	if got := findCodeSystemConceptByCode(concepts, "missing"); got != nil {
		t.Fatal("should not find missing concept")
	}
}

func TestApplySliceElementConstraint(t *testing.T) {
	// nil guards.
	applySliceElementConstraint(nil, nil)
	// Fixed map applies nested merge.
	slice := &model.SliceNode{Definition: &model.ElementDefinition{Fixed: map[string]any{"code": map[string]any{"coding": []any{}}}}}
	value := map[string]any{}
	applySliceElementConstraint(value, slice)
	if value["code"] == nil {
		t.Fatalf("fixed map not applied: %v", value)
	}
	// Pattern scalar.
	slice = &model.SliceNode{Definition: &model.ElementDefinition{Pattern: map[string]any{"type": "physical"}}}
	value = map[string]any{}
	applySliceElementConstraint(value, slice)
	if value["type"] != "physical" {
		t.Fatalf("pattern scalar not applied: %v", value)
	}
}

func TestMergeCodingArrayAndMap(t *testing.T) {
	binding := generatedCoding{System: "http://sys", Code: "c", Display: "D"}
	// Existing codings array -> merge each, fill missing fields.
	merged := mergeCodingArray([]any{map[string]any{"code": "existing"}}, binding)
	if len(merged) != 1 {
		t.Fatalf("mergeCodingArray = %v", merged)
	}
	m := merged[0].(map[string]any)
	if m["code"] != "existing" || m["system"] != "http://sys" {
		t.Fatalf("merged coding = %v", m)
	}
	// Non-array existing -> binding map.
	merged = mergeCodingArray("not-array", binding)
	if len(merged) != 1 || merged[0].(map[string]any)["code"] != "c" {
		t.Fatalf("mergeCodingArray(non-array) = %v", merged)
	}
	// Empty binding.
	if merged := mergeCodingArray(nil, generatedCoding{}); merged != nil {
		t.Fatalf("mergeCodingArray(empty binding) = %v", merged)
	}
	// mergeCodingMap nil.
	if got := mergeCodingMap(nil, binding); got["code"] != "c" {
		t.Fatalf("mergeCodingMap(nil) = %v", got)
	}
}

func TestFirstCodingInValueAndCodingFromMap(t *testing.T) {
	// CodeableConcept with coding.
	v := map[string]any{"coding": []any{map[string]any{"code": "c", "system": "s"}}}
	c, ok := firstCodingInValue(v)
	if !ok || c.Code != "c" {
		t.Fatalf("firstCodingInValue = %+v, %v", c, ok)
	}
	// Array.
	if _, ok := firstCodingInValue([]any{"nope", map[string]any{"code": "x"}}); !ok {
		t.Fatal("firstCodingInValue(array) should find a coding")
	}
	// Empty / placeholder.
	if _, ok := firstCodingInValue(map[string]any{"coding": []any{}}); ok {
		t.Fatal("empty coding array should be false")
	}
	// codingFromMap with placeholder code.
	if _, ok := codingFromMap(map[string]any{"code": "XX"}); ok {
		t.Fatal("placeholder code should not be meaningful")
	}
}

func TestCodingAtPath(t *testing.T) {
	raw := map[string]any{
		"communication": []any{map[string]any{"coding": []any{map[string]any{"code": "it", "system": "urn:ietf:bcp:47"}}}},
	}
	c, ok := codingAtPath(raw, "communication.coding")
	if !ok || c.Code != "it" {
		t.Fatalf("codingAtPath = %+v, %v", c, ok)
	}
	if _, ok := codingAtPath(raw, "missing.path"); ok {
		t.Fatal("codingAtPath(missing) should be false")
	}
	if _, ok := codingAtPath(nil, "x"); ok {
		t.Fatal("codingAtPath(nil) should be false")
	}
	if _, ok := codingAtPath(raw, ""); ok {
		t.Fatal("codingAtPath(empty path) should be false")
	}
}

func TestFirstExpansionCodingAndFirstCodeSystemConcept(t *testing.T) {
	// Expansion with a placeholder then a real code.
	entries := []model.ValueSetExpansionContains{
		{Code: "XX", Contains: []model.ValueSetExpansionContains{{Code: "RI", Display: "RI"}}},
	}
	c, ok := firstExpansionCoding(entries)
	if !ok || c.Code != "RI" {
		t.Fatalf("firstExpansionCoding = %+v, %v", c, ok)
	}
	concepts := []model.CodeSystemConcept{
		{Code: "XX", Concepts: []model.CodeSystemConcept{{Code: "real", Display: "Real"}}},
	}
	concept, ok := firstCodeSystemConcept(concepts)
	if !ok || concept.Code != "real" {
		t.Fatalf("firstCodeSystemConcept = %+v, %v", concept, ok)
	}
}

func TestEnsurePractitionerRoleAndHealthcareServiceIdentifierMatchers(t *testing.T) {
	// Known type matches.
	identifier := map[string]any{"type": map[string]any{"coding": []any{map[string]any{"system": "http://terminology.hl7.org.au/CodeSystem/v2-0203", "code": "UPIN"}}}}
	if !identifierMatchesPractitionerRoleKnownType(identifier) {
		t.Fatal("UPIN should match practitioner role known type")
	}
	// NOI matches the healthcare service known type.
	noi := map[string]any{"type": map[string]any{"coding": []any{map[string]any{"system": "http://terminology.hl7.org.au/CodeSystem/v2-0203", "code": "NOI"}}}}
	if !identifierMatchesHealthcareServiceKnownType(noi) {
		t.Fatal("NOI should match healthcare service known type")
	}
	// Wrong system.
	bad := map[string]any{"type": map[string]any{"coding": []any{map[string]any{"system": "http://other", "code": "UPIN"}}}}
	if identifierMatchesPractitionerRoleKnownType(bad) {
		t.Fatal("wrong system should not match")
	}
	// Missing type.
	if identifierMatchesPractitionerRoleKnownType(map[string]any{}) {
		t.Fatal("missing type should not match")
	}
}

func TestEnsurePractitionerRoleAddsWhenMissing(t *testing.T) {
	body := map[string]any{}
	ensurePractitionerRoleKnownIdentifier(body)
	ids := body["identifier"].([]any)
	if len(ids) != 1 {
		t.Fatalf("identifier count = %d, want 1", len(ids))
	}
	// Non-array identifier is replaced.
	body = map[string]any{"identifier": "scalar"}
	ensurePractitionerRoleKnownIdentifier(body)
	if ids := body["identifier"].([]any); len(ids) != 1 {
		t.Fatalf("non-array identifier not replaced: %v", body["identifier"])
	}
}

func TestNormalizeHealthcareServiceTypeCodingAndReferenceResourceType(t *testing.T) {
	// A CodeableConcept with a text but no coding gets a coding.
	cc := map[string]any{"text": "Service Type"}
	arr := []any{cc}
	body := map[string]any{"type": arr}
	_ = body
	// normalizeHealthcareServiceTypeCoding iterates body["type"].
	body = map[string]any{"type": []any{map[string]any{"text": "Service Type"}}}
	normalizeHealthcareServiceTypeCoding(body)
	types := body["type"].([]any)
	first := types[0].(map[string]any)
	if first["coding"] == nil {
		t.Fatalf("expected coding populated: %v", first)
	}
	// A concept with an existing coding is left alone.
	body = map[string]any{"type": []any{map[string]any{"coding": []any{map[string]any{"code": "x"}}}}}
	normalizeHealthcareServiceTypeCoding(body)
	// referenceResourceType.
	if got := referenceResourceType("Patient/p1"); got != "Patient" {
		t.Fatalf("referenceResourceType = %q", got)
	}
}

func TestIsMeaningfulCodingPlaceholders(t *testing.T) {
	for _, code := range []string{"XX", "UNK", "NULL", "_Abstract"} {
		if isMeaningfulCoding(code, "") {
			t.Fatalf("placeholder %q should not be meaningful", code)
		}
	}
	if !isMeaningfulCoding("real-code", "Display") {
		t.Fatal("real code should be meaningful")
	}
}

func TestEnsureEndpointKnownIdentifier(t *testing.T) {
	body := map[string]any{}
	ensureEndpointKnownIdentifier(body)
	ids := body["identifier"].([]any)
	if len(ids) != 1 {
		t.Fatalf("endpoint identifier count = %d, want 1", len(ids))
	}
	// Already has an identifier -> a new one appended.
	body = map[string]any{"identifier": []any{map[string]any{"system": "http://example.org", "value": "x"}}}
	ensureEndpointKnownIdentifier(body)
	if ids := body["identifier"].([]any); len(ids) != 2 {
		t.Fatalf("endpoint identifier count = %d, want 2", len(ids))
	}
}
