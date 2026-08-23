package runner

import (
	"net/http"
	"testing"

	"github.com/jlcoulter/momus/internal/core/assertions"
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
