package ast

import (
	"bytes"
	"encoding/json"
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

func TestRequestBodyLargeIntegerRoundTrip(t *testing.T) {
	original := &Plan{
		Version: "1",
		Root: &Sequence{Steps: []Node{
			&Request{
				Method: "POST",
				URL:    "https://example.com/Patient",
				Body:   map[string]any{"id": int64(9223372036854775807)},
			},
		}},
	}

	encoded, err := EncodePlan(original)
	if err != nil {
		t.Fatalf("EncodePlan: %v", err)
	}

	// Marshal to JSON and unmarshal with UseNumber so integer values are
	// preserved as json.Number rather than being mangled through float64.
	data, err := json.Marshal(encoded)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var roundTripped map[string]any
	if err := dec.Decode(&roundTripped); err != nil {
		t.Fatalf("json.Decode: %v", err)
	}

	decoded, err := decodePlanRoot(t, roundTripped)
	if err != nil {
		t.Fatalf("decodePlanRoot: %v", err)
	}

	seq, ok := decoded.Root.(*Sequence)
	if !ok {
		t.Fatalf("root: want *Sequence, got %T", decoded.Root)
	}
	req, ok := seq.Steps[0].(*Request)
	if !ok {
		t.Fatalf("step 0: want *Request, got %T", seq.Steps[0])
	}
	body, ok := req.Body.(map[string]any)
	if !ok {
		t.Fatalf("body: want map[string]any, got %T", req.Body)
	}
	id, ok := body["id"].(json.Number)
	if !ok {
		t.Fatalf("body[\"id\"]: want json.Number, got %T", body["id"])
	}
	if id.String() != "9223372036854775807" {
		t.Errorf("body[\"id\"]: got %s want 9223372036854775807", id.String())
	}
}

func TestDecodeRequestBodyNormalizesWholeNumberFloat(t *testing.T) {
	// A body decoded from JSON without UseNumber arrives with numbers as
	// float64. Whole-number float64 values must be normalized back to
	// json.Number so integer fidelity is preserved.
	req, err := DecodeNode(map[string]any{
		"type":   "request",
		"method": "POST",
		"url":    "https://example.com/Patient",
		"body":   map[string]any{"id": float64(42), "nested": []any{float64(7)}},
	})
	if err != nil {
		t.Fatalf("DecodeNode: %v", err)
	}
	body := req.(*Request).Body.(map[string]any)
	if id, ok := body["id"].(json.Number); !ok || id.String() != "42" {
		t.Errorf("body[\"id\"]: got %#v want json.Number(42)", body["id"])
	}
	nested, ok := body["nested"].([]any)
	if !ok {
		t.Fatalf("body[\"nested\"]: want []any, got %T", body["nested"])
	}
	if n, ok := nested[0].(json.Number); !ok || n.String() != "7" {
		t.Errorf("nested[0]: got %#v want json.Number(7)", nested[0])
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
