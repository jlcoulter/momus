package bulk

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
)

func testInstances() []*model.ResourceInstance {
	return []*model.ResourceInstance{
		{LocalID: "momus-Observation-2", ResourceType: "Observation", Resource: map[string]any{"resourceType": "Observation", "id": "momus-Observation-2", "status": "final"}},
		{LocalID: "momus-Observation", ResourceType: "Observation", Resource: map[string]any{"resourceType": "Observation", "id": "momus-Observation", "status": "final"}},
	}
}

func TestWriteInstancesEmitsOneJSONResourcePerLine(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	if err := w.WriteInstances(testInstances()); err != nil {
		t.Fatalf("WriteInstances returned error: %v", err)
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

func TestWriteInstanceNilHandling(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	if err := w.WriteInstance(nil); err == nil {
		t.Fatal("expected error for nil instance")
	}
	if err := w.WriteInstance(&model.ResourceInstance{}); err == nil {
		t.Fatal("expected error for instance with nil resource")
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
}
