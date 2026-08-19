package ast

import (
	"reflect"
	"testing"
)

// decodePlanRoot decodes an encoded plan (from EncodePlan) via DecodeNode,
// mirroring how the CLI loads a test plan without a whole-plan decoder.
func decodePlanRoot(t *testing.T, encoded map[string]any) (*Plan, error) {
	t.Helper()
	rootRaw, ok := encoded["root"]
	if !ok {
		t.Fatal("encoded plan missing root")
	}
	root, err := DecodeNode(rootRaw.(map[string]any))
	if err != nil {
		return nil, err
	}
	version, _ := encoded["version"].(string)
	return &Plan{Version: version, Root: root}, nil
}

func TestDecodeRoundTrip(t *testing.T) {
	original := &Plan{
		Version: "1",
		Root: &Sequence{
			Steps: []Node{
				&Request{
					Method:  "POST",
					URL:     "https://example.com/Patient",
					Headers: map[string]string{"Content-Type": "application/fhir+json", "Accept": "application/fhir+json"},
					Body:    map[string]any{"resourceType": "Patient", "name": []any{"foo"}},
				},
				&Capture{Name: "patientId", Path: "$.id"},
				&Assert{
					Description:   "server returns 201",
					RequirementID: "REQ-1",
					Expression:    "response.status == 201",
					Trace: &Trace{
						ConstraintID: "C-1",
						ProfileURL:   "https://example.com/profile",
						ResourceType: "Patient",
						ElementPath:  "Patient.id",
						Domain:       "core",
						Variant:      "create",
						Expected:     "accept",
					},
				},
			},
		},
	}

	encoded, err := EncodePlan(original)
	if err != nil {
		t.Fatalf("EncodePlan: %v", err)
	}

	decoded, err := decodePlanRoot(t, encoded)
	if err != nil {
		t.Fatalf("decodePlanRoot: %v", err)
	}

	if decoded.Version != original.Version {
		t.Errorf("version: got %q want %q", decoded.Version, original.Version)
	}

	seq, ok := decoded.Root.(*Sequence)
	if !ok {
		t.Fatalf("root: want *Sequence, got %T", decoded.Root)
	}
	if len(seq.Steps) != 3 {
		t.Fatalf("steps: got %d want 3", len(seq.Steps))
	}

	// Request
	req, ok := seq.Steps[0].(*Request)
	if !ok {
		t.Fatalf("step 0: want *Request, got %T", seq.Steps[0])
	}
	wantReq := original.Root.(*Sequence).Steps[0].(*Request)
	if req.Method != wantReq.Method {
		t.Errorf("method: got %q want %q", req.Method, wantReq.Method)
	}
	if req.URL != wantReq.URL {
		t.Errorf("url: got %q want %q", req.URL, wantReq.URL)
	}
	if !reflect.DeepEqual(req.Headers, wantReq.Headers) {
		t.Errorf("headers: got %#v want %#v", req.Headers, wantReq.Headers)
	}
	if !reflect.DeepEqual(req.Body, wantReq.Body) {
		t.Errorf("body: got %#v want %#v", req.Body, wantReq.Body)
	}

	// Capture
	cap, ok := seq.Steps[1].(*Capture)
	if !ok {
		t.Fatalf("step 1: want *Capture, got %T", seq.Steps[1])
	}
	wantCap := original.Root.(*Sequence).Steps[1].(*Capture)
	if cap.Name != wantCap.Name {
		t.Errorf("capture name: got %q want %q", cap.Name, wantCap.Name)
	}
	if cap.Path != wantCap.Path {
		t.Errorf("capture path: got %q want %q", cap.Path, wantCap.Path)
	}

	// Assert
	asrt, ok := seq.Steps[2].(*Assert)
	if !ok {
		t.Fatalf("step 2: want *Assert, got %T", seq.Steps[2])
	}
	wantAsrt := original.Root.(*Sequence).Steps[2].(*Assert)
	if asrt.Description != wantAsrt.Description {
		t.Errorf("description: got %q want %q", asrt.Description, wantAsrt.Description)
	}
	if asrt.RequirementID != wantAsrt.RequirementID {
		t.Errorf("requirementId: got %q want %q", asrt.RequirementID, wantAsrt.RequirementID)
	}
	if asrt.Expression != wantAsrt.Expression {
		t.Errorf("expression: got %q want %q", asrt.Expression, wantAsrt.Expression)
	}
	if asrt.Trace == nil {
		t.Fatal("trace: nil")
	}
	if !reflect.DeepEqual(*asrt.Trace, *wantAsrt.Trace) {
		t.Errorf("trace: got %#v want %#v", *asrt.Trace, *wantAsrt.Trace)
	}
}

func TestDecodeNodeMissingType(t *testing.T) {
	_, err := DecodeNode(map[string]any{"foo": "bar"})
	if err == nil {
		t.Fatal("missing type: expected error, got nil")
	}

	_, err = DecodeNode(map[string]any{"type": "bogus"})
	if err == nil {
		t.Fatal("unknown type: expected error, got nil")
	}
}

func TestDecodePlanParallel(t *testing.T) {
	original := &Plan{
		Version: "1",
		Root: &Parallel{
			Steps: []Node{
				&Sequence{Steps: []Node{
					&Request{Method: "GET", URL: "https://example.com/A"},
				}},
				&Sequence{Steps: []Node{
					&Request{Method: "GET", URL: "https://example.com/B"},
				}},
			},
		},
	}

	encoded, err := EncodePlan(original)
	if err != nil {
		t.Fatalf("EncodePlan: %v", err)
	}
	decoded, err := decodePlanRoot(t, encoded)
	if err != nil {
		t.Fatalf("decodePlanRoot: %v", err)
	}

	par, ok := decoded.Root.(*Parallel)
	if !ok {
		t.Fatalf("root: want *Parallel, got %T", decoded.Root)
	}
	if len(par.Steps) != 2 {
		t.Fatalf("steps: got %d want 2", len(par.Steps))
	}
	for i, step := range par.Steps {
		seq, ok := step.(*Sequence)
		if !ok {
			t.Fatalf("step %d: want *Sequence, got %T", i, step)
		}
		if len(seq.Steps) != 1 {
			t.Fatalf("step %d: inner steps got %d want 1", i, len(seq.Steps))
		}
		req, ok := seq.Steps[0].(*Request)
		if !ok {
			t.Fatalf("step %d: want *Request, got %T", i, seq.Steps[0])
		}
		wantURLs := []string{"https://example.com/A", "https://example.com/B"}
		if req.URL != wantURLs[i] {
			t.Errorf("step %d url: got %q want %q", i, req.URL, wantURLs[i])
		}
	}
}
