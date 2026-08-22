package fhirpath

import (
	"context"
	"testing"
)

func evalStr(t *testing.T, expr string, ctx any) (Result, error) {
	t.Helper()
	return Eval(context.Background(), expr, ctx)
}

func TestEvalExists(t *testing.T) {
	// Patient.name.exists() against a resource with a name.
	ctx := map[string]any{"name": []any{map[string]any{"family": "Smith"}}}
	res, err := Eval(context.Background(), "name.exists()", ctx)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if !resTruthyBool(res) {
		t.Fatalf("name.exists() = false, want true")
	}
}

func TestEvalEmpty(t *testing.T) {
	ctx := map[string]any{"active": false}
	res, err := evalStr(t, "active.empty()", ctx)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	// active is false (present) so empty() should be false
	if resTruthyBool(res) {
		t.Fatalf("active.empty() = true, want false")
	}
	ctx2 := map[string]any{}
	res2, err := evalStr(t, "active.empty()", ctx2)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if !resTruthyBool(res2) {
		t.Fatalf("missing active.empty() = false, want true")
	}
}

func TestEvalCount(t *testing.T) {
	ctx := map[string]any{"name": []any{map[string]any{}, map[string]any{}}}
	res, err := evalStr(t, "name.count() = 2", ctx)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if !resTruthyBool(res) {
		t.Fatalf("name.count() = 2 is false, want true")
	}
}

func TestEvalAll(t *testing.T) {
	ctx := map[string]any{
		"contained": []any{
			map[string]any{"id": "a"},
			map[string]any{"id": "b"},
		},
	}
	// contained.all(it.id.count() = 1) — simplified to contained.id.count() pattern
	res, err := evalStr(t, "contained.all(id.exists())", ctx)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if !resTruthyBool(res) {
		t.Fatalf("contained.all(id.exists()) = false, want true")
	}
}

func TestEvalNot(t *testing.T) {
	res, err := evalStr(t, "not(active.exists())", map[string]any{})
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if !resTruthyBool(res) {
		t.Fatalf("not(active.exists()) = false, want true (active absent)")
	}
}

func TestEvalUnknownFunctionReturnsUnknown(t *testing.T) {
	// A function outside the subset must yield unknown, not an error or false.
	_, err := Eval(context.Background(), "unknownFunction()", map[string]any{})
	if err != nil {
		t.Fatalf("Eval should not error on out-of-scope function, got %v", err)
	}
	b, known, err := EvalBool(context.Background(), "unknownFunction()", map[string]any{})
	if err != nil {
		t.Fatalf("EvalBool: %v", err)
	}
	if known {
		t.Fatalf("unknown function should be unknown (known=false), got known=%v val=%v", known, b)
	}
}

func TestEvalStringLiteral(t *testing.T) {
	res, err := evalStr(t, "'hello' = 'hello'", map[string]any{})
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if !resTruthyBool(res) {
		t.Fatalf("'hello' = 'hello' should be true")
	}
}

func TestEvalSubtraction(t *testing.T) {
	res, err := evalStr(t, "3 - 2 = 1", map[string]any{})
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if !resTruthyBool(res) {
		t.Fatalf("3 - 2 = 1 should be true, got %+v", res.value)
	}
}

func TestLexerSeparatesNumberAndMinus(t *testing.T) {
	l := &lexer{input: []rune("3-2")}
	tok, err := l.next()
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if tok.kind != tokNumber || tok.text != "3" {
		t.Fatalf("first token = %+v, want number '3'", tok)
	}
	tok, err = l.next()
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if tok.kind != tokMinus {
		t.Fatalf("second token = %+v, want tokMinus", tok)
	}
	tok, err = l.next()
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if tok.kind != tokNumber || tok.text != "2" {
		t.Fatalf("third token = %+v, want number '2'", tok)
	}
}

func resTruthyBool(r Result) bool {
	b, _ := resTruthy(r)
	return b
}
