package assertions

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// ParseExpression builds an Assertion from a minimal expression grammar.
// Supported format: "status in [200,201]".
func ParseExpression(expression string) (Assertion, error) {
	expr := strings.TrimSpace(expression)
	prefix := "status in ["
	if !strings.HasPrefix(expr, prefix) || !strings.HasSuffix(expr, "]") {
		return nil, fmt.Errorf("unsupported assertion expression %q", expression)
	}
	listPart := strings.TrimSuffix(strings.TrimPrefix(expr, prefix), "]")
	if strings.TrimSpace(listPart) == "" {
		return nil, fmt.Errorf("unsupported assertion expression %q", expression)
	}

	allowed := make(map[int]struct{})
	for _, token := range strings.Split(listPart, ",") {
		statusText := strings.TrimSpace(token)
		code, err := strconv.Atoi(statusText)
		if err != nil {
			return nil, fmt.Errorf("invalid status code %q in expression %q", statusText, expression)
		}
		allowed[code] = struct{}{}
	}

	return &statusInAssertion{allowed: allowed, expression: expr}, nil
}

type statusInAssertion struct {
	allowed    map[int]struct{}
	expression string
}

func (a *statusInAssertion) Evaluate(ctx context.Context, result Result) error {
	_ = ctx
	if _, ok := a.allowed[result.StatusCode]; ok {
		return nil
	}
	return fmt.Errorf("assertion failed: %s (got status %d)", a.expression, result.StatusCode)
}
