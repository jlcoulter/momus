package bulk

import (
	"strings"
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

func TestSplitReference(t *testing.T) {
	rt, id := splitReference("Patient/abc")
	if rt != "Patient" || id != "abc" {
		t.Fatalf("splitReference = %q, %q", rt, id)
	}
	if rt, id := splitReference("no-slash"); rt != "" || id != "" {
		t.Fatalf("splitReference(no-slash) = %q, %q", rt, id)
	}
	if rt, id := splitReference("Patient/"); rt != "" || id != "" {
		t.Fatalf("splitReference(empty-id) = %q, %q", rt, id)
	}
}

func TestStripVersion(t *testing.T) {
	if got := stripVersion("http://x/Patient|4.0.1"); got != "http://x/Patient" {
		t.Fatalf("stripVersion = %q", got)
	}
	if got := stripVersion("http://x/Patient"); got != "http://x/Patient" {
		t.Fatalf("stripVersion(no version) = %q", got)
	}
}

func TestSanitizeID(t *testing.T) {
	if got := sanitizeID("A/B | x"); got != "A-B---x" {
		t.Fatalf("sanitizeID = %q", got)
	}
}

func TestResourceTypeOfProfile(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/Organization", Type: "Organization", Kind: "resource", Elements: []model.ElementDefinition{{Path: "Organization", Min: 0, Max: "1"}}})
	if got := resourceTypeOfProfile(reg, "http://example.org/StructureDefinition/Organization|4.0.1"); got != "Organization" {
		t.Fatalf("resourceTypeOfProfile = %q", got)
	}
	if got := resourceTypeOfProfile(reg, ""); got != "" {
		t.Fatalf("resourceTypeOfProfile(empty) = %q", got)
	}
	if got := resourceTypeOfProfile(nil, "http://x"); got != "" {
		t.Fatalf("resourceTypeOfProfile(nil) = %q", got)
	}
	if got := resourceTypeOfProfile(reg, "http://missing"); got != "" {
		t.Fatalf("resourceTypeOfProfile(missing) = %q", got)
	}
}

func TestReferenceTargetType(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/Patient", Type: "Patient", Kind: "resource", Elements: []model.ElementDefinition{{Path: "Patient", Min: 0, Max: "*"}}})
	def := &model.ElementDefinition{TargetProfile: []string{"http://example.org/StructureDefinition/Patient"}}
	if got := referenceTargetType(def, reg); got != "Patient" {
		t.Fatalf("referenceTargetType = %q", got)
	}
	if got := referenceTargetType(&model.ElementDefinition{}, reg); got != "" {
		t.Fatalf("referenceTargetType(empty) = %q", got)
	}
}

func TestDescendForReference(t *testing.T) {
	// Missing key -> map created.
	parent := map[string]any{}
	got := descendForReference(parent, "subject")
	if _, ok := parent["subject"].(map[string]any); !ok {
		t.Fatalf("descend missing = %v", parent)
	}
	// Array descends into first element.
	parent = map[string]any{"author": []any{map[string]any{"display": "x"}}}
	got = descendForReference(parent, "author")
	if got["display"] != "x" {
		t.Fatalf("descend array = %v", got)
	}
	// Empty array creates element.
	parent = map[string]any{"author": []any{}}
	got = descendForReference(parent, "author")
	if parent["author"].([]any)[0] == nil {
		t.Fatalf("descend empty array = %v", parent)
	}
	// Non-map array element replaced.
	parent = map[string]any{"author": []any{"str"}}
	got = descendForReference(parent, "author")
	if parent["author"].([]any)[0].(map[string]any) == nil {
		t.Fatalf("descend non-map = %v", parent)
	}
}

func TestSetReferenceLeaf(t *testing.T) {
	target := refTarget{resourceType: "Patient", localID: "p-1"}
	// Existing map.
	obj := map[string]any{"subject": map[string]any{"reference": "old"}}
	setReferenceLeaf(obj, "subject", target)
	if obj["subject"].(map[string]any)["reference"] != "Patient/p-1" {
		t.Fatalf("setReferenceLeaf(map) = %v", obj)
	}
	// Existing array.
	obj = map[string]any{"author": []any{map[string]any{"reference": "old"}}}
	setReferenceLeaf(obj, "author", target)
	if obj["author"].([]any)[0].(map[string]any)["reference"] != "Patient/p-1" {
		t.Fatalf("setReferenceLeaf(array) = %v", obj)
	}
	// Empty array.
	obj = map[string]any{"author": []any{}}
	setReferenceLeaf(obj, "author", target)
	if obj["author"].([]any)[0].(map[string]any)["reference"] != "Patient/p-1" {
		t.Fatalf("setReferenceLeaf(empty array) = %v", obj)
	}
	// Scalar.
	obj = map[string]any{"subject": "scalar"}
	setReferenceLeaf(obj, "subject", target)
	if obj["subject"].(map[string]any)["reference"] != "Patient/p-1" {
		t.Fatalf("setReferenceLeaf(scalar) = %v", obj)
	}
}

func TestHashCorpus(t *testing.T) {
	if hashCorpus("a") == hashCorpus("b") {
		t.Fatal("hashCorpus should distinguish inputs")
	}
	if hashCorpus("a") != hashCorpus("a") {
		t.Fatal("hashCorpus should be deterministic")
	}
}

func TestWireCorpusReferences(t *testing.T) {
	// Nil / empty resource is a no-op.
	wireCorpusReferences(nil, map[string]string{}, nil)
	wireCorpusReferences(&model.ResourceInstance{}, map[string]string{}, nil)
	// A ref field with an empty pool is skipped.
	inst := &model.ResourceInstance{LocalID: "o1", Resource: map[string]any{}}
	wireCorpusReferences(inst, map[string]string{"Observation.subject": "Patient"}, map[string][]string{"Patient": {}})
	if len(inst.Resource) != 0 {
		t.Fatalf("empty pool should not wire: %v", inst.Resource)
	}
	// A ref field with a non-empty pool wires the reference.
	inst = &model.ResourceInstance{LocalID: "o1", Resource: map[string]any{}}
	wireCorpusReferences(inst, map[string]string{"Observation.subject": "Patient"}, map[string][]string{"Patient": {"p1", "p2"}})
	subject := inst.Resource["subject"].(map[string]any)
	if !strings.HasPrefix(subject["reference"].(string), "Patient/") {
		t.Fatalf("wired reference = %v", subject["reference"])
	}
}

func TestReferenceFields(t *testing.T) {
	reg := registry.New()
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/obs", Type: "Observation", Elements: []model.ElementDefinition{
		{Path: "Observation", Min: 0, Max: "*"},
		{Path: "Observation.subject", Min: 0, Max: "1", Types: []model.ElementType{{Code: "Reference", TargetProfile: []string{"http://example.org/StructureDefinition/patient"}}}},
	}})
	reg.AddStructureDefinition(&model.StructureDefinition{URL: "http://example.org/StructureDefinition/patient", Type: "Patient", Elements: []model.ElementDefinition{{Path: "Patient", Min: 0, Max: "*"}}})
	g := NewCorpusGenerator(reg, false)
	fields := g.referenceFields("Observation")
	if fields["Observation.subject"] != "Patient" {
		t.Fatalf("referenceFields = %v", fields)
	}
}
