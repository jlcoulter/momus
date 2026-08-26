package assertions

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ParseExpression builds an Assertion from the expression grammar.
//
// Supported forms:
//
//	status in [200,201]                       status code membership
//	body.total >= 2                           JSON body value comparison
//	body.resourceType == "OperationOutcome"   string equality on a body path
//	body.issue[0].severity == "error"         array-indexed body path
//	header.ETag != ""                         response header comparison
//	variable.Patient.id == "abc"              captured variable comparison
//
// Comparison operators are ==, !=, <, <=, >, >=. Values may be numbers, quoted
// strings, true/false, or null.
func ParseExpression(expression string) (Assertion, error) {
	expr := strings.TrimSpace(expression)
	if expr == "" {
		return nil, fmt.Errorf("empty assertion expression")
	}
	if status, err := parseStatusIn(expr); err == nil {
		return status, nil
	}
	if comparison, err := parseComparison(expr); err == nil {
		return comparison, nil
	}
	return nil, fmt.Errorf("unsupported assertion expression %q", expression)
}

// parseStatusIn parses "status in [200,201]".
func parseStatusIn(expr string) (Assertion, error) {
	prefix := "status in ["
	if !strings.HasPrefix(expr, prefix) || !strings.HasSuffix(expr, "]") {
		return nil, fmt.Errorf("not a status expression")
	}
	listPart := strings.TrimSuffix(strings.TrimPrefix(expr, prefix), "]")
	if strings.TrimSpace(listPart) == "" {
		return nil, fmt.Errorf("empty status list")
	}
	allowed := make(map[int]struct{})
	for _, token := range strings.Split(listPart, ",") {
		statusText := strings.TrimSpace(token)
		code, err := strconv.Atoi(statusText)
		if err != nil {
			return nil, fmt.Errorf("invalid status code %q", statusText)
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

// comparisonOps are the supported comparison operators, longest first so that
// two-character operators are matched before single-character ones.
var comparisonOps = []string{"==", "!=", "<=", ">=", "<", ">"}

// parseComparison parses "<selector> <op> <value>".
func parseComparison(expr string) (Assertion, error) {
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
		sel, err := parseSelector(lhs)
		if err != nil {
			continue
		}
		value, err := parseValue(rhs)
		if err != nil {
			continue
		}
		return &comparisonAssertion{sel: sel, op: op, rhs: value, expression: expr}, nil
	}
	return nil, fmt.Errorf("unsupported comparison expression %q", expr)
}

type selectorKind int

const (
	selectorBody selectorKind = iota
	selectorHeader
	selectorVariable
)

type pathStep struct {
	key   string
	index int // -1 when the step is not an array index; anyIndex for "[]"
}

// anyIndex marks a path step that fans out across every element of an array
// (the "[]" selector). A comparison passes when any extracted element satisfies
// it, e.g. `body.entry[].resource.resourceType == "Organization"`.
const anyIndex = -2

type selector struct {
	kind  selectorKind
	steps []pathStep // for body selectors
	name  string     // for header/variable selectors
}

// parseSelector parses the left-hand side of a comparison into a selector.
func parseSelector(lhs string) (selector, error) {
	switch {
	case strings.HasPrefix(lhs, "body."):
		return selector{kind: selectorBody, steps: parsePath(strings.TrimPrefix(lhs, "body."))}, nil
	case strings.HasPrefix(lhs, "header."):
		name := strings.TrimPrefix(lhs, "header.")
		if name == "" {
			return selector{}, fmt.Errorf("empty header name")
		}
		return selector{kind: selectorHeader, name: name}, nil
	case strings.HasPrefix(lhs, "variable."):
		name := strings.TrimPrefix(lhs, "variable.")
		if name == "" {
			return selector{}, fmt.Errorf("empty variable name")
		}
		return selector{kind: selectorVariable, name: name}, nil
	default:
		return selector{}, fmt.Errorf("unsupported selector %q", lhs)
	}
}

// parsePath parses a dotted JSON path with array indices, e.g.
// "issue[0].severity" -> [key=issue,index=0, key=severity].
func parsePath(path string) []pathStep {
	segments := strings.Split(path, ".")
	steps := make([]pathStep, 0, len(segments))
	for _, seg := range segments {
		if idx := strings.IndexByte(seg, '['); idx >= 0 {
			key := seg[:idx]
			rest := seg[idx+1:]
			rest = strings.TrimSuffix(rest, "]")
			// An empty bracket is the "any" quantifier over the array: emit the
			// key step followed by a fan-out step (mirroring "[0]" which emits a
			// key step then an index step).
			if strings.TrimSpace(rest) == "" {
				if key != "" {
					steps = append(steps, pathStep{key: key, index: -1})
				}
				steps = append(steps, pathStep{index: anyIndex})
				continue
			}
			n, err := strconv.Atoi(strings.TrimSpace(rest))
			if err != nil {
				n = -1
			}
			if key != "" {
				steps = append(steps, pathStep{key: key, index: -1})
			}
			if n >= 0 {
				steps = append(steps, pathStep{index: n})
			}
			continue
		}
		if seg != "" {
			steps = append(steps, pathStep{key: seg, index: -1})
		}
	}
	return steps
}

// parseValue parses the right-hand side of a comparison.
func parseValue(raw string) (any, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, `"`) && strings.HasSuffix(raw, `"`) && len(raw) >= 2 {
		return raw[1 : len(raw)-1], nil
	}
	switch raw {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null":
		return nil, nil
	}
	if f, err := strconv.ParseFloat(raw, 64); err == nil {
		if math.IsInf(f, 0) || math.IsNaN(f) {
			return nil, fmt.Errorf("invalid value %q: non-finite number", raw)
		}
		return f, nil
	}
	return nil, fmt.Errorf("invalid value %q", raw)
}

type comparisonAssertion struct {
	sel        selector
	op         string
	rhs        any
	expression string
}

func (a *comparisonAssertion) Evaluate(ctx context.Context, result Result) error {
	_ = ctx
	if hasAnyStep(a.sel) {
		matches := 0
		_ = extractAny(a.sel, result, func(actual any) {
			if ok, err := compareValues(actual, a.op, a.rhs); err == nil && ok {
				matches++
			}
		})
		if matches == 0 {
			return fmt.Errorf("assertion failed: %s (no element satisfied the expression)", a.expression)
		}
		return nil
	}
	actual, err := extractValue(a.sel, result)
	if err != nil {
		return fmt.Errorf("assertion failed: %s: %w", a.expression, err)
	}
	ok, err := compareValues(actual, a.op, a.rhs)
	if err != nil {
		return fmt.Errorf("assertion failed: %s: %w", a.expression, err)
	}
	if !ok {
		return fmt.Errorf("assertion failed: %s (%v %s %v)", a.expression, actual, a.op, a.rhs)
	}
	return nil
}

// hasAnyStep reports whether a body selector contains an anyIndex ("[]") step,
// which requires the fan-out evaluation in extractAny.
func hasAnyStep(sel selector) bool {
	if sel.kind != selectorBody {
		return false
	}
	for _, step := range sel.steps {
		if step.index == anyIndex {
			return true
		}
	}
	return false
}

// extractAny fans out a body selector across every element of the array at an
// anyIndex step, invoking visit for each fully-resolved value. It is a best-effort
// walk: elements that fail to resolve (non-array at the fan-out step, absent
// keys, index bounds) are skipped rather than aborting the walk.
func extractAny(sel selector, result Result, visit func(any)) error {
	var data any
	if err := json.Unmarshal(result.Body, &data); err != nil {
		return fmt.Errorf("response body is not valid JSON: %w", err)
	}
	var walk func(cur any, steps []pathStep)
	walk = func(cur any, steps []pathStep) {
		if len(steps) == 0 {
			visit(cur)
			return
		}
		step := steps[0]
		switch {
		case step.index == anyIndex:
			arr, ok := cur.([]any)
			if !ok {
				return
			}
			for _, elem := range arr {
				walk(elem, steps[1:])
			}
		case step.key != "":
			m, ok := cur.(map[string]any)
			if !ok {
				return
			}
			next, ok := m[step.key]
			if !ok {
				return
			}
			walk(next, steps[1:])
		case step.index >= 0:
			arr, ok := cur.([]any)
			if !ok || step.index >= len(arr) {
				return
			}
			walk(arr[step.index], steps[1:])
		}
	}
	walk(data, sel.steps)
	return nil
}

// extractValue resolves a selector against a result.
func extractValue(sel selector, result Result) (any, error) {
	switch sel.kind {
	case selectorBody:
		var data any
		if err := json.Unmarshal(result.Body, &data); err != nil {
			return nil, fmt.Errorf("response body is not valid JSON: %w", err)
		}
		cur := data
		for _, step := range sel.steps {
			switch {
			case step.key != "":
				m, ok := cur.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("path %q not found in body", step.key)
				}
				cur, ok = m[step.key]
				if !ok {
					// An absent key resolves to nil so that e.g. `body.x != "y"`
					// passes (nil != "y") instead of failing on a lookup error.
					return nil, nil
				}
			case step.index >= 0:
				arr, ok := cur.([]any)
				if !ok || step.index >= len(arr) {
					return nil, fmt.Errorf("index [%d] not found in body", step.index)
				}
				cur = arr[step.index]
			}
		}
		return cur, nil
	case selectorHeader:
		if result.Headers == nil {
			return nil, fmt.Errorf("no response headers")
		}
		v := result.Headers.Get(sel.name)
		if v == "" {
			return nil, fmt.Errorf("header %q not present", sel.name)
		}
		return v, nil
	case selectorVariable:
		v, ok := result.Variables[sel.name]
		if !ok {
			return nil, fmt.Errorf("variable %q not present", sel.name)
		}
		return v, nil
	default:
		return nil, fmt.Errorf("unsupported selector")
	}
}

// compareValues compares actual against expected using op. Numeric operands are
// compared numerically; everything else by string equality (ordering on
// non-numeric values is an error).
func compareValues(actual any, op string, expected any) (bool, error) {
	af, aNum := toFloat(actual)
	ef, eNum := toFloat(expected)
	switch {
	case aNum && eNum:
		switch op {
		case "==":
			return af == ef, nil
		case "!=":
			return af != ef, nil
		case "<":
			return af < ef, nil
		case "<=":
			return af <= ef, nil
		case ">":
			return af > ef, nil
		case ">=":
			return af >= ef, nil
		}
	case aNum != eNum:
		// Mixed numeric/non-numeric operands cannot be compared.
		return false, fmt.Errorf("cannot compare %v with %v: mixed numeric and non-numeric operands", actual, expected)
	}
	as, es := fmt.Sprint(actual), fmt.Sprint(expected)
	switch op {
	case "==":
		return as == es, nil
	case "!=":
		return as != es, nil
	default:
		return false, fmt.Errorf("cannot order non-numeric values %q vs %q", as, es)
	}
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}
