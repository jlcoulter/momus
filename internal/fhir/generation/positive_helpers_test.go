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
