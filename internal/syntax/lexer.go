package syntax

import (
	"fmt"
	"strings"
)

// Lexer scans .eiche source into tokens. It never stops on a bad character —
// it records a Diagnostic, emits an ILLEGAL token, and keeps going, so a
// single typo doesn't hide every other error in the file.
type Lexer struct {
	file   string
	src    []rune
	pos    int // index into src of the next unread rune
	line   int
	col    int
	Errors []Diagnostic
}

func NewLexer(file, source string) *Lexer {
	return &Lexer{
		file: file,
		src:  []rune(source),
		pos:  0,
		line: 1,
		col:  1,
	}
}

func (l *Lexer) here() Position {
	return Position{Line: l.line, Column: l.col, Offset: l.pos}
}

func (l *Lexer) peek() rune {
	if l.pos >= len(l.src) {
		return 0
	}
	return l.src[l.pos]
}

func (l *Lexer) peekAt(off int) rune {
	i := l.pos + off
	if i >= len(l.src) {
		return 0
	}
	return l.src[i]
}

func (l *Lexer) advance() rune {
	r := l.src[l.pos]
	l.pos++
	if r == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return r
}

func (l *Lexer) errorf(pos Position, format string, args ...any) {
	l.Errors = append(l.Errors, Diagnostic{
		File:    l.file,
		Pos:     pos,
		Message: fmt.Sprintf(format, args...),
	})
}

// Next returns the next token. Callers should keep calling Next until it
// returns a Token with Kind == EOF.
func (l *Lexer) Next() Token {
	l.skipWhitespaceAndComments()

	start := l.here()
	if l.pos >= len(l.src) {
		return Token{Kind: EOF, Pos: start}
	}

	r := l.peek()
	switch {
	case isLetter(r):
		return l.scanIdent(start)
	case isDigit(r):
		return l.scanNumber(start)
	case r == '"':
		return l.scanString(start)
	}

	return l.scanOperator(start)
}

func (l *Lexer) skipWhitespaceAndComments() {
	for l.pos < len(l.src) {
		r := l.peek()
		switch {
		case r == ' ' || r == '\t' || r == '\r' || r == '\n':
			l.advance()
		case r == '/' && l.peekAt(1) == '/':
			for l.pos < len(l.src) && l.peek() != '\n' {
				l.advance()
			}
		default:
			return
		}
	}
}

func (l *Lexer) scanIdent(start Position) Token {
	var b strings.Builder
	for l.pos < len(l.src) && (isLetter(l.peek()) || isDigit(l.peek())) {
		b.WriteRune(l.advance())
	}
	text := b.String()
	return Token{Kind: LookupIdent(text), Text: text, Pos: start}
}

func (l *Lexer) scanNumber(start Position) Token {
	var b strings.Builder
	for l.pos < len(l.src) && isDigit(l.peek()) {
		b.WriteRune(l.advance())
	}
	if l.peek() == '.' && isDigit(l.peekAt(1)) {
		b.WriteRune(l.advance()) // '.'
		for l.pos < len(l.src) && isDigit(l.peek()) {
			b.WriteRune(l.advance())
		}
		return Token{Kind: DECIMAL_LIT, Text: b.String(), Pos: start}
	}
	return Token{Kind: INT_LIT, Text: b.String(), Pos: start}
}

func (l *Lexer) scanString(start Position) Token {
	l.advance() // opening quote
	var b strings.Builder
	for {
		if l.pos >= len(l.src) {
			l.errorf(start, "unterminated string literal")
			return Token{Kind: STRING_LIT, Text: b.String(), Pos: start}
		}
		r := l.peek()
		if r == '"' {
			l.advance()
			return Token{Kind: STRING_LIT, Text: b.String(), Pos: start}
		}
		if r == '\n' {
			l.errorf(start, "unterminated string literal")
			return Token{Kind: STRING_LIT, Text: b.String(), Pos: start}
		}
		if r == '\\' {
			l.advance()
			esc := l.peek()
			switch esc {
			case '"':
				b.WriteRune('"')
			case '\\':
				b.WriteRune('\\')
			case 'n':
				b.WriteRune('\n')
			case 't':
				b.WriteRune('\t')
			default:
				l.errorf(l.here(), "unknown escape sequence '\\%c'", esc)
				b.WriteRune(esc)
			}
			if l.pos < len(l.src) {
				l.advance()
			}
			continue
		}
		b.WriteRune(l.advance())
	}
}

func (l *Lexer) scanOperator(start Position) Token {
	r := l.advance()
	two := func(next rune, kind TokenKind, single TokenKind) Token {
		if l.peek() == next {
			l.advance()
			return Token{Kind: kind, Text: string(r) + string(next), Pos: start}
		}
		return Token{Kind: single, Text: string(r), Pos: start}
	}

	switch r {
	case '{':
		return Token{Kind: LBRACE, Text: "{", Pos: start}
	case '}':
		return Token{Kind: RBRACE, Text: "}", Pos: start}
	case '(':
		return Token{Kind: LPAREN, Text: "(", Pos: start}
	case ')':
		return Token{Kind: RPAREN, Text: ")", Pos: start}
	case '[':
		return Token{Kind: LBRACKET, Text: "[", Pos: start}
	case ']':
		return Token{Kind: RBRACKET, Text: "]", Pos: start}
	case ',':
		return Token{Kind: COMMA, Text: ",", Pos: start}
	case ';':
		return Token{Kind: SEMI, Text: ";", Pos: start}
	case '=':
		return two('=', EQ, EQUAL)
	case '?':
		return Token{Kind: QUESTION, Text: "?", Pos: start}
	case '@':
		return Token{Kind: AT, Text: "@", Pos: start}
	case '+':
		return Token{Kind: PLUS, Text: "+", Pos: start}
	case '-':
		return Token{Kind: MINUS, Text: "-", Pos: start}
	case '*':
		return Token{Kind: STAR, Text: "*", Pos: start}
	case '/':
		return Token{Kind: SLASH, Text: "/", Pos: start}
	case '%':
		return Token{Kind: PERCENT, Text: "%", Pos: start}
	case '!':
		return two('=', NEQ, NOT)
	case '<':
		return two('=', LTE, LT)
	case '>':
		return two('=', GTE, GT)
	case '&':
		if l.peek() == '&' {
			l.advance()
			return Token{Kind: AND, Text: "&&", Pos: start}
		}
		l.errorf(start, "unexpected character '&' (did you mean '&&'?)")
		return Token{Kind: ILLEGAL, Text: "&", Pos: start}
	case '|':
		if l.peek() == '|' {
			l.advance()
			return Token{Kind: OR, Text: "||", Pos: start}
		}
		l.errorf(start, "unexpected character '|' (did you mean '||'?)")
		return Token{Kind: ILLEGAL, Text: "|", Pos: start}
	case ':':
		if l.peek() == ':' {
			l.advance()
			return Token{Kind: DOUBLE_COLON, Text: "::", Pos: start}
		}
		return Token{Kind: COLON, Text: ":", Pos: start}
	case '.':
		if l.peek() == '.' && l.peekAt(1) == '.' {
			l.advance()
			l.advance()
			return Token{Kind: ELLIPSIS, Text: "...", Pos: start}
		}
		return Token{Kind: DOT, Text: ".", Pos: start}
	}

	l.errorf(start, "unexpected character %q", r)
	return Token{Kind: ILLEGAL, Text: string(r), Pos: start}
}

func isLetter(r rune) bool {
	return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isDigit(r rune) bool {
	return r >= '0' && r <= '9'
}
