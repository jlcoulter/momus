package bulk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
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

// TestWriterConcurrentWrites verifies that a Writer is safe for concurrent use:
// concurrent WriteInstance calls must not interleave or corrupt the NDJSON
// stream, and every line must be a complete, distinct JSON resource.
func TestWriterConcurrentWrites(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	const goroutines = 8
	const perGoroutine = 200

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				id := fmt.Sprintf("momus-Observation-%d-%d", g, i)
				inst := &model.ResourceInstance{
					LocalID:      id,
					ResourceType: "Observation",
					Resource:     map[string]any{"resourceType": "Observation", "id": id, "status": "final"},
				}
				if err := w.WriteInstance(inst); err != nil {
					t.Errorf("WriteInstance: %v", err)
				}
			}
		}(g)
	}
	wg.Wait()
	if err := w.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != goroutines*perGoroutine {
		t.Fatalf("got %d lines, want %d", len(lines), goroutines*perGoroutine)
	}
	seen := make(map[string]bool)
	for _, line := range lines {
		var res map[string]any
		if err := json.Unmarshal([]byte(line), &res); err != nil {
			t.Fatalf("line is not valid JSON: %v: %q", err, line)
		}
		id, _ := res["id"].(string)
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
	}
}

// TestWriterCloseIsIdempotent verifies that Close is safe to call more than once
// and that buffered lines are flushed on the first call.
func TestWriterCloseIsIdempotent(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	if err := w.WriteInstance(testInstances()[0]); err != nil {
		t.Fatalf("WriteInstance: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("first Close returned error: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("second Close returned error: %v", err)
	}
	if !strings.Contains(buf.String(), "momus-Observation-2") {
		t.Fatalf("buffered line not flushed: %q", buf.String())
	}
}
