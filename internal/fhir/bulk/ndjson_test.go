package bulk

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
)

func testDataset() *model.Dataset {
	return &model.Dataset{
		Resources: map[string]*model.ResourceInstance{
			"momus-Observation-2": {
				LocalID:      "momus-Observation-2",
				ResourceType: "Observation",
				Resource:     map[string]any{"resourceType": "Observation", "id": "momus-Observation-2", "status": "final"},
			},
			"momus-Observation": {
				LocalID:      "momus-Observation",
				ResourceType: "Observation",
				Resource:     map[string]any{"resourceType": "Observation", "id": "momus-Observation", "status": "final"},
			},
		},
	}
}

func TestWriteDatasetEmitsOneJSONResourcePerLine(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	if err := w.WriteDataset(testDataset()); err != nil {
		t.Fatalf("WriteDataset returned error: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2:\n%s", len(lines), buf.String())
	}
	for i, line := range lines {
		var res map[string]any
		if err := json.Unmarshal([]byte(line), &res); err != nil {
			t.Fatalf("line %d is not valid JSON: %v", i, err)
		}
		if res["resourceType"] != "Observation" {
			t.Fatalf("line %d resourceType = %v, want Observation", i, res["resourceType"])
		}
		if res["id"] == nil {
			t.Fatalf("line %d missing id", i)
		}
	}
}

func TestWriteDatasetIsDeterministic(t *testing.T) {
	first, err := EncodeDataset(testDataset())
	if err != nil {
		t.Fatalf("EncodeDataset returned error: %v", err)
	}
	second, err := EncodeDataset(testDataset())
	if err != nil {
		t.Fatalf("EncodeDataset returned error: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("non-deterministic output:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	// Sort order: momus-Observation < momus-Observation-2, so the first line
	// must be the plain Observation id.
	if !strings.HasPrefix(string(first), `{"id":"momus-Observation"`) {
		t.Fatalf("unexpected ordering, got prefix: %s", string(first[:40]))
	}
}

func TestWriteDatasetsAndCount(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	ds := testDataset()
	if err := w.WriteDatasets([]*model.Dataset{ds, ds}); err != nil {
		t.Fatalf("WriteDatasets returned error: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if got := strings.Count(buf.String(), "\n"); got != 4 {
		t.Fatalf("got %d newlines, want 4", got)
	}
	if got := Count([]*model.Dataset{ds, ds}); got != 4 {
		t.Fatalf("got %d resources, want 4", got)
	}
}

func TestWriteNilHandling(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	if err := w.WriteInstance(nil); err == nil {
		t.Fatal("expected error for nil instance")
	}
	if err := w.WriteDataset(nil); err == nil {
		t.Fatal("expected error for nil dataset")
	}
	if _, err := EncodeDataset(nil); err == nil {
		t.Fatal("expected error for nil dataset")
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
}
