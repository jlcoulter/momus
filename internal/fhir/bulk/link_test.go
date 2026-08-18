package bulk

import (
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
)

func instance(id, resourceType string, resource map[string]any) *model.ResourceInstance {
	return &model.ResourceInstance{LocalID: id, ResourceType: resourceType, Resource: resource}
}

func TestLinkRetainsAllInstances(t *testing.T) {
	datasets := []*model.Dataset{
		{Resources: map[string]*model.ResourceInstance{
			"momus-req-1": instance("momus-req-1", "Observation", map[string]any{"resourceType": "Observation", "id": "momus-req-1"}),
			"momus-req-2": instance("momus-req-2", "Observation", map[string]any{"resourceType": "Observation", "id": "momus-req-2"}),
		}},
		{Resources: map[string]*model.ResourceInstance{
			"momus-Patient-1": instance("momus-Patient-1", "Patient", map[string]any{"resourceType": "Patient", "id": "momus-Patient-1"}),
			"momus-Patient-2": instance("momus-Patient-2", "Patient", map[string]any{"resourceType": "Patient", "id": "momus-Patient-2"}),
		}},
	}

	out := Link(datasets)
	if len(out) != 4 {
		t.Fatalf("got %d resources, want 4 (all retained)", len(out))
	}
}

func TestLinkRewiresDanglingReferencesDistributed(t *testing.T) {
	datasets := []*model.Dataset{
		{Resources: map[string]*model.ResourceInstance{
			"momus-Observation-1": instance("momus-Observation-1", "Observation", map[string]any{"resourceType": "Observation", "id": "momus-Observation-1", "subject": map[string]any{"reference": "Patient/unknown"}}),
			"momus-Observation-2": instance("momus-Observation-2", "Observation", map[string]any{"resourceType": "Observation", "id": "momus-Observation-2", "subject": map[string]any{"reference": "Patient/unknown"}}),
			"momus-Observation-3": instance("momus-Observation-3", "Observation", map[string]any{"resourceType": "Observation", "id": "momus-Observation-3", "subject": map[string]any{"reference": "Patient/unknown"}}),
			"momus-Patient-1":     instance("momus-Patient-1", "Patient", map[string]any{"resourceType": "Patient", "id": "momus-Patient-1"}),
			"momus-Patient-2":     instance("momus-Patient-2", "Patient", map[string]any{"resourceType": "Patient", "id": "momus-Patient-2"}),
		}},
	}

	out := Link(datasets)
	targets := make(map[string]bool)
	for _, inst := range out {
		if inst.ResourceType != "Observation" {
			continue
		}
		ref := inst.Resource["subject"].(map[string]any)["reference"].(string)
		if ref == "Patient/unknown" {
			t.Fatalf("reference %s was not rewired", inst.LocalID)
		}
		targets[ref] = true
	}
	// The three observations should spread across the two patients rather than
	// all pointing at the same one.
	if len(targets) < 2 {
		t.Fatalf("references were not distributed: all point at %v", targets)
	}
}

func TestLinkRewiresDanglingToSingleAvailableTarget(t *testing.T) {
	datasets := []*model.Dataset{
		{Resources: map[string]*model.ResourceInstance{
			"momus-req-1":     instance("momus-req-1", "Observation", map[string]any{"resourceType": "Observation", "id": "momus-req-1", "subject": map[string]any{"reference": "Patient/unknown"}}),
			"momus-Patient-1": instance("momus-Patient-1", "Patient", map[string]any{"resourceType": "Patient", "id": "momus-Patient-1"}),
		}},
	}

	out := Link(datasets)
	obs := out[0].Resource
	if got := obs["subject"].(map[string]any)["reference"]; got != "Patient/momus-Patient-1" {
		t.Fatalf("subject reference = %v, want Patient/momus-Patient-1", got)
	}
}

func TestLinkIsDeterministicAndIgnoresURLs(t *testing.T) {
	datasets := []*model.Dataset{
		{Resources: map[string]*model.ResourceInstance{
			"momus-req-1": instance("momus-req-1", "Binary", map[string]any{
				"resourceType": "Binary",
				"id":           "momus-req-1",
				// An Attachment-style reference that is a URL, not a resource ref.
				"content": map[string]any{"reference": "http://example.org/fhir/reference"},
				"subject": map[string]any{"reference": "Patient/unknown"},
			}),
			"momus-Patient-1": instance("momus-Patient-1", "Patient", map[string]any{"resourceType": "Patient", "id": "momus-Patient-1"}),
		}},
	}

	first := Link(datasets)
	second := Link(datasets)
	if got := first[0].Resource["content"].(map[string]any)["reference"]; got != "http://example.org/fhir/reference" {
		t.Fatalf("URL reference was mutated: %v", got)
	}
	if got := first[0].Resource["subject"].(map[string]any)["reference"]; got != "Patient/momus-Patient-1" {
		t.Fatalf("dangling ref not rewired: %v", got)
	}
	if len(first) != len(second) {
		t.Fatalf("Link not deterministic: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].LocalID != second[i].LocalID {
			t.Fatalf("Link not deterministic at %d: %s vs %s", i, first[i].LocalID, second[i].LocalID)
		}
	}
}
