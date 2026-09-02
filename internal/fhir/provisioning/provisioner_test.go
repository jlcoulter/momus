package provisioning

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jlcoulter/momus/internal/fhir/model"
)

func TestProvisionWritesTargetsBeforeDependents(t *testing.T) {
	var mu sync.Mutex
	var order []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		order = append(order, r.URL.Path)
		mu.Unlock()
		w.Header().Set("ETag", `W/"1"`)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"ok"}`))
	}))
	defer server.Close()

	ds := &model.Dataset{
		Resources: map[string]*model.ResourceInstance{
			"obs": {LocalID: "obs", ResourceType: "Observation", Resource: map[string]any{
				"resourceType": "Observation", "id": "obs",
				"subject": map[string]any{"reference": "Patient/pat"},
			}},
			"pat": {LocalID: "pat", ResourceType: "Patient", Resource: map[string]any{
				"resourceType": "Patient", "id": "pat",
			}},
		},
		Relationships: []model.Reference{{SourceID: "obs", Path: "Observation.subject", TargetID: "pat"}},
	}

	p := New(server.URL, &Options{HTTPClient: server.Client()})
	res := p.ProvisionAll(context.Background(), ds)
	if !res.Complete() {
		t.Fatalf("provisioning incomplete: %d provisioned, %d failed", res.Provisioned, res.Failed)
	}

	// Patient (target) must be PUT before Observation (dependent).
	if len(order) != 2 {
		t.Fatalf("got %d requests, want 2: %v", len(order), order)
	}
	if order[0] != "/Patient/pat" {
		t.Fatalf("first PUT was %s, want /Patient/pat (target first)", order[0])
	}
	if order[1] != "/Observation/obs" {
		t.Fatalf("second PUT was %s, want /Observation/obs", order[1])
	}

	if ds.Resources["pat"].ServerID != "pat" {
		t.Fatalf("expected Patient server id to be recorded, got %q", ds.Resources["pat"].ServerID)
	}
	if ds.Resources["obs"].ServerID != "obs" {
		t.Fatalf("expected Observation server id to be recorded, got %q", ds.Resources["obs"].ServerID)
	}
	if ds.Resources["pat"].Version != `W/"1"` {
		t.Fatalf("expected ETag version, got %q", ds.Resources["pat"].Version)
	}
}

// TestProvisionBatchUploadsInstances verifies that ProvisionBatch uploads a
// slice of instances concurrently and reports the outcome, without requiring a
// full Dataset.
func TestProvisionBatchUploadsInstances(t *testing.T) {
	var mu sync.Mutex
	seen := make(map[string]bool)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.URL.Path] = true
		mu.Unlock()
		w.Header().Set("ETag", `W/"1"`)
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	instances := []*model.ResourceInstance{
		{LocalID: "pat", ResourceType: "Patient", Resource: map[string]any{"resourceType": "Patient", "id": "pat"}},
		{LocalID: "obs", ResourceType: "Observation", Resource: map[string]any{"resourceType": "Observation", "id": "obs"}},
	}
	res := New(server.URL, &Options{HTTPClient: server.Client()}).ProvisionBatch(context.Background(), instances)
	if !res.Complete() {
		t.Fatalf("ProvisionBatch incomplete: %d provisioned, %d failed", res.Provisioned, res.Failed)
	}
	if res.Provisioned != 2 {
		t.Fatalf("ProvisionBatch provisioned %d, want 2", res.Provisioned)
	}
	if !seen["/Patient/pat"] || !seen["/Observation/obs"] {
		t.Fatalf("ProvisionBatch did not upload all instances: %v", seen)
	}
	if instances[0].ServerID != "pat" || instances[1].ServerID != "obs" {
		t.Fatalf("ProvisionBatch did not record server ids: %+v", instances)
	}
}

func TestProvisionSendsFhirJSONContentType(t *testing.T) {
	var gotContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ds := &model.Dataset{
		Resources: map[string]*model.ResourceInstance{
			"pat": {LocalID: "pat", ResourceType: "Patient", Resource: map[string]any{"resourceType": "Patient", "id": "pat"}},
		},
	}
	if res := New(server.URL, &Options{HTTPClient: server.Client()}).ProvisionAll(context.Background(), ds); !res.Complete() {
		t.Fatalf("provisioning incomplete: %d failed", res.Failed)
	}
	if gotContentType != "application/fhir+json" {
		t.Fatalf("got content-type %q, want application/fhir+json", gotContentType)
	}
}

func TestProvisionAppliesBearerToken(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ds := &model.Dataset{
		Resources: map[string]*model.ResourceInstance{
			"pat": {LocalID: "pat", ResourceType: "Patient", Resource: map[string]any{"resourceType": "Patient", "id": "pat"}},
		},
	}
	if res := New(server.URL, &Options{HTTPClient: server.Client(), BearerToken: "secret"}).ProvisionAll(context.Background(), ds); !res.Complete() {
		t.Fatalf("provisioning incomplete: %d failed", res.Failed)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("got authorization %q, want Bearer secret", gotAuth)
	}
}

func TestProvisionReturnsErrorOnNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"resourceType":"OperationOutcome"}`))
	}))
	defer server.Close()

	ds := &model.Dataset{
		Resources: map[string]*model.ResourceInstance{
			"pat": {LocalID: "pat", ResourceType: "Patient", Resource: map[string]any{"resourceType": "Patient", "id": "pat"}},
		},
	}
	res := New(server.URL, &Options{HTTPClient: server.Client()}).ProvisionAll(context.Background(), ds)
	if res.Failed == 0 {
		t.Fatal("expected failure for non-2xx response")
	}
}

func TestProvisionFailureReportsOperationOutcomeReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/fhir+json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{
			"resourceType": "OperationOutcome",
			"issue": [{
				"severity": "error",
				"diagnostics": "Location.physicalType: minimum required = 1, but only found 0",
				"location": ["Location.physicalType", "Line[1]"],
				"expression": ["Location.physicalType"]
			}]
		}`))
	}))
	defer server.Close()

	ds := &model.Dataset{
		Resources: map[string]*model.ResourceInstance{
			"loc": {LocalID: "loc", ResourceType: "Location", Resource: map[string]any{"resourceType": "Location", "id": "loc"}},
		},
	}
	res := New(server.URL, &Options{HTTPClient: server.Client()}).ProvisionAll(context.Background(), ds)
	if res.Failed != 1 || len(res.Failures) != 1 {
		t.Fatalf("Failed = %d, Failures = %d, want 1/1", res.Failed, len(res.Failures))
	}
	f := res.Failures[0]
	if f.ID != "loc" || f.ResourceType != "Location" {
		t.Fatalf("failure identity = %s/%s, want loc/Location", f.ResourceType, f.ID)
	}
	if f.Status != http.StatusUnprocessableEntity {
		t.Fatalf("Status = %d, want 422", f.Status)
	}
	if !strings.Contains(f.Reason, "minimum required = 1") {
		t.Fatalf("Reason = %q, want it to contain the OperationOutcome diagnostics", f.Reason)
	}
	if !strings.Contains(f.Reason, "Location.physicalType") {
		t.Fatalf("Reason = %q, want it to contain the issue location", f.Reason)
	}
	if !strings.Contains(f.Response, "OperationOutcome") {
		t.Fatalf("Response = %q, want the raw response body", f.Response)
	}
	if !strings.Contains(string(f.Resource), "\"resourceType\":\"Location\"") {
		t.Fatalf("Resource = %s, want the rejected payload", f.Resource)
	}
	desc := f.Describe()
	if !strings.Contains(desc, "Location/loc") || !strings.Contains(desc, "HTTP 422") {
		t.Fatalf("Describe = %q, want resource identity and status", desc)
	}
}

func TestProvisionFailureCapsIssuesAndFallsBackToRawBody(t *testing.T) {
	issues := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		issues = append(issues, fmt.Sprintf(`{"severity":"error","diagnostics":"issue %d"}`, i))
	}
	outcome := `{"resourceType":"OperationOutcome","issue":[` + strings.Join(issues, ",") + `]}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/Location/many" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(outcome))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("plain text rejection"))
	}))
	defer server.Close()

	ds := &model.Dataset{
		Resources: map[string]*model.ResourceInstance{
			"many":  {LocalID: "many", ResourceType: "Location", Resource: map[string]any{"resourceType": "Location", "id": "many"}},
			"plain": {LocalID: "plain", ResourceType: "Location", Resource: map[string]any{"resourceType": "Location", "id": "plain"}},
		},
	}
	res := New(server.URL, &Options{HTTPClient: server.Client()}).ProvisionAll(context.Background(), ds)
	if res.Failed != 2 {
		t.Fatalf("Failed = %d, want 2", res.Failed)
	}
	var many, plain Failure
	for _, f := range res.Failures {
		switch f.ID {
		case "many":
			many = f
		case "plain":
			plain = f
		}
	}
	if !strings.Contains(many.Reason, "(+2 more issues)") {
		t.Fatalf("Reason = %q, want issue cap indicator", many.Reason)
	}
	if plain.Reason != "plain text rejection" {
		t.Fatalf("Reason = %q, want raw body fallback for non-OperationOutcome response", plain.Reason)
	}
	if plain.Status != http.StatusBadRequest {
		t.Fatalf("Status = %d, want 400", plain.Status)
	}
}

func TestProvisionAllReportsPartialSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/Patient/pat" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	ds := &model.Dataset{
		Resources: map[string]*model.ResourceInstance{
			"ok":  {LocalID: "ok", ResourceType: "Observation", Resource: map[string]any{"resourceType": "Observation", "id": "ok"}},
			"pat": {LocalID: "pat", ResourceType: "Patient", Resource: map[string]any{"resourceType": "Patient", "id": "pat"}},
		},
	}
	res := New(server.URL, &Options{HTTPClient: server.Client()}).ProvisionAll(context.Background(), ds)
	if res.Provisioned != 1 {
		t.Fatalf("Provisioned = %d, want 1", res.Provisioned)
	}
	if res.Failed != 1 {
		t.Fatalf("Failed = %d, want 1", res.Failed)
	}
	if len(res.FailedIDs) != 1 || res.FailedIDs[0] != "pat" {
		t.Fatalf("FailedIDs = %v, want [pat]", res.FailedIDs)
	}
	if res.Complete() {
		t.Fatal("Complete should be false when a resource failed")
	}
}

func TestProvisionRequiresBaseURL(t *testing.T) {
	res := New("", nil).ProvisionAll(context.Background(), &model.Dataset{})
	if res.Provisioned != 0 || res.Failed != 0 {
		t.Fatalf("expected no provisioning for empty base URL, got %d provisioned, %d failed", res.Provisioned, res.Failed)
	}
	// Nothing failed, so the run is complete even though nothing was provisioned.
	if !res.Complete() {
		t.Fatal("Complete should be true when nothing failed")
	}
}

func TestProvisionSendsResourceBody(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ds := &model.Dataset{
		Resources: map[string]*model.ResourceInstance{
			"pat": {LocalID: "pat", ResourceType: "Patient", Resource: map[string]any{"resourceType": "Patient", "id": "pat", "name": "x"}},
		},
	}
	if res := New(server.URL, &Options{HTTPClient: server.Client()}).ProvisionAll(context.Background(), ds); !res.Complete() {
		t.Fatalf("provisioning incomplete: %d failed", res.Failed)
	}
	if got["name"] != "x" {
		t.Fatalf("got body %v, expected name=x", got)
	}
}

// TestProvisionRejectsNilResource verifies that a resource instance with a nil
// body is reported as a failure rather than being PUT as JSON null.
func TestProvisionRejectsNilResource(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ds := &model.Dataset{
		Resources: map[string]*model.ResourceInstance{
			"pat": {LocalID: "pat", ResourceType: "Patient", Resource: nil},
		},
	}
	res := New(server.URL, &Options{HTTPClient: server.Client()}).ProvisionAll(context.Background(), ds)
	if res.Failed != 1 || len(res.Failures) != 1 {
		t.Fatalf("Failed = %d, Failures = %d, want 1/1", res.Failed, len(res.Failures))
	}
	if res.Failures[0].ID != "pat" {
		t.Fatalf("failure id = %q, want pat", res.Failures[0].ID)
	}
	if !strings.Contains(res.Failures[0].Reason, "nil") {
		t.Fatalf("Reason = %q, want it to mention the nil body", res.Failures[0].Reason)
	}
	if gotPath != "" {
		t.Fatalf("server received a request for %q, want none (nil resource must not be PUT)", gotPath)
	}
}

// TestProvisionTrimsTrailingSlashFromBaseURL verifies that a base URL with a
// trailing slash does not produce a double-slash resource path.
func TestProvisionTrimsTrailingSlashFromBaseURL(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ds := &model.Dataset{
		Resources: map[string]*model.ResourceInstance{
			"pat": {LocalID: "pat", ResourceType: "Patient", Resource: map[string]any{"resourceType": "Patient", "id": "pat"}},
		},
	}
	if res := New(server.URL+"/", &Options{HTTPClient: server.Client()}).ProvisionAll(context.Background(), ds); !res.Complete() {
		t.Fatalf("provisioning incomplete: %d failed", res.Failed)
	}
	if gotPath != "/Patient/pat" {
		t.Fatalf("PUT path = %q, want /Patient/pat (no double slash)", gotPath)
	}
}

// TestProvisionBatchOrdersSameBatchTargetsBeforeDependents verifies that
// ProvisionBatch serialises resources whose references point at earlier
// instances in the same batch. The corpus wires same-type references (e.g.
// Location.partOf) to earlier instances of the same type, so a batch is not
// necessarily independent: a concurrent upload PUTs a dependent before its
// same-batch target exists and a server enforcing referential integrity
// rejects it with HAPI-1094 "not found".
func TestProvisionBatchOrdersSameBatchTargetsBeforeDependents(t *testing.T) {
	var mu sync.Mutex
	created := map[string]bool{}
	locationChecked := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The Location child references the Location parent. The parent must be
		// created first; delay the parent's creation until the child's request
		// has been checked so a concurrent upload deterministically fails here.
		if strings.HasSuffix(r.URL.Path, "/Location/parent") {
			select {
			case <-locationChecked:
			case <-time.After(100 * time.Millisecond):
			}
			mu.Lock()
			created["parent"] = true
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/Location/child") {
			mu.Lock()
			ok := created["parent"]
			mu.Unlock()
			if ok {
				w.WriteHeader(http.StatusCreated)
			} else {
				w.WriteHeader(http.StatusUnprocessableEntity)
			}
			close(locationChecked)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	instances := []*model.ResourceInstance{
		{LocalID: "parent", ResourceType: "Location", Resource: map[string]any{
			"resourceType": "Location", "id": "parent",
		}},
		{LocalID: "child", ResourceType: "Location", Resource: map[string]any{
			"resourceType": "Location", "id": "child",
			"partOf": map[string]any{"reference": "Location/parent"},
		}},
	}
	res := New(server.URL, &Options{HTTPClient: server.Client()}).ProvisionBatch(context.Background(), instances)
	if !res.Complete() {
		t.Fatalf("ProvisionBatch incomplete: %d provisioned, %d failed: %v", res.Provisioned, res.Failed, res.FailedIDs)
	}
}

func TestResultCompleteEmptyDataset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	// An empty, successful dataset has nothing to provision; Complete must
	// report success (nothing failed) rather than incomplete.
	res := New(server.URL, &Options{HTTPClient: server.Client()}).ProvisionAll(context.Background(), &model.Dataset{})
	if res.Provisioned != 0 || res.Failed != 0 {
		t.Fatalf("got %d provisioned, %d failed; want 0/0", res.Provisioned, res.Failed)
	}
	if !res.Complete() {
		t.Fatal("Complete should be true for an empty, successful dataset")
	}
}

func TestProvisionLevelsOrdersCycleTargetsBeforeDependents(t *testing.T) {
	// A cycle (b<->c) with dependents a->c and d->a. The cyclic level must be
	// ordered so targets precede dependents (c before a, a before d) and marked
	// serial so it is provisioned one resource at a time.
	ds := &model.Dataset{
		Resources: map[string]*model.ResourceInstance{
			"a": {LocalID: "a", ResourceType: "Patient", Resource: map[string]any{"resourceType": "Patient", "id": "a"}},
			"b": {LocalID: "b", ResourceType: "Patient", Resource: map[string]any{"resourceType": "Patient", "id": "b"}},
			"c": {LocalID: "c", ResourceType: "Patient", Resource: map[string]any{"resourceType": "Patient", "id": "c"}},
			"d": {LocalID: "d", ResourceType: "Patient", Resource: map[string]any{"resourceType": "Patient", "id": "d"}},
		},
		Relationships: []model.Reference{
			{SourceID: "b", Path: "Patient.link", TargetID: "c"},
			{SourceID: "c", Path: "Patient.link", TargetID: "b"},
			{SourceID: "a", Path: "Patient.link", TargetID: "c"},
			{SourceID: "d", Path: "Patient.link", TargetID: "a"},
		},
	}

	levels := provisionLevels(ds)
	if len(levels) != 1 {
		t.Fatalf("got %d levels, want 1 (a single cyclic level): %v", len(levels), levels)
	}
	lvl := levels[0]
	if !lvl.serial {
		t.Fatal("cyclic level should be marked serial so it is provisioned one resource at a time")
	}
	want := []string{"b", "c", "a", "d"}
	if !reflect.DeepEqual(lvl.ids, want) {
		t.Fatalf("cycle level order = %v, want %v (targets before dependents)", lvl.ids, want)
	}
}

func TestProvisionCycleLevelSerially(t *testing.T) {
	// A self-referencing Patient "a" (a cycle) plus an Observation "c" that
	// references it. The server allows self-references but enforces referential
	// integrity for cross-resource references, so "c" must be created only after
	// "a" exists. The cyclic level is provisioned serially (a then c); a
	// concurrent provisioning would PUT c before a is created and be rejected.
	var mu sync.Mutex
	created := map[string]bool{}
	cChecked := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ResourceType string `json:"resourceType"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)

		if body.ResourceType == "Observation" {
			mu.Lock()
			ok := created["a"]
			mu.Unlock()
			if !ok {
				w.WriteHeader(http.StatusUnprocessableEntity)
			} else {
				w.WriteHeader(http.StatusCreated)
			}
			close(cChecked)
			return
		}

		// Patient "a": wait until the dependent's request has been checked before
		// creating it, so a concurrent provisioning of "c" observes "a" as not yet
		// created and is rejected.
		select {
		case <-cChecked:
		case <-time.After(100 * time.Millisecond):
		}
		mu.Lock()
		created["a"] = true
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	ds := &model.Dataset{
		Resources: map[string]*model.ResourceInstance{
			"a": {LocalID: "a", ResourceType: "Patient", Resource: map[string]any{
				"resourceType": "Patient", "id": "a",
				"link": []any{map[string]any{"other": map[string]any{"reference": "Patient/a"}}},
			}},
			"c": {LocalID: "c", ResourceType: "Observation", Resource: map[string]any{
				"resourceType": "Observation", "id": "c",
				"subject": map[string]any{"reference": "Patient/a"},
			}},
		},
		Relationships: []model.Reference{
			{SourceID: "a", Path: "Patient.link.other", TargetID: "a"},
			{SourceID: "c", Path: "Observation.subject", TargetID: "a"},
		},
	}

	res := New(server.URL, &Options{HTTPClient: server.Client()}).ProvisionAll(context.Background(), ds)
	if !res.Complete() {
		t.Fatalf("provisioning incomplete: %d provisioned, %d failed: %v", res.Provisioned, res.Failed, res.FailedIDs)
	}
}

// TestProvisionBatchStripsReferencesToFailedTargets verifies that when a target
// resource fails to provision (e.g. a validation error), a later resource in the
// same batch that references it has the reference stripped from its body instead
// of cascading a HAPI-1094 "not found" failure. This mirrors the failure profile
// observed in `coverage bulk`: Location-1 is rejected (412), and every Location
// whose partOf references it, plus every downstream type referencing that
// Location, would otherwise fail for referential integrity.
func TestProvisionBatchStripsReferencesToFailedTargets(t *testing.T) {
	var mu sync.Mutex
	seen := make(map[string]string) // path -> received body
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		seen[r.URL.Path] = r.URL.Path
		mu.Unlock()
		if strings.HasSuffix(r.URL.Path, "/Location/root") {
			// The root Location fails for a validation (non-referential) reason.
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		w.Header().Set("ETag", `W/"1"`)
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	instances := []*model.ResourceInstance{
		{LocalID: "root", ResourceType: "Location", Resource: map[string]any{
			"resourceType": "Location", "id": "root",
		}},
		{LocalID: "child", ResourceType: "Location", Resource: map[string]any{
			"resourceType": "Location", "id": "child",
			"partOf": map[string]any{"reference": "Location/root"},
		}},
	}
	res := New(server.URL, &Options{HTTPClient: server.Client()}).ProvisionBatch(context.Background(), instances)
	if res.Provisioned != 1 || res.Failed != 1 {
		t.Fatalf("got %d provisioned, %d failed; want 1 provisioned (child) and 1 failed (root)", res.Provisioned, res.Failed)
	}
	if len(res.FailedIDs) != 1 || res.FailedIDs[0] != "root" {
		t.Fatalf("failed ids = %v, want [root]", res.FailedIDs)
	}
	// The child must have had its reference to the failed root stripped: it must
	// have been uploaded without the partOf field, so the server did not reject
	// it with a "not found" error.
	mu.Lock()
	defer mu.Unlock()
	if seen["/Location/child"] != "/Location/child" {
		t.Fatalf("child Location was not uploaded; seen = %v", seen)
	}
	if _, ok := instances[1].Resource["partOf"]; ok {
		t.Fatalf("child Location still carries a reference to failed target: %v", instances[1].Resource["partOf"])
	}
}

// TestStripFailedReferences verifies the reference-stripping helper removes
// references to failed targets from nested maps and repeatable arrays.
func TestStripFailedReferences(t *testing.T) {
	failed := map[string]bool{"bad": true}
	body := map[string]any{
		"resourceType": "Location",
		"partOf":       map[string]any{"reference": "Location/bad"},
		"endpoint": []any{
			map[string]any{"reference": "Endpoint/good"},
			map[string]any{"reference": "Endpoint/bad"},
		},
		"managingOrganization": map[string]any{"reference": "Organization/good"},
	}
	stripFailedReferences(body, failed)
	if _, ok := body["partOf"]; ok {
		t.Fatalf("partOf reference to failed target not stripped: %v", body["partOf"])
	}
	endpoints, ok := body["endpoint"].([]any)
	if !ok || len(endpoints) != 1 {
		t.Fatalf("endpoint = %v, want only the good reference retained", body["endpoint"])
	}
	if ref := endpoints[0].(map[string]any)["reference"]; ref != "Endpoint/good" {
		t.Fatalf("endpoint[0] = %v, want Endpoint/good", ref)
	}
	if mo := body["managingOrganization"].(map[string]any)["reference"]; mo != "Organization/good" {
		t.Fatalf("managingOrganization = %v, want Organization/good", mo)
	}
	// A reference to a live target (not in the failed set) is preserved.
	if _, ok := body["endpoint"]; !ok {
		t.Fatal("live endpoint reference should be preserved")
	}
	// nil/empty failed set is a no-op.
	copyBody := map[string]any{"partOf": map[string]any{"reference": "Location/x"}}
	stripFailedReferences(copyBody, nil)
	if _, ok := copyBody["partOf"]; !ok {
		t.Fatal("nil failed set should be a no-op")
	}
}
