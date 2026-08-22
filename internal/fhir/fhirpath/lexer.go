package fhirpath

import (
	"fmt"
	"strings"
	"unicode"
)

// tokenKind enumerates FHIRPath lexical token types.
type tokenKind int

const (
	tokEOF tokenKind = iota
	tokIdentifier
	tokString
	tokNumber
	tokDot      // .
	tokDotDot   // ..
	tokLBracket // [
	tokRBracket // ]
	tokLParen   // (
	tokRParen   // )
	tokComma    // ,
	tokPipe     // |
	tokStar     // *
	tokEq       // =
	tokNe       // !=
	tokLt       // <
	tokLe       // <=
	tokGt       // >
	tokGe       // >=
	tokPlus     // +
	tokMinus    // -
	tokSlash    // /
	tokAnd      // and
	tokOr       // or
	tokNot      // not
	tokTrue     // true
	tokFalse    // false
	tokPercent  // %context
)

// token is a single lexed token.
type token struct {
	kind tokenKind
	text string
	pos  int
}

// lexer turns a FHIRPath expression string into a token stream.
type lexer struct {
	input []rune
	pos   int
}

func lex(expression string) ([]token, error) {
	l := &lexer{input: []rune(expression)}
	var toks []token
	for {
		t, err := l.next()
		if err != nil {
			return nil, err
		}
		toks = append(toks, t)
		if t.kind == tokEOF {
			return toks, nil
		}
	}
}

func (l *lexer) peek() (rune, bool) {
	if l.pos >= len(l.input) {
		return 0, false
	}
	return l.input[l.pos], true
}

func (l *lexer) peekAt(off int) (rune, bool) {
	if l.pos+off >= len(l.input) {
		return 0, false
	}
	return l.input[l.pos+off], true
}

func (l *lexer) next() (token, error) {
	// Skip whitespace.
	for {
		r, ok := l.peek()
		if !ok {
			return token{kind: tokEOF, pos: l.pos}, nil
		}
		if unicode.IsSpace(r) {
			l.pos++
			continue
		}
		break
	}
	start := l.pos
	r, _ := l.peek()

	switch r {
	case '.':
		if r2, ok := l.peekAt(1); ok && r2 == '.' {
			l.pos += 2
			return token{kind: tokDotDot, text: "..", pos: start}, nil
		}
		l.pos++
		return token{kind: tokDot, text: ".", pos: start}, nil
	case '[':
		l.pos++
		return token{kind: tokLBracket, text: "[", pos: start}, nil
	case ']':
		l.pos++
		return token{kind: tokRBracket, text: "]", pos: start}, nil
	case '(':
		l.pos++
		return token{kind: tokLParen, text: "(", pos: start}, nil
	case ')':
		l.pos++
		return token{kind: tokRParen, text: ")", pos: start}, nil
	case ',':
		l.pos++
		return token{kind: tokComma, text: ",", pos: start}, nil
	case '|':
		l.pos++
		return token{kind: tokPipe, text: "|", pos: start}, nil
	case '*':
		l.pos++
		return token{kind: tokStar, text: "*", pos: start}, nil
	case '=':
		l.pos++
		return token{kind: tokEq, text: "=", pos: start}, nil
	case '!':
		if r2, ok := l.peekAt(1); ok && r2 == '=' {
			l.pos += 2
			return token{kind: tokNe, text: "!=", pos: start}, nil
		}
		return token{}, fmt.Errorf("fhirpath: unexpected '!' at position %d", start)
	case '<':
		if r2, ok := l.peekAt(1); ok && r2 == '=' {
			l.pos += 2
			return token{kind: tokLe, text: "<=", pos: start}, nil
		}
		l.pos++
		return token{kind: tokLt, text: "<", pos: start}, nil
	case '>':
		if r2, ok := l.peekAt(1); ok && r2 == '=' {
			l.pos += 2
			return token{kind: tokGe, text: ">=", pos: start}, nil
		}
		l.pos++
		return token{kind: tokGt, text: ">", pos: start}, nil
	case '+':
		l.pos++
		return token{kind: tokPlus, text: "+", pos: start}, nil
	case '-':
		l.pos++
		return token{kind: tokMinus, text: "-", pos: start}, nil
	case '/':
		l.pos++
		return token{kind: tokSlash, text: "/", pos: start}, nil
	case '%':
		l.pos++
		name := l.readIdent()
		return token{kind: tokPercent, text: name, pos: start}, nil
	case '\'':
		return l.readString(start)
	}

	if unicode.IsDigit(r) {
		return l.lexNumber(), nil
	}
	if unicode.IsLetter(r) {
		text := l.readIdent()
		switch strings.ToLower(text) {
		case "and":
			return token{kind: tokAnd, text: text, pos: start}, nil
		case "or":
			return token{kind: tokOr, text: text, pos: start}, nil
		case "not":
			return token{kind: tokNot, text: text, pos: start}, nil
		case "true":
			return token{kind: tokTrue, text: text, pos: start}, nil
		case "false":
			return token{kind: tokFalse, text: text, pos: start}, nil
		default:
			return token{kind: tokIdentifier, text: text, pos: start}, nil
		}
	}
	return token{}, fmt.Errorf("fhirpath: unexpected character %q at position %d", string(r), start)
}

func (l *lexer) readIdent() string {
	start := l.pos
	for l.pos < len(l.input) {
		r := l.input[l.pos]
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			l.pos++
		} else {
			break
		}
	}
	return string(l.input[start:l.pos])
}

func (l *lexer) lexNumber() token {
	start := l.pos
	for l.pos < len(l.input) {
		r := l.input[l.pos]
		if unicode.IsDigit(r) || r == '.' {
			l.pos++
		} else {
			break
		}
	}
	return token{kind: tokNumber, text: string(l.input[start:l.pos]), pos: start}
}

// readString consumes a single-quoted string literal starting at pos (the
// opening quote has not yet been consumed).
func (l *lexer) readString(start int) (token, error) {
	l.pos++ // consume opening quote
	var sb strings.Builder
	for l.pos < len(l.input) {
		r := l.input[l.pos]
		if r == '\'' {
			l.pos++
			return token{kind: tokString, text: sb.String(), pos: start}, nil
		}
		if r == '\\' && l.pos+1 < len(l.input) {
			next := l.input[l.pos+1]
			switch next {
			case 'n':
				sb.WriteByte('\n')
			case 't':
				sb.WriteByte('\t')
			case '\\':
				sb.WriteByte('\\')
			case '\'':
				sb.WriteByte('\'')
			default:
				sb.WriteRune(next)
			}
			l.pos += 2
			continue
		}
		sb.WriteRune(r)
		l.pos++
	}
	return token{}, fmt.Errorf("fhirpath: unterminated string literal at position %d", start)
}
