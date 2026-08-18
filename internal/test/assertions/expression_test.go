package assertions

import (
	"context"
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
