// Package karate exports a Momus test-plan AST as Karate .feature files.
//
// It is domain-agnostic: it depends only on the core AST and translates the
// plan's request/assert/capture nodes into Karate Gherkin syntax, one feature
// file per resource type. URLs are parameterized against baseUrl/writeBaseUrl
// variables (configured in karate-config.js) and assertion expressions are
// translated into Karate match/assert steps.
package karate

import (
	"fmt"
	"strings"
)

// TranslateExpression converts a Momus assertion expression into an equivalent
// Karate assertion step text.
//
// Supported forms:
//
//	status in [200,201]                       ->  assert responseStatus in [200, 201]
//	body.total >= 2                           ->  match response.total >= 2
//	body.resourceType == "Patient"            ->  match response.resourceType == 'Patient'
//	body.issue[0].severity == "error"         ->  match response.issue[0].severity == 'error'
//	header.ETag != ""                         ->  match responseHeaders['ETag'] != ''
//	variable.Patient.id == "abc"              ->  match Patient_id == 'abc'
func TranslateExpression(expression string) (string, error) {
	expr := strings.TrimSpace(expression)
	if expr == "" {
		return "", fmt.Errorf("empty assertion expression")
	}
	if status, ok := translateStatusIn(expr); ok {
		return status, nil
	}
	if comparison, err := translateComparison(expr); err == nil {
		return comparison, nil
	}
	return "", fmt.Errorf("unsupported assertion expression %q", expression)
}

// translateStatusIn handles "status in [200,201]" -> "assert responseStatus in [200, 201]".
func translateStatusIn(expr string) (string, bool) {
	prefix := "status in ["
	if !strings.HasPrefix(expr, prefix) || !strings.HasSuffix(expr, "]") {
		return "", false
	}
	list := strings.TrimSuffix(strings.TrimPrefix(expr, prefix), "]")
	list = strings.TrimSpace(list)
	if list == "" {
		return "", false
	}
	// Normalize whitespace after each comma: "200,201" -> "200, 201".
	parts := make([]string, 0)
	for _, p := range strings.Split(list, ",") {
		parts = append(parts, strings.TrimSpace(p))
	}
	return "assert responseStatus in [" + strings.Join(parts, ", ") + "]", true
}

// comparisonOps are the supported comparison operators, longest first so that
// two-character operators are matched before single-character ones.
var comparisonOps = []string{"==", "!=", "<=", ">=", "<", ">"}

// translateComparison handles "<selector> <op> <value>" -> Karate match.
func translateComparison(expr string) (string, error) {
	for _, op := range comparisonOps {
		idx := strings.Index(expr, op)
		if idx <= 0 {
			continue
		}
		lhs := strings.TrimSpace(expr[:idx])
		rhs := strings.TrimSpace(expr[idx+len(op):])
		if lhs == "" || rhs == "" {
			continue
		}
		selector, err := translateSelector(lhs)
		if err != nil {
			continue
		}
		value, err := translateValue(rhs)
		if err != nil {
			continue
		}
		return "match " + selector + " " + op + " " + value, nil
	}
	return "", fmt.Errorf("unsupported comparison expression %q", expr)
}

// translateSelector converts the left-hand side of a comparison.
func translateSelector(lhs string) (string, error) {
	switch {
	case strings.HasPrefix(lhs, "body."):
		path := strings.TrimPrefix(lhs, "body.")
		if path == "" {
			return "", fmt.Errorf("empty body path")
		}
		return "response." + path, nil
	case strings.HasPrefix(lhs, "header."):
		name := strings.TrimPrefix(lhs, "header.")
		if name == "" {
			return "", fmt.Errorf("empty header name")
		}
		return "responseHeaders['" + name + "']", nil
	case strings.HasPrefix(lhs, "variable."):
		name := strings.TrimPrefix(lhs, "variable.")
		if name == "" {
			return "", fmt.Errorf("empty variable name")
		}
		// Captured variables become Karate def variables; dots are replaced with
		// underscores (e.g. variable.Patient.id -> Patient_id).
		return sanitizeVariable(name), nil
	default:
		return "", fmt.Errorf("unsupported selector %q", lhs)
	}
}

// sanitizeVariable converts a dotted variable path into a Karate variable name.
func sanitizeVariable(name string) string {
	return strings.ReplaceAll(name, ".", "_")
}

// translateValue converts the right-hand side literal to Karate syntax. Strings
// use single quotes; everything else passes through unchanged.
func translateValue(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, `"`) && strings.HasSuffix(raw, `"`) && len(raw) >= 2 {
		return "'" + raw[1:len(raw)-1] + "'", nil
	}
	switch raw {
	case "true", "false", "null":
		return raw, nil
	}
	// Numeric literals pass through unchanged; any other token is rejected so
	// that unsupported expressions fail loudly instead of emitting broken Karate.
	if isNumericLiteral(raw) {
		return raw, nil
	}
	return "", fmt.Errorf("invalid value %q", raw)
}

// isNumericLiteral reports whether s looks like a numeric literal (optionally
// negative, possibly with a decimal point and exponent).
func isNumericLiteral(s string) bool {
	if s == "" {
		return false
	}
	body := s
	if strings.HasPrefix(body, "-") || strings.HasPrefix(body, "+") {
		body = body[1:]
	}
	if body == "" {
		return false
	}
	digits := 0
	hasDot := false
	hasExp := false
	for i := 0; i < len(body); i++ {
		c := body[i]
		switch {
		case c >= '0' && c <= '9':
			digits++
		case c == '.':
			if hasDot || hasExp {
				return false
			}
			hasDot = true
		case c == 'e' || c == 'E':
			if hasExp || digits == 0 {
				return false
			}
			hasExp = true
			// Allow a sign immediately after the exponent.
			if i+1 < len(body) && (body[i+1] == '-' || body[i+1] == '+') {
				i++
			}
		default:
			return false
		}
	}
	return digits > 0
}
