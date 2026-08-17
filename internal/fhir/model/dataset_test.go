package model

import "testing"

func TestDatasetRepresentsPatientReferencedByObservation(t *testing.T) {
	ds := &Dataset{
		Resources: map[string]*ResourceInstance{
			"patient-1": {LocalID: "patient-1", ResourceType: "Patient"},
			"obs-1":     {LocalID: "obs-1", ResourceType: "Observation"},
		},
		Relationships: []Reference{
			{SourceID: "obs-1", Path: "subject", TargetID: "patient-1"},
		},
	}

	obs, ok := ds.Resources["obs-1"]
	if !ok {
		t.Fatal("expected observation in dataset")
	}
	if obs.ResourceType != "Observation" {
		t.Fatalf("got resource type %q, want Observation", obs.ResourceType)
	}
	if obs.LocalID != "obs-1" {
		t.Fatalf("got local id %q, want obs-1", obs.LocalID)
	}

	if len(ds.Relationships) != 1 {
		t.Fatalf("got %d relationships, want 1", len(ds.Relationships))
	}
	rel := ds.Relationships[0]
	if rel.SourceID != "obs-1" || rel.Path != "subject" || rel.TargetID != "patient-1" {
		t.Fatalf("unexpected relationship %+v", rel)
	}
}
