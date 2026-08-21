package bulk

import (
	"context"
	"strings"
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

const obsProfile = "http://example.org/StructureDefinition/observation"
const patientProfile = "http://example.org/StructureDefinition/patient"

func testRegistry(t *testing.T) *registry.Registry {
	t.Helper()
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{
		URL:  obsProfile,
		Type: "Observation",
		Elements: []model.ElementDefinition{
			{Path: "Observation", Min: 0, Max: "*"},
			{Path: "Observation.status", Min: 1, Max: "1", Types: []model.ElementType{{Code: "code"}}},
			{Path: "Observation.value", Min: 0, Max: "1", Types: []model.ElementType{{Code: "string"}}},
			{Path: "Observation.subject", Min: 0, Max: "1", Types: []model.ElementType{{Code: "Reference", TargetProfile: []string{patientProfile}}}},
			{Path: "Observation.code", Min: 1, Max: "1", Types: []model.ElementType{{Code: "CodeableConcept"}}},
		},
	})
	reg.AddStructureDefinition(&model.StructureDefinition{
		URL:  patientProfile,
		Type: "Patient",
		Elements: []model.ElementDefinition{
			{Path: "Patient", Min: 0, Max: "*"},
			{Path: "Patient.name", Min: 1, Max: "*", Types: []model.ElementType{{Code: "HumanName"}}},
			{Path: "Patient.birthDate", Min: 1, Max: "1", Types: []model.ElementType{{Code: "date"}}},
		},
	})
	return reg
}

func TestGenerateCorpusWiresDistributedReferences(t *testing.T) {
	reg := testRegistry(t)
	gen := NewCorpusGenerator(reg, true)

	ds, err := gen.GenerateCorpus(context.Background(), []string{"Observation", "Patient"}, 5, nil)
	if err != nil {
		t.Fatalf("GenerateCorpus returned error: %v", err)
	}

	obsCount, patCount := 0, 0
	for _, inst := range ds.Resources {
		switch inst.ResourceType {
		case "Observation":
			obsCount++
		case "Patient":
			patCount++
		}
	}
	if obsCount != 5 || patCount != 5 {
		t.Fatalf("got %d observations and %d patients, want 5 and 5", obsCount, patCount)
	}

	targets := make(map[string]bool)
	for _, inst := range ds.Resources {
		if inst.ResourceType != "Observation" {
			continue
		}
		subject, ok := inst.Resource["subject"].(map[string]any)
		if !ok {
			t.Fatalf("observation %s missing subject reference", inst.LocalID)
		}
		ref, _ := subject["reference"].(string)
		if !strings.HasPrefix(ref, "Patient/") {
			t.Fatalf("observation %s subject ref = %q, want Patient/…", inst.LocalID, ref)
		}
		targets[ref] = true
	}
	if len(targets) < 2 {
		t.Fatalf("references not distributed across patients: %v", targets)
	}
}

func TestGenerateCorpusHonoursPerTypeCounts(t *testing.T) {
	reg := testRegistry(t)
	gen := NewCorpusGenerator(reg, true)

	ds, err := gen.GenerateCorpus(context.Background(), []string{"Observation", "Patient"}, 3, map[string]int{"Patient": 7})
	if err != nil {
		t.Fatalf("GenerateCorpus returned error: %v", err)
	}
	counts := map[string]int{}
	for _, inst := range ds.Resources {
		counts[inst.ResourceType]++
	}
	if counts["Observation"] != 3 {
		t.Fatalf("observation count = %d, want 3", counts["Observation"])
	}
	if counts["Patient"] != 7 {
		t.Fatalf("patient count = %d, want 7 (override)", counts["Patient"])
	}
}

func TestGenerateCorpusExpandsReferenceTargets(t *testing.T) {
	reg := testRegistry(t)
	gen := NewCorpusGenerator(reg, true)

	// Only Observation requested; Patient is a reference target and must be
	// pulled in automatically so references resolve.
	ds, err := gen.GenerateCorpus(context.Background(), []string{"Observation"}, 3, nil)
	if err != nil {
		t.Fatalf("GenerateCorpus returned error: %v", err)
	}
	hasPatient := false
	for _, inst := range ds.Resources {
		if inst.ResourceType == "Patient" {
			hasPatient = true
		}
	}
	if !hasPatient {
		t.Fatal("expected Patient to be auto-added as a reference target")
	}
}

func TestGenerateCorpusRequiresTypes(t *testing.T) {
	reg := testRegistry(t)
	gen := NewCorpusGenerator(reg, true)
	if _, err := gen.GenerateCorpus(context.Background(), nil, 3, nil); err == nil {
		t.Fatal("expected error for empty resource types")
	}
}

// TestGenerateCorpusDisambiguatesCollidingTypeIDs verifies that resource types
// whose sanitized id segments collide (e.g. "A/B" and "A-B" both sanitize to
// "A-B") still produce distinct local ids, so neither type's resources are
// silently overwritten in the dataset.
func TestGenerateCorpusDisambiguatesCollidingTypeIDs(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{
		URL: "http://example.org/StructureDefinition/ab", Type: "A/B",
		Elements: []model.ElementDefinition{
			{Path: "A/B", Min: 0, Max: "*"},
			{Path: "A/B.value", Min: 1, Max: "1", Types: []model.ElementType{{Code: "string"}}},
		},
	})
	reg.AddStructureDefinition(&model.StructureDefinition{
		URL: "http://example.org/StructureDefinition/ab2", Type: "A-B",
		Elements: []model.ElementDefinition{
			{Path: "A-B", Min: 0, Max: "*"},
			{Path: "A-B.value", Min: 1, Max: "1", Types: []model.ElementType{{Code: "string"}}},
		},
	})
	gen := NewCorpusGenerator(reg, true)

	ds, err := gen.GenerateCorpus(context.Background(), []string{"A/B", "A-B"}, 2, nil)
	if err != nil {
		t.Fatalf("GenerateCorpus returned error: %v", err)
	}
	counts := map[string]int{}
	for _, inst := range ds.Resources {
		counts[inst.ResourceType]++
	}
	if counts["A/B"] != 2 || counts["A-B"] != 2 {
		t.Fatalf("counts = %v, want A/B:2 A-B:2 (ids must not collide across types)", counts)
	}
}

// TestGenerateCorpusSurfacesSynthesisFailures verifies that a resource type that
// cannot be synthesized (its profile has no element tree) is surfaced as an error
// rather than silently reported as having the requested number of resources.
func TestGenerateCorpusSurfacesSynthesisFailures(t *testing.T) {
	reg := registry.New()
	// A profile with no elements resolves to a nil root, so the type cannot be
	// synthesized.
	reg.AddStructureDefinition(&model.StructureDefinition{
		URL: "http://example.org/StructureDefinition/broken", Type: "Broken",
	})
	gen := NewCorpusGenerator(reg, true)

	if _, err := gen.GenerateCorpus(context.Background(), []string{"Broken"}, 3, nil); err == nil {
		t.Fatal("expected error when a resource type cannot be synthesized")
	}
}

// TestPopulateChildrenKeepsRequiredIntermediate verifies that a complex
// intermediate node without its own definition is still emitted in non-exhaustive
// mode when a descendant is required (Min > 0), so required containers are never
// dropped from the resource.
func TestPopulateChildrenKeepsRequiredIntermediate(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{
		URL: "http://example.org/StructureDefinition/obs", Type: "Observation",
		Elements: []model.ElementDefinition{
			{Path: "Observation", Min: 0, Max: "*"},
			// component is an intermediate with no Definition; its child code is required.
			{Path: "Observation.component.code", Min: 1, Max: "1", Types: []model.ElementType{{Code: "code"}}},
		},
	})
	resolved, err := reg.ResolveProfile("http://example.org/StructureDefinition/obs")
	if err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}

	body := map[string]any{"resourceType": "Observation", "id": "obs-1"}
	populateChildren(body, resolved.Root, reg, nil, false, newRNG("obs-1"))

	comp, ok := body["component"].(map[string]any)
	if !ok {
		t.Fatalf("component = %#v, want map (required intermediate must be kept in non-exhaustive mode)", body["component"])
	}
	if comp["code"] == nil {
		t.Fatalf("component.code missing: %#v", comp)
	}
}

// TestSetReferencePathPreservesRepeatableReferenceArray verifies that wiring a
// reference into a repeatable Reference field (e.g. Observation.performer as a
// Reference[]) updates the existing array instead of replacing it with a single
// object, which would produce invalid FHIR.
func TestSetReferencePathPreservesRepeatableReferenceArray(t *testing.T) {
	body := map[string]any{
		"resourceType": "Observation",
		"performer": []any{
			map[string]any{"reference": "Practitioner/old", "display": "Dr Old"},
			map[string]any{"reference": "Practitioner/other"},
		},
	}
	setReferencePath(body, "Observation.performer", refTarget{resourceType: "Practitioner", localID: "prac-7"})

	arr, ok := body["performer"].([]any)
	if !ok {
		t.Fatalf("performer = %T, want []any (array must not be replaced)", body["performer"])
	}
	if len(arr) != 2 {
		t.Fatalf("performer length = %d, want 2 (array elements preserved)", len(arr))
	}
	first, ok := arr[0].(map[string]any)
	if !ok {
		t.Fatalf("performer[0] = %T, want map", arr[0])
	}
	if first["reference"] != "Practitioner/prac-7" {
		t.Fatalf("performer[0].reference = %v, want Practitioner/prac-7", first["reference"])
	}
	if first["display"] != "Dr Old" {
		t.Fatalf("performer[0].display = %v, want Dr Old (other fields preserved)", first["display"])
	}
	if second, ok := arr[1].(map[string]any); !ok || second["reference"] != "Practitioner/other" {
		t.Fatalf("performer[1] = %v, want unchanged second element", arr[1])
	}
}

// TestSetReferencePathDescendsIntoRepeatableContainer verifies that a reference
// nested under a repeatable complex container (e.g. MedicationDispense.performer
// as a BackboneElement[] whose leaf is actor) descends into the first element
// instead of replacing the repeatable container with an empty map.
func TestSetReferencePathDescendsIntoRepeatableContainer(t *testing.T) {
	body := map[string]any{
		"resourceType": "MedicationDispense",
		"performer": []any{
			map[string]any{"function": map[string]any{"text": "responsible"}},
		},
	}
	setReferencePath(body, "MedicationDispense.performer.actor", refTarget{resourceType: "Practitioner", localID: "prac-3"})

	arr, ok := body["performer"].([]any)
	if !ok {
		t.Fatalf("performer = %T, want []any; repeatable container must not be replaced", body["performer"])
	}
	if len(arr) != 1 {
		t.Fatalf("performer length = %d, want 1", len(arr))
	}
	performer, ok := arr[0].(map[string]any)
	if !ok {
		t.Fatalf("performer[0] = %T, want map", arr[0])
	}
	if performer["function"] == nil {
		t.Fatal("performer[0].function lost when descending through the array")
	}
	actor, ok := performer["actor"].(map[string]any)
	if !ok {
		t.Fatalf("performer[0].actor = %T, want map", performer["actor"])
	}
	if actor["reference"] != "Practitioner/prac-3" {
		t.Fatalf("performer[0].actor.reference = %v, want Practitioner/prac-3", actor["reference"])
	}
}

// TestBulkSynthesizesSlicedExtensionWithFixedCoding verifies that the bulk
// synthesizer emits sliced extensions and resolves their nested Fixed coding. A
// profile whose extension is sliced (e.g. the suppressed extension on
// hcpd-organization) must appear in some instances with its required
// suppressedBy sub-extension carrying the fixed organisation-initiated coding,
// rather than being dropped or left with a generic placeholder coding.
func TestBulkSynthesizesSlicedExtensionWithFixedCoding(t *testing.T) {
	suppressedURL := "http://example.org/StructureDefinition/suppressed"
	fixedCoding := map[string]any{
		"system": "http://example.org/CodeSystem/responsible-party-type",
		"code":   "organisation-initiated",
	}
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{
		URL: suppressedURL, Type: "Extension",
		Elements: []model.ElementDefinition{
			{Path: "Extension", Min: 0, Max: "1"},
			{Path: "Extension.url", Min: 1, Max: "1", Fixed: suppressedURL},
			{Path: "Extension.extension", Min: 1, Max: "*"},
			{ID: "Extension.extension:suppressedBy", Path: "Extension.extension", SliceName: "suppressedBy", Min: 1, Max: "1"},
			{ID: "Extension.extension:suppressedBy.url", Path: "Extension.extension.url", Min: 1, Max: "1", Fixed: "suppressedBy"},
			{ID: "Extension.extension:suppressedBy.value[x]", Path: "Extension.extension.value[x]", Min: 1, Max: "1", Types: []model.ElementType{{Code: "CodeableConcept"}}},
			{ID: "Extension.extension:suppressedBy.value[x].coding", Path: "Extension.extension.value[x].coding", Min: 1, Max: "1", Fixed: fixedCoding, Types: []model.ElementType{{Code: "Coding"}}},
		},
	})
	reg.AddStructureDefinition(&model.StructureDefinition{
		URL: "http://example.org/StructureDefinition/org", Type: "Organization", Kind: "resource",
		Elements: []model.ElementDefinition{
			{Path: "Organization", Min: 0, Max: "1"},
			{Path: "Organization.extension", Min: 0, Max: "*"},
			{ID: "Organization.extension:suppressed", Path: "Organization.extension", SliceName: "suppressed", Min: 0, Max: "1", Types: []model.ElementType{{Code: "Extension", Profile: []string{suppressedURL}}}},
			{ID: "Organization.extension:suppressed.url", Path: "Organization.extension.url", Min: 1, Max: "1", Fixed: suppressedURL},
			{ID: "Organization.extension:suppressed.extension", Path: "Organization.extension.extension", Min: 1, Max: "*"},
			{ID: "Organization.extension:suppressed.extension:suppressedBy", Path: "Organization.extension.extension", SliceName: "suppressedBy", Min: 1, Max: "1"},
			{ID: "Organization.extension:suppressed.extension:suppressedBy.url", Path: "Organization.extension.extension.url", Min: 1, Max: "1", Fixed: "suppressedBy"},
			{ID: "Organization.extension:suppressed.extension:suppressedBy.value[x]", Path: "Organization.extension.extension.value[x]", Min: 1, Max: "1", Types: []model.ElementType{{Code: "CodeableConcept"}}},
			{ID: "Organization.extension:suppressed.extension:suppressedBy.value[x].coding", Path: "Organization.extension.extension.value[x].coding", Min: 1, Max: "1", Fixed: fixedCoding, Types: []model.ElementType{{Code: "Coding"}}},
		},
	})

	// synthesizeSliceValue must resolve the suppressed slice's nested fixed coding.
	resolved, err := reg.ResolveProfile("http://example.org/StructureDefinition/org")
	if err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}
	ext := resolved.Root.Children["extension"]
	if ext == nil {
		t.Fatal("expected extension child node")
	}
	suppressed := ext.Slices["suppressed"]
	if suppressed == nil {
		t.Fatal("expected suppressed slice")
	}
	val := synthesizeSliceValue(suppressed, reg, nil, newRNG("test"))
	m, ok := val.(map[string]any)
	if !ok {
		t.Fatalf("synthesizeSliceValue = %T, want map", val)
	}
	if m["url"] != suppressedURL {
		t.Fatalf("suppressed url = %v, want %s", m["url"], suppressedURL)
	}
	rawExt, ok := m["extension"].([]any)
	if !ok || len(rawExt) == 0 {
		t.Fatalf("suppressed extension missing nested suppressedBy, got %#v", m)
	}
	sub, ok := rawExt[0].(map[string]any)
	if !ok || sub["url"] != "suppressedBy" {
		t.Fatalf("expected suppressedBy sub-extension, got %#v", rawExt[0])
	}
	cc, ok := sub["valueCodeableConcept"].(map[string]any)
	if !ok {
		t.Fatalf("suppressedBy missing valueCodeableConcept, got %#v", sub)
	}
	codings, ok := cc["coding"].([]any)
	if !ok || len(codings) == 0 {
		t.Fatalf("suppressedBy valueCodeableConcept missing coding, got %#v", cc)
	}
	coding, ok := codings[0].(map[string]any)
	if !ok || coding["code"] != "organisation-initiated" {
		t.Fatalf("suppressedBy coding = %#v, want organisation-initiated", codings[0])
	}
}

// TestDefaultProfilePrefersScopedProfile verifies that defaultProfile prefers a
// scoped (package) profile over the base FHIR profile for a resource type, so
// package-specific extensions are exercised by bulk generation.
func TestDefaultProfilePrefersScopedProfile(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://hl7.org/fhir/StructureDefinition/Organization", Type: "Organization", Kind: "resource"})
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/org", Type: "Organization", Kind: "resource"})
	reg.SetScope([]string{"http://example.org/StructureDefinition/org"})
	if got := defaultProfile(reg, "Organization"); got != "http://example.org/StructureDefinition/org" {
		t.Fatalf("defaultProfile = %q, want scoped example profile", got)
	}
	reg.SetScope(nil)
	if got := defaultProfile(reg, "Organization"); got != "http://hl7.org/fhir/StructureDefinition/Organization" {
		t.Fatalf("defaultProfile unscoped = %q, want first (base) profile", got)
	}
}
