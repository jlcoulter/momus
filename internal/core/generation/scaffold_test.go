package generation

import (
	"errors"
	"strings"
	"testing"

	"github.com/jlcoulter/momus/internal/core/ast"
	"github.com/jlcoulter/momus/internal/core/coverage"
)

// mockBuilder is a minimal PayloadBuilder for exercising GenerateFromCoveragePlan
// and its error paths without pulling in the FHIR domain.
type mockBuilder struct {
	depPlan   *coverage.DependencyPlan
	planValue *coverage.DependencyPlan
	depErr    error
}

func (m *mockBuilder) DependencyPlan(*coverage.CoveragePlan, map[string]struct{}) (*coverage.DependencyPlan, error) {
	if m.planValue != nil {
		return m.planValue, m.depErr
	}
	return &coverage.DependencyPlan{
		Levels:       [][]string{{"Patient"}},
		Dependencies: map[string][]string{"Patient": nil},
	}, m.depErr
}

func (m *mockBuilder) BuildResourceCases(reqs []coverage.CoverageRequirement, _ *coverage.CoveragePlan, _ BuildOptions, _ []string, progress func()) []ast.Node {
	out := make([]ast.Node, 0, len(reqs))
	for _, req := range reqs {
		if progress != nil {
			progress()
		}
		out = append(out, &ast.Sequence{Steps: []ast.Node{&ast.Assert{RequirementID: req.ID, Expression: "status in [200]"}}})
	}
	return out
}

func (m *mockBuilder) BuildBody(coverage.CoverageRequirement, string, []string, string, []string, bool) (map[string]any, bool) {
	return map[string]any{}, true
}

func (m *mockBuilder) SearchParamType(coverage.CoverageRequirement, string) string { return "" }
func (m *mockBuilder) SearchAcceptValue(coverage.CoverageRequirement, string) string {
	return "momus-search"
}
func (m *mockBuilder) SearchInvalidValue(coverage.CoverageRequirement, string) (string, bool) {
	return "momus-invalid", true
}

var _ PayloadBuilder = (*mockBuilder)(nil)

type mockWithPlan struct {
	planPtr *coverage.DependencyPlan
}

func (m *mockWithPlan) DependencyPlan(*coverage.CoveragePlan, map[string]struct{}) (*coverage.DependencyPlan, error) {
	return m.planPtr, nil
}
func (m *mockWithPlan) BuildResourceCases(reqs []coverage.CoverageRequirement, _ *coverage.CoveragePlan, _ BuildOptions, _ []string, _ func()) []ast.Node {
	out := make([]ast.Node, 0, len(reqs))
	for _, req := range reqs {
		out = append(out, &ast.Assert{RequirementID: req.ID, Expression: "x"})
	}
	return out
}
func (m *mockWithPlan) BuildBody(coverage.CoverageRequirement, string, []string, string, []string, bool) (map[string]any, bool) {
	return map[string]any{}, true
}
func (m *mockWithPlan) SearchParamType(coverage.CoverageRequirement, string) string { return "" }
func (m *mockWithPlan) SearchAcceptValue(coverage.CoverageRequirement, string) string {
	return "v"
}
func (m *mockWithPlan) SearchInvalidValue(coverage.CoverageRequirement, string) (string, bool) {
	return "i", false
}

var _ PayloadBuilder = (*mockWithPlan)(nil)

func TestGenerateFromCoveragePlan(t *testing.T) {
	plan, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{
		Requirements: []coverage.CoverageRequirement{
			{ID: "r1", ResourceType: "Patient", Domain: coverage.CoverageDomainCardinality, Variant: coverage.CoverageVariantValidMin},
			{ID: "r2", ResourceType: "Patient", Domain: coverage.CoverageDomainCardinality, Variant: coverage.CoverageVariantMultipleValues},
			{ID: "r3", ResourceType: "Observation", Domain: coverage.CoverageDomainCardinality, Variant: coverage.CoverageVariantValidMin},
		},
	}, BuildOptions{Builder: &mockBuilder{}})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan: %v", err)
	}
	if plan == nil || plan.Root == nil {
		t.Fatal("expected a plan with a root")
	}
	if plan.Version != "v1" {
		t.Fatalf("version = %q, want v1", plan.Version)
	}
}

func TestGenerateFromCoveragePlanNilPlan(t *testing.T) {
	if _, err := GenerateFromCoveragePlan(nil, BuildOptions{Builder: &mockBuilder{}}); err == nil {
		t.Fatal("expected error for nil plan")
	}
}

func TestGenerateFromCoveragePlanNilBuilder(t *testing.T) {
	if _, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{}, BuildOptions{}); err == nil {
		t.Fatal("expected error for nil builder")
	}
}

func TestGenerateFromCoveragePlanMissingResourceType(t *testing.T) {
	if _, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{
		Requirements: []coverage.CoverageRequirement{{ID: "r1"}},
	}, BuildOptions{Builder: &mockBuilder{}}); err == nil {
		t.Fatal("expected error for missing resource type")
	}
}

func TestGenerateFromCoveragePlanDependencyError(t *testing.T) {
	if _, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{
		Requirements: []coverage.CoverageRequirement{{ID: "r1", ResourceType: "Patient"}},
	}, BuildOptions{Builder: &mockBuilder{depErr: errors.New("boom")}}); err == nil {
		t.Fatal("expected dependency error to propagate")
	}
}

func TestGenerateFromCoveragePlanParallelizesTypes(t *testing.T) {
	// Both types in the same level => a Parallel node at the root.
	depPlan := &coverage.DependencyPlan{
		Levels:       [][]string{{"Patient", "Observation"}},
		Dependencies: map[string][]string{"Patient": nil, "Observation": nil},
	}
	plan, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{
		Requirements: []coverage.CoverageRequirement{
			{ID: "r1", ResourceType: "Patient", Domain: coverage.CoverageDomainCardinality, Variant: coverage.CoverageVariantValidMin},
			{ID: "r2", ResourceType: "Observation", Domain: coverage.CoverageDomainCardinality, Variant: coverage.CoverageVariantValidMin},
		},
	}, BuildOptions{Builder: &mockWithPlan{planPtr: depPlan}})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan: %v", err)
	}
	// The root is a Sequence; the Parallel node (with 2 resource steps) is its
	// first step.
	root, ok := plan.Root.(*ast.Sequence)
	if !ok || len(root.Steps) != 1 {
		t.Fatalf("expected root Sequence with 1 step, got %T", plan.Root)
	}
	par, ok := root.Steps[0].(*ast.Parallel)
	if !ok || len(par.Steps) != 2 {
		t.Fatalf("expected Parallel with 2 steps, got %T", root.Steps[0])
	}
}

func TestGenerateFromCoveragePlanSkipsSeedOnlyTypes(t *testing.T) {
	// The dependency plan declares Observation but no requirement targets it.
	depPlan := &coverage.DependencyPlan{
		Levels:       [][]string{{"Observation", "Patient"}},
		Dependencies: map[string][]string{"Observation": nil, "Patient": nil},
	}
	plan, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{
		Requirements: []coverage.CoverageRequirement{
			{ID: "r1", ResourceType: "Patient", Domain: coverage.CoverageDomainCardinality, Variant: coverage.CoverageVariantValidMin},
		},
	}, BuildOptions{Builder: &mockWithPlan{planPtr: depPlan}})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan: %v", err)
	}
	// Only Patient (the requirement-bearing type) appears in the AST; Observation
	// is skipped as a seed dependency with no obligations.
	assertCount := RequirementCount(plan)
	if assertCount != 1 {
		t.Fatalf("RequirementCount = %d, want 1", assertCount)
	}
}

func TestGenerateFromCoveragePlanProgress(t *testing.T) {
	var done, total int
	_, err := GenerateFromCoveragePlan(&coverage.CoveragePlan{
		Requirements: []coverage.CoverageRequirement{
			{ID: "r1", ResourceType: "Patient", Domain: coverage.CoverageDomainCardinality, Variant: coverage.CoverageVariantValidMin},
			{ID: "r2", ResourceType: "Patient", Domain: coverage.CoverageDomainCardinality, Variant: coverage.CoverageVariantMultipleValues},
		},
	}, BuildOptions{Builder: &mockBuilder{}, Progress: func(d, t2 int) { done = d; total = t2 }})
	if err != nil {
		t.Fatalf("GenerateFromCoveragePlan: %v", err)
	}
	if done != 2 || total != 2 {
		t.Fatalf("progress = %d/%d, want 2/2", done, total)
	}
}

func TestRequirementCount(t *testing.T) {
	if RequirementCount(nil) != 0 {
		t.Fatal("RequirementCount(nil) should be 0")
	}
	if RequirementCount(&ast.Plan{}) != 0 {
		t.Fatal("RequirementCount(empty plan) should be 0")
	}
	plan := &ast.Plan{Root: &ast.Sequence{Steps: []ast.Node{
		&ast.Assert{RequirementID: "setup:seed", Expression: "x"},
		&ast.Assert{RequirementID: "r1", Expression: "x"},
		&ast.Assert{RequirementID: "r1", Expression: "x"}, // duplicate
		&ast.Parallel{Steps: []ast.Node{&ast.Assert{RequirementID: "r2", Expression: "x"}}},
	}}}
	if got := RequirementCount(plan); got != 2 {
		t.Fatalf("RequirementCount = %d, want 2", got)
	}
}

func TestJoinURL(t *testing.T) {
	if got := JoinURL("", "Patient"); got != "/Patient" {
		t.Fatalf("JoinURL(empty) = %q, want /Patient", got)
	}
	if got := JoinURL("http://x/fhir", "Patient"); got != "http://x/fhir/Patient" {
		t.Fatalf("JoinURL = %q", got)
	}
	if got := JoinURL("http://x/fhir/", "/Patient"); got != "http://x/fhir/Patient" {
		t.Fatalf("JoinURL trailing slash = %q", got)
	}
}

func TestJoinInstanceURL(t *testing.T) {
	if got := JoinInstanceURL("http://x/fhir", "Patient", "p1"); got != "http://x/fhir/Patient/p1" {
		t.Fatalf("JoinInstanceURL = %q", got)
	}
	if got := JoinInstanceURL("http://x", "Patient", "/p1"); got != "http://x/Patient/p1" {
		t.Fatalf("JoinInstanceURL trim = %q", got)
	}
}

func TestBaseURLForMethod(t *testing.T) {
	opts := BuildOptions{BaseURL: "http://read", WriteBaseURL: "http://write"}
	if got := BaseURLForMethod(opts, "GET"); got != "http://read" {
		t.Fatalf("GET base = %q, want read", got)
	}
	if got := BaseURLForMethod(opts, "POST"); got != "http://write" {
		t.Fatalf("POST base = %q, want write", got)
	}
	if got := BaseURLForMethod(opts, "DELETE"); got != "http://write" {
		t.Fatalf("DELETE base = %q, want write", got)
	}
	// No write base URL -> falls back to read.
	opts2 := BuildOptions{BaseURL: "http://read"}
	if got := BaseURLForMethod(opts2, "PUT"); got != "http://read" {
		t.Fatalf("PUT base (no write) = %q, want read", got)
	}
}

func TestFirstProfileURL(t *testing.T) {
	if got := FirstProfileURL([]string{"", "  ", "http://a"}); got != "http://a" {
		t.Fatalf("FirstProfileURL = %q", got)
	}
	if got := FirstProfileURL(nil); got != "" {
		t.Fatalf("FirstProfileURL(nil) = %q, want empty", got)
	}
}

func TestOrderedProfilesForResource(t *testing.T) {
	got := OrderedProfilesForResource("Patient", "http://req", map[string][]string{
		"Patient": {"http://pref1", "http://pref2", "http://pref1"},
		"pATIEnt": {"http://case"},
	})
	want := []string{"http://pref1", "http://pref2", "http://req"}
	if len(got) != len(want) {
		t.Fatalf("profiles = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("profiles = %v, want %v", got, want)
		}
	}
	// No preferences.
	if got := OrderedProfilesForResource("Patient", "http://req", nil); len(got) != 1 || got[0] != "http://req" {
		t.Fatalf("no-pref profiles = %v", got)
	}
}

func TestRequirementResourceID(t *testing.T) {
	req := coverage.CoverageRequirement{ID: "r1", ResourceType: "Patient", Variant: coverage.CoverageVariantValidMin}
	id := RequirementResourceID(req)
	if id == "" || !strings.HasPrefix(id, "momus-") {
		t.Fatalf("RequirementResourceID = %q", id)
	}
	// Empty resource type and variant fall back.
	if got := RequirementResourceID(coverage.CoverageRequirement{ID: "x"}); !strings.HasPrefix(got, "momus-") {
		t.Fatalf("RequirementResourceID(fallback) = %q", got)
	}
}

func TestSetupResourceID(t *testing.T) {
	if got := SetupResourceID("Patient"); got != "momus-setup-patient" {
		t.Fatalf("SetupResourceID = %q", got)
	}
}

func TestSanitizeFHIRID(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "momus-id"},
		{"Patient", "patient"},
		{"Patient Name", "patient-name"},
		{"  ", "momus-id"},
		{"---", "momus-id"},
		{"a/b/c", "a-b-c"},
		{"Patient.Name", "patient.name"},
		{"UPPER", "upper"},
		{strings.Repeat("x", 100), strings.Repeat("x", 64)},
	}
	for _, c := range cases {
		if got := SanitizeFHIRID(c.in); got != c.want {
			t.Errorf("SanitizeFHIRID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestUniqueProfileURLs(t *testing.T) {
	got := UniqueProfileURLs([]coverage.CoverageRequirement{
		{ProfileURL: "http://a"},
		{ProfileURL: "http://a"},
		{ProfileURL: "http://b"},
		{ProfileURL: " "},
	})
	if len(got) != 2 {
		t.Fatalf("UniqueProfileURLs = %v, want 2 entries", got)
	}
}

func TestMax(t *testing.T) {
	if Max(2, 3) != 3 || Max(5, 1) != 5 || Max(4, 4) != 4 {
		t.Fatal("Max returned wrong value")
	}
}

func TestBuildMeta(t *testing.T) {
	if BuildMeta(nil) != nil {
		t.Fatal("BuildMeta(nil) should be nil")
	}
	m := BuildMeta([]string{"http://a", "http://a", "http://b"})
	profiles := m["profile"].([]any)
	if len(profiles) != 2 {
		t.Fatalf("BuildMeta profiles = %v, want 2", profiles)
	}
}

func TestStableChecksumValue(t *testing.T) {
	if StableChecksum("a") == StableChecksum("b") {
		t.Fatal("StableChecksum should distinguish inputs")
	}
}
