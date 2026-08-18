package resource

import (
	"context"
	"strings"
	"testing"
)

func TestGenerateCorpusWiresDistributedReferences(t *testing.T) {
	reg := testRegistry(t)
	gen := NewGeneratorWithOptions(reg, Options{Exhaustive: true})

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
	gen := NewGeneratorWithOptions(reg, Options{Exhaustive: true})

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
	gen := NewGeneratorWithOptions(reg, Options{Exhaustive: true})

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
	gen := NewGeneratorWithOptions(reg, Options{Exhaustive: true})
	if _, err := gen.GenerateCorpus(context.Background(), nil, 3, nil); err == nil {
		t.Fatal("expected error for empty resource types")
	}
}
