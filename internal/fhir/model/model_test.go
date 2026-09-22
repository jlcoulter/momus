package model

import (
	"encoding/json"
	"testing"

	fhir "github.com/jlcoulter/fhir-registry"
)

func TestToFhirSnapshot(t *testing.T) {
	sd := &StructureDefinition{
		URL:      "http://example.org/StructureDefinition/patient",
		Name:     "PatientProfile",
		Type:     "Patient",
		Kind:     "resource",
		Abstract: true,
		Elements: []ElementDefinition{{Path: "Patient", Min: 0, Max: fhir.MaxUnbounded}},
	}
	out := sd.ToFhir()
	if out == nil {
		t.Fatal("ToFhir returned nil")
	}
	if out.URL != sd.URL || out.Name != sd.Name {
		t.Fatalf("ToFhir did not copy identity fields: %+v", out)
	}
	if out.Abstract != true {
		t.Fatal("ToFhir did not set Abstract")
	}
	if out.Snapshot == nil {
		t.Fatal("ToFhir snapshot should be set for a definition without a base")
	}
	if out.Differential != nil {
		t.Fatal("ToFhir differential should be nil for a definition without a base")
	}
}

func TestToFhirDifferential(t *testing.T) {
	sd := &StructureDefinition{
		URL:            "http://example.org/StructureDefinition/patient",
		Name:           "PatientProfile",
		Type:           "Patient",
		Kind:           "resource",
		BaseDefinition: "http://hl7.org/fhir/StructureDefinition/Patient",
		Derivation:     "constraint",
		Elements:       []ElementDefinition{{Path: "Patient.name", Min: 1, Max: fhir.MaxUnbounded}},
	}
	out := sd.ToFhir()
	if out == nil {
		t.Fatal("ToFhir returned nil")
	}
	if out.Differential == nil {
		t.Fatal("ToFhir differential should be set for a derived definition without a snapshot")
	}
	if out.Snapshot != nil {
		t.Fatal("ToFhir snapshot should be nil for a differential definition")
	}
}

func TestUnmarshalJSONLegacyElementsShape(t *testing.T) {
	data := `{
		"url": "http://example.org/StructureDefinition/patient",
		"type": "Patient",
		"elements": [
			{"path": "Patient", "min": 0, "max": "*"},
			{"path": "Patient.name", "min": 1, "max": "1"}
		]
	}`
	var sd StructureDefinition
	if err := json.Unmarshal([]byte(data), &sd); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if sd.URL != "http://example.org/StructureDefinition/patient" || sd.Type != "Patient" {
		t.Fatalf("identity not decoded: %+v", sd)
	}
	if sd.HasSnapshot {
		t.Fatal("HasSnapshot should be false for the flat elements shape")
	}
	if len(sd.Elements) != 2 {
		t.Fatalf("Elements = %d, want 2", len(sd.Elements))
	}
	if sd.Elements[0].Max != fhir.MaxUnbounded {
		t.Fatalf("Elements[0].Max = %v, want MaxUnbounded", sd.Elements[0].Max)
	}
	if sd.Elements[1].Max != fhir.Max(1) {
		t.Fatalf("Elements[1].Max = %v, want Max(1)", sd.Elements[1].Max)
	}
}

func TestUnmarshalJSONSnapshotShape(t *testing.T) {
	data := `{
		"url": "http://example.org/StructureDefinition/patient",
		"type": "Patient",
		"snapshot": {"element": []}
	}`
	var sd StructureDefinition
	if err := json.Unmarshal([]byte(data), &sd); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if !sd.HasSnapshot {
		t.Fatal("HasSnapshot should be true when a snapshot block is present")
	}
}

func TestUnmarshalJSONDifferentialShape(t *testing.T) {
	data := `{
		"url": "http://example.org/StructureDefinition/patient",
		"type": "Patient",
		"differential": {"element": []}
	}`
	var sd StructureDefinition
	if err := json.Unmarshal([]byte(data), &sd); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if sd.HasSnapshot {
		t.Fatal("HasSnapshot should be false for a differential-only definition")
	}
}

func TestUnmarshalJSONInvalid(t *testing.T) {
	var sd StructureDefinition
	if err := json.Unmarshal([]byte(`{"url": `), &sd); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
