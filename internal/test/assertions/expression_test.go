package assertions

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestParseExpressionStatusInEvaluatesPassAndFail(t *testing.T) {
	assertion, err := ParseExpression("status in [200,201]")
	if err != nil {
		t.Fatalf("ParseExpression returned error: %v", err)
	}

	if err := assertion.Evaluate(context.Background(), Result{StatusCode: 201}); err != nil {
		t.Fatalf("expected assertion to pass, got error: %v", err)
	}
	if err := assertion.Evaluate(context.Background(), Result{StatusCode: 400}); err == nil {
		t.Fatal("expected assertion to fail for status 400")
	}
}

func TestParseExpressionRejectsUnsupportedFormat(t *testing.T) {
	if _, err := ParseExpression("status == 200"); err == nil {
		t.Fatal("expected unsupported format error")
	}
}

func TestParseBodyEqualityRejectsMixedNumericString(t *testing.T) {
	for _, expr := range []string{`body.total == "3"`, `body.total != "3"`} {
		a, err := ParseExpression(expr)
		if err != nil {
			t.Fatalf("ParseExpression(%q) returned error: %v", expr, err)
		}
		err = a.Evaluate(context.Background(), Result{Body: []byte(`{"total":3}`)})
		if err == nil {
			t.Fatalf("expected %q to error on mixed numeric/string operands, got pass", expr)
		}
		if !strings.Contains(err.Error(), "cannot compare") {
			t.Fatalf("expected comparison error for %q, got: %v", expr, err)
		}
	}
}

func TestParseBodyNumericComparison(t *testing.T) {
	a, err := ParseExpression("body.total >= 2")
	if err != nil {
		t.Fatalf("ParseExpression returned error: %v", err)
	}
	result := Result{Body: []byte(`{"resourceType":"Bundle","total":3}`)}
	if err := a.Evaluate(context.Background(), result); err != nil {
		t.Fatalf("expected pass, got %v", err)
	}
	if err := a.Evaluate(context.Background(), Result{Body: []byte(`{"total":1}`)}); err == nil {
		t.Fatal("expected fail for total 1")
	}
}

func TestParseBodyStringComparison(t *testing.T) {
	a, err := ParseExpression(`body.resourceType == "OperationOutcome"`)
	if err != nil {
		t.Fatalf("ParseExpression returned error: %v", err)
	}
	if err := a.Evaluate(context.Background(), Result{Body: []byte(`{"resourceType":"OperationOutcome"}`)}); err != nil {
		t.Fatalf("expected pass, got %v", err)
	}
	if err := a.Evaluate(context.Background(), Result{Body: []byte(`{"resourceType":"Patient"}`)}); err == nil {
		t.Fatal("expected fail for Patient resourceType")
	}
}

func TestParseBodyArrayIndexComparison(t *testing.T) {
	a, err := ParseExpression(`body.issue[0].severity == "error"`)
	if err != nil {
		t.Fatalf("ParseExpression returned error: %v", err)
	}
	body := []byte(`{"issue":[{"severity":"error"}]}`)
	if err := a.Evaluate(context.Background(), Result{Body: body}); err != nil {
		t.Fatalf("expected pass, got %v", err)
	}
}

func TestParseHeaderComparison(t *testing.T) {
	a, err := ParseExpression(`header.ETag != ""`)
	if err != nil {
		t.Fatalf("ParseExpression returned error: %v", err)
	}
	if err := a.Evaluate(context.Background(), Result{Headers: http.Header{"Etag": {"W/\"1\""}}}); err != nil {
		t.Fatalf("expected pass, got %v", err)
	}
}

func TestParseVariableComparison(t *testing.T) {
	a, err := ParseExpression(`variable.Patient.id == "abc"`)
	if err != nil {
		t.Fatalf("ParseExpression returned error: %v", err)
	}
	result := Result{Variables: map[string]any{"Patient.id": "abc"}}
	if err := a.Evaluate(context.Background(), result); err != nil {
		t.Fatalf("expected pass, got %v", err)
	}
	result.Variables["Patient.id"] = "other"
	if err := a.Evaluate(context.Background(), result); err == nil {
		t.Fatal("expected fail for mismatched variable")
	}
}

func TestParseRejectsBareSelector(t *testing.T) {
	if _, err := ParseExpression("total >= 2"); err == nil {
		t.Fatal("expected unsupported format for bare selector")
	}
}

func TestParseRejectsNonFiniteNumericValues(t *testing.T) {
	for _, expr := range []string{"body.x == NaN", "body.x == Inf", "body.x == Infinity", "body.x == 1e999"} {
		if _, err := ParseExpression(expr); err == nil {
			t.Fatalf("expected ParseExpression(%q) to reject non-finite value", expr)
		}
	}
}

func TestParseBodyMissingKeyTreatsAsNil(t *testing.T) {
	// An absent body key resolves to nil, so `!=` against a value passes and
	// `==` fails, rather than failing on a lookup error.
	ne, err := ParseExpression(`body.x != "y"`)
	if err != nil {
		t.Fatalf("ParseExpression returned error: %v", err)
	}
	if err := ne.Evaluate(context.Background(), Result{Body: []byte(`{}`)}); err != nil {
		t.Fatalf("expected body.x != \"y\" to pass for absent x, got %v", err)
	}

	eq, err := ParseExpression(`body.x == "y"`)
	if err != nil {
		t.Fatalf("ParseExpression returned error: %v", err)
	}
	if err := eq.Evaluate(context.Background(), Result{Body: []byte(`{}`)}); err == nil {
		t.Fatal("expected body.x == \"y\" to fail for absent x")
	}
}

func TestToFloat(t *testing.T) {
	cases := []struct {
		in   any
		want float64
		ok   bool
	}{
		{float64(3.5), 3.5, true},
		{float32(2.5), 2.5, true},
		{int(7), 7, true},
		{int64(9), 9, true},
		{json.Number("12.5"), 12.5, true},
		{json.Number("not-a-number"), 0, false},
		{"3", 0, false},
		{nil, 0, false},
	}
	for _, c := range cases {
		got, ok := toFloat(c.in)
		if got != c.want || ok != c.ok {
			t.Fatalf("toFloat(%v) = %v, %v; want %v, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestCompareValuesNumericAndString(t *testing.T) {
	// Numeric ordering.
	if ok, err := compareValues(3, ">", 2); err != nil || !ok {
		t.Fatalf("compareValues(3,>,2) = %v, %v; want true", ok, err)
	}
	if ok, err := compareValues(2, ">", 3); err != nil || ok {
		t.Fatalf("compareValues(2,>,3) = %v, %v; want false", ok, err)
	}
	// Mixed numeric/string -> error.
	if _, err := compareValues(3, "==", "3"); err == nil {
		t.Fatal("expected error comparing mixed numeric/string operands")
	}
	// String equality.
	if ok, err := compareValues("a", "==", "a"); err != nil || !ok {
		t.Fatalf("compareValues(a,==,a) = %v, %v; want true", ok, err)
	}
	if ok, err := compareValues("a", "!=", "b"); err != nil || !ok {
		t.Fatalf("compareValues(a,!=,b) = %v, %v; want true", ok, err)
	}
	// Ordering non-numeric strings -> error.
	if _, err := compareValues("a", ">", "b"); err == nil {
		t.Fatal("expected error ordering non-numeric strings")
	}
}
