package fhirpath

import (
	"fmt"
)

// parsePrimary parses a literal, %variable, parenthesised expression, or a
// bare path expression.
func (p *parser) parsePrimary() (expr, error) {
	t := p.peek()
	switch t.kind {
	case tokString:
		p.advance()
		return &literalExpr{value: t.text}, nil
	case tokNumber:
		p.advance()
		return &literalExpr{value: parseNumber(t.text)}, nil
	case tokTrue:
		p.advance()
		return &literalExpr{value: true}, nil
	case tokFalse:
		p.advance()
		return &literalExpr{value: false}, nil
	case tokPercent:
		p.advance()
		// %context (and other %vars). %context resolves to the context value.
		if t.text == "context" {
			return &pathExpr{name: "", deep: false}, nil
		}
		return &pathExpr{name: "", deep: false}, nil
	case tokLParen:
		p.advance()
		inner, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, ok := p.match(tokRParen); !ok {
			return nil, fmt.Errorf("fhirpath: expected ')' at position %d", p.peek().pos)
		}
		return inner, nil
	case tokIdentifier:
		// A path rooted at the context: .name already handled in postfix;
		// a leading bare identifier is treated as a path on the context.
		p.advance()
		// Could be a function call `name(...)` or a property `name`.
		if _, ok := p.match(tokLParen); ok {
			args, err := p.parseArgs()
			if err != nil {
				return nil, err
			}
			return &funcExpr{name: t.text, args: args}, nil
		}
		return &pathExpr{name: t.text}, nil
	default:
		return nil, fmt.Errorf("fhirpath: unexpected token %q at position %d", t.text, t.pos)
	}
}

// parseArgs parses a comma-separated argument list following an opening '('.
func (p *parser) parseArgs() ([]expr, error) {
	var args []expr
	if _, ok := p.match(tokRParen); ok {
		return args, nil
	}
	for {
		a, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		args = append(args, a)
		if _, ok := p.match(tokComma); ok {
			continue
		}
		if _, ok := p.match(tokRParen); !ok {
			return nil, fmt.Errorf("fhirpath: expected ')' at position %d", p.peek().pos)
		}
		return args, nil
	}
}

// parseBracketFilter parses [<fn>(<arg>)] where fn is where/all/any/first/last.
func (p *parser) parseBracketFilter(base expr) (expr, error) {
	nameTok, ok := p.match(tokIdentifier)
	if !ok {
		return nil, fmt.Errorf("fhirpath: expected filter function in '[' at position %d", p.peek().pos)
	}
	if _, ok := p.match(tokLParen); !ok {
		return nil, fmt.Errorf("fhirpath: expected '(' after %q", nameTok.text)
	}
	fe := &filterExpr{base: base, filter: nameTok.text}
	switch nameTok.text {
	case "first", "last":
		if _, ok := p.match(tokRParen); !ok {
			return nil, fmt.Errorf("fhirpath: expected ')'")
		}
	case "where", "all", "any", "exists":
		arg, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		fe.arg = arg
		if _, ok := p.match(tokRParen); !ok {
			return nil, fmt.Errorf("fhirpath: expected ')'")
		}
	default:
		return nil, fmt.Errorf("fhirpath: unsupported filter %q", nameTok.text)
	}
	if _, ok := p.match(tokRBracket); !ok {
		return nil, fmt.Errorf("fhirpath: expected ']'")
	}
	return fe, nil
}

// parseNumber parses a numeric literal into a float64.
func parseNumber(text string) float64 {
	var f float64
	fmt.Sscanf(text, "%g", &f)
	return f
}

// parseIndex parses a numeric array index.
func parseIndex(text string) int {
	var i int
	fmt.Sscanf(text, "%d", &i)
	return i
}
