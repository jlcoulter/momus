package fhirpath

import "fmt"

// parser is a recursive-descent parser over the FHIRPath token stream.
type parser struct {
	toks []token
	pos  int
}

// Parse parses a FHIRPath expression string into an AST.
func Parse(expression string) (expr, error) {
	toks, err := lex(expression)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	e, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if t := p.peek(); t.kind != tokEOF {
		return nil, fmt.Errorf("fhirpath: unexpected token %q at position %d", t.text, t.pos)
	}
	return e, nil
}

func (p *parser) peek() token {
	if p.pos < len(p.toks) {
		return p.toks[p.pos]
	}
	return token{kind: tokEOF}
}

func (p *parser) advance() token {
	t := p.peek()
	if p.pos < len(p.toks) {
		p.pos++
	}
	return t
}

func (p *parser) match(kind tokenKind) (token, bool) {
	if p.peek().kind == kind {
		return p.advance(), true
	}
	return token{}, false
}

// parseExpr parses an expression, handling 'or' at the lowest precedence.
func (p *parser) parseExpr() (expr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for {
		if t, ok := p.match(tokOr); ok {
			_ = t
			right, err := p.parseAnd()
			if err != nil {
				return nil, err
			}
			left = &binExpr{op: "or", lhs: left, rhs: right}
		} else {
			return left, nil
		}
	}
}

// parseAnd handles 'and' over equality/comparison terms.
func (p *parser) parseAnd() (expr, error) {
	left, err := p.parseComparison()
	if err != nil {
		return nil, err
	}
	for {
		if t, ok := p.match(tokAnd); ok {
			_ = t
			right, err := p.parseComparison()
			if err != nil {
				return nil, err
			}
			left = &binExpr{op: "and", lhs: left, rhs: right}
		} else {
			return left, nil
		}
	}
}

// parseComparison handles =, !=, <, <=, >, >= over additive terms.
func (p *parser) parseComparison() (expr, error) {
	left, err := p.parseAdditive()
	if err != nil {
		return nil, err
	}
	for {
		var op string
		switch p.peek().kind {
		case tokEq:
			op = "="
		case tokNe:
			op = "!="
		case tokLt:
			op = "<"
		case tokLe:
			op = "<="
		case tokGt:
			op = ">"
		case tokGe:
			op = ">="
		default:
			return left, nil
		}
		p.advance()
		right, err := p.parseAdditive()
		if err != nil {
			return nil, err
		}
		left = &binExpr{op: op, lhs: left, rhs: right}
	}
}

// parseAdditive handles + and - over multiplicative terms.
func (p *parser) parseAdditive() (expr, error) {
	left, err := p.parseMultiplicative()
	if err != nil {
		return nil, err
	}
	for {
		var op string
		switch p.peek().kind {
		case tokPlus:
			op = "+"
		case tokMinus:
			op = "-"
		default:
			return left, nil
		}
		p.advance()
		right, err := p.parseMultiplicative()
		if err != nil {
			return nil, err
		}
		left = &binExpr{op: op, lhs: left, rhs: right}
	}
}

// parseMultiplicative handles * and / over unary terms.
func (p *parser) parseMultiplicative() (expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		var op string
		switch p.peek().kind {
		case tokStar:
			op = "*"
		case tokSlash:
			op = "/"
		default:
			return left, nil
		}
		p.advance()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = &binExpr{op: op, lhs: left, rhs: right}
	}
}

// parseUnary handles the unary 'not'.
func (p *parser) parseUnary() (expr, error) {
	if t, ok := p.match(tokNot); ok {
		_ = t
		operand, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &unaryExpr{op: "not", operand: operand}, nil
	}
	return p.parsePostfix()
}

// parsePostfix parses a primary followed by .name, (args), [filter].
func (p *parser) parsePostfix() (expr, error) {
	left, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	return p.parsePostfixTail(left)
}

func (p *parser) parsePostfixTail(left expr) (expr, error) {
	for {
		switch p.peek().kind {
		case tokDot:
			p.advance()
			// Could be .name, .name(args), or '..' already handled as tokDotDot.
			nameTok, ok := p.match(tokIdentifier)
			if !ok {
				if p.peek().kind == tokStar {
					p.advance()
					left = &pathExpr{name: "*", deep: false}
					continue
				}
				return nil, fmt.Errorf("fhirpath: expected identifier after '.' at position %d", p.peek().pos)
			}
			// function call?
			if _, ok := p.match(tokLParen); ok {
				args, err := p.parseArgs()
				if err != nil {
					return nil, err
				}
				left = &funcExpr{name: nameTok.text, args: args, base: left}
			} else {
				left = &pathExpr{name: nameTok.text}
			}
		case tokDotDot:
			p.advance()
			nameTok, ok := p.match(tokIdentifier)
			if !ok {
				return nil, fmt.Errorf("fhirpath: expected identifier after '..'")
			}
			left = &pathExpr{name: nameTok.text, deep: true}
		case tokLBracket:
			p.advance()
			// [index] or [where(...)]/[all(...)]/[first()]/[last()]
			if t, ok := p.match(tokNumber); ok {
				// parse number as index
				idx := parseIndex(t.text)
				left = &filterExpr{base: left, filter: "index", index: idx}
				if _, ok := p.match(tokRBracket); !ok {
					return nil, fmt.Errorf("fhirpath: expected ']'")
				}
				continue
			}
			// filter function inside brackets
			fe, err := p.parseBracketFilter(left)
			if err != nil {
				return nil, err
			}
			left = fe
		default:
			return left, nil
		}
	}
}
