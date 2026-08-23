package runner

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/jlcoulter/momus/internal/core/assertions"
	"github.com/jlcoulter/momus/internal/core/ast"
)

func TestExtractResourceID(t *testing.T) {
	if got := extractResourceID(nil); got != "" {
		t.Fatalf("extractResourceID(nil) = %q", got)
	}
	if got := extractResourceID([]byte("not-json")); got != "" {
		t.Fatalf("extractResourceID(invalid) = %q", got)
	}
	if got := extractResourceID([]byte(`{"id":"p-1"}`)); got != "p-1" {
		t.Fatalf("extractResourceID = %q", got)
	}
}

func TestResolveTemplates(t *testing.T) {
	vars := map[string]any{"Patient.id": "p-1", "n": 42}
	// Nested maps, arrays, and string slices.
	in := map[string]any{
		"subject": map[string]any{"reference": "Patient/{{Patient.id}}"},
		"list":    []any{"{{n}}", "x"},
		"plain":   "no-template",
	}
	out := resolveTemplates(in, vars)
	m := out.(map[string]any)
	if m["subject"].(map[string]any)["reference"] != "Patient/p-1" {
		t.Fatalf("map template not resolved: %v", m)
	}
	if m["list"].([]any)[0] != "42" {
		t.Fatalf("array template not resolved: %v", m["list"])
	}
	if m["plain"] != "no-template" {
		t.Fatalf("plain value changed: %v", m["plain"])
	}
	// []map[string]any variant.
	if out := resolveTemplates([]map[string]any{{"a": "{{n}}"}}, vars).([]any)[0].(map[string]any)["a"]; out != "42" {
		t.Fatalf("map-slice template = %v", out)
	}
	// Unknown variable left untouched.
	if out := resolveTemplates("{{missing}}", vars); out != "{{missing}}" {
		t.Fatalf("unknown template = %v", out)
	}
}

func TestReplaceTemplateString(t *testing.T) {
	if got := replaceTemplateString("x {{a}} y", map[string]any{"a": "val"}); got != "x val y" {
		t.Fatalf("replaceTemplateString = %q", got)
	}
	if got := replaceTemplateString("{{a}}", map[string]any{"a": 5}); got != "5" {
		t.Fatalf("replaceTemplateString numeric = %q", got)
	}
}

func TestTruncateDebugBody(t *testing.T) {
	if got := truncateDebugBody(nil); got != "" {
		t.Fatalf("truncateDebugBody(nil) = %q", got)
	}
	if got := truncateDebugBody([]byte("short")); got != "short" {
		t.Fatalf("truncateDebugBody(short) = %q", got)
	}
	big := make([]byte, maxDebugBodyBytes+100)
	for i := range big {
		big[i] = 'a'
	}
	if got := truncateDebugBody(big); len(got) <= maxDebugBodyBytes {
		t.Fatalf("truncateDebugBody not truncated, len=%d", len(got))
	}
}

func TestValidateSetupReferenceResolvable(t *testing.T) {
	created := map[string]struct{}{"Patient/p-1": {}}
	// Resolvable nested reference.
	if err := validateSetupReferenceResolvable(map[string]any{"subject": map[string]any{"reference": "Patient/p-1"}}, created); err != nil {
		t.Fatalf("resolvable ref error: %v", err)
	}
	// Unresolvable reference.
	if err := validateSetupReferenceResolvable(map[string]any{"reference": "Patient/momus-setup-missing"}, created); err == nil {
		t.Fatal("expected error for unresolved reference")
	}
	// Array of maps.
	if err := validateSetupReferenceResolvable([]any{map[string]any{"reference": "Patient/p-1"}}, created); err != nil {
		t.Fatalf("array ref error: %v", err)
	}
	// Scalar passes.
	if err := validateSetupReferenceResolvable("scalar", created); err != nil {
		t.Fatalf("scalar error: %v", err)
	}
}

func TestValidateSetupReference(t *testing.T) {
	created := map[string]struct{}{"Patient/p-1": {}}
	// Empty / template / non-setup refs pass.
	if err := validateSetupReference("", created); err != nil {
		t.Fatalf("empty ref error: %v", err)
	}
	if err := validateSetupReference("{{var}}", created); err != nil {
		t.Fatalf("template ref error: %v", err)
	}
	if err := validateSetupReference("no-slash", created); err != nil {
		t.Fatalf("no-slash ref error: %v", err)
	}
	if err := validateSetupReference("Patient/other", created); err != nil {
		t.Fatalf("non-setup ref error: %v", err)
	}
	// Resolvable setup ref.
	if err := validateSetupReference("Patient/p-1", created); err != nil {
		t.Fatalf("resolvable ref error: %v", err)
	}
	// Unresolvable setup ref.
	if err := validateSetupReference("Patient/momus-setup-x", created); err == nil {
		t.Fatal("expected error for unresolved setup reference")
	}
}

func TestWarningOnlySuccessAndOperationOutcome(t *testing.T) {
	// Correct expression, warning-only 412 body.
	if !warningOnlySuccess(successStatusExpression, assertions.Result{StatusCode: http.StatusPreconditionFailed, Body: []byte(`{"resourceType":"OperationOutcome","issue":[{"severity":"warning"}]}`)}) {
		t.Fatal("legacy success expression with warning-only 412 should pass")
	}
	// Wrong expression -> false even with 412.
	if warningOnlySuccess("status in [200]", assertions.Result{StatusCode: http.StatusPreconditionFailed, Body: []byte(`{"issue":[{"severity":"warning"}]}`)}) {
		t.Fatal("wrong expression should not be warning-only success")
	}
	// Non-412.
	if warningOnlySuccess(successStatusExpression, assertions.Result{StatusCode: http.StatusOK, Body: []byte(`{"issue":[{"severity":"warning"}]}`)}) {
		t.Fatal("non-412 should not be warning-only success")
	}
	// 412 with only warnings.
	if !warningOnlySuccess(successStatusExpression, assertions.Result{StatusCode: http.StatusPreconditionFailed, Body: []byte(`{"resourceType":"OperationOutcome","issue":[{"severity":"warning"},{"severity":"information"}]}`)}) {
		t.Fatal("warning-only 412 should be success")
	}
	// 412 with an error issue.
	if warningOnlySuccess(successStatusExpression, assertions.Result{StatusCode: http.StatusPreconditionFailed, Body: []byte(`{"resourceType":"OperationOutcome","issue":[{"severity":"error"}]}`)}) {
		t.Fatal("412 with error should not be success")
	}
	// Empty / non-OperationOutcome / malformed.
	if operationOutcomeHasOnlyWarnings(nil) {
		t.Fatal("empty body should not be warning-only")
	}
	if operationOutcomeHasOnlyWarnings([]byte("not-json")) {
		t.Fatal("invalid JSON should not be warning-only")
	}
	if operationOutcomeHasOnlyWarnings([]byte(`{"resourceType":"Patient"}`)) {
		t.Fatal("non-OperationOutcome should not be warning-only")
	}
	if operationOutcomeHasOnlyWarnings([]byte(`{"resourceType":"OperationOutcome","issue":[]}`)) {
		t.Fatal("empty issue array should not be warning-only")
	}
}

func TestNormalizeDiagnosticAndHelpers(t *testing.T) {
	if got := normalizeDiagnostic("  Hello   world  ", ""); got != "Hello world" {
		t.Fatalf("normalizeDiagnostic = %q", got)
	}
	if got := normalizeDiagnostic("", "  details  "); got != "details" {
		t.Fatalf("normalizeDiagnostic(details) = %q", got)
	}
	if got := normalizeDiagnostic("", ""); got != "" {
		t.Fatalf("normalizeDiagnostic(empty) = %q", got)
	}
	if got := firstNonEmpty([]string{"", "  ", "value"}); got != "value" {
		t.Fatalf("firstNonEmpty = %q", got)
	}
	if got := firstNonEmpty([]string{"", " "}); got != "" {
		t.Fatalf("firstNonEmpty(all empty) = %q", got)
	}
	if got := normalizeToken("  a   b  "); got != "a b" {
		t.Fatalf("normalizeToken = %q", got)
	}
	if got := normalizeToken(""); got != "" {
		t.Fatalf("normalizeToken(empty) = %q", got)
	}
}

func TestTruncateText(t *testing.T) {
	if got := truncateText("abc", 5); got != "abc" {
		t.Fatalf("truncateText(short) = %q", got)
	}
	if got := truncateText("abcdef", 3); got != "abc..." {
		t.Fatalf("truncateText(long) = %q", got)
	}
	if got := truncateText("abc", 0); got != "" {
		t.Fatalf("truncateText(max0) = %q", got)
	}
	if got := truncateText("abcdef", -1); got != "" {
		t.Fatalf("truncateText(neg) = %q", got)
	}
}

func TestApplyRequestAuth(t *testing.T) {
	// Existing auth preserved.
	e := &executor{bearerToken: "tok"}
	req, _ := http.NewRequest("GET", "http://x", nil)
	req.Header.Set("Authorization", "Existing")
	e.applyRequestAuth(req, "GET")
	if req.Header.Get("Authorization") != "Existing" {
		t.Fatal("existing auth overwritten")
	}
	// Nil request is a no-op.
	e.applyRequestAuth(nil, "GET")
	// Write method with write base URL and write basic creds.
	e = &executor{writeBaseURL: "http://write", writeBasicUsername: "wu", writeBasicPassword: "wp"}
	req, _ = http.NewRequest("POST", "http://x", nil)
	e.applyRequestAuth(req, "POST")
	if _, _, ok := req.BasicAuth(); !ok {
		t.Fatal("write basic auth not applied")
	}
	// Bearer token.
	e = &executor{bearerToken: "tok"}
	req, _ = http.NewRequest("GET", "http://x", nil)
	e.applyRequestAuth(req, "GET")
	if req.Header.Get("Authorization") != "Bearer tok" {
		t.Fatal("bearer token not applied")
	}
	// Basic auth.
	e = &executor{basicUsername: "u", basicPassword: "p"}
	req, _ = http.NewRequest("GET", "http://x", nil)
	e.applyRequestAuth(req, "GET")
	if _, _, ok := req.BasicAuth(); !ok {
		t.Fatal("basic auth not applied")
	}
}

func TestChildCopiesExecutorState(t *testing.T) {
	parent := &executor{
		ctx:           context.Background(),
		baseURL:       "http://base",
		writeBaseURL:  "http://write",
		bearerToken:   "tok",
		includeDebug:  true,
		variables:     map[string]any{"Patient.id": "p-1"},
		created:       map[string]struct{}{"Patient/p-1": {}},
		failuresBySig: map[string]*FailureSignature{"sig": {Signature: "s"}},
	}
	child := parent.child()
	if child.baseURL != "http://base" || child.writeBaseURL != "http://write" {
		t.Fatalf("child base urls = %q, %q", child.baseURL, child.writeBaseURL)
	}
	if child.variables["Patient.id"] != "p-1" {
		t.Fatalf("child variables = %v", child.variables)
	}
	if _, ok := child.created["Patient/p-1"]; !ok {
		t.Fatalf("child created = %v", child.created)
	}
	if child.report == nil || child.failuresBySig == nil {
		t.Fatal("child report/failures should be initialized")
	}
	// Mutating the child must not affect the parent's maps.
	child.variables["new"] = "x"
	if _, ok := parent.variables["new"]; ok {
		t.Fatal("child should not share the parent's variables map")
	}
}

func TestEvaluateAssertNoResult(t *testing.T) {
	e := &executor{report: &Report{}}
	e.evaluateAssert(&ast.Assert{RequirementID: "r1", Description: "d", Expression: "status in [200]"})
	if e.report.Failed != 1 || e.report.Cases[0].Error != "no request result available for assertion" {
		t.Fatalf("no-result assert = %+v", e.report)
	}
}

func TestEvaluateAssertAttributesRecordedError(t *testing.T) {
	// A request error already recorded; the following assert attributes metadata.
	e := &executor{report: &Report{Cases: []CaseResult{{Passed: false, Error: "boom"}}}, errorRecorded: true, lastErr: errors.New("boom")}
	e.evaluateAssert(&ast.Assert{RequirementID: "r2", Description: "d2", Expression: "x", Trace: &ast.Trace{}})
	if e.report.Cases[0].RequirementID != "r2" {
		t.Fatalf("assert not attributed to recorded error: %+v", e.report.Cases[0])
	}
	if e.errorRecorded {
		t.Fatal("errorRecorded should be cleared")
	}
}

func TestExtractMissingResourceKeyEdge(t *testing.T) {
	// A match with empty type or id is rejected.
	if _, ok := extractMissingResourceKey("Resource /x not found"); ok {
		t.Fatal("empty type should not match")
	}
	if _, ok := extractMissingResourceKey("Resource Patient/ not found"); ok {
		t.Fatal("empty id should not match")
	}
}

func TestSetupResourceTypeFromRequirementID(t *testing.T) {
	if _, ok := setupResourceTypeFromRequirementID("req-1"); ok {
		t.Fatal("non-setup id should not match")
	}
	rt, ok := setupResourceTypeFromRequirementID("setup:Patient")
	if !ok || rt != "Patient" {
		t.Fatalf("setupResourceTypeFromRequirementID = %q, %v", rt, ok)
	}
	if _, ok := setupResourceTypeFromRequirementID("setup:"); ok {
		t.Fatal("setup with empty type should not match")
	}
}

func TestHintForOutcome(t *testing.T) {
	if got := hintForOutcome(TriageOutcomeAcceptRejected); got == "" {
		t.Fatal("accept-rejected hint should be non-empty")
	}
	if got := hintForOutcome(TriageOutcomeRejectAccepted); got == "" {
		t.Fatal("reject-accepted hint should be non-empty")
	}
	if got := hintForOutcome(TriageOutcomeServerError); got == "" {
		t.Fatal("server-error hint should be non-empty")
	}
	if got := hintForOutcome(TriageOutcomeAmbiguous); got == "" {
		t.Fatal("ambiguous hint should be non-empty")
	}
	if got := hintForOutcome(""); got != "" {
		t.Fatalf("unknown outcome hint = %q", got)
	}
}

func TestSummaryHint(t *testing.T) {
	// Empty groups -> empty.
	if got := summaryHint(&TriageSummary{}); got != "" {
		t.Fatalf("empty summary hint = %q", got)
	}
	// Interaction accept-rejected.
	if got := summaryHint(&TriageSummary{Groups: []TriageGroup{{Outcome: TriageOutcomeAcceptRejected, Count: 2, Domain: "interaction"}}}); got == "" {
		t.Fatal("interaction hint should be non-empty")
	}
	// Reject-accepted.
	if got := summaryHint(&TriageSummary{Groups: []TriageGroup{{Outcome: TriageOutcomeRejectAccepted, Count: 2}}}); got == "" {
		t.Fatal("reject-accepted hint should be non-empty")
	}
	// Server error.
	if got := summaryHint(&TriageSummary{Groups: []TriageGroup{{Outcome: TriageOutcomeServerError, Count: 2}}}); got == "" {
		t.Fatal("server-error hint should be non-empty")
	}
}

func TestIsLikelyAuthFailure(t *testing.T) {
	// Not all failures share the lead signature.
	if isLikelyAuthFailure([]FailureSignature{{Count: 1, StatusCode: http.StatusForbidden}}, 2) {
		t.Fatal("mismatched count should not be auth failure")
	}
	// Non-auth status.
	if isLikelyAuthFailure([]FailureSignature{{Count: 2, StatusCode: http.StatusBadRequest}}, 2) {
		t.Fatal("400 should not be auth failure")
	}
	// Auth status with auth token.
	if !isLikelyAuthFailure([]FailureSignature{{Count: 2, StatusCode: http.StatusForbidden, Diagnostics: "invalid authentication"}}, 2) {
		t.Fatal("auth token should be recognized")
	}
	// Auth status without auth token.
	if isLikelyAuthFailure([]FailureSignature{{Count: 2, StatusCode: http.StatusForbidden, Diagnostics: "other error"}}, 2) {
		t.Fatal("no auth token should not be auth failure")
	}
}

func TestCollectFailedSetupResources(t *testing.T) {
	cases := []CaseResult{
		{RequirementID: "setup:Patient", Passed: false, Trace: &ast.Trace{ResourceType: "Patient"}},
		{RequirementID: "req-1", Passed: false, Trace: &ast.Trace{ResourceType: "Observation"}},
		{RequirementID: "setup:Organization", Passed: true},
	}
	got := collectFailedSetupResources(cases)
	// The key is the lowercased resource-type/instance key for a failed setup case.
	if _, ok := got["patient/momus-setup-patient"]; !ok {
		t.Fatalf("collectFailedSetupResources = %v, want patient/momus-setup-patient", got)
	}
	if len(got) != 1 {
		t.Fatalf("collectFailedSetupResources = %v, want exactly one entry", got)
	}
}

func TestCapture(t *testing.T) {
	e := &executor{variables: map[string]any{}}
	// Nil capture -> no-op.
	e.capture(nil)
	// No result yet -> no-op.
	e.capture(&ast.Capture{Name: "id", Path: "id"})
	if len(e.variables) != 0 {
		t.Fatalf("capture with no result set variables: %v", e.variables)
	}
	// With a result, extracts id.
	e.hasResult = true
	e.lastResult = assertions.Result{Body: []byte(`{"id":"p-1"}`)}
	e.capture(&ast.Capture{Name: "Patient.id", Path: "id"})
	if e.variables["Patient.id"] != "p-1" {
		t.Fatalf("capture = %v", e.variables)
	}
}
