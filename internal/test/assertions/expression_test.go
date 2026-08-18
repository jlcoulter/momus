package assertions

import (
	"context"
	"net/http"
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
