package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jlcoulter/momus/internal/test/ast"
)

// parallelTwo returns a Sequence whose only step is a Parallel of two
// independent Request+Assert branches.
func parallelTwo(reqPath1, reqPath2 string) *ast.Plan {
	return &ast.Plan{Root: &ast.Sequence{Steps: []ast.Node{
		&ast.Parallel{Steps: []ast.Node{
			&ast.Sequence{Steps: []ast.Node{
				&ast.Request{Method: http.MethodPut, URL: reqPath1},
				&ast.Assert{Description: "branch a", RequirementID: "a", Expression: "status in [200,201]"},
			}},
			&ast.Sequence{Steps: []ast.Node{
				&ast.Request{Method: http.MethodPut, URL: reqPath2},
				&ast.Assert{Description: "branch b", RequirementID: "b", Expression: "status in [200,201]"},
			}},
		}},
	}}}
}

func TestExecuteParallelRunsConcurrently(t *testing.T) {
	var inFlight, maxInFlight atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := inFlight.Add(1)
		for {
			m := maxInFlight.Load()
			if cur <= m || maxInFlight.CompareAndSwap(m, cur) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond) // force overlap
		inFlight.Add(-1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	plan := parallelTwo(server.URL+"/A/1", server.URL+"/B/1")
	report, err := Execute(context.Background(), plan.Root, ExecuteOptions{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if report.Total != 2 || report.Passed != 2 || report.Failed != 0 {
		t.Fatalf("report = %+v, want 2 passed", report)
	}
	if maxInFlight.Load() < 2 {
		t.Fatalf("expected branches to run concurrently, max in-flight = %d", maxInFlight.Load())
	}
}

func TestExecuteParallelAggregatesDeterministically(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	plan := parallelTwo(server.URL+"/A/1", server.URL+"/B/1")
	report, err := Execute(context.Background(), plan.Root, ExecuteOptions{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(report.Cases) != 2 {
		t.Fatalf("got %d cases, want 2", len(report.Cases))
	}
	// Cases must be aggregated in branch order.
	if report.Cases[0].RequirementID != "a" || report.Cases[1].RequirementID != "b" {
		t.Fatalf("cases out of order: %v, %v", report.Cases[0].RequirementID, report.Cases[1].RequirementID)
	}
	if !report.Cases[0].Passed || !report.Cases[1].Passed {
		t.Fatalf("expected both branches to pass: %+v", report.Cases)
	}
}

func TestExecuteParallelIsolatesThenMergesCaptures(t *testing.T) {
	var capturedRef string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/A/") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"a-1"}`))
			return
		}
		var body struct {
			Ref string `json:"ref"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		capturedRef = body.Ref
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	plan := &ast.Plan{Root: &ast.Sequence{Steps: []ast.Node{
		&ast.Parallel{Steps: []ast.Node{
			&ast.Sequence{Steps: []ast.Node{
				&ast.Request{Method: http.MethodPut, URL: server.URL + "/A/1"},
				&ast.Capture{Name: "A.id", Path: "id"},
				&ast.Assert{Description: "a", RequirementID: "a", Expression: "status in [200,201]"},
			}},
			&ast.Sequence{Steps: []ast.Node{
				&ast.Request{Method: http.MethodPut, URL: server.URL + "/B/1"},
				&ast.Assert{Description: "b", RequirementID: "b", Expression: "status in [200,201]"},
			}},
		}},
		// After the parallel completes, the captured A.id must be available.
		&ast.Request{Method: http.MethodPut, URL: server.URL + "/post", Body: map[string]any{"ref": "{{A.id}}"}},
		&ast.Assert{Description: "post", RequirementID: "post", Expression: "status in [200,201]"},
	}}}

	report, err := Execute(context.Background(), plan.Root, ExecuteOptions{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if capturedRef != "a-1" {
		t.Fatalf("expected captured A.id to be available after parallel, got %q", capturedRef)
	}
	if report.Total != 3 || report.Failed != 0 {
		t.Fatalf("report = %+v, want 3 passed", report)
	}
}
