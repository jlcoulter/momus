package resource

import (
	"context"
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

func requireInstance(t *testing.T, ds *model.Dataset, localID string) *model.ResourceInstance {
	t.Helper()
	inst, ok := ds.Resources[localID]
	if !ok {
		t.Fatalf("expected resource %s in dataset; have: %v", localID, keys(ds.Resources))
	}
	return inst
}

func keys(m map[string]*model.ResourceInstance) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestGeneratePopulatesRequiredElements(t *testing.T) {
	reg := testRegistry(t)
	gen := NewGenerator(reg)

	ds, err := gen.Generate(context.Background(), model.DataRequirement{
		Resource:    model.ResourceRequirement{Type: "Observation", Profile: []string{obsProfile}},
		Cardinality: model.Exactly(1),
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	inst := requireInstance(t, ds, "momus-Observation")
	if inst.ResourceType != "Observation" {
		t.Fatalf("got resource type %q", inst.ResourceType)
	}
	body := inst.Resource
	if body["status"] == nil {
		t.Fatal("expected required status element to be populated")
	}
	if body["code"] == nil {
		t.Fatal("expected required code element to be populated")
	}
}

func TestGenerateWiresRelationshipReference(t *testing.T) {
	reg := testRegistry(t)
	gen := NewGenerator(reg)

	ds, err := gen.Generate(context.Background(), model.DataRequirement{
		ID:          "obs-with-subject",
		Resource:    model.ResourceRequirement{Type: "Observation", Profile: []string{obsProfile}},
		Cardinality: model.Exactly(1),
		Relationships: []model.RelationshipRequirement{{
			Path:   "Observation.subject",
			Target: model.ResourceRequirement{Type: "Patient", Profile: []string{patientProfile}},
		}},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	// The Patient target was generated and the Observation references it.
	var patient *model.ResourceInstance
	for _, inst := range ds.Resources {
		if inst.ResourceType == "Patient" {
			patient = inst
			break
		}
	}
	if patient == nil {
		t.Fatalf("expected a generated Patient target; have %v", keys(ds.Resources))
	}

	obs := requireInstance(t, ds, "momus-obs-with-subject")
	subject, ok := obs.Resource["subject"].(map[string]any)
	if !ok {
		t.Fatalf("expected subject reference in observation, got %v", obs.Resource["subject"])
	}
	if subject["reference"] != "Patient/"+patient.LocalID {
		t.Fatalf("got reference %v, want Patient/%s", subject["reference"], patient.LocalID)
	}

	if len(ds.Relationships) != 1 {
		t.Fatalf("got %d relationships, want 1", len(ds.Relationships))
	}
	rel := ds.Relationships[0]
	if rel.Path != "Observation.subject" || rel.TargetID != patient.LocalID {
		t.Fatalf("unexpected relationship: %+v", rel)
	}
}

func TestGenerateHonoursCardinality(t *testing.T) {
	reg := testRegistry(t)
	gen := NewGenerator(reg)

	ds, err := gen.Generate(context.Background(), model.DataRequirement{
		Resource:    model.ResourceRequirement{Type: "Observation", Profile: []string{obsProfile}},
		Cardinality: model.Exactly(2),
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if len(ds.Resources) != 2 {
		t.Fatalf("got %d resources, want 2", len(ds.Resources))
	}
	for _, id := range []string{"momus-Observation", "momus-Observation-2"} {
		requireInstance(t, ds, id)
	}
}

func TestGenerateAppliesEqualsConstraints(t *testing.T) {
	reg := testRegistry(t)
	gen := NewGenerator(reg)

	ds, err := gen.Generate(context.Background(), model.DataRequirement{
		Resource:    model.ResourceRequirement{Type: "Observation", Profile: []string{obsProfile}},
		Cardinality: model.Exactly(1),
		Constraints: []model.Constraint{{Path: "Observation.status", Operator: model.OpEquals, Value: "final"}},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	obs := requireInstance(t, ds, "momus-Observation")
	if obs.Resource["status"] != "final" {
		t.Fatalf("got status %v, want final", obs.Resource["status"])
	}
}

func TestGenerateRequiresRegistryAndType(t *testing.T) {
	if _, err := NewGenerator(nil).Generate(context.Background(), model.DataRequirement{}); err == nil {
		t.Fatal("expected error for nil registry")
	}
	reg := testRegistry(t)
	if _, err := NewGenerator(reg).Generate(context.Background(), model.DataRequirement{}); err == nil {
		t.Fatal("expected error for missing resource type")
	}
}
