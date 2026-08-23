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

	_, err = DecodeNode(map[string]any{"type": 42})
	if err == nil {
		t.Fatal("non-string type: expected error, got nil")
	}
}

func TestDecodeNodeCaptureMissingFields(t *testing.T) {
	node, err := DecodeNode(map[string]any{"type": "capture"})
	if err != nil {
		t.Fatalf("DecodeNode: %v", err)
	}
	cap, ok := node.(*Capture)
	if !ok {
		t.Fatalf("want *Capture, got %T", node)
	}
	if cap.Name != "" || cap.Path != "" {
		t.Fatalf("capture = %+v, want empty fields", cap)
	}
}

func TestDecodeStepsErrors(t *testing.T) {
	// Missing steps.
	if _, err := DecodeNode(map[string]any{"type": "sequence"}); err == nil {
		t.Fatal("missing steps: expected error")
	}
	// Steps not an array.
	if _, err := DecodeNode(map[string]any{"type": "sequence", "steps": "nope"}); err == nil {
		t.Fatal("steps not array: expected error")
	}
	// Step not an object.
	if _, err := DecodeNode(map[string]any{"type": "sequence", "steps": []any{"nope"}}); err == nil {
		t.Fatal("step not object: expected error")
	}
	// Step with bad type propagates.
	if _, err := DecodeNode(map[string]any{"type": "sequence", "steps": []any{map[string]any{"type": "bogus"}}}); err == nil {
		t.Fatal("bad step type: expected error")
	}
}

func TestDecodeRequestHeaderVariants(t *testing.T) {
	// headers as map[string]string.
	req, err := DecodeNode(map[string]any{
		"type":    "request",
		"method":  "GET",
		"url":     "/x",
		"headers": map[string]string{"Accept": "application/json"},
	})
	if err != nil {
		t.Fatalf("DecodeNode: %v", err)
	}
	if h := req.(*Request).Headers; h["Accept"] != "application/json" {
		t.Fatalf("headers = %v", h)
	}

	// headers as map[string]any with non-string values coerced to strings.
	req, err = DecodeNode(map[string]any{
		"type":    "request",
		"method":  "GET",
		"url":     "/x",
		"headers": map[string]any{"X-Count": 42},
	})
	if err != nil {
		t.Fatalf("DecodeNode: %v", err)
	}
	if h := req.(*Request).Headers; h["X-Count"] != "42" {
		t.Fatalf("headers = %v, want X-Count=42", h)
	}

	// No body -> nil body.
	req, err = DecodeNode(map[string]any{"type": "request", "method": "GET", "url": "/x"})
	if err != nil {
		t.Fatalf("DecodeNode: %v", err)
	}
	if req.(*Request).Body != nil {
		t.Fatalf("body = %v, want nil", req.(*Request).Body)
	}
}

func TestDecodeAssertRequirementTypeError(t *testing.T) {
	if _, err := DecodeNode(map[string]any{"type": "assert", "requirement": "not-a-map"}); err == nil {
		t.Fatal("expected error for non-map requirement")
	}
}

func TestDecodeTraceNil(t *testing.T) {
	if _, err := decodeTrace(nil); err == nil {
		t.Fatal("decodeTrace(nil) should error")
	}
}

func TestNormalizeJSONValue(t *testing.T) {
	// int and int64 and int32 to json.Number.
	if v, ok := normalizeJSONValue(int(5)).(json.Number); !ok || v.String() != "5" {
		t.Fatalf("int normalize = %#v", normalizeJSONValue(int(5)))
	}
	if v, ok := normalizeJSONValue(int64(7)).(json.Number); !ok || v.String() != "7" {
		t.Fatalf("int64 normalize = %#v", normalizeJSONValue(int64(7)))
	}
	if v, ok := normalizeJSONValue(int32(3)).(json.Number); !ok || v.String() != "3" {
		t.Fatalf("int32 normalize = %#v", normalizeJSONValue(int32(3)))
	}
	// json.Number passthrough.
	num := json.Number("9")
	if normalizeJSONValue(num) != num {
		t.Fatal("json.Number should pass through")
	}
	// Fractional float64 preserved as float64.
	if normalizeJSONValue(1.5) != 1.5 {
		t.Fatal("fractional float64 should be preserved")
	}
	// Other types unchanged.
	if normalizeJSONValue("s") != "s" || normalizeJSONValue(true) != true {
		t.Fatal("other types should be unchanged")
	}
}

func TestDecodePlanErrors(t *testing.T) {
	if _, err := DecodePlan(nil); err == nil {
		t.Fatal("DecodePlan(nil) should error")
	}
	if _, err := DecodePlan(map[string]any{}); err == nil {
		t.Fatal("DecodePlan missing root should error")
	}
	if _, err := DecodePlan(map[string]any{"root": "nope"}); err == nil {
		t.Fatal("DecodePlan root not object should error")
	}
	// Root decode error propagates.
	if _, err := DecodePlan(map[string]any{"root": map[string]any{"type": "bogus"}}); err == nil {
		t.Fatal("DecodePlan bad root should error")
	}
	// dataset not object.
	if _, err := DecodePlan(map[string]any{"root": map[string]any{"type": "capture"}, "dataset": "nope"}); err == nil {
		t.Fatal("DecodePlan dataset not object should error")
	}
}

func TestDecodePlanWithDataset(t *testing.T) {
	plan, err := DecodePlan(map[string]any{
		"version": "1",
		"root":    map[string]any{"type": "capture", "name": "a", "path": "b"},
		"dataset": map[string]any{
			"resources": map[string]any{
				"p1": map[string]any{"localId": "p1", "resourceType": "Patient", "resource": map[string]any{"id": "p1"}},
			},
			"relationships": []any{map[string]any{"sourceId": "o1", "path": "subject", "targetId": "p1"}},
		},
	})
	if err != nil {
		t.Fatalf("DecodePlan: %v", err)
	}
	if plan.Dataset == nil || plan.Dataset.Resources["p1"] == nil {
		t.Fatal("expected decoded dataset with resource p1")
	}
	inst := plan.Dataset.Resources["p1"]
	if inst.LocalID != "p1" || inst.ResourceType != "Patient" {
		t.Fatalf("instance = %+v", inst)
	}
	if len(plan.Dataset.Relationships) != 1 || plan.Dataset.Relationships[0].SourceID != "o1" {
		t.Fatalf("relationships = %+v", plan.Dataset.Relationships)
	}
}

func TestDecodeDatasetErrors(t *testing.T) {
	// resources not object.
	if _, err := decodeDataset(map[string]any{"resources": "nope"}); err == nil {
		t.Fatal("resources not object should error")
	}
	// resource value not object.
	if _, err := decodeDataset(map[string]any{"resources": map[string]any{"p1": "nope"}}); err == nil {
		t.Fatal("resource value not object should error")
	}
	// relationships not array.
	if _, err := decodeDataset(map[string]any{"relationships": "nope"}); err == nil {
		t.Fatal("relationships not array should error")
	}
	// relationship not object.
	if _, err := decodeDataset(map[string]any{"relationships": []any{"nope"}}); err == nil {
		t.Fatal("relationship not object should error")
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
