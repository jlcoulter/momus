package karate

import (
	"testing"

	"github.com/jlcoulter/momus/internal/core/ast"
)

// mkRequest builds a Request node.
func mkRequest(method, url string, body map[string]any) *ast.Request {
	return &ast.Request{Method: method, URL: url, Body: body}
}

// mkAssert builds an Assert node with an optional trace.
func mkAssert(expr, rid, resourceType, domain, variant string) *ast.Assert {
	a := &ast.Assert{
		Expression:    expr,
		RequirementID: rid,
		Trace:         &ast.Trace{},
	}
	if resourceType != "" {
		a.Trace.ResourceType = resourceType
	}
	if domain != "" {
		a.Trace.Domain = domain
	}
	if variant != "" {
		a.Trace.Variant = variant
	}
	return a
}

func TestExportSingleResource(t *testing.T) {
	plan := &ast.Plan{
		Version: "v1",
		Root: &ast.Sequence{Steps: []ast.Node{
			&ast.Sequence{Steps: []ast.Node{
				&ast.Sequence{Steps: []ast.Node{
					mkRequest("GET", "http://host/fhir/Patient?name=momus-search", nil),
					mkAssert("status in [200,201]", "search|Patient|name|search-valid", "Patient", "search", "search-valid"),
				}},
			}},
		}},
	}
	files, err := Export(plan, Options{})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 feature file, got %d", len(files))
	}
	f := files[0]
	if f.Name != "Patient" {
		t.Errorf("feature name = %q, want Patient", f.Name)
	}
	if len(f.Scenarios) != 1 {
		t.Fatalf("expected 1 scenario, got %d", len(f.Scenarios))
	}
	sc := f.Scenarios[0]
	if sc.Name != "search|Patient|name|search-valid" {
		t.Errorf("scenario name = %q", sc.Name)
	}
	foundRequirement := false
	for _, tag := range sc.Tags {
		if tag == "@requirement:search|Patient|name|search-valid" {
			foundRequirement = true
		}
	}
	if !foundRequirement {
		t.Errorf("missing requirement tag in %v", sc.Tags)
	}
	// Verify request decomposed into url/path/param/method steps.
	if len(sc.Steps) < 5 {
		t.Fatalf("expected at least 5 steps, got %d: %+v", len(sc.Steps), sc.Steps)
	}
	if sc.Steps[0].Text != "url baseUrl" {
		t.Errorf("step 0 = %q, want url baseUrl", sc.Steps[0].Text)
	}
	if sc.Steps[1].Text != "path 'Patient'" {
		t.Errorf("step 1 = %q, want path 'Patient'", sc.Steps[1].Text)
	}
	if sc.Steps[2].Text != "param name = 'momus-search'" {
		t.Errorf("step 2 = %q, want param name = 'momus-search'", sc.Steps[2].Text)
	}
	if sc.Steps[3].Text != "method GET" {
		t.Errorf("step 3 = %q, want method GET", sc.Steps[3].Text)
	}
	// The last step should be the translated assertion.
	last := sc.Steps[len(sc.Steps)-1]
	if last.Text != "assert responseStatus in [200, 201]" {
		t.Errorf("last step = %q", last.Text)
	}
}

func TestExportWriteUsesWriteBaseURL(t *testing.T) {
	plan := &ast.Plan{
		Root: &ast.Sequence{Steps: []ast.Node{
			&ast.Sequence{Steps: []ast.Node{
				&ast.Sequence{Steps: []ast.Node{
					mkRequest("PUT", "http://write/fhir/Patient/abc", map[string]any{"resourceType": "Patient", "id": "abc"}),
					mkAssert("status in [200,201]", "operation|Patient|operation-create", "Patient", "operation", "operation-create"),
				}},
			}},
		}},
	}
	files, err := Export(plan, Options{})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	sc := files[0].Scenarios[0]
	if sc.Steps[0].Text != "url writeBaseUrl" {
		t.Errorf("write step 0 = %q, want url writeBaseUrl", sc.Steps[0].Text)
	}
	// Body present -> request doc-string + method step.
	hasDocString := false
	hasMethod := false
	for _, s := range sc.Steps {
		if s.DocString != "" {
			hasDocString = true
		}
		if s.Text == "method PUT" {
			hasMethod = true
		}
	}
	if !hasDocString {
		t.Error("expected a request doc-string for a request with a body")
	}
	if !hasMethod {
		t.Error("expected method PUT step")
	}
}

func TestExportMultiResource(t *testing.T) {
	plan := &ast.Plan{
		Root: &ast.Sequence{Steps: []ast.Node{
			&ast.Parallel{Steps: []ast.Node{
				&ast.Sequence{Steps: []ast.Node{
					&ast.Sequence{Steps: []ast.Node{
						mkRequest("GET", "http://host/fhir/Patient?name=momus-search", nil),
						mkAssert("status in [200,201]", "search|Patient|name|search-valid", "Patient", "search", "search-valid"),
					}},
				}},
				&ast.Sequence{Steps: []ast.Node{
					&ast.Sequence{Steps: []ast.Node{
						mkRequest("GET", "http://host/fhir/Observation?status=final", nil),
						mkAssert("status in [200,201]", "search|Observation|status|search-valid", "Observation", "search", "search-valid"),
					}},
				}},
			}},
		}},
	}
	files, err := Export(plan, Options{})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 feature files, got %d", len(files))
	}
	if files[0].Name != "Observation" || files[1].Name != "Patient" {
		t.Errorf("files = [%s, %s], want [Observation, Patient]", files[0].Name, files[1].Name)
	}
}

func TestExportBackgroundWithDataset(t *testing.T) {
	plan := &ast.Plan{
		Root: &ast.Sequence{Steps: []ast.Node{
			&ast.Sequence{Steps: []ast.Node{
				&ast.Sequence{Steps: []ast.Node{
					mkRequest("GET", "http://host/fhir/Patient?name=momus-search", nil),
					mkAssert("status in [200,201]", "search|Patient|name|search-valid", "Patient", "search", "search-valid"),
				}},
			}},
		}},
		Dataset: &ast.Dataset{
			Resources: map[string]*ast.ResourceInstance{
				"seed1": {
					LocalID:      "momus-setup-Patient",
					ResourceType: "Patient",
					Resource:     map[string]any{"resourceType": "Patient", "id": "momus-setup-Patient"},
				},
			},
		},
	}
	files, err := Export(plan, Options{})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	f := files[0]
	if len(f.Background) == 0 {
		t.Fatal("expected a Background block")
	}
	if f.Background[0].Text != "url writeBaseUrl" {
		t.Errorf("background step 0 = %q, want url writeBaseUrl", f.Background[0].Text)
	}
}

func TestExportBackgroundExcludesOtherResourceTypes(t *testing.T) {
	plan := &ast.Plan{
		Root: &ast.Sequence{Steps: []ast.Node{
			&ast.Sequence{Steps: []ast.Node{
				&ast.Sequence{Steps: []ast.Node{
					mkRequest("GET", "http://host/fhir/Patient?name=momus-search", nil),
					mkAssert("status in [200,201]", "search|Patient|name|search-valid", "Patient", "search", "search-valid"),
				}},
			}},
		}},
		Dataset: &ast.Dataset{
			Resources: map[string]*ast.ResourceInstance{
				"patientSeed": {LocalID: "s1", ResourceType: "Patient", Resource: map[string]any{}},
				"obsSeed":     {LocalID: "s2", ResourceType: "Observation", Resource: map[string]any{}},
			},
		},
	}
	files, err := Export(plan, Options{})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	// Only the Patient seed should appear in the Patient Background.
	background := files[0].Background
	if len(background) == 0 {
		t.Fatal("expected background")
	}
	if len(background) < 5 {
		t.Fatalf("expected patient provisioning steps, got %d", len(background))
	}
}

func TestExportCapture(t *testing.T) {
	plan := &ast.Plan{
		Root: &ast.Sequence{Steps: []ast.Node{
			&ast.Sequence{Steps: []ast.Node{
				&ast.Sequence{Steps: []ast.Node{
					mkRequest("GET", "http://host/fhir/Patient/momus-setup-Patient", nil),
					&ast.Capture{Name: "Patient.id", Path: "id"},
					mkAssert("status in [200,201]", "operation|Patient|operation-read", "Patient", "operation", "operation-read"),
				}},
			}},
		}},
	}
	files, err := Export(plan, Options{})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	sc := files[0].Scenarios[0]
	foundDef := false
	for _, s := range sc.Steps {
		if s.Text == "def Patient_id = response.id" {
			foundDef = true
		}
	}
	if !foundDef {
		t.Errorf("expected a capture def step, got steps: %+v", sc.Steps)
	}
}

func TestExportCRUDSequence(t *testing.T) {
	plan := &ast.Plan{
		Root: &ast.Sequence{Steps: []ast.Node{
			&ast.Sequence{Steps: []ast.Node{
				&ast.Sequence{Steps: []ast.Node{
					mkRequest("PUT", "http://host/fhir/Patient/momus-crud-patient", map[string]any{"resourceType": "Patient", "id": "momus-crud-patient"}),
					mkAssert("status in [200,201]", "state|Patient|state-crud-sequence", "Patient", "state", "state-crud-sequence"),
					mkRequest("GET", "http://host/fhir/Patient/momus-crud-patient", nil),
					mkAssert("status in [200]", "state|Patient|state-crud-sequence", "Patient", "state", "state-crud-sequence"),
				}},
			}},
		}},
	}
	files, err := Export(plan, Options{})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	sc := files[0].Scenarios[0]
	if len(sc.Steps) < 8 {
		t.Fatalf("expected multi-step CRUD scenario, got %d steps", len(sc.Steps))
	}
}
