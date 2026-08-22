package fhirpath

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strings"
	"sync"
)

// evalFunction implements the supported FHIRPath functions. Any function
// outside the supported set returns unknown (never an error).
func evalFunction(ctx context.Context, name string, contextValue any, args []Result) (Result, error) {
	switch name {
	case "exists":
		// returns true if context is non-empty
		coll := toCollection(contextValue)
		if isUnknownResult(contextValue) {
			return unknownResult(), nil
		}
		return asResult(len(coll) > 0), nil
	case "empty":
		if isUnknownResult(contextValue) {
			return unknownResult(), nil
		}
		return asResult(len(toCollection(contextValue)) == 0), nil
	case "not":
		b, known := resTruthy(asResult(contextValue))
		if !known {
			return unknownResult(), nil
		}
		return asResult(!b), nil
	case "count":
		if isUnknownResult(contextValue) {
			return unknownResult(), nil
		}
		return asResult(float64(len(toCollection(contextValue)))), nil
	case "first":
		coll := toCollection(contextValue)
		if len(coll) == 0 {
			return asResult(nil), nil
		}
		return asResult(coll[0]), nil
	case "last":
		coll := toCollection(contextValue)
		if len(coll) == 0 {
			return asResult(nil), nil
		}
		return asResult(coll[len(coll)-1]), nil
	case "distinct":
		return distinctResult(contextValue), nil
	case "length":
		s, ok := contextValue.(string)
		if !ok {
			return asResult(float64(0)), nil
		}
		return asResult(float64(len([]rune(s)))), nil
	case "startsWith":
		return evalStartsWith(contextValue, args), nil
	case "matches":
		return evalMatches(contextValue, args), nil
	case "toInteger":
		return evalToInteger(contextValue), nil
	case "toString":
		if isUnknownResult(contextValue) {
			return unknownResult(), nil
		}
		return asResult(fmt.Sprintf("%v", contextValue)), nil
	case "iif":
		return evalIif(contextValue, args), nil
	default:
		return unknownResult(), nil
	}
}

// evalFilter evaluates [where(...)], [all(...)], [any(...)], [first()],
// [last()].
func evalFilter(ctx context.Context, filter string, base Result, arg expr, contextValue any) (Result, error) {
	if isUnknown(base) {
		return unknownResult(), nil
	}
	coll := toCollection(base.value)
	switch filter {
	case "first":
		if len(coll) == 0 {
			return asResult(nil), nil
		}
		return asResult(coll[0]), nil
	case "last":
		if len(coll) == 0 {
			return asResult(nil), nil
		}
		return asResult(coll[len(coll)-1]), nil
	case "where":
		var out []any
		for _, item := range coll {
			if arg == nil {
				continue
			}
			r, err := arg.eval(ctx, item)
			if err != nil {
				return Result{}, err
			}
			b, known := resTruthy(r)
			if known && b {
				out = append(out, item)
			}
		}
		if len(out) == 1 {
			return asResult(out[0]), nil
		}
		return asResult(out), nil
	case "all":
		for _, item := range coll {
			if arg == nil {
				return unknownResult(), nil
			}
			r, err := arg.eval(ctx, item)
			if err != nil {
				return Result{}, err
			}
			b, known := resTruthy(r)
			if known && !b {
				return asResult(false), nil
			}
			if !known {
				return unknownResult(), nil
			}
		}
		return asResult(true), nil
	case "any":
		for _, item := range coll {
			if arg == nil {
				continue
			}
			r, err := arg.eval(ctx, item)
			if err != nil {
				return Result{}, err
			}
			b, known := resTruthy(r)
			if known && b {
				return asResult(true), nil
			}
		}
		return asResult(false), nil
	case "exists":
		return asResult(len(coll) > 0), nil
	default:
		return unknownResult(), nil
	}
}

// evalUnary evaluates a prefix operator such as 'not'.
func evalUnary(op string, r Result) Result {
	switch op {
	case "not":
		if isUnknown(r) {
			return unknownResult()
		}
		b, _ := resTruthy(r)
		return asResult(!b)
	default:
		return unknownResult()
	}
}

// evalBinary evaluates a binary operator.
func evalBinary(op string, l, r Result) Result {
	if isUnknown(l) || isUnknown(r) {
		return unknownResult()
	}
	switch op {
	case "and":
		lb, lk := resTruthy(l)
		rb, rk := resTruthy(r)
		if lk && !lb {
			return asResult(false)
		}
		if rk && !rb {
			return asResult(false)
		}
		if !lk || !rk {
			return unknownResult()
		}
		return asResult(true)
	case "or":
		lb, lk := resTruthy(l)
		rb, rk := resTruthy(r)
		if lk && lb {
			return asResult(true)
		}
		if rk && rb {
			return asResult(true)
		}
		if !lk || !rk {
			return unknownResult()
		}
		return asResult(false)
	case "=":
		return asResult(equalValues(l.value, r.value))
	case "!=":
		return asResult(!equalValues(l.value, r.value))
	case "<", "<=", ">", ">=":
		return asResult(compareValues(op, l.value, r.value))
	case "+":
		return numericOp(l.value, r.value, func(a, b float64) float64 { return a + b })
	case "-":
		return numericOp(l.value, r.value, func(a, b float64) float64 { return a - b })
	case "*":
		return numericOp(l.value, r.value, func(a, b float64) float64 { return a * b })
	case "/":
		return numericOp(l.value, r.value, func(a, b float64) float64 { return a / b })
	default:
		return unknownResult()
	}
}

func equalValues(a, b any) bool {
	af, aok := a.(float64)
	bf, bok := b.(float64)
	if aok && bok {
		return af == bf
	}
	as, aok := a.(string)
	bs, bok := b.(string)
	if aok && bok {
		return as == bs
	}
	ab, aok := a.(bool)
	bb, bok := b.(bool)
	if aok && bok {
		return ab == bb
	}
	return false
}

func compareValues(op string, a, b any) bool {
	af, aok := toFloat(a)
	bf, bok := toFloat(b)
	if aok && bok {
		switch op {
		case "<":
			return af < bf
		case "<=":
			return af <= bf
		case ">":
			return af > bf
		case ">=":
			return af >= bf
		}
	}
	as, aok := a.(string)
	bs, bok := b.(string)
	if aok && bok {
		switch op {
		case "<":
			return as < bs
		case "<=":
			return as <= bs
		case ">":
			return as > bs
		case ">=":
			return as >= bs
		}
	}
	return false
}

func numericOp(a, b any, f func(float64, float64) float64) Result {
	af, aok := toFloat(a)
	bf, bok := toFloat(b)
	if !aok || !bok {
		return unknownResult()
	}
	return asResult(f(af, bf))
}

func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	default:
		return 0, false
	}
}

// distinctResult removes duplicate values from a collection, preserving order.
func distinctResult(v any) Result {
	coll := toCollection(v)
	seen := make(map[string]struct{})
	var out []any
	for _, item := range coll {
		key := fmt.Sprintf("%#v", item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	if len(out) == 1 {
		return asResult(out[0])
	}
	return asResult(out)
}

func evalStartsWith(contextValue any, args []Result) Result {
	s, ok := contextValue.(string)
	if !ok || len(args) < 1 {
		return asResult(false)
	}
	prefix, ok := args[0].value.(string)
	if !ok {
		return asResult(false)
	}
	return asResult(strings.HasPrefix(s, prefix))
}

func evalMatches(contextValue any, args []Result) Result {
	s, ok := contextValue.(string)
	if !ok || len(args) < 1 {
		return asResult(false)
	}
	pattern, ok := args[0].value.(string)
	if !ok {
		return asResult(false)
	}
	re, err := compileMatches(pattern)
	if err != nil {
		return unknownResult()
	}
	return asResult(re.MatchString(s))
}

// matchesRegexCache memoizes compiled regexes so invariant expressions with the
// same pattern are not recompiled on every element evaluation.
var matchesRegexCache sync.Map // pattern string -> *regexp.Regexp

// compileMatches returns a compiled regexp for pattern, caching the result.
func compileMatches(pattern string) (*regexp.Regexp, error) {
	if v, ok := matchesRegexCache.Load(pattern); ok {
		return v.(*regexp.Regexp), nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	matchesRegexCache.Store(pattern, re)
	return re, nil
}

func evalToInteger(contextValue any) Result {
	switch t := contextValue.(type) {
	case float64:
		return asResult(math.Trunc(t))
	case int:
		return asResult(float64(t))
	case string:
		var f float64
		if _, err := fmt.Sscanf(t, "%g", &f); err == nil {
			return asResult(math.Trunc(f))
		}
	}
	return asResult(nil)
}

func evalIif(contextValue any, args []Result) Result {
	_ = contextValue
	if len(args) < 2 {
		return unknownResult()
	}
	cond, known := resTruthy(args[0])
	if !known {
		return unknownResult()
	}
	if cond {
		return args[1]
	}
	if len(args) >= 3 {
		return args[2]
	}
	return asResult(nil)
}
