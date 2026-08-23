package golden

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jlcoulter/momus/internal/core/ast"
)

// TestGoldenAll runs the golden-matrix self-test against every reference
// fixture in testdata/golden. For each: derive -> generate -> snapshot ->
// provision -> run against the semantic mock, asserting 100% pass. It writes
// the .plan.json snapshot on first run and fails on any mismatch or failing
// case.
func TestGoldenAll(t *testing.T) {
	fixtures := []string{"patient", "observation-slice", "search-operations", "observation-invariant", "patient-date", "patient-search", "observation-value", "location-near", "observation-composite", "practitioner-role"}
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			fx, err := LoadFixture(filepath.Join(goldenDir, name+".json"))
			if err != nil {
				t.Fatalf("load fixture: %v", err)
			}
			res, err := Run(context.Background(), name, fx, nil)
			if err != nil {
				t.Fatalf("golden run: %v", err)
			}
			if res.Failed != 0 {
				t.Fatalf("golden run had %d failed cases: %v", res.Failed, res.FailedReqs)
			}
		})
	}
}

func TestMarshalDeterministic(t *testing.T) {
	plan := &ast.Plan{Version: "v1", Root: &ast.Request{Method: "GET", URL: "/Patient"}}
	got := string(marshalDeterministic(plan))
	if got == "" || got[len(got)-1] != '\n' {
		t.Fatalf("marshalDeterministic = %q", got)
	}
}

func TestRewriteBase(t *testing.T) {
	plan := &ast.Sequence{Steps: []ast.Node{
		&ast.Parallel{Steps: []ast.Node{
			&ast.Request{Method: "GET", URL: "http://old/fhir/Patient"},
			&ast.Request{Method: "PUT", URL: "http://old/fhir/Patient/1"},
		}},
		&ast.Capture{Name: "id", Path: "id"},
	}}
	rewriteBase(plan, "http://old/fhir", "http://new/fhir")
	par := plan.Steps[0].(*ast.Parallel)
	req := par.Steps[0].(*ast.Request)
	if req.URL != "http://new/fhir/Patient" {
		t.Fatalf("request URL not rewritten: %q", req.URL)
	}
	if par.Steps[1].(*ast.Request).URL != "http://new/fhir/Patient/1" {
		t.Fatalf("second request URL not rewritten: %q", par.Steps[1].(*ast.Request).URL)
	}
}
