package generation

import (
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
)

// TestRecordBodyReferencesDeterministic verifies that recordBodyReferences emits
// relationships in deterministic SourceID order regardless of map iteration
// order, so the generated dataset is stable across runs.
func TestRecordBodyReferencesDeterministic(t *testing.T) {
	ds := &model.Dataset{Resources: map[string]*model.ResourceInstance{
		"z-obs": {LocalID: "z-obs", ResourceType: "Observation", Resource: map[string]any{
			"resourceType": "Observation",
			"subject":      map[string]any{"reference": "Patient/a-pat"},
		}},
		"a-pat": {LocalID: "a-pat", ResourceType: "Patient", Resource: map[string]any{
			"resourceType":         "Patient",
			"managingOrganization": map[string]any{"reference": "Organization/m-org"},
		}},
		"m-org": {LocalID: "m-org", ResourceType: "Organization", Resource: map[string]any{
			"resourceType": "Organization",
		}},
	}}

	recordBodyReferences(ds)

	if len(ds.Relationships) != 2 {
		t.Fatalf("got %d relationships, want 2", len(ds.Relationships))
	}
	// Relationships must be ordered by SourceID: a-pat before z-obs.
	if ds.Relationships[0].SourceID != "a-pat" || ds.Relationships[1].SourceID != "z-obs" {
		t.Fatalf("relationships not sorted by SourceID: %+v", ds.Relationships)
	}
}
