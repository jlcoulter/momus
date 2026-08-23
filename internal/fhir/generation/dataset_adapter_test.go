package generation

import (
	"testing"

	"github.com/jlcoulter/momus/internal/core/ast"
	"github.com/jlcoulter/momus/internal/fhir/model"
)

func TestToCoreDatasetRoundTrip(t *testing.T) {
	fhirDS := &model.Dataset{
		Resources: map[string]*model.ResourceInstance{
			"p1": {
				LocalID:      "p1",
				ResourceType: "Patient",
				Profile:      "http://example.org/StructureDefinition/patient",
				Resource:     map[string]any{"id": "p1", "active": true},
				ServerID:     "srv-1",
				Version:      "1",
			},
		},
		Relationships: []model.Reference{
			{SourceID: "o1", Path: "subject", TargetID: "p1"},
		},
	}
	coreDS := ToCoreDataset(fhirDS)
	if coreDS == nil {
		t.Fatal("ToCoreDataset returned nil")
	}
	inst := coreDS.Resources["p1"]
	if inst == nil {
		t.Fatal("core dataset missing resource p1")
	}
	if inst.ResourceType != "Patient" || inst.Profile != "http://example.org/StructureDefinition/patient" || inst.ServerID != "srv-1" {
		t.Fatalf("core instance = %+v, fields not copied", inst)
	}
	if len(coreDS.Relationships) != 1 || coreDS.Relationships[0].Path != "subject" {
		t.Fatalf("relationships not copied: %+v", coreDS.Relationships)
	}

	// Round-trip back to FHIR.
	back := FromCoreDataset(coreDS)
	if back == nil {
		t.Fatal("FromCoreDataset returned nil")
	}
	bi := back.Resources["p1"]
	if bi == nil || bi.LocalID != "p1" || bi.ResourceType != "Patient" || bi.ServerID != "srv-1" {
		t.Fatalf("round-trip instance = %+v", bi)
	}
	if len(back.Relationships) != 1 || back.Relationships[0].SourceID != "o1" {
		t.Fatalf("round-trip relationships = %+v", back.Relationships)
	}
}

func TestDatasetAdapterNilHandling(t *testing.T) {
	if ToCoreDataset(nil) != nil {
		t.Fatal("ToCoreDataset(nil) should return nil")
	}
	if FromCoreDataset(nil) != nil {
		t.Fatal("FromCoreDataset(nil) should return nil")
	}
}

func TestDatasetAdapterSkipsNilInstances(t *testing.T) {
	fhirDS := &model.Dataset{
		Resources: map[string]*model.ResourceInstance{
			"p1": {LocalID: "p1", ResourceType: "Patient"},
			"p2": nil,
		},
	}
	core := ToCoreDataset(fhirDS)
	if len(core.Resources) != 1 {
		t.Fatalf("expected 1 resource after skipping nil, got %d", len(core.Resources))
	}
	if _, ok := core.Resources["p2"]; ok {
		t.Fatal("nil instance should not be copied")
	}

	coreDS := &ast.Dataset{
		Resources: map[string]*ast.ResourceInstance{
			"p1": {LocalID: "p1", ResourceType: "Patient"},
			"p2": nil,
		},
	}
	back := FromCoreDataset(coreDS)
	if len(back.Resources) != 1 {
		t.Fatalf("expected 1 FHIR resource after skipping nil, got %d", len(back.Resources))
	}
}
