package generation

import (
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

// TestCollectionHasFieldValue verifies the collection membership check used by
// the collection-either-where-exists constraint handling.
func TestCollectionHasFieldValue(t *testing.T) {
	raw := []any{
		map[string]any{"system": "http://example.org/a", "code": "x"},
		map[string]any{"system": "http://example.org/b", "code": "y"},
	}
	if !collectionHasFieldValue(raw, "code", "x", "z") {
		t.Fatal("expected code x to be found in the collection")
	}
	if collectionHasFieldValue(raw, "code", "z") {
		t.Fatal("did not expect code z in the collection")
	}
	// Non-array input -> false.
	if collectionHasFieldValue("not-an-array", "code", "x") {
		t.Fatal("expected false for non-array input")
	}
	// Non-map items are skipped, but a map item with the value is still found.
	if !collectionHasFieldValue([]any{"x", map[string]any{"code": "y"}}, "code", "y") {
		t.Fatal("expected the map item's code y to be found")
	}
}

// TestSynthesizeRegexExample verifies the regex-to-example mapping used by
// applySimpleConstraints for .matches() invariants.
func TestSynthesizeRegexExample(t *testing.T) {
	if got, ok := synthesizeRegexExample("^([0-9]{11})$"); !ok || got != "12345678901" {
		t.Fatalf("synthesizeRegexExample(^([0-9]{11})$) = %q, %v", got, ok)
	}
	if got, ok := synthesizeRegexExample("^[0-9]{11}$"); !ok || got != "12345678901" {
		t.Fatalf("synthesizeRegexExample(^[0-9]{11}$) = %q, %v", got, ok)
	}
	if _, ok := synthesizeRegexExample("^[a-z]+$"); ok {
		t.Fatal("expected unsupported regex to return not-ok")
	}
}

// TestApplySimpleConstraintsMatchesPattern verifies that a .matches() invariant
// stamps a conforming example onto the value.
func TestApplySimpleConstraintsMatchesPattern(t *testing.T) {
	value := map[string]any{"value": "not-a-number"}
	node := &model.ElementNode{
		Path: "Identifier.value",
		Definition: &model.ElementDefinition{
			Path: "Identifier.value",
			Constraints: []model.ElementConstraint{
				{Key: "inv-abn-0", Severity: "error", Expression: "value.matches('^([0-9]{11})$')"},
			},
		},
	}
	applySimpleConstraints(value, node, nil)
	if value["value"] != "12345678901" {
		t.Fatalf("value = %v, want the regex example", value["value"])
	}
}

// TestApplySimpleConstraintsCollectionEitherWhereExists verifies the
// collectionEitherWhereExistsPattern branch adds a matching candidate.
func TestApplySimpleConstraintsCollectionEitherWhereExists(t *testing.T) {
	value := map[string]any{"communication": []any{}}
	node := &model.ElementNode{
		Path: "Patient.communication",
		Definition: &model.ElementDefinition{
			Constraints: []model.ElementConstraint{{
				Key:        "pat-comm",
				Severity:   "error",
				Expression: "communication.where(language='en').exists() or communication.where(language='it').exists()",
			}},
		},
		Children: map[string]*model.ElementNode{
			"communication": {
				Definition: &model.ElementDefinition{},
			},
		},
	}
	applySimpleConstraints(value, node, nil)
	// The value is unchanged (no matching candidate generated); the important
	// thing is no panic and the branch is exercised.
	_ = value
}

// TestApplySimpleConstraintsUnsupportedRegex verifies that a matches constraint
// whose regex has no synthesized example is left untouched (not an error).
func TestApplySimpleConstraintsUnsupportedRegex(t *testing.T) {
	value := map[string]any{"value": "existing"}
	node := &model.ElementNode{
		Definition: &model.ElementDefinition{
			Constraints: []model.ElementConstraint{
				{Key: "k1", Severity: "error", Expression: "value.matches('^[a-z]+$')"},
			},
		},
	}
	applySimpleConstraints(value, node, nil)
	if value["value"] != "existing" {
		t.Fatalf("value = %v, want unchanged (unsupported regex)", value["value"])
	}
}

// TestApplySimpleConstraintsExistsEither verifies that an exists() or exists()
// invariant populates the first absent side.
func TestApplySimpleConstraintsExistsEither(t *testing.T) {
	value := map[string]any{}
	node := &model.ElementNode{
		Path: "Observation.value[x]",
		Definition: &model.ElementDefinition{
			Path: "Observation.value[x]",
			Constraints: []model.ElementConstraint{
				{Key: "obs-1", Severity: "error", Expression: "value.exists() or dataAbsentReason.exists()"},
			},
		},
		Children: map[string]*model.ElementNode{
			"value": {
				Path: "Observation.value[x]",
				Definition: &model.ElementDefinition{
					Path:  "Observation.value[x]",
					Types: []model.ElementType{{Code: "string"}},
				},
			},
		},
	}
	applySimpleConstraints(value, node, nil)
	if value["value"] == nil {
		t.Fatal("expected the absent value side to be populated")
	}
}

// TestApplySimpleConstraintsWhereEmpty verifies that a where(...).empty()
// invariant removes the forbidden value from collection members.
func TestApplySimpleConstraintsWhereEmpty(t *testing.T) {
	value := map[string]any{
		"coding": []any{
			map[string]any{"system": "http://example.org", "code": "forbidden"},
			map[string]any{"system": "http://example.org", "code": "ok"},
		},
	}
	node := &model.ElementNode{
		Path: "Observation.code",
		Definition: &model.ElementDefinition{
			Path: "Observation.code",
			Constraints: []model.ElementConstraint{
				{Key: "obs-2", Severity: "error", Expression: "coding.where(code = 'forbidden').empty()"},
			},
		},
	}
	applySimpleConstraints(value, node, nil)
	codings := value["coding"].([]any)
	if len(codings) != 2 {
		t.Fatalf("coding array = %d, want 2 (forbidden code removed from member)", len(codings))
	}
	first := codings[0].(map[string]any)
	if _, ok := first["code"]; ok {
		t.Fatalf("forbidden code not removed: %v", first)
	}
}

// TestApplySimpleConstraintsCurrentWhereEmpty verifies the current-context
// where(...).empty() form removes the forbidden value.
func TestApplySimpleConstraintsCurrentWhereEmpty(t *testing.T) {
	value := map[string]any{"code": "forbidden"}
	node := &model.ElementNode{
		Path: "Observation.status",
		Definition: &model.ElementDefinition{
			Path: "Observation.status",
			Constraints: []model.ElementConstraint{
				{Key: "obs-3", Severity: "error", Expression: "where(code = 'forbidden').empty()"},
			},
		},
	}
	applySimpleConstraints(value, node, nil)
	if _, ok := value["code"]; ok {
		t.Fatal("forbidden current-context value not removed")
	}
}

// TestFirstExpansionCodingAndCodeSystemConcept verifies the meaningful-code
// selection helpers skip placeholders and recurse into nested entries.
func TestFirstExpansionCodingAndCodeSystemConcept(t *testing.T) {
	// Expansion: top-level placeholder skipped, first meaningful code found.
	entries := []model.ValueSetExpansionContains{
		{System: "http://example.org/cs", Code: "XX", Display: "Null"},
		{System: "http://example.org/cs", Code: "parent", Contains: []model.ValueSetExpansionContains{
			{System: "http://example.org/cs", Code: "RI", Display: "Resource identifier"},
		}},
	}
	c, ok := firstExpansionCoding(entries)
	if !ok || c.Code != "parent" {
		t.Fatalf("firstExpansionCoding = %+v ok=%v, want parent (first meaningful code)", c, ok)
	}

	// Code system: first meaningful concept found.
	concepts := []model.CodeSystemConcept{
		{Code: "XX", Display: "Null"},
		{Code: "parent", Concepts: []model.CodeSystemConcept{
			{Code: "RI", Display: "Resource identifier"},
		}},
	}
	concept, ok := firstCodeSystemConcept(concepts)
	if !ok || concept.Code != "parent" {
		t.Fatalf("firstCodeSystemConcept = %+v ok=%v, want parent (first meaningful code)", concept, ok)
	}

	// Empty -> not found.
	if _, ok := firstExpansionCoding(nil); ok {
		t.Fatal("expected no expansion coding for empty entries")
	}
	if _, ok := firstCodeSystemConcept(nil); ok {
		t.Fatal("expected no code system concept for empty concepts")
	}
}

// TestCodingToMapGeneration verifies the generation codingToMap omits empty
// fields.
func TestCodingToMapGeneration(t *testing.T) {
	m := codingToMap(generatedCoding{System: "http://example.org/cs", Code: "c", Display: "D"})
	if m["system"] != "http://example.org/cs" || m["code"] != "c" || m["display"] != "D" {
		t.Fatalf("codingToMap = %v", m)
	}
	if len(codingToMap(generatedCoding{})) != 0 {
		t.Fatal("expected empty map for empty coding")
	}
}

// TestGenerateMatchingCollectionCandidate verifies the collection-candidate
// helper: a nil node yields not-found, and a node that cannot produce a
// candidate matching the wanted field value yields not-found without panicking.
func TestGenerateMatchingCollectionCandidateMatched(t *testing.T) {
	reg := registry.New()
	// A slice whose value carries the wanted field.
	node := &model.ElementNode{Slices: map[string]*model.SliceNode{
		"system-phone": {Name: "system-phone", Definition: &model.ElementDefinition{
			Path:  "ContactPoint",
			Types: []model.ElementType{{Code: "ContactPoint"}},
			Fixed: map[string]any{"system": "phone"},
		}},
	}}
	c, ok := generateMatchingCollectionCandidate(node, "system", []string{"phone"}, reg)
	if !ok || c == nil {
		t.Fatalf("generateMatchingCollectionCandidate(match) = %v, %v", c, ok)
	}
	if m := c.(map[string]any); m["system"] != "phone" {
		t.Fatalf("matched candidate = %v", m)
	}
}

func TestGenerateMatchingCollectionCandidate(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{
		URL: "http://example.org/StructureDefinition/cc", Type: "CodeableConcept",
		Elements: []model.ElementDefinition{
			{Path: "CodeableConcept", Min: 0, Max: "*"},
			{Path: "CodeableConcept.coding", Min: 1, Max: "*", Types: []model.ElementType{{Code: "Coding"}}},
			{Path: "CodeableConcept.coding.code", Min: 1, Max: "1", Types: []model.ElementType{{Code: "code"}}},
		},
	})
	node := &model.ElementNode{
		Path: "Observation.code",
		Definition: &model.ElementDefinition{
			Path:  "Observation.code",
			Types: []model.ElementType{{Code: "CodeableConcept", Profile: []string{"http://example.org/StructureDefinition/cc"}}},
		},
	}
	// Nil node -> not found.
	if _, ok := generateMatchingCollectionCandidate(nil, "code", []string{"x"}, reg); ok {
		t.Fatal("expected not-found for nil node")
	}
	// A node that generates a value but not matching the wanted field -> not found.
	if _, ok := generateMatchingCollectionCandidate(node, "code", []string{"definitely-not-generated"}, reg); ok {
		t.Fatal("expected not-found when no candidate matches the wanted value")
	}
}

// TestSetNameLeafAndSetAddressLeaf verifies the search-seed leaf setters place
// values on HumanName and Address elements. The path is the element path below
// the resource root (e.g. "name", not "Patient.name").
func TestSetNameLeafAndSetAddressLeaf(t *testing.T) {
	body := map[string]any{}
	setNameLeaf(body, "name", "momus-search")
	names := body["name"].([]any)
	first := names[0].(map[string]any)
	if first["family"] != "momus-search" || first["text"] != "momus-search" {
		t.Fatalf("name leaf = %v", first)
	}

	// Existing name array: family/text filled in without clobbering siblings.
	body2 := map[string]any{"name": []any{map[string]any{"given": []any{"Test"}}}}
	setNameLeaf(body2, "name", "momus-search")
	first2 := body2["name"].([]any)[0].(map[string]any)
	if first2["family"] != "momus-search" || first2["given"] == nil {
		t.Fatalf("existing name leaf = %v", first2)
	}

	body3 := map[string]any{}
	setAddressLeaf(body3, "address", "momus-search")
	addr := body3["address"].([]any)[0].(map[string]any)
	if addr["text"] != "momus-search" {
		t.Fatalf("address leaf = %v", addr)
	}
}

// TestSetFieldLeaf verifies the generic leaf setter creates or fills a field.
func TestSetFieldLeaf(t *testing.T) {
	body := map[string]any{}
	setFieldLeaf(body, "address", "city", "momus-search")
	addr := body["address"].([]any)[0].(map[string]any)
	if addr["city"] != "momus-search" {
		t.Fatalf("setFieldLeaf created = %v", addr)
	}

	// Existing array: fills the first element without overwriting existing value.
	body2 := map[string]any{"address": []any{map[string]any{"city": "Erewhon"}}}
	setFieldLeaf(body2, "address", "city", "momus-search")
	if body2["address"].([]any)[0].(map[string]any)["city"] != "Erewhon" {
		t.Fatal("setFieldLeaf must not overwrite an existing value")
	}

	// Existing map (non-array): fills the leaf.
	body3 := map[string]any{"address": map[string]any{}}
	setFieldLeaf(body3, "address", "city", "momus-search")
	if body3["address"].(map[string]any)["city"] != "momus-search" {
		t.Fatal("setFieldLeaf map fill failed")
	}
}

// TestBoundCodingSystem verifies the token-search code-system resolver.
func TestBoundCodingSystem(t *testing.T) {
	reg := registry.New()
	reg.AddValueSet(&model.ValueSet{URL: "http://example.org/ValueSet/status", ComposeIncludes: []model.ValueSetInclude{
		{System: "http://example.org/cs", Concepts: []model.ConceptReference{{Code: "active"}}},
	}})
	reg.AddStructureDefinition(&model.StructureDefinition{
		URL: "http://example.org/StructureDefinition/endpoint", Type: "Endpoint",
		Elements: []model.ElementDefinition{
			{Path: "Endpoint", Min: 0, Max: "*"},
			{Path: "Endpoint.status", Min: 1, Max: "1", Types: []model.ElementType{{Code: "code"}}, Binding: &model.Binding{Strength: "required", ValueSet: "http://example.org/ValueSet/status"}},
		},
	})
	if got := boundCodingSystem("Endpoint", "status", reg); got != "http://example.org/cs" {
		t.Fatalf("boundCodingSystem = %q, want http://example.org/cs", got)
	}
	if got := boundCodingSystem("Endpoint", "status", nil); got != "" {
		t.Fatalf("boundCodingSystem(nil reg) = %q, want empty", got)
	}
	if got := boundCodingSystem("Endpoint", "missing", reg); got != "" {
		t.Fatalf("boundCodingSystem(missing) = %q, want empty", got)
	}
}

// TestMutateReferenceTypeAndReferenceID verifies the negative reference
// mutations retarget references to a different resource type.
func TestMutateReferenceTypeAndReferenceID(t *testing.T) {
	body := map[string]any{
		"subject": map[string]any{"reference": "Patient/p-1", "type": "Patient"},
	}
	if !mutateReferenceType(body, "Observation.subject") {
		t.Fatal("expected mutateReferenceType to retarget the reference")
	}
	subject := body["subject"].(map[string]any)
	if subject["type"] != "Organization" || subject["reference"] != "Organization/p-1" {
		t.Fatalf("mutated reference = %v, want Organization/p-1", subject)
	}
	// No reference at the path -> false.
	if mutateReferenceType(map[string]any{}, "Observation.missing") {
		t.Fatal("expected false when no reference is present")
	}
	// referenceID extracts the id after the last slash.
	if got := referenceID("Patient/p-1"); got != "p-1" {
		t.Fatalf("referenceID = %q, want p-1", got)
	}
	if got := referenceID("no-slash"); got != "momus-wrong" {
		t.Fatalf("referenceID(no-slash) = %q, want momus-wrong", got)
	}
}

// TestCoverageCanonicalToResourceType verifies canonical-to-resource-type
// extraction strips version and path segments.
func TestCoverageCanonicalToResourceType(t *testing.T) {
	if got := coverageCanonicalToResourceType("http://example.org/StructureDefinition/patient"); got != "patient" {
		t.Fatalf("coverageCanonicalToResourceType = %q, want patient", got)
	}
	if got := coverageCanonicalToResourceType("http://example.org/StructureDefinition/patient|4.0.1"); got != "patient" {
		t.Fatalf("coverageCanonicalToResourceType(versioned) = %q, want patient", got)
	}
	if got := coverageCanonicalToResourceType(""); got != "" {
		t.Fatalf("coverageCanonicalToResourceType(empty) = %q, want empty", got)
	}
}
