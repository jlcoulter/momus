package fhirpath

import (
	"context"
	"testing"
)

func TestResultValue(t *testing.T) {
	res := asResult("x")
	if got := res.Value(); got != "x" {
		t.Fatalf("Value() = %v, want x", got)
	}
	var r Result
	if got := r.Value(); got != nil {
		t.Fatalf("zero Result Value() = %v, want nil", got)
	}
}

func TestEvalBinaryComparisons(t *testing.T) {
	cases := []struct {
		expr string
		want bool
	}{
		{"1 < 2", true},
		{"2 < 1", false},
		{"2 <= 2", true},
		{"3 > 2", true},
		{"2 > 3", false},
		{"3 >= 3", true},
		{"'a' < 'b'", true},
		{"'b' > 'a'", true},
		{"'a' <= 'a'", true},
		{"'b' >= 'a'", true},
		{"1 < 'x'", false},
	}
	for _, c := range cases {
		t.Run(c.expr, func(t *testing.T) {
			res, err := evalStr(t, c.expr, map[string]any{})
			if err != nil {
				t.Fatalf("Eval: %v", err)
			}
			if got := resTruthyBool(res); got != c.want {
				t.Fatalf("%s = %v, want %v", c.expr, got, c.want)
			}
		})
	}
}

func TestEvalArithmetic(t *testing.T) {
	cases := []struct {
		expr string
		want float64
	}{
		{"1 + 2", 3},
		{"5 - 3", 2},
		{"2 * 3", 6},
		{"8 / 2", 4},
	}
	for _, c := range cases {
		t.Run(c.expr, func(t *testing.T) {
			res, err := evalStr(t, c.expr, map[string]any{})
			if err != nil {
				t.Fatalf("Eval: %v", err)
			}
			got := res.value.(float64)
			if got != c.want {
				t.Fatalf("%s = %v, want %v", c.expr, got, c.want)
			}
		})
	}
}

func TestEvalBooleanOperators(t *testing.T) {
	cases := []struct {
		expr string
		want bool
	}{
		{"true and true", true},
		{"true and false", false},
		{"false and true", false},
		{"false and false", false},
		{"true or false", true},
		{"false or false", false},
		{"true or true", true},
	}
	for _, c := range cases {
		t.Run(c.expr, func(t *testing.T) {
			res, err := evalStr(t, c.expr, map[string]any{})
			if err != nil {
				t.Fatalf("Eval: %v", err)
			}
			if got := resTruthyBool(res); got != c.want {
				t.Fatalf("%s = %v, want %v", c.expr, got, c.want)
			}
		})
	}
}

func TestEvalFunctionStartsWith(t *testing.T) {
	ctx := map[string]any{}
	res, err := evalStr(t, "'hello'.startsWith('hel')", ctx)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if !resTruthyBool(res) {
		t.Fatal("'hello'.startsWith('hel') should be true")
	}
	res, err = evalStr(t, "'hello'.startsWith('xyz')", ctx)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if resTruthyBool(res) {
		t.Fatal("'hello'.startsWith('xyz') should be false")
	}
}

func TestEvalFunctionMatches(t *testing.T) {
	ctx := map[string]any{}
	res, err := evalStr(t, "'abc123'.matches('[0-9]+')", ctx)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if !resTruthyBool(res) {
		t.Fatal("'abc123'.matches('[0-9]+') should be true")
	}
	res, err = evalStr(t, "'hello'.matches('[0-9]+')", ctx)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if resTruthyBool(res) {
		t.Fatal("'hello'.matches('[0-9]+') should be false")
	}
}

func TestEvalFunctionToInteger(t *testing.T) {
	ctx := map[string]any{}
	res, err := evalStr(t, "'42.9'.toInteger()", ctx)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if got := res.value.(float64); got != 42 {
		t.Fatalf("toInteger() = %v, want 42", got)
	}
	res, err = evalStr(t, "'abc'.toInteger()", ctx)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if res.value != nil {
		t.Fatalf("toInteger('abc') = %v, want nil", res.value)
	}
}

func TestEvalFunctionIif(t *testing.T) {
	ctx := map[string]any{}
	res, err := evalStr(t, "iif(true, 'yes', 'no')", ctx)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if res.value != "yes" {
		t.Fatalf("iif(true,...) = %v, want 'yes'", res.value)
	}
	res, err = evalStr(t, "iif(false, 'yes', 'no')", ctx)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if res.value != "no" {
		t.Fatalf("iif(false,...) = %v, want 'no'", res.value)
	}
	res, err = evalStr(t, "iif(false, 'yes')", ctx)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if res.value != nil {
		t.Fatalf("iif(false,'yes') = %v, want nil", res.value)
	}
}

func TestEvalFunctionFirstLastLength(t *testing.T) {
	ctx := map[string]any{"name": []any{"a", "b", "c"}}
	res, err := evalStr(t, "name.first()", ctx)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if res.value != "a" {
		t.Fatalf("first() = %v, want a", res.value)
	}
	res, err = evalStr(t, "name.last()", ctx)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if res.value != "c" {
		t.Fatalf("last() = %v, want c", res.value)
	}
	res, err = evalStr(t, "'abc'.length()", ctx)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if got := res.value.(float64); got != 3 {
		t.Fatalf("length() = %v, want 3", got)
	}
}

func TestEvalFunctionDistinct(t *testing.T) {
	ctx := map[string]any{"name": []any{"a", "a", "b"}}
	res, err := evalStr(t, "name.distinct().count()", ctx)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if got := res.value.(float64); got != 2 {
		t.Fatalf("distinct().count() = %v, want 2", got)
	}
}

func TestEvalDeepDescent(t *testing.T) {
	ctx := map[string]any{
		"contained": []any{
			map[string]any{"id": "a", "nested": map[string]any{"code": "x"}},
			map[string]any{"id": "b", "code": "y"},
		},
	}
	res, err := evalStr(t, "contained..code.exists()", ctx)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if !resTruthyBool(res) {
		t.Fatal("contained..code.exists() should be true")
	}
}

func TestEvalIndex(t *testing.T) {
	ctx := map[string]any{"name": []any{map[string]any{"family": "Smith"}, map[string]any{"family": "Jones"}}}
	res, err := evalStr(t, "name[0].family", ctx)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if res.value != "Smith" {
		t.Fatalf("name[0].family = %v, want Smith", res.value)
	}
	res, err = evalStr(t, "name[1].family", ctx)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if res.value != "Jones" {
		t.Fatalf("name[1].family = %v, want Jones", res.value)
	}
	// Out-of-range index yields an empty/falsy result.
	res, err = evalStr(t, "name[5].exists()", ctx)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if resTruthyBool(res) {
		t.Fatal("name[5].exists() should be false for out-of-range index")
	}
}

func TestEvalBracketFilters(t *testing.T) {
	ctx := map[string]any{"name": []any{map[string]any{"family": "Smith"}, map[string]any{"family": "Jones"}}}

	// where filter (dot-notation)
	res, err := evalStr(t, "name.where(family = 'Smith').exists()", ctx)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if !resTruthyBool(res) {
		t.Fatal("name.where(family='Smith').exists() should be true")
	}

	// bracket-filter syntax [where(...)]/[first()]/[last()]
	res, err = evalStr(t, "name[where(family = 'Jones')].family", ctx)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if res.value != "Jones" {
		t.Fatalf("name[where(family='Jones')].family = %v, want Jones", res.value)
	}
	res, err = evalStr(t, "name[first()].family", ctx)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if res.value != "Smith" {
		t.Fatalf("name[first()].family = %v, want Smith", res.value)
	}
	res, err = evalStr(t, "name[last()].family", ctx)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if res.value != "Jones" {
		t.Fatalf("name[last()].family = %v, want Jones", res.value)
	}

	// all filter
	res, err = evalStr(t, "name.all(family.exists())", ctx)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if !resTruthyBool(res) {
		t.Fatal("name.all(family.exists()) should be true")
	}

	// any filter
	res, err = evalStr(t, "name.any(family = 'Jones')", ctx)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if !resTruthyBool(res) {
		t.Fatal("name.any(family='Jones') should be true")
	}

	// exists filter
	res, err = evalStr(t, "name.exists()", ctx)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if !resTruthyBool(res) {
		t.Fatal("name.exists() should be true")
	}
}

func TestResTruthy(t *testing.T) {
	cases := []struct {
		name  string
		val   any
		wantB bool
		wantK bool
	}{
		{"bool true", true, true, true},
		{"bool false", false, false, true},
		{"nil", nil, false, true},
		{"empty array", []any{}, false, true},
		{"single array", []any{true}, true, true},
		{"multi array", []any{float64(1), float64(2)}, true, true},
		{"string", "x", true, true},
		{"number", float64(0), true, true},
		{"unknown", unknownSentinel, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, known := resTruthy(Result{value: c.val})
			if b != c.wantB || known != c.wantK {
				t.Fatalf("resTruthy(%v) = (%v, %v), want (%v, %v)", c.val, b, known, c.wantB, c.wantK)
			}
		})
	}
}

func TestEvalUnknownConstruct(t *testing.T) {
	// An out-of-scope binary operator must evaluate to unknown, not error.
	res := evalBinary("xor", asResult(true), asResult(false))
	b, known := resTruthy(res)
	if known {
		t.Fatalf("expected unknown for unsupported op, got known=%v b=%v", known, b)
	}
}

func TestParseErrors(t *testing.T) {
	invalid := []string{"(", "name.where(", "1 ++ 2", ""}
	for _, expr := range invalid {
		t.Run(expr, func(t *testing.T) {
			_, err := Parse(expr)
			if err == nil {
				t.Fatalf("Parse(%q) expected error, got nil", expr)
			}
		})
	}
}

func TestEvalNumberLiteralComparison(t *testing.T) {
	res, err := evalStr(t, "3 - 2", map[string]any{})
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if got := res.value.(float64); got != 1 {
		t.Fatalf("3 - 2 = %v, want 1", got)
	}
}

func TestLexerStringEscapes(t *testing.T) {
	toks, err := lex(`'a\nb\tc\\d\'e'`)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	if len(toks) != 2 || toks[0].kind != tokString {
		t.Fatalf("tokens = %v", toks)
	}
	want := "a\nb\tc\\d'e"
	if toks[0].text != want {
		t.Fatalf("string = %q, want %q", toks[0].text, want)
	}
}

func TestLexerUnterminatedString(t *testing.T) {
	if _, err := lex("'abc"); err == nil {
		t.Fatal("expected error for unterminated string")
	}
}

func TestLexerUnexpectedCharacter(t *testing.T) {
	if _, err := lex("!"); err == nil {
		t.Fatal("expected error for lone '!'")
	}
	if _, err := lex("#"); err == nil {
		t.Fatal("expected error for unexpected character")
	}
}

func TestLexerPercentVariable(t *testing.T) {
	toks, err := lex("%context")
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	if toks[0].kind != tokPercent || toks[0].text != "context" {
		t.Fatalf("percent token = %v", toks[0])
	}
}

func TestEvalPercentContext(t *testing.T) {
	res, err := evalStr(t, "%context.name.exists()", map[string]any{"name": []any{map[string]any{}}})
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if !resTruthyBool(res) {
		t.Fatal("percent-context.name.exists() should be true")
	}
}

func TestEvalBoolKnownUnknown(t *testing.T) {
	b, known, err := EvalBool(context.Background(), "true", map[string]any{})
	if err != nil {
		t.Fatalf("EvalBool: %v", err)
	}
	if !b || !known {
		t.Fatalf("EvalBool(true) = %v, %v", b, known)
	}
}

func TestAsResult(t *testing.T) {
	if got := asResult("x"); got.value != "x" {
		t.Fatalf("asResult = %v", got)
	}
	var nilResult Result
	if nilResult.value != nil {
		t.Fatalf("zero Result = %v", nilResult.value)
	}
}

func TestEvalUnaryNot(t *testing.T) {
	res := evalUnary("not", asResult(true))
	if resTruthyBool(res) {
		t.Fatal("not(true) should be false")
	}
	// Unknown propagates.
	u := evalUnary("not", unknownResult())
	if _, known := resTruthy(u); known {
		t.Fatal("not(unknown) should be unknown")
	}
	// Unknown operator.
	u = evalUnary("weird", asResult(true))
	if _, known := resTruthy(u); known {
		t.Fatal("unknown unary operator should be unknown")
	}
}

func TestEvalFilterFirstLastWhereAnyAll(t *testing.T) {
	ctx := map[string]any{"items": []any{1, 2, 3}}
	// first().
	res, err := evalStr(t, "items.first()", ctx)
	if err != nil || res.value != 1 {
		t.Fatalf("items.first() = %v, err=%v", res.value, err)
	}
	// last().
	res, err = evalStr(t, "items.last()", ctx)
	if err != nil || res.value != 3 {
		t.Fatalf("items.last() = %v, err=%v", res.value, err)
	}
	// where() with a matching predicate on maps.
	mctx := map[string]any{"items": []any{map[string]any{"v": 1}, map[string]any{"v": 2}, map[string]any{"v": 3}}}
	res, err = evalStr(t, "items.where(v > 1).count()", mctx)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if got := res.value.(float64); got != 2 {
		t.Fatalf("items.where(v>1).count() = %v, want 2", got)
	}
	// all() true when every item satisfies the predicate.
	res, err = evalStr(t, "items.all(v > 0)", mctx)
	if err != nil || !resTruthyBool(res) {
		t.Fatalf("items.all(v>0) = %v, err=%v", res.value, err)
	}
	// any() true when at least one item satisfies a numeric comparison.
	res, err = evalStr(t, "items.any(v > 1)", mctx)
	if err != nil || !resTruthyBool(res) {
		t.Fatalf("items.any(v>1) = %v, err=%v", res.value, err)
	}
	// exists() filter on empty collection.
	res, err = evalStr(t, "missing.exists()", map[string]any{})
	if err != nil || resTruthyBool(res) {
		t.Fatalf("missing.exists() = %v, err=%v", res.value, err)
	}
}

func TestEvalFunctionExistsEmptyNotCount(t *testing.T) {
	// exists() on present.
	res, err := evalStr(t, "name.exists()", map[string]any{"name": []any{map[string]any{}}})
	if err != nil || !resTruthyBool(res) {
		t.Fatalf("exists() = %v, err=%v", res.value, err)
	}
	// count() as a bare function.
	res, err = evalStr(t, "count()", map[string]any{"a": 1})
	if err != nil {
		t.Fatalf("count() err: %v", err)
	}
	// not() top-level function.
	res, err = evalStr(t, "not(active.exists())", map[string]any{})
	if err != nil || !resTruthyBool(res) {
		t.Fatalf("not() = %v, err=%v", res.value, err)
	}
	// toString().
	res, err = evalStr(t, "toString()", map[string]any{"x": 1})
	if err != nil {
		t.Fatalf("toString: %v", err)
	}
	// length() on non-string returns 0.
	res, err = evalStr(t, "length()", map[string]any{"x": 1})
	if err != nil || res.value.(float64) != 0 {
		t.Fatalf("length(non-string) = %v, err=%v", res.value, err)
	}
	// distinct on a single value.
	res, err = evalStr(t, "distinct()", map[string]any{"x": []any{"a", "a"}})
	if err != nil {
		t.Fatalf("distinct: %v", err)
	}
}

func TestEvalNumericOpNonNumeric(t *testing.T) {
	res := numericOp("x", 1, func(a, b float64) float64 { return a + b })
	if _, known := resTruthy(res); known {
		t.Fatal("numericOp on non-numeric should be unknown")
	}
}

func TestToFloat(t *testing.T) {
	if _, ok := toFloat(float64(1)); !ok {
		t.Fatal("toFloat(float64) should succeed")
	}
	if _, ok := toFloat(1); !ok {
		t.Fatal("toFloat(int) should succeed")
	}
	if _, ok := toFloat(int64(1)); !ok {
		t.Fatal("toFloat(int64) should succeed")
	}
	if _, ok := toFloat("x"); ok {
		t.Fatal("toFloat(string) should fail")
	}
}

func TestEqualValues(t *testing.T) {
	if !equalValues("a", "a") || equalValues("a", "b") {
		t.Fatal("equalValues(string)")
	}
	if !equalValues(true, true) || equalValues(true, false) {
		t.Fatal("equalValues(bool)")
	}
	if equalValues(1, "1") {
		t.Fatal("equalValues should reject mixed types")
	}
}

func TestCompileMatchesCache(t *testing.T) {
	re, err := compileMatches("^a+$")
	if err != nil {
		t.Fatalf("compileMatches: %v", err)
	}
	if !re.MatchString("aaa") {
		t.Fatal("compiled regex should match")
	}
	if _, err := compileMatches("["); err == nil {
		t.Fatal("compileMatches(invalid) should error")
	}
}
