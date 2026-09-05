package fhircoverage

import (
	"testing"

	"github.com/jlcoulter/momus/internal/core/coverage"
	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

func TestCanonicalToResourceType(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/Patient", Type: "Patient"})
	if got := canonicalToResourceType(reg, "http://example.org/StructureDefinition/Patient"); got != "Patient" {
		t.Fatalf("canonicalToResourceType = %q", got)
	}
	// Version stripped.
	if got := canonicalToResourceType(reg, "http://example.org/StructureDefinition/Patient|4.0.1"); got != "Patient" {
		t.Fatalf("canonicalToResourceType(versioned) = %q", got)
	}
	// Fragment stripped.
	if got := canonicalToResourceType(reg, "http://x/Patient#frag"); got != "Patient" {
		t.Fatalf("canonicalToResourceType(fragment) = %q", got)
	}
	// Unknown canonical -> base name.
	if got := canonicalToResourceType(reg, "http://x/Observation"); got != "Observation" {
		t.Fatalf("canonicalToResourceType(unknown) = %q", got)
	}
	// Empty.
	if got := canonicalToResourceType(reg, ""); got != "" {
		t.Fatalf("canonicalToResourceType(empty) = %q", got)
	}
	// path base is StructureDefinition -> empty.
	if got := canonicalToResourceType(reg, "http://x/StructureDefinition"); got != "" {
		t.Fatalf("canonicalToResourceType(structdef) = %q", got)
	}
}

func TestAppendUniqueString(t *testing.T) {
	if got := appendUniqueString([]string{"a", "b"}, "a"); len(got) != 2 {
		t.Fatalf("appendUniqueString(dup) = %v", got)
	}
	if got := appendUniqueString([]string{"a"}, "b"); len(got) != 2 || got[1] != "b" {
		t.Fatalf("appendUniqueString(new) = %v", got)
	}
}

func TestIsDerivableElement(t *testing.T) {
	options := coverage.DeriveOptions{}
	cases := []struct {
		name    string
		element model.ElementDefinition
		opts    coverage.DeriveOptions
		wantOK  bool
		wantPr  coverage.PruneReason
	}{
		{"empty path", model.ElementDefinition{}, options, false, coverage.PruneReasonRootPath},
		{"root path", model.ElementDefinition{Path: "Patient"}, options, false, coverage.PruneReasonRootPath},
		{"derivable", model.ElementDefinition{Path: "Patient.name", Min: 1}, options, true, ""},
		{"excluded prefix", model.ElementDefinition{Path: "Patient.extension"}, coverage.DeriveOptions{ExcludePathPrefixes: []string{"Patient.extension"}}, false, coverage.PruneReasonExcludedPathPrefix},
		{"low value path", model.ElementDefinition{Path: "Patient.meta"}, options, false, coverage.PruneReasonLowValuePath},
		{"must support only", model.ElementDefinition{Path: "Patient.name", MustSupport: false}, coverage.DeriveOptions{MustSupportOnly: true}, false, coverage.PruneReasonMustSupportFiltered},
		{"optional filtered", model.ElementDefinition{Path: "Patient.name", Min: 0}, coverage.DeriveOptions{}, false, coverage.PruneReasonOptionalFiltered},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ok, reason := isDerivableElement(c.element, c.opts)
			if ok != c.wantOK || reason != c.wantPr {
				t.Fatalf("isDerivableElement = %v, %q; want %v, %q", ok, reason, c.wantOK, c.wantPr)
			}
		})
	}
}

func TestAllowsMultiple(t *testing.T) {
	if !allowsMultiple("*") {
		t.Fatal("allowsMultiple(*) should be true")
	}
	if !allowsMultiple("2") {
		t.Fatal("allowsMultiple(2) should be true")
	}
	if allowsMultiple("1") || allowsMultiple("0") {
		t.Fatal("allowsMultiple(1/0) should be false")
	}
	if allowsMultiple("abc") {
		t.Fatal("allowsMultiple(non-numeric) should be false")
	}
}

func TestHasExcludedPrefix(t *testing.T) {
	if !hasExcludedPrefix("Patient.extension", []string{"Patient.extension", ""}) {
		t.Fatal("should match excluded prefix")
	}
	if hasExcludedPrefix("Patient.name", []string{"Patient.extension"}) {
		t.Fatal("should not match non-matching prefix")
	}
	if hasExcludedPrefix("Patient.name", nil) {
		t.Fatal("nil prefixes should not exclude")
	}
}

func TestIsLowValuePath(t *testing.T) {
	if !isLowValuePath("Patient.meta") {
		t.Fatal("Patient.meta should be a low-value path")
	}
	if isLowValuePath("Patient.name") {
		t.Fatal("Patient.name should not be low-value")
	}
	if isLowValuePath("Patient") {
		t.Fatal("root path should not be low-value")
	}
}

func TestIsSearchCodeAllowed(t *testing.T) {
	// No capability codes -> all allowed.
	if !isSearchCodeAllowed("Patient", "name", nil) {
		t.Fatal("nil capability should allow all")
	}
	// Resource type absent -> all allowed.
	if !isSearchCodeAllowed("Observation", "code", map[string][]string{"Patient": {"name"}}) {
		t.Fatal("absent type should allow all")
	}
	// Declared code (case-insensitive).
	if !isSearchCodeAllowed("Patient", "Name", map[string][]string{"Patient": {"name"}}) {
		t.Fatal("declared code should be allowed")
	}
	// Undeclared code.
	if isSearchCodeAllowed("Patient", "active", map[string][]string{"Patient": {"name"}}) {
		t.Fatal("undeclared code should be disallowed")
	}
}

func TestIsUniversalSearchBase(t *testing.T) {
	if !isUniversalSearchBase("*") || !isUniversalSearchBase("Resource") || !isUniversalSearchBase("DomainResource") {
		t.Fatal("universal bases should be recognized")
	}
	if isUniversalSearchBase("Patient") {
		t.Fatal("Patient is not a universal base")
	}
}

func TestSortedSetKeys(t *testing.T) {
	keys := sortedSetKeys(map[string]struct{}{"b": {}, "a": {}, "c": {}})
	if len(keys) != 3 || keys[0] != "a" || keys[2] != "c" {
		t.Fatalf("sortedSetKeys = %v", keys)
	}
}

func TestToSet(t *testing.T) {
	if toSet(nil) != nil {
		t.Fatal("toSet(nil) should be nil")
	}
	if toSet([]string{}) != nil {
		t.Fatal("toSet(empty) should be nil")
	}
	set := toSet([]string{"A", " b ", ""})
	if len(set) != 2 || set["a"] != struct{}{} || set["b"] != struct{}{} {
		t.Fatalf("toSet = %v", set)
	}
	if toSet([]string{""}) != nil {
		t.Fatal("toSet(all empty) should be nil")
	}
}

func TestCollectDependencyTargets(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/Patient", Type: "Patient", Kind: "resource", Elements: []model.ElementDefinition{{Path: "Patient", Min: 0, Max: "*"}}})
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/Organization", Type: "Organization", Kind: "resource", Elements: []model.ElementDefinition{{Path: "Organization", Min: 0, Max: "*"}}})
	// Element-level TargetProfile.
	el := model.ElementDefinition{TargetProfile: []string{"http://example.org/StructureDefinition/Patient", "http://example.org/StructureDefinition/Patient"}}
	got := collectDependencyTargets(reg, el)
	if len(got) != 1 || got[0] != "Patient" {
		t.Fatalf("collectDependencyTargets = %v", got)
	}
	// Type-level TargetProfile.
	el = model.ElementDefinition{Types: []model.ElementType{{TargetProfile: []string{"http://example.org/StructureDefinition/Organization"}}}}
	got = collectDependencyTargets(reg, el)
	if len(got) != 1 || got[0] != "Organization" {
		t.Fatalf("collectDependencyTargets(type) = %v", got)
	}
	// No targets.
	if got := collectDependencyTargets(reg, model.ElementDefinition{}); len(got) != 0 {
		t.Fatalf("collectDependencyTargets(none) = %v", got)
	}
}

func TestTrackPruned(t *testing.T) {
	plan := &coverage.CoveragePlan{Summary: coverage.CoverageSummary{PrunedByReason: map[coverage.PruneReason]int{}}}
	trackPruned(plan, coverage.PruneReasonLowValuePath)
	if plan.Summary.PrunedByReason[coverage.PruneReasonLowValuePath] != 1 {
		t.Fatalf("trackPruned count = %d, want 1", plan.Summary.PrunedByReason[coverage.PruneReasonLowValuePath])
	}
	trackPruned(plan, "")
	if plan.Summary.PrunedByReason[coverage.PruneReasonLowValuePath] != 1 {
		t.Fatal("empty reason should not be tracked")
	}
}

func TestExtensionSlicePrefixes(t *testing.T) {
	suppressedURL := "http://example.org/StructureDefinition/suppressed"
	elements := []model.ElementDefinition{
		{Path: "Organization.extension", Min: 0, Max: "*", Types: []model.ElementType{{Code: "Extension"}}},
		{ID: "Organization.extension:suppressed", Path: "Organization.extension", SliceName: "suppressed", Min: 1, Max: "1", Types: []model.ElementType{{Code: "Extension", Profile: []string{suppressedURL}}}},
		{ID: "Organization.extension:suppressed.url", Path: "Organization.extension.url", Min: 1, Max: "1"},
		{ID: "Organization.extension:other", Path: "Organization.extension", SliceName: "other", Min: 1, Max: "1", Types: []model.ElementType{{Code: "Extension", Profile: []string{"https://example.org/StructureDefinition/other"}}}},
	}

	// Empty exclusion list yields no prefixes.
	if got := extensionSlicePrefixes(elements, nil); got != nil {
		t.Fatalf("extensionSlicePrefixes(nil) = %v, want nil", got)
	}

	got := extensionSlicePrefixes(elements, []string{suppressedURL})
	if len(got) != 2 || got[0] != "Organization.extension:suppressed" || got[1] != "Organization.extension:suppressed." {
		t.Fatalf("extensionSlicePrefixes = %v, want [Organization.extension:suppressed Organization.extension:suppressed.]", got)
	}
}

func TestIsExcludedExtensionElement(t *testing.T) {
	prefixes := []string{"Organization.extension:suppressed."}

	if !isExcludedExtensionElement(model.ElementDefinition{ID: "Organization.extension:suppressed.url"}, prefixes) {
		t.Fatal("descendant element should be excluded")
	}
	if !isExcludedExtensionElement(model.ElementDefinition{ID: "Organization.extension:suppressed.extension:sub.url"}, prefixes) {
		t.Fatal("nested descendant element should be excluded")
	}
	if isExcludedExtensionElement(model.ElementDefinition{ID: "Organization.extension:other.url"}, prefixes) {
		t.Fatal("unrelated slice element should not be excluded")
	}
	if isExcludedExtensionElement(model.ElementDefinition{ID: "Organization.name"}, prefixes) {
		t.Fatal("non-extension element should not be excluded")
	}
	if isExcludedExtensionElement(model.ElementDefinition{ID: "Organization.extension:suppressed.url"}, nil) {
		t.Fatal("no prefixes means nothing excluded")
	}
}

func TestElementIsExcludedExtension(t *testing.T) {
	// Keys are normalized via normalizeCanonicalURL (lowercased, version/fragment
	// stripped); mirror that here.
	excluded := map[string]struct{}{"https://example.org/structuredefinition/suppressed": {}}

	if !elementIsExcludedExtension(model.ElementDefinition{
		Types: []model.ElementType{{Code: "Extension", Profile: []string{"https://example.org/StructureDefinition/suppressed"}}},
	}, excluded) {
		t.Fatal("matching extension profile should be excluded")
	}
	if elementIsExcludedExtension(model.ElementDefinition{
		Types: []model.ElementType{{Code: "Extension", Profile: []string{"https://example.org/StructureDefinition/other"}}},
	}, excluded) {
		t.Fatal("non-matching extension profile should not be excluded")
	}
	if elementIsExcludedExtension(model.ElementDefinition{
		Types: []model.ElementType{{Code: "string"}},
	}, excluded) {
		t.Fatal("non-extension element should not be excluded")
	}
	if !elementIsExcludedExtension(model.ElementDefinition{
		Types: []model.ElementType{{Code: "Extension", Profile: []string{"https://example.org/StructureDefinition/suppressed|26.0.0"}}},
	}, excluded) {
		t.Fatal("version-suffixed matching profile should be excluded")
	}
}
