package fhirpath

import "context"

// expr is a parsed FHIRPath expression node.
type expr interface {
	eval(ctx context.Context, contextValue any) (Result, error)
}

// literalExpr is a literal value (string, number, boolean, nil).
type literalExpr struct {
	value any
}

func (e *literalExpr) eval(ctx context.Context, _ any) (Result, error) {
	return asResult(e.value), nil
}

// pathExpr navigates a property (or descends into children with '..') from a
// context value. When the context is a collection, the property is resolved
// against every element and the results are flattened.
type pathExpr struct {
	name string // property name; empty means "self"
	deep bool   // true for '..' descent
}

func (e *pathExpr) eval(ctx context.Context, contextValue any) (Result, error) {
	// Normalise context to a collection.
	coll := toCollection(contextValue)
	if isUnknownResult(contextValue) {
		return unknownResult(), nil
	}
	var out []any
	for _, item := range coll {
		if e.name == "" && !e.deep {
			out = append(out, item)
			continue
		}
		sub := resolvePath(item, e.name, e.deep)
		out = append(out, sub...)
	}
	if len(out) == 1 {
		return asResult(out[0]), nil
	}
	return asResult(out), nil
}

// funcExpr is a function call. When the call is a method on a path (e.g.
// "name.count()"), base is the path expression the function is applied to; when
// it is a top-level function (e.g. "not(...)"), base is nil and the function
// receives the context value.
type funcExpr struct {
	name string
	args []expr
	base expr
}

func (e *funcExpr) eval(ctx context.Context, contextValue any) (Result, error) {
	argResults := make([]Result, 0, len(e.args))
	for _, a := range e.args {
		r, err := a.eval(ctx, contextValue)
		if err != nil {
			return Result{}, err
		}
		argResults = append(argResults, r)
	}
	// Determine the function's context: the base path result if present,
	// otherwise the incoming context value.
	fnContext := contextValue
	hasBase := e.base != nil
	if e.base != nil {
		baseResult, err := e.base.eval(ctx, contextValue)
		if err != nil {
			return Result{}, err
		}
		fnContext = baseResult.value
	}
	// Collection-filtering methods (all/where/any/exists/first/last) operate on
	// the base result as a collection, mirroring [all(...)] style filters.
	if hasBase {
		switch e.name {
		case "all", "where", "any", "exists", "first", "last":
			arg := expr(nil)
			if len(e.args) > 0 {
				arg = e.args[0]
			}
			return evalFilter(ctx, e.name, asResult(fnContext), arg, contextValue)
		}
	}
	return evalFunction(ctx, e.name, fnContext, argResults)
}

// binExpr is a binary operation.
type binExpr struct {
	op  string // "=", "!=", "<", "<=", ">", ">=", "+", "-", "*", "/", "and", "or"
	lhs expr
	rhs expr
}

func (e *binExpr) eval(ctx context.Context, contextValue any) (Result, error) {
	l, err := e.lhs.eval(ctx, contextValue)
	if err != nil {
		return Result{}, err
	}
	r, err := e.rhs.eval(ctx, contextValue)
	if err != nil {
		return Result{}, err
	}
	return evalBinary(e.op, l, r), nil
}

// unaryExpr is a prefix operation such as 'not'.
type unaryExpr struct {
	op      string // "not"
	operand expr
}

func (e *unaryExpr) eval(ctx context.Context, contextValue any) (Result, error) {
	r, err := e.operand.eval(ctx, contextValue)
	if err != nil {
		return Result{}, err
	}
	return evalUnary(e.op, r), nil
}

// filterExpr is a path followed by a filter such as [where(...)], [all(...)],
// [first()], or [index].
type filterExpr struct {
	base   expr
	filter string // "where", "all", "any", "first", "last", "index"
	arg    expr   // the argument expression (for where/all/any) or nil
	index  int    // the array index for "index" filters
}

func (e *filterExpr) eval(ctx context.Context, contextValue any) (Result, error) {
	base, err := e.base.eval(ctx, contextValue)
	if err != nil {
		return Result{}, err
	}
	if e.filter == "index" {
		coll := toCollection(base.value)
		if e.index < 0 || e.index >= len(coll) {
			return asResult(nil), nil
		}
		return asResult(coll[e.index]), nil
	}
	return evalFilter(ctx, e.filter, base, e.arg, contextValue)
}

// toCollection converts a Go value to a slice for path iteration.
func toCollection(v any) []any {
	switch t := v.(type) {
	case nil:
		return nil
	case []any:
		return t
	default:
		return []any{v}
	}
}

// isUnknownResult reports whether a Go value is the unknown sentinel.
func isUnknownResult(v any) bool {
	_, ok := v.(unknownMark)
	return ok
}

// resolvePath resolves a property (or deep-descent) against a single value.
func resolvePath(item any, name string, deep bool) []any {
	if isUnknownResult(item) {
		return []any{unknownSentinel}
	}
	if deep {
		// '..' collects the property at any depth.
		return collectDeep(item, name)
	}
	m, ok := item.(map[string]any)
	if !ok {
		return nil
	}
	// Choice-type resolution: "value" -> "valueString".
	if v, ok := m[name]; ok {
		return []any{v}
	}
	for k, v := range m {
		if choiceKeyMatches(k, name) {
			return []any{v}
		}
	}
	return nil
}

// collectDeep returns every value named name at any nesting depth under item.
func collectDeep(item any, name string) []any {
	var out []any
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			for k, val := range t {
				if k == name {
					out = append(out, val)
				}
				walk(val)
			}
		case []any:
			for _, e := range t {
				walk(e)
			}
		}
	}
	walk(item)
	return out
}

// choiceKeyMatches mirrors validate's choice-key resolution for the fhirpath
// package (kept local to avoid an import cycle).
func choiceKeyMatches(key, name string) bool {
	if len(key) <= len(name) {
		return false
	}
	runes := []rune(key)
	base := runes[:len(name)]
	if string(base) != name {
		return false
	}
	// The char after the name must be uppercase (choice suffix).
	if len(runes) > len(name) {
		next := runes[len(name)]
		return next >= 'A' && next <= 'Z'
	}
	return false
}
