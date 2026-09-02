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
	if len(ds.Relationships) != 5 {
		t.Fatalf("relationships = %d, want one Observation.subject relationship per observation", len(ds.Relationships))
	}
	for _, rel := range ds.Relationships {
		if rel.SourceID == "" || rel.TargetID == "" || rel.Path != "Observation.subject" {
			t.Fatalf("unexpected relationship: %+v", rel)
		}
		if ds.Resources[rel.SourceID].ResourceType != "Observation" {
			t.Fatalf("relationship source = %s, want Observation", ds.Resources[rel.SourceID].ResourceType)
		}
		if ds.Resources[rel.TargetID].ResourceType != "Patient" {
			t.Fatalf("relationship target = %s, want Patient", ds.Resources[rel.TargetID].ResourceType)
		}
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

// TestGenerateCorpusWiresOnlyToAlreadyCreatedTargets verifies that reference
// wiring only points at resources that will already exist when the referencing
// resource is provisioned. A forward reference (Observation.subject → Patient,
// where Patient is created after Observation in the topological order) must be
// stripped rather than left dangling, so the corpus contains no references to
// not-yet-created resources.
func TestGenerateCorpusWiresOnlyToAlreadyCreatedTargets(t *testing.T) {
	reg := testRegistry(t)
	gen := NewCorpusGenerator(reg, true)

	// Observation references Patient. In the topological order Patient (no deps)
	// is created before Observation, so Observation.subject must resolve to a
	// real Patient instance.
	ds, err := gen.GenerateCorpus(context.Background(), []string{"Observation"}, 5, nil)
	if err != nil {
		t.Fatalf("GenerateCorpus returned error: %v", err)
	}
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
	}
}

// TestGenerateCorpusStripsForwardReferences verifies that no reference in the
// corpus points to a not-yet-created resource: forward references (to a type
// created later in the topological order) and self-references with no earlier
// peer are stripped, leaving only references to resources that will already
// exist when provisioned.
func TestGenerateCorpusStripsForwardReferences(t *testing.T) {
	reg := registry.New()
	// Organization references Endpoint and Organization (self); Endpoint
	// references Organization. This forms a mutual cycle, so the topological
	// order breaks it deterministically (Endpoint before Organization).
	reg.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/org",
		Type: "Organization",
		Elements: []model.ElementDefinition{
			{Path: "Organization", Min: 0, Max: "*"},
			{Path: "Organization.partOf", Min: 0, Max: "1", Types: []model.ElementType{{Code: "Reference", TargetProfile: []string{"http://example.org/StructureDefinition/org"}}}},
			{Path: "Organization.endpoint", Min: 0, Max: "*", Types: []model.ElementType{{Code: "Reference", TargetProfile: []string{"http://example.org/StructureDefinition/endpoint"}}}},
		},
	})
	reg.AddStructureDefinition(&model.StructureDefinition{
		URL:  "http://example.org/StructureDefinition/endpoint",
		Type: "Endpoint",
		Elements: []model.ElementDefinition{
			{Path: "Endpoint", Min: 0, Max: "*"},
			{Path: "Endpoint.managingOrganization", Min: 0, Max: "1", Types: []model.ElementType{{Code: "Reference", TargetProfile: []string{"http://example.org/StructureDefinition/org"}}}},
		},
	})
	gen := NewCorpusGenerator(reg, true)

	ds, err := gen.GenerateCorpus(context.Background(), []string{"Organization", "Endpoint"}, 3, nil)
	if err != nil {
		t.Fatalf("GenerateCorpus returned error: %v", err)
	}
	// Every reference in every resource body must point to a resource that
	// exists in the corpus (no dangling placeholders, no forward references).
	for _, inst := range ds.Resources {
		collectReferences(inst.Resource, func(ref string) {
			if danglingRef.MatchString(ref) {
				t.Fatalf("resource %s has dangling reference %q", inst.LocalID, ref)
			}
			targetType, targetID := splitReference(ref)
			if targetType == "" || targetID == "" {
				t.Fatalf("resource %s has malformed reference %q", inst.LocalID, ref)
			}
			if _, ok := ds.Resources[targetID]; !ok {
				t.Fatalf("resource %s references %q which is not in the corpus", inst.LocalID, ref)
			}
		})
	}
}

// collectReferences walks a resource body and invokes fn for every reference
// string found.
func collectReferences(value any, fn func(ref string)) {
	switch typed := value.(type) {
	case map[string]any:
		if ref, ok := typed["reference"].(string); ok {
			fn(ref)
		}
		for _, v := range typed {
			collectReferences(v, fn)
		}
	case []any:
		for _, item := range typed {
			collectReferences(item, fn)
		}
	}
}

func TestExampleReferenceTargetsFromInstances(t *testing.T) {
	reg := registry.New()
	// A HealthcareService example referencing Organization, Location, Endpoint,
	// and a coverageArea Location.
	reg.AddResource(&model.Resource{
		ResourceType: "HealthcareService",
		Raw: map[string]any{
			"resourceType": "HealthcareService",
			"providedBy":   map[string]any{"reference": "Organization/org-1"},
			"location":     []any{map[string]any{"reference": "Location/loc-1"}},
			"endpoint":     []any{map[string]any{"reference": "Endpoint/ep-1"}},
			"coverageArea": []any{map[string]any{"reference": "Location/loc-2"}},
		},
	})
	// A second example with a different providedBy target to ensure first-wins.
	reg.AddResource(&model.Resource{
		ResourceType: "HealthcareService",
		Raw: map[string]any{
			"resourceType": "HealthcareService",
			"providedBy":   map[string]any{"reference": "Organization/org-2"},
		},
	})

	refs := exampleReferenceTargets(reg, "HealthcareService")
	expected := map[string]string{
		"HealthcareService.providedBy":   "Organization",
		"HealthcareService.location":     "Location",
		"HealthcareService.endpoint":     "Endpoint",
		"HealthcareService.coverageArea": "Location",
	}
	for path, want := range expected {
		if got, ok := refs[path]; !ok || got != want {
			t.Fatalf("exampleReferenceTargets[%q] = %q, want %q (full refs: %v)", path, got, want, refs)
		}
	}
}

func TestExampleReferenceTargetsSkippedForOtherTypes(t *testing.T) {
	reg := registry.New()
	reg.AddResource(&model.Resource{ResourceType: "PractitionerRole", Raw: map[string]any{
		"resourceType":      "PractitionerRole",
		"practitioner":      map[string]any{"reference": "Practitioner/p-1"},
		"organization":      map[string]any{"reference": "Organization/o-1"},
		"location":          []any{map[string]any{"reference": "Location/l-1"}},
		"healthcareService": []any{map[string]any{"reference": "HealthcareService/hs-1"}},
	}})
	refs := exampleReferenceTargets(reg, "PractitionerRole")
	if refs["PractitionerRole.practitioner"] != "Practitioner" {
		t.Fatalf("practitioner target = %q, want Practitioner", refs["PractitionerRole.practitioner"])
	}
	if refs["PractitionerRole.healthcareService"] != "HealthcareService" {
		t.Fatalf("healthcareService target = %q, want HealthcareService", refs["PractitionerRole.healthcareService"])
	}
	// A type with no examples yields no targets.
	if got := exampleReferenceTargets(reg, "Observation"); len(got) != 0 {
		t.Fatalf("exampleReferenceTargets(Observation) = %v, want empty", got)
	}
}

func TestResourceTypeOfProfileStripsVersion(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{
		URL: "http://hl7.org/fhir/StructureDefinition/Organization", Type: "Organization", Kind: "resource",
		Elements: []model.ElementDefinition{{Path: "Organization", Min: 0, Max: "1"}},
	})
	// A versioned target-profile canonical must resolve to the resource type by
	// stripping the "|4.0.1" suffix. Before the fix this returned "" and the
	// reference target was lost (masked by the removed static table).
	if got := resourceTypeOfProfile(reg, "http://hl7.org/fhir/StructureDefinition/Organization|4.0.1"); got != "Organization" {
		t.Fatalf("resourceTypeOfProfile(versioned) = %q, want Organization", got)
	}
	if got := resourceTypeOfProfile(reg, "http://hl7.org/fhir/StructureDefinition/Organization"); got != "Organization" {
		t.Fatalf("resourceTypeOfProfile(plain) = %q, want Organization", got)
	}
	if got := resourceTypeOfProfile(reg, "http://example.org/missing"); got != "" {
		t.Fatalf("resourceTypeOfProfile(missing) = %q, want empty", got)
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

func TestGenerateCorpusSkipsAbstractResourceTypes(t *testing.T) {
	reg := testRegistry(t)
	reg.AddStructureDefinition(&model.StructureDefinition{
		URL:      "http://hl7.org/fhir/StructureDefinition/Resource",
		Type:     "Resource",
		Kind:     "resource",
		Elements: []model.ElementDefinition{{Path: "Resource", Min: 0, Max: "*"}},
	})
	gen := NewCorpusGenerator(reg, true)

	ds, err := gen.GenerateCorpus(context.Background(), []string{"Patient", "Resource"}, 2, nil)
	if err != nil {
		t.Fatalf("GenerateCorpus returned error: %v", err)
	}
	for _, inst := range ds.Resources {
		if inst.ResourceType == "Resource" {
			t.Fatalf("generated abstract Resource instance: %+v", inst)
		}
	}
	if len(ds.Resources) != 2 {
		t.Fatalf("generated resource count = %d, want 2 concrete Patient resources", len(ds.Resources))
	}

	if _, err := gen.GenerateCorpus(context.Background(), []string{"Resource"}, 2, nil); err == nil {
		t.Fatal("expected an error when only abstract resource types are requested")
	}
}

func TestGenerateCorpusDoesNotExpandAbstractResourceTarget(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{
		URL:  patientProfile,
		Type: "Patient",
		Kind: "resource",
		Elements: []model.ElementDefinition{
			{Path: "Patient", Min: 0, Max: "*"},
			{Path: "Patient.managingOrganization", Min: 0, Max: "1", Types: []model.ElementType{{Code: "Reference", TargetProfile: []string{"http://hl7.org/fhir/StructureDefinition/Resource"}}}},
		},
	})
	reg.AddStructureDefinition(&model.StructureDefinition{
		URL:      "http://hl7.org/fhir/StructureDefinition/Resource",
		Type:     "Resource",
		Kind:     "resource",
		Elements: []model.ElementDefinition{{Path: "Resource", Min: 0, Max: "*"}},
	})
	gen := NewCorpusGenerator(reg, true)

	ds, err := gen.GenerateCorpus(context.Background(), []string{"Patient"}, 2, nil)
	if err != nil {
		t.Fatalf("GenerateCorpus returned error: %v", err)
	}
	for _, inst := range ds.Resources {
		if inst.ResourceType == "Resource" {
			t.Fatalf("generated abstract Resource instance from reference target: %+v", inst)
		}
	}
}

func TestGenerateCorpusRequiresTypes(t *testing.T) {
	reg := testRegistry(t)
	gen := NewCorpusGenerator(reg, true)
	if _, err := gen.GenerateCorpus(context.Background(), nil, 3, nil); err == nil {
		t.Fatal("expected error for empty resource types")
	}
}

// TestGenerateCorpusHonoursCancellation verifies that a cancelled context stops
// the parallel synthesis fan-in and surfaces the cancellation instead of
// blocking on the per-type goroutines.
func TestGenerateCorpusHonoursCancellation(t *testing.T) {
	reg := testRegistry(t)
	gen := NewCorpusGenerator(reg, true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := gen.GenerateCorpus(ctx, []string{"Observation", "Patient"}, 5, nil); err == nil {
		t.Fatal("expected error when context is cancelled")
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
	setReferencePath(body, "Observation.performer", refTarget{resourceType: "Practitioner", localID: "prac-7"}, true)

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
	setReferencePath(body, "MedicationDispense.performer.actor", refTarget{resourceType: "Practitioner", localID: "prac-3"}, false)

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
