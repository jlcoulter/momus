package ast

import "testing"

func TestEncodeNodeSequence(t *testing.T) {
	node := &Sequence{Steps: []Node{
		&Request{Method: "GET", URL: "/Patient"},
		&Assert{Description: "ok", RequirementID: "r", Expression: "status == 200"},
	}}
	encoded, err := EncodeNode(node)
	if err != nil {
		t.Fatalf("EncodeNode: %v", err)
	}
	if encoded["type"] != "sequence" {
		t.Fatalf("type = %v, want sequence", encoded["type"])
	}
	steps, ok := encoded["steps"].([]any)
	if !ok || len(steps) != 2 {
		t.Fatalf("steps = %v, want 2 encoded steps", encoded["steps"])
	}
}

func TestEncodeNodeParallel(t *testing.T) {
	node := &Parallel{Steps: []Node{
		&Capture{Name: "id", Path: "resource.id"},
	}}
	encoded, err := EncodeNode(node)
	if err != nil {
		t.Fatalf("EncodeNode: %v", err)
	}
	if encoded["type"] != "parallel" {
		t.Fatalf("type = %v, want parallel", encoded["type"])
	}
}

func TestEncodeNodeRequest(t *testing.T) {
	node := &Request{
		Method:  "POST",
		URL:     "/Patient",
		Headers: map[string]string{"Content-Type": "application/fhir+json"},
		Body:    map[string]any{"resourceType": "Patient"},
	}
	encoded, err := EncodeNode(node)
	if err != nil {
		t.Fatalf("EncodeNode: %v", err)
	}
	if encoded["type"] != "request" || encoded["method"] != "POST" || encoded["url"] != "/Patient" {
		t.Fatalf("unexpected request encoding: %+v", encoded)
	}
	if encoded["body"] == nil {
		t.Fatal("request body was not encoded")
	}
}

func TestEncodeNodeCapture(t *testing.T) {
	node := &Capture{Name: "id", Path: "resource.id"}
	encoded, err := EncodeNode(node)
	if err != nil {
		t.Fatalf("EncodeNode: %v", err)
	}
	if encoded["type"] != "capture" || encoded["name"] != "id" || encoded["path"] != "resource.id" {
		t.Fatalf("unexpected capture encoding: %+v", encoded)
	}
}

func TestEncodeNodeUnsupportedType(t *testing.T) {
	_, err := EncodeNode(nil)
	if err == nil {
		t.Fatal("expected error for unsupported node type, got nil")
	}
}

func TestEncodePlan(t *testing.T) {
	plan := &Plan{
		Version: "1.0",
		Root:    &Request{Method: "GET", URL: "/Patient"},
		Dataset: &Dataset{Resources: map[string]*ResourceInstance{}},
	}
	encoded, err := EncodePlan(plan)
	if err != nil {
		t.Fatalf("EncodePlan: %v", err)
	}
	if encoded["version"] != "1.0" {
		t.Fatalf("version = %v, want 1.0", encoded["version"])
	}
	if _, ok := encoded["root"]; !ok {
		t.Fatal("expected root in encoded plan")
	}
	if _, ok := encoded["dataset"]; !ok {
		t.Fatal("expected dataset in encoded plan")
	}
}

func TestEncodePlanOmitsDatasetWhenNil(t *testing.T) {
	plan := &Plan{Version: "1.0", Root: &Capture{Name: "a", Path: "b"}}
	encoded, err := EncodePlan(plan)
	if err != nil {
		t.Fatalf("EncodePlan: %v", err)
	}
	if _, ok := encoded["dataset"]; ok {
		t.Fatal("did not expect dataset when nil")
	}
}

func TestEncodePlanNil(t *testing.T) {
	if _, err := EncodePlan(nil); err == nil {
		t.Fatal("expected error for nil plan, got nil")
	}
}

func TestEncodePlanNestedErrorPropagates(t *testing.T) {
	plan := &Plan{Version: "1.0", Root: &Parallel{Steps: []Node{nil}}}
	if _, err := EncodePlan(plan); err == nil {
		t.Fatal("expected error from nested node encoding to propagate, got nil")
	}
}
