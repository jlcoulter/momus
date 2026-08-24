package generation

import (
	"strings"
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
	// Non-array identifier is replaced.
	body = map[string]any{"identifier": "scalar"}
	ensureEndpointKnownIdentifier(body)
	if _, ok := body["identifier"].([]any); !ok {
		t.Fatalf("non-array endpoint identifier not replaced: %v", body["identifier"])
	}
}

func TestEnsureHealthcareServiceKnownIdentifierBranches(t *testing.T) {
	// Missing identifier -> added.
	body := map[string]any{}
	ensureHealthcareServiceKnownIdentifier(body)
	if ids := body["identifier"].([]any); len(ids) != 1 {
		t.Fatalf("missing identifier count = %d, want 1", len(ids))
	}
	// Non-array identifier -> replaced.
	body = map[string]any{"identifier": "scalar"}
	ensureHealthcareServiceKnownIdentifier(body)
	if ids := body["identifier"].([]any); len(ids) != 1 {
		t.Fatalf("non-array identifier count = %d, want 1", len(ids))
	}
	// Array with an already-known identifier -> not appended.
	known := map[string]any{"system": "http://ns.electronichealth.net.au/id/hi/hpio/1.0", "type": map[string]any{"coding": []any{map[string]any{"system": "http://terminology.hl7.org.au/CodeSystem/v2-0203", "code": "NOI"}}}}
	body = map[string]any{"identifier": []any{known}}
	ensureHealthcareServiceKnownIdentifier(body)
	if ids := body["identifier"].([]any); len(ids) != 1 {
		t.Fatalf("known identifier count = %d, want 1 (no duplicate)", len(ids))
	}
	// Array without a known identifier -> appended.
	body = map[string]any{"identifier": []any{map[string]any{"system": "http://other", "value": "x"}}}
	ensureHealthcareServiceKnownIdentifier(body)
	if ids := body["identifier"].([]any); len(ids) != 2 {
		t.Fatalf("unknown identifier count = %d, want 2", len(ids))
	}
}

func TestReferenceIs(t *testing.T) {
	if !referenceIs(map[string]any{"reference": "Patient/p1"}, "Patient/p1") {
		t.Fatal("referenceIs should match")
	}
	if referenceIs(nil, "x") || referenceIs(map[string]any{}, "x") {
		t.Fatal("referenceIs should be false for nil/empty")
	}
	if referenceIs(map[string]any{"reference": "Patient/p1"}, "Patient/p2") {
		t.Fatal("referenceIs should not match different ref")
	}
}

func TestStripSelfReferences(t *testing.T) {
	// Object-valued self reference removed.
	body := map[string]any{"subject": map[string]any{"reference": "Patient/p1"}}
	stripSelfReferences(body, "Patient/p1")
	if _, ok := body["subject"]; ok {
		t.Fatalf("self-referencing object not removed: %v", body)
	}
	// Array self reference filtered.
	body = map[string]any{"author": []any{map[string]any{"reference": "Patient/p1"}, map[string]any{"reference": "Practitioner/p2"}}}
	stripSelfReferences(body, "Patient/p1")
	if len(body["author"].([]any)) != 1 {
		t.Fatalf("self-referencing array element not removed: %v", body["author"])
	}
	// Empty selfRef is a no-op.
	body = map[string]any{"subject": map[string]any{"reference": "x"}}
	stripSelfReferences(body, "")
	if body["subject"] == nil {
		t.Fatal("empty selfRef should not strip")
	}
}

func TestFilterSelfReferences(t *testing.T) {
	arr := []any{map[string]any{"reference": "Patient/p1"}, "not-a-ref", map[string]any{"reference": "Patient/p2"}}
	out := filterSelfReferences(arr, "Patient/p1")
	if len(out) != 2 {
		t.Fatalf("filterSelfReferences = %v", out)
	}
}

func TestUpperCamelTypeName(t *testing.T) {
	if got := upperCamelTypeName("dateTime"); got != "DateTime" {
		t.Fatalf("upperCamelTypeName(dateTime) = %q", got)
	}
	if got := upperCamelTypeName("HumanName"); got != "HumanName" {
		t.Fatalf("upperCamelTypeName(HumanName) = %q", got)
	}
	if got := upperCamelTypeName(""); got != "" {
		t.Fatalf("upperCamelTypeName(empty) = %q", got)
	}
}

func TestHasRequiredSlices(t *testing.T) {
	if hasRequiredSlices(nil) {
		t.Fatal("hasRequiredSlices(nil) should be false")
	}
	node := &model.ElementNode{Slices: map[string]*model.SliceNode{
		"s": {Definition: &model.ElementDefinition{Min: 1}},
	}}
	if !hasRequiredSlices(node) {
		t.Fatal("hasRequiredSlices should detect required slice")
	}
	if hasRequiredSlices(&model.ElementNode{Slices: map[string]*model.SliceNode{"s": {Definition: &model.ElementDefinition{Min: 0}}}}) {
		t.Fatal("optional slice should not be required")
	}
}

func TestPrefersContractValueAndHasContractSignal(t *testing.T) {
	// Nil.
	if prefersContractValue(nil) || hasContractSignal(nil) {
		t.Fatal("nil node should have no contract signal")
	}
	// Fixed value.
	node := &model.ElementNode{Definition: &model.ElementDefinition{Fixed: "x"}}
	if !hasContractSignal(node) || !prefersContractValue(node) {
		t.Fatal("fixed value should signal contract")
	}
	// Binding.
	node = &model.ElementNode{Definition: &model.ElementDefinition{Binding: &model.Binding{}}}
	if !hasContractSignal(node) {
		t.Fatal("binding should signal contract")
	}
	// Example.
	node = &model.ElementNode{Definition: &model.ElementDefinition{Examples: []any{"x"}}}
	if !hasContractSignal(node) {
		t.Fatal("example should signal contract")
	}
	// No signal.
	node = &model.ElementNode{Definition: &model.ElementDefinition{}}
	if hasContractSignal(node) {
		t.Fatal("empty definition should have no contract signal")
	}
}

func TestHasProfileTypes(t *testing.T) {
	if !hasProfileTypes(&model.ElementDefinition{Types: []model.ElementType{{Profile: []string{"http://x"}}}}) {
		t.Fatal("profile types should be detected")
	}
	if hasProfileTypes(nil) || hasProfileTypes(&model.ElementDefinition{Types: []model.ElementType{{Code: "string"}}}) {
		t.Fatal("no profile types should be false")
	}
}

func TestPropertyNameForNode(t *testing.T) {
	// Non-choice name.
	node := &model.ElementNode{Name: "subject", Definition: &model.ElementDefinition{Types: []model.ElementType{{Code: "Reference"}}}}
	if got := propertyNameForNode(node); got != "subject" {
		t.Fatalf("propertyNameForNode = %q", got)
	}
	// Choice name with a type.
	node = &model.ElementNode{Name: "value[x]", Definition: &model.ElementDefinition{Types: []model.ElementType{{Code: "Coding"}}}}
	if got := propertyNameForNode(node); got != "valueCoding" {
		t.Fatalf("propertyNameForNode(choice) = %q", got)
	}
	// Choice name with Element type falls back to slices.
	node = &model.ElementNode{Name: "value[x]", Definition: &model.ElementDefinition{Types: []model.ElementType{{Code: "Element"}}}, Slices: map[string]*model.SliceNode{"v": {Definition: &model.ElementDefinition{Types: []model.ElementType{{Code: "string"}}, Min: 1}}}}
	if got := propertyNameForNode(node); got != "valueString" {
		t.Fatalf("propertyNameForNode(slice) = %q", got)
	}
	// Choice with no resolvable type.
	node = &model.ElementNode{Name: "value[x]", Definition: &model.ElementDefinition{}}
	if got := propertyNameForNode(node); got != "value" {
		t.Fatalf("propertyNameForNode(no type) = %q", got)
	}
}

func TestChoiceTypeFromSlices(t *testing.T) {
	if got := choiceTypeFromSlices(map[string]*model.SliceNode{"a": {Definition: &model.ElementDefinition{Types: []model.ElementType{{Code: "string"}}, Min: 1}}}); got != "string" {
		t.Fatalf("choiceTypeFromSlices = %q", got)
	}
	if got := choiceTypeFromSlices(map[string]*model.SliceNode{"a": {Definition: &model.ElementDefinition{Min: 1}}}); got != "" {
		t.Fatalf("choiceTypeFromSlices(no type) = %q", got)
	}
	// An empty-name slice with Min 0 is skipped.
	if got := choiceTypeFromSlices(map[string]*model.SliceNode{"": {Definition: &model.ElementDefinition{Types: []model.ElementType{{Code: "string"}}, Min: 0}}}); got != "" {
		t.Fatalf("choiceTypeFromSlices(empty-name optional) = %q", got)
	}
	// A nil slice is skipped.
	if got := choiceTypeFromSlices(map[string]*model.SliceNode{"a": nil}); got != "" {
		t.Fatalf("choiceTypeFromSlices(nil slice) = %q", got)
	}
}

func TestElementAllowsMultiple(t *testing.T) {
	if elementAllowsMultiple(nil) {
		t.Fatal("elementAllowsMultiple(nil) should be false")
	}
	if !elementAllowsMultiple(&model.ElementDefinition{Max: "*"}) {
		t.Fatal("Max=* should allow multiple")
	}
	if !elementAllowsMultiple(&model.ElementDefinition{Max: "2"}) {
		t.Fatal("Max=2 should allow multiple")
	}
	if elementAllowsMultiple(&model.ElementDefinition{Max: "1"}) {
		t.Fatal("Max=1 should not allow multiple")
	}
	// Falls back to BaseMax.
	if !elementAllowsMultiple(&model.ElementDefinition{Max: "1", BaseMax: "*"}) {
		t.Fatal("BaseMax=* should allow multiple")
	}
}

func TestSliceExtensionRootAndFindSliceValueXBranches(t *testing.T) {
	reg := registry.New()
	// A simple extension profile.
	reg.AddStructureDefinition(&model.StructureDefinition{
		URL: "http://example.org/StructureDefinition/simple-ext", Type: "Extension",
		Elements: []model.ElementDefinition{
			{Path: "Extension", Min: 0, Max: "1"},
			{Path: "Extension.url", Min: 1, Max: "1"},
			{Path: "Extension.value[x]", Min: 0, Max: "1", Types: []model.ElementType{{Code: "string"}}},
		},
	})
	// A complex extension (value[x] is Max 0, has sub-extensions).
	reg.AddStructureDefinition(&model.StructureDefinition{
		URL: "http://example.org/StructureDefinition/complex-ext", Type: "Extension",
		Elements: []model.ElementDefinition{
			{Path: "Extension", Min: 0, Max: "1"},
			{Path: "Extension.url", Min: 1, Max: "1"},
			{Path: "Extension.extension", Min: 1, Max: "*"},
			{Path: "Extension.value[x]", Min: 0, Max: "0"},
		},
	})

	// A slice whose definition carries its own value[x] child uses a synthetic root.
	slice := &model.SliceNode{
		Name:       "own",
		Definition: &model.ElementDefinition{Path: "x.extension", Name: "ext"},
		Children: map[string]*model.ElementNode{
			"value[x]": {Definition: &model.ElementDefinition{Path: "x.value[x]", Max: "1"}},
		},
	}
	root := sliceExtensionRoot(slice, reg)
	if root == nil {
		t.Fatal("sliceExtensionRoot(own children) should return a synthetic root")
	}
	// Complex extension: findSliceValueX returns false.
	complexSlice := &model.SliceNode{Definition: &model.ElementDefinition{
		Types: []model.ElementType{{Code: "Extension", Profile: []string{"http://example.org/StructureDefinition/complex-ext"}}},
	}}
	if _, ok := findSliceValueX(complexSlice, reg); ok {
		t.Fatal("findSliceValueX(complex) should be false")
	}
	// Nil-definition slice.
	if _, ok := findSliceValueX(&model.SliceNode{}, reg); ok {
		t.Fatal("findSliceValueX(no def) should be false")
	}
	// sliceExtensionRoot with a definition but no profile match.
	noProfile := &model.SliceNode{Definition: &model.ElementDefinition{Types: []model.ElementType{{Code: "Extension"}}}}
	if root := sliceExtensionRoot(noProfile, reg); root != nil {
		t.Fatalf("sliceExtensionRoot(no profile) = %v, want nil", root)
	}
}

func TestMergeSlicePatternRecursive(t *testing.T) {
	// A nested pattern where the existing value has a matching nested map.
	value := map[string]any{"coding": map[string]any{"system": "http://old"}}
	mergeSlicePattern(value, "coding", map[string]any{"code": "new", "system": "http://new"})
	coding := value["coding"].(map[string]any)
	if coding["code"] != "new" || coding["system"] != "http://new" {
		t.Fatalf("recursive merge = %v", coding)
	}
	// A nested pattern where the existing value is a map.
	value = map[string]any{"coding": map[string]any{"code": "keep"}}
	mergeSlicePattern(value, "coding", map[string]any{"system": "http://new"})
	coding = value["coding"].(map[string]any)
	if coding["code"] != "keep" || coding["system"] != "http://new" {
		t.Fatalf("partial merge = %v", coding)
	}
}

func TestApplySliceNodeChildren(t *testing.T) {
	// Nil guards.
	applySliceNodeChildren(nil, &model.ElementNode{}, nil)
	applySliceNodeChildren(map[string]any{}, nil, nil)
	// A node with children that have fixed/pattern constraints.
	value := map[string]any{}
	node := &model.ElementNode{Children: map[string]*model.ElementNode{
		"type": {Name: "type", Definition: &model.ElementDefinition{Fixed: "physical"}},
	}}
	applySliceNodeChildren(value, node, nil)
	if value["type"] != "physical" {
		t.Fatalf("applySliceNodeChildren = %v", value)
	}
}

func TestMod89Valid(t *testing.T) {
	// Wrong length.
	if mod89Valid("123", []int{1, 2, 3, 4}, false) {
		t.Fatal("wrong length should be invalid")
	}
	// Non-digit.
	if mod89Valid("12a", []int{1, 2, 3}, false) {
		t.Fatal("non-digit should be invalid")
	}
	// Valid ABN.
	abnWeights := []int{10, 1, 3, 5, 7, 9, 11, 13, 15, 17, 19}
	abn := generateABN()
	if !mod89Valid(abn, abnWeights, true) {
		t.Fatalf("generated ABN %q is not valid", abn)
	}
}

func TestIdentifierMatchersNonMapCoding(t *testing.T) {
	// A coding array containing a non-map element is skipped.
	identifier := map[string]any{"type": map[string]any{"coding": []any{"not-a-map"}}}
	if identifierMatchesPractitionerRoleKnownType(identifier) {
		t.Fatal("non-map coding should not match practitioner role")
	}
	if identifierMatchesHealthcareServiceKnownType(identifier) {
		t.Fatal("non-map coding should not match healthcare service")
	}
	// A non-coding type field is skipped.
	identifier = map[string]any{"type": map[string]any{"coding": "not-an-array"}}
	if identifierMatchesPractitionerRoleKnownType(identifier) {
		t.Fatal("non-array coding should not match")
	}
}

func TestEnsureRecordedSexOrGenderValue(t *testing.T) {
	// No extension array -> no-op.
	ensureRecordedSexOrGenderValue(map[string]any{})
	// An extension with the recorded URL and an existing value slice -> no change.
	existing := []any{map[string]any{"url": "value", "valueCodeableConcept": map[string]any{}}}
	body := map[string]any{"extension": []any{map[string]any{"url": recordedSexOrGenderExtensionURL, "extension": existing}}}
	ensureRecordedSexOrGenderValue(body)
	if len(body["extension"].([]any)[0].(map[string]any)["extension"].([]any)) != 1 {
		t.Fatal("existing value slice should not be duplicated")
	}
	// A non-matching extension URL is skipped.
	ensureRecordedSexOrGenderValue(map[string]any{"extension": []any{map[string]any{"url": "http://other"}}})
}

func TestNormalizePractitionerFieldsMissingName(t *testing.T) {
	// Missing name -> both official and usual slices added.
	body := map[string]any{}
	normalizePractitionerFields(body)
	if body["active"] != true {
		t.Fatal("active should be true")
	}
	names := body["name"].([]any)
	if len(names) != 2 {
		t.Fatalf("name count = %d, want 2", len(names))
	}
	// Names already present but missing uses -> appended.
	body = map[string]any{"name": []any{map[string]any{"family": "X"}}}
	normalizePractitionerFields(body)
	names = body["name"].([]any)
	if len(names) != 3 {
		t.Fatalf("name count = %d, want 3", len(names))
	}
}

func TestDerivedURIValueAndSampleCodeValue(t *testing.T) {
	// derivedURIValue returns a uuid.
	u := derivedURIValue("patient-id")
	if !strings.HasPrefix(u, "urn:uuid:") || len(u) != len("urn:uuid:")+36 {
		t.Fatalf("derivedURIValue = %q", u)
	}
	// sampleCodeValue with a leaf.
	if got := sampleCodeValue("Patient.status"); got != "status" {
		t.Fatalf("sampleCodeValue = %q", got)
	}
}

func TestIsEmptyExtensionBranches(t *testing.T) {
	// Extension with value.
	if isEmptyExtension(map[string]any{"url": "http://x", "valueString": "v"}) {
		t.Fatal("extension with value should not be empty")
	}
	// Extension with sub-extension.
	if isEmptyExtension(map[string]any{"url": "http://x", "extension": []any{map[string]any{"url": "sub"}}}) {
		t.Fatal("extension with sub-extension should not be empty")
	}
	// Empty extension (only url).
	if !isEmptyExtension(map[string]any{"url": "http://x"}) {
		t.Fatal("extension with only url should be empty")
	}
}

func TestEnsureSimpleExtensionValue(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{
		URL: "http://example.org/StructureDefinition/simple-ext", Type: "Extension",
		Elements: []model.ElementDefinition{
			{Path: "Extension", Min: 0, Max: "1"},
			{Path: "Extension.url", Min: 1, Max: "1"},
			{Path: "Extension.value[x]", Min: 0, Max: "1", Types: []model.ElementType{{Code: "string"}}},
		},
	})
	slice := &model.SliceNode{Definition: &model.ElementDefinition{
		Types: []model.ElementType{{Code: "Extension", Profile: []string{"http://example.org/StructureDefinition/simple-ext"}}},
	}}
	// Nil guards.
	ensureSimpleExtensionValue(nil, slice, reg)
	// No url -> no-op.
	ensureSimpleExtensionValue(map[string]any{}, slice, reg)
	// Existing extension sub-array -> no-op.
	ensureSimpleExtensionValue(map[string]any{"url": "u", "extension": []any{}}, slice, reg)
	// Existing value -> no-op.
	ensureSimpleExtensionValue(map[string]any{"url": "u", "valueString": "x"}, slice, reg)
	// Missing value -> sets one.
	value := map[string]any{"url": "u"}
	ensureSimpleExtensionValue(value, slice, reg)
	if _, ok := value["valueString"]; !ok {
		t.Fatalf("ensureSimpleExtensionValue did not add valueString: %v", value)
	}
}

func TestNormalizeGeneratedPayloadAndCodeableConceptMap(t *testing.T) {
	// All-fixed codings: text dropped, no display added.
	value := map[string]any{"coding": []any{map[string]any{fixedCodingKey: true, "code": "c"}}}
	normalizeCodeableConceptMap(value)
	if _, ok := value["text"]; ok {
		t.Fatal("all-fixed coding should drop text")
	}
	// Non-fixed coding with a missing display gets a sample display.
	value = map[string]any{"coding": []any{map[string]any{"code": "status"}}}
	normalizeCodeableConceptMap(value)
	if value["coding"].([]any)[0].(map[string]any)["display"] == "" {
		t.Fatal("non-fixed coding should get a display")
	}
	if value["text"] == "" {
		t.Fatal("concept should get a text from the first label")
	}
	// nil guard.
	normalizeCodeableConceptMap(nil)
	// A codeable concept with no coding array.
	normalizeCodeableConceptMap(map[string]any{})
	// normalizeGeneratedPayload with an address (AU state dropped).
	payload := map[string]any{"address": map[string]any{"line": "x", "city": "Sydney", "country": "AU", "state": "NSW"}, "name": []any{map[string]any{"text": "x"}}}
	normalizeGeneratedPayload(payload)
	addr := payload["address"].(map[string]any)
	if _, ok := addr["state"]; ok {
		t.Fatal("AU state should be dropped")
	}
}

func TestNormalizeGeneratedAddress(t *testing.T) {
	normalizeGeneratedAddress(nil)
	// Non-AU preserved.
	addr := map[string]any{"country": "US", "state": "CA"}
	normalizeGeneratedAddress(addr)
	if addr["state"] != "CA" {
		t.Fatalf("non-AU state = %v", addr["state"])
	}
	// AU dropped.
	addr = map[string]any{"country": "au", "state": "NSW"}
	normalizeGeneratedAddress(addr)
	if _, ok := addr["state"]; ok {
		t.Fatal("AU state should be dropped")
	}
}

func TestResolveBoundCodingFromExtensionValueGuards(t *testing.T) {
	if _, ok := resolveBoundCodingFromExtensionValue("", nil); ok {
		t.Fatal("resolveBoundCodingFromExtensionValue(empty) should be false")
	}
	reg := registry.New()
	if _, ok := resolveBoundCodingFromExtensionValue("http://x", reg); ok {
		t.Fatal("resolveBoundCodingFromExtensionValue(no resources) should be false")
	}
	// A nil instance is skipped.
	reg.AddResource(nil)
	if _, ok := resolveBoundCodingFromExtensionValue("http://x", reg); ok {
		t.Fatal("resolveBoundCodingFromExtensionValue(nil instances) should be false")
	}
}

func TestFindExtensionValueCoding(t *testing.T) {
	if _, ok := findExtensionValueCoding(nil, "http://x"); ok {
		t.Fatal("findExtensionValueCoding(nil) should be false")
	}
	if _, ok := findExtensionValueCoding(map[string]any{}, ""); ok {
		t.Fatal("findExtensionValueCoding(empty url) should be false")
	}
	raw := map[string]any{"extension": []any{map[string]any{
		"url":                  "http://example.org/ext",
		"valueCodeableConcept": map[string]any{"coding": []any{map[string]any{"code": "c", "system": "s"}}},
	}}}
	c, ok := findExtensionValueCoding(raw, "http://example.org/ext")
	if !ok || c.Code != "c" {
		t.Fatalf("findExtensionValueCoding = %+v, %v", c, ok)
	}
	if _, ok := findExtensionValueCoding(raw, "http://other"); ok {
		t.Fatal("findExtensionValueCoding(missing) should be false")
	}
}

func TestIsFixedCodingAndMarkFixedCoding(t *testing.T) {
	if isFixedCoding(nil) {
		t.Fatal("isFixedCoding(nil) should be false")
	}
	// Marking a CodeableConcept map strips display/text and marks the coding.
	m := map[string]any{"coding": []any{map[string]any{"code": "c", "system": "s", "display": "D"}}, "text": "T"}
	markFixedCoding(m)
	if _, ok := m["text"]; ok {
		t.Fatal("markFixedCoding should strip CodeableConcept text")
	}
	coding := m["coding"].([]any)[0].(map[string]any)
	if _, ok := coding["display"]; ok {
		t.Fatal("markFixedCoding should strip coding display")
	}
	if !isFixedCoding(coding) {
		t.Fatal("marked coding should be fixed")
	}
	// Marking a bare coding map.
	bare := map[string]any{"code": "c"}
	markFixedCoding(bare)
	if !isFixedCoding(bare) {
		t.Fatal("bare coding should be marked fixed")
	}
	// Array of codings.
	arr := []any{map[string]any{"code": "a"}, map[string]any{"code": "b"}}
	markFixedCoding(arr)
	for _, el := range arr {
		if !isFixedCoding(el.(map[string]any)) {
			t.Fatal("array coding should be marked fixed")
		}
	}
}

func TestStripFixedCodingMarkers(t *testing.T) {
	m := map[string]any{"coding": []any{map[string]any{fixedCodingKey: true}}, fixedCodingKey: true}
	stripFixedCodingMarkers(m)
	if _, ok := m[fixedCodingKey]; ok {
		t.Fatal("top-level marker not stripped")
	}
	if _, ok := m["coding"].([]any)[0].(map[string]any)[fixedCodingKey]; ok {
		t.Fatal("nested marker not stripped")
	}
}

func TestNormaliseCodingDisplay(t *testing.T) {
	reg := registry.New()
	reg.AddCodeSystem(&model.CodeSystem{URL: "http://cs", Concepts: []model.CodeSystemConcept{{Code: "C", Display: "Canonical"}}})
	// Map with a coding array.
	m := map[string]any{"coding": []any{map[string]any{"system": "http://cs", "code": "C", "display": "C"}}}
	normaliseCodingDisplay(m, reg)
	if m["coding"].([]any)[0].(map[string]any)["display"] != "Canonical" {
		t.Fatalf("normaliseCodingDisplay(map) = %v", m)
	}
	// Array of codings.
	arr := []any{map[string]any{"system": "http://cs", "code": "C", "display": "C"}}
	normaliseCodingDisplay(arr, reg)
	if arr[0].(map[string]any)["display"] != "Canonical" {
		t.Fatalf("normaliseCodingDisplay(array) = %v", arr)
	}
}

func TestNormalizeReferenceType(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/Organization", Type: "Organization", Kind: "resource", Elements: []model.ElementDefinition{{Path: "Organization", Min: 0, Max: "*"}}})
	// Nil guards.
	normalizeReferenceType(nil, nil, reg)
	// Reference-derived type.
	value := map[string]any{"reference": "Organization/o-1"}
	normalizeReferenceType(
		value,
		&model.ElementDefinition{TargetProfile: []string{"http://example.org/StructureDefinition/Organization"}},
		reg,
	)
	if value["type"] != "Organization" {
		t.Fatalf("normalizeReferenceType = %v", value)
	}
	// Abstract-only targets should not force a concrete type.
	value = map[string]any{"reference": "Organization/o-1", "type": "Organization"}
	normalizeReferenceType(value, &model.ElementDefinition{TargetProfile: []string{"http://hl7.org/fhir/StructureDefinition/Resource"}}, reg)
	if _, ok := value["type"]; ok {
		t.Fatalf("normalizeReferenceType(abstract target) = %v", value)
	}
	// Type derived from the element's target profile when the reference has no
	// parseable type.
	value = map[string]any{"reference": "urn:uuid:123"}
	def := &model.ElementDefinition{TargetProfile: []string{"http://example.org/StructureDefinition/Organization"}}
	normalizeReferenceType(value, def, reg)
	if value["type"] != "Organization" {
		t.Fatalf("normalizeReferenceType(profile) = %v", value)
	}
}

func TestNormalizeGeneratedIdentifier(t *testing.T) {
	if normalizeGeneratedIdentifier(nil); true {
		// no-op
	}
	// ABN.
	id := map[string]any{"system": "http://hl7.org.au/id/abn", "value": ""}
	normalizeGeneratedIdentifier(id)
	if id["value"] == "" {
		t.Fatal("ABN identifier should be normalized")
	}
	// ACN.
	id = map[string]any{"system": "http://hl7.org.au/id/acn", "value": ""}
	normalizeGeneratedIdentifier(id)
	if id["value"] == "" {
		t.Fatal("ACN identifier should be normalized")
	}
	// Unknown system left alone.
	id = map[string]any{"system": "http://other", "value": "x"}
	normalizeGeneratedIdentifier(id)
	if id["value"] != "x" {
		t.Fatal("unknown system should be left alone")
	}
}

func TestEnsureEndpointManagingOrganization(t *testing.T) {
	body := map[string]any{}
	ensureEndpointManagingOrganization(body)
	if body["managingOrganization"] == nil {
		t.Fatal("managingOrganization not set")
	}
	// Existing value preserved.
	body = map[string]any{"managingOrganization": map[string]any{"reference": "existing"}}
	ensureEndpointManagingOrganization(body)
	if body["managingOrganization"].(map[string]any)["reference"] != "existing" {
		t.Fatal("existing managingOrganization should be preserved")
	}
}

func TestDependencyReferenceElementName(t *testing.T) {
	// Nil registry.
	if got := dependencyReferenceElementName("Patient", "http://x", "Observation", nil); got != "" {
		t.Fatalf("dependencyReferenceElementName(nil reg) = %q", got)
	}
	// Empty dependency.
	reg := registry.New()
	if got := dependencyReferenceElementName("Patient", "http://x", "", reg); got != "" {
		t.Fatalf("dependencyReferenceElementName(empty dep) = %q", got)
	}
	// Unknown profile.
	if got := dependencyReferenceElementName("Patient", "http://missing", "Observation", reg); got != "" {
		t.Fatalf("dependencyReferenceElementName(missing) = %q", got)
	}
	// A profile that references the dependency via a Reference element.
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/patient", Type: "Patient", Elements: []model.ElementDefinition{
		{Path: "Patient", Min: 0, Max: "*"},
		{Path: "Patient.generalPractitioner", Min: 0, Max: "*", Types: []model.ElementType{{Code: "Reference", TargetProfile: []string{"http://example.org/StructureDefinition/practitioner"}}}},
	}})
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/practitioner", Type: "Practitioner", Kind: "resource", Elements: []model.ElementDefinition{{Path: "Practitioner", Min: 0, Max: "*"}}})
	if got := dependencyReferenceElementName("Patient", "http://example.org/StructureDefinition/patient", "Practitioner", reg); got != "generalPractitioner" {
		t.Fatalf("dependencyReferenceElementName = %q, want generalPractitioner", got)
	}
	// A dependency with no matching reference element.
	if got := dependencyReferenceElementName("Patient", "http://example.org/StructureDefinition/patient", "Observation", reg); got != "" {
		t.Fatalf("dependencyReferenceElementName(no match) = %q", got)
	}
}

func TestGenerateDatatypeValueFromProfile(t *testing.T) {
	// Nil reg / empty URL.
	if _, ok := generateDatatypeValueFromProfile("", nil); ok {
		t.Fatal("generateDatatypeValueFromProfile(nil) should be false")
	}
	reg := registry.New()
	if _, ok := generateDatatypeValueFromProfile("http://missing", reg); ok {
		t.Fatal("generateDatatypeValueFromProfile(unknown) should be false")
	}
	// A profile that generates a value.
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/identifier", Type: "Identifier", Elements: []model.ElementDefinition{
		{Path: "Identifier", Min: 0, Max: "*"},
		{Path: "Identifier.system", Min: 1, Max: "1", Types: []model.ElementType{{Code: "uri"}}},
	}})
	v, ok := generateDatatypeValueFromProfile("http://example.org/StructureDefinition/identifier", reg)
	if !ok || v == nil {
		t.Fatal("generateDatatypeValueFromProfile(valid) should succeed")
	}
}

func TestReferenceTargetID(t *testing.T) {
	if got := referenceTargetID("Patient/p1"); got != "p1" {
		t.Fatalf("referenceTargetID = %q", got)
	}
	if got := referenceTargetID("Patient/momus-setup-patient"); got != "momus-setup-patient" {
		t.Fatalf("referenceTargetID(setup) = %q", got)
	}
	if got := referenceTargetID(""); got != "" {
		t.Fatalf("referenceTargetID(empty) = %q", got)
	}
	if got := referenceTargetID("no-slash"); got != "" {
		t.Fatalf("referenceTargetID(no-slash) = %q", got)
	}
	if got := referenceTargetID("Patient/"); got != "" {
		t.Fatalf("referenceTargetID(trailing) = %q", got)
	}
	if got := referenceTargetID("Patient/p1?x=1"); got != "" {
		t.Fatalf("referenceTargetID(query) = %q", got)
	}
}

func TestFirstSliceNode(t *testing.T) {
	if firstSliceNode(nil) != nil {
		t.Fatal("firstSliceNode(nil) should be nil")
	}
	if firstSliceNode(&model.ElementNode{}) != nil {
		t.Fatal("firstSliceNode(no slices) should be nil")
	}
	node := &model.ElementNode{Slices: map[string]*model.SliceNode{"a": {Name: "a", Definition: &model.ElementDefinition{Min: 1}}}}
	if got := firstSliceNode(node); got == nil || got.Name != "a" {
		t.Fatalf("firstSliceNode = %+v", got)
	}
	// Slice with no definition.
	if got := firstSliceNode(&model.ElementNode{Slices: map[string]*model.SliceNode{"a": {}}}); got != nil {
		t.Fatalf("firstSliceNode(no def) = %+v", got)
	}
}

func TestMatchingSlice(t *testing.T) {
	// Nil / non-map generic.
	if matchingSlice(nil, nil) != nil {
		t.Fatal("matchingSlice(nil) should be nil")
	}
	if matchingSlice(&model.ElementNode{}, "not-a-map") != nil {
		t.Fatal("matchingSlice(non-map) should be nil")
	}
	// A slice whose child fixed value matches the generic value.
	node := &model.ElementNode{Slices: map[string]*model.SliceNode{
		"phone": {Name: "phone", Definition: &model.ElementDefinition{Min: 1}, Children: map[string]*model.ElementNode{
			"system": {Definition: &model.ElementDefinition{Fixed: "phone"}},
		}},
	}}
	if got := matchingSlice(node, map[string]any{"system": "phone"}); got == nil || got.Name != "phone" {
		t.Fatalf("matchingSlice = %+v", got)
	}
	// No matching slice.
	if got := matchingSlice(node, map[string]any{"system": "email"}); got != nil {
		t.Fatalf("matchingSlice(no match) = %+v", got)
	}
}

func TestSliceHelperFunctions(t *testing.T) {
	// sortedNodeChildren sorts.
	if got := sortedNodeChildren(&model.ElementNode{Children: map[string]*model.ElementNode{"b": {}, "a": {}, "c": {}}}); len(got) != 3 || got[0] != "a" {
		t.Fatalf("sortedNodeChildren = %v", got)
	}
	// sortedSliceChildren sorts.
	slice := &model.SliceNode{Children: map[string]*model.ElementNode{"b": {}, "a": {}}}
	if got := sortedSliceChildren(slice); len(got) != 2 || got[0] != "a" {
		t.Fatalf("sortedSliceChildren = %v", got)
	}
	// wrapFixedSlice: current array -> wraps.
	if got := wrapFixedSlice([]any{}, &model.ElementDefinition{}, "x"); got.([]any)[0] != "x" {
		t.Fatalf("wrapFixedSlice(array) = %v", got)
	}
	// wrapFixedSlice: repeatable element -> wraps.
	if got := wrapFixedSlice(nil, &model.ElementDefinition{Max: "*"}, "x"); got.([]any)[0] != "x" {
		t.Fatalf("wrapFixedSlice(repeatable) = %v", got)
	}
	// wrapFixedSlice: scalar element -> returns scalar.
	if got := wrapFixedSlice(nil, &model.ElementDefinition{Max: "1"}, "x"); got != "x" {
		t.Fatalf("wrapFixedSlice(scalar) = %v", got)
	}
	// mergeSlicePattern: no existing value -> clone pattern.
	value := map[string]any{}
	mergeSlicePattern(value, "coding", map[string]any{"code": "c", "system": "s"})
	if value["coding"].(map[string]any)["code"] != "c" {
		t.Fatalf("mergeSlicePattern(new) = %v", value)
	}
	// mergeSlicePattern: existing nested map -> recurse.
	value = map[string]any{"coding": map[string]any{"code": "old"}}
	mergeSlicePattern(value, "coding", map[string]any{"system": "s"})
	if value["coding"].(map[string]any)["system"] != "s" || value["coding"].(map[string]any)["code"] != "old" {
		t.Fatalf("mergeSlicePattern(existing) = %v", value)
	}
	// applySliceConstractions with nil guards.
	applySliceConstractions(nil, nil, nil)
	// applySliceChildConstraints with a child that has no fixed/pattern -> recurse.
	child := &model.ElementNode{Name: "coding", Definition: &model.ElementDefinition{}}
	applySliceChildConstraints(map[string]any{}, child, nil) // no value[prop], no-op
}

func TestNewRNGDeterministic(t *testing.T) {
	if newRNG("seed").Intn(1000) != newRNG("seed").Intn(1000) {
		t.Fatal("newRNG should be deterministic for the same seed")
	}
	if newRNG("a").Intn(1000) == newRNG("b").Intn(1000) {
		t.Log("seeds may rarely collide; not asserted")
	}
}

func TestResolveBoundCodingPath(t *testing.T) {
	if _, ok := resolveBoundCoding(nil, nil); ok {
		t.Fatal("resolveBoundCoding(nil) should be false")
	}
	if _, ok := resolveBoundCoding(&model.ElementDefinition{}, nil); ok {
		t.Fatal("resolveBoundCoding(no binding) should be false")
	}
	if _, ok := resolveBoundCoding(&model.ElementDefinition{Binding: &model.Binding{}}, nil); ok {
		t.Fatal("resolveBoundCoding(no vs url) should be false")
	}
	reg := registry.New()
	if _, ok := resolveBoundCoding(&model.ElementDefinition{Binding: &model.Binding{ValueSet: "http://missing"}}, reg); ok {
		t.Fatal("resolveBoundCoding(unknown vs) should be false")
	}
	// Compose include referencing a code system.
	reg.AddValueSet(&model.ValueSet{URL: "http://vs", ComposeIncludes: []model.ValueSetInclude{{System: "http://cs"}}})
	reg.AddCodeSystem(&model.CodeSystem{URL: "http://cs", Concepts: []model.CodeSystemConcept{{Code: "k", Display: "Key"}}})
	c, ok := resolveBoundCoding(&model.ElementDefinition{Binding: &model.Binding{ValueSet: "http://vs"}}, reg)
	if !ok || c.Code != "k" {
		t.Fatalf("resolveBoundCoding(cs include) = %+v, %v", c, ok)
	}
	// Expansion contains.
	reg2 := registry.New()
	reg2.AddValueSet(&model.ValueSet{URL: "http://expanded", ExpansionContains: []model.ValueSetExpansionContains{{Code: "XX"}, {Code: "real", Display: "Real"}}})
	c, ok = resolveBoundCoding(&model.ElementDefinition{Binding: &model.Binding{ValueSet: "http://expanded"}}, reg2)
	if !ok || c.Code != "real" {
		t.Fatalf("resolveBoundCoding(expansion) = %+v, %v", c, ok)
	}
	// Compose concepts.
	reg3 := registry.New()
	reg3.AddValueSet(&model.ValueSet{URL: "http://composed", ComposeIncludes: []model.ValueSetInclude{{System: "http://sys", Concepts: []model.ConceptReference{{Code: "direct", Display: "Direct"}}}}})
	c, ok = resolveBoundCoding(&model.ElementDefinition{Binding: &model.Binding{ValueSet: "http://composed"}}, reg3)
	if !ok || c.Code != "direct" {
		t.Fatalf("resolveBoundCoding(composed) = %+v, %v", c, ok)
	}
	// Only placeholder codes -> not found.
	reg4 := registry.New()
	reg4.AddValueSet(&model.ValueSet{URL: "http://placeholders", ComposeIncludes: []model.ValueSetInclude{{System: "http://sys", Concepts: []model.ConceptReference{{Code: "XX"}}}}})
	if _, ok := resolveBoundCoding(&model.ElementDefinition{Binding: &model.Binding{ValueSet: "http://placeholders"}}, reg4); ok {
		t.Fatal("resolveBoundCoding(placeholders) should be false")
	}
}
