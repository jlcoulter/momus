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
