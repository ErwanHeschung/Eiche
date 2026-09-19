package syntax

import "fmt"

// Parser is a recursive-descent parser over a single .eiche file. Parse
// errors don't abort the whole file: each fails the declaration or field
// it's inside, records a Diagnostic, and the parser resynchronizes at the
// next safe boundary so one typo doesn't hide every other error.
type Parser struct {
	file string
	lex  *Lexer

	cur  Token
	peek Token

	Errors []Diagnostic
}

func NewParser(file, source string) *Parser {
	p := &Parser{file: file, lex: NewLexer(file, source)}
	p.cur = p.lex.Next()
	p.peek = p.lex.Next()
	return p
}

// parseError is a sentinel panic value used for local error recovery; it
// never escapes ParseFile.
type parseError struct{}

func (p *Parser) errorf(pos Position, format string, args ...any) {
	p.Errors = append(p.Errors, Diagnostic{
		File:    p.file,
		Pos:     pos,
		Message: fmt.Sprintf(format, args...),
	})
}

func (p *Parser) fail(pos Position, format string, args ...any) {
	p.errorf(pos, format, args...)
	panic(parseError{})
}

func (p *Parser) advance() {
	p.cur = p.peek
	p.peek = p.lex.Next()
}

func (p *Parser) at(kind TokenKind) bool { return p.cur.Kind == kind }

// atRuleStart reports whether the parser is looking at the "@rule(" that
// begins a model member, as opposed to a plain "@annotation(...)". Both
// share the same "@" Ident "(" shape, so a field's trailing-annotation
// loop needs this lookahead to know when to stop: otherwise the "@rule"
// following a field with no annotations of its own gets parsed as an
// annotation literally named "rule" attached to that field, instead of
// as its own model member.
func (p *Parser) atRuleStart() bool {
	return p.cur.Kind == AT && p.peek.Kind == IDENT && p.peek.Text == "rule"
}

func (p *Parser) expect(kind TokenKind) Token {
	if p.cur.Kind != kind {
		p.fail(p.cur.Pos, "expected %s, found %s", kind, describeToken(p.cur))
	}
	tok := p.cur
	p.advance()
	return tok
}

func describeToken(t Token) string {
	if t.Kind == IDENT || t.Kind == STRING_LIT || t.Kind == INT_LIT || t.Kind == DECIMAL_LIT {
		return fmt.Sprintf("%s %q", t.Kind, t.Text)
	}
	return t.Kind.String()
}

// ParseFile parses a complete .eiche source file. It always returns a
// non-nil *File; check p.Errors (merged with lexer errors) to know whether
// parsing was clean.
func ParseFile(filename, source string) (*File, []Diagnostic) {
	p := NewParser(filename, source)
	f := p.parseFile()
	errs := append(append([]Diagnostic{}, p.lex.Errors...), p.Errors...)
	return f, errs
}

var declStarters = map[TokenKind]bool{
	IMPORT: true, CAPABILITY: true, ENUM: true, MODEL: true, VIEW: true, EOF: true,
}

func (p *Parser) syncToDecl() {
	for !declStarters[p.cur.Kind] {
		p.advance()
	}
}

func (p *Parser) parseFile() (f *File) {
	f = &File{}
	for p.at(IMPORT) {
		f.Imports = append(f.Imports, p.parseImport())
	}
	for !p.at(EOF) {
		func() {
			defer func() {
				if r := recover(); r != nil {
					if _, ok := r.(parseError); !ok {
						panic(r)
					}
					p.syncToDecl()
				}
			}()
			switch p.cur.Kind {
			case CAPABILITY:
				f.Decls = append(f.Decls, p.parseCapability())
			case ENUM:
				f.Decls = append(f.Decls, p.parseEnum())
			case MODEL:
				f.Decls = append(f.Decls, p.parseModel())
			case VIEW:
				f.Decls = append(f.Decls, p.parseView())
			case IMPORT:
				p.fail(p.cur.Pos, "imports must come before all other declarations")
			default:
				p.fail(p.cur.Pos, "expected a declaration (capability, enum, model, view), found %s", describeToken(p.cur))
			}
		}()
	}
	return f
}

func (p *Parser) parseImport() *ImportDecl {
	pos := p.cur.Pos
	p.expect(IMPORT)
	path := p.expect(STRING_LIT).Text
	if p.at(SEMI) {
		p.advance()
	}
	return &ImportDecl{Path: path, Pos: pos}
}

func (p *Parser) parseCapability() *CapabilityDecl {
	pos := p.cur.Pos
	p.expect(CAPABILITY)
	name := p.expect(IDENT).Text
	p.expect(FROM)
	from := p.expect(STRING_LIT).Text
	return &CapabilityDecl{Name: name, From: from, Pos: pos}
}

func (p *Parser) parseEnum() *EnumDecl {
	pos := p.cur.Pos
	p.expect(ENUM)
	name := p.expect(IDENT).Text
	p.expect(LBRACE)
	var members []string
	for !p.at(RBRACE) {
		members = append(members, p.expect(IDENT).Text)
		if p.at(COMMA) {
			p.advance()
		} else {
			break
		}
	}
	p.expect(RBRACE)
	return &EnumDecl{Name: name, Members: members, Pos: pos}
}

func (p *Parser) parseModel() *ModelDecl {
	pos := p.cur.Pos
	p.expect(MODEL)
	name := p.expect(IDENT).Text
	p.expect(LBRACE)

	m := &ModelDecl{Name: name, Pos: pos}
	for !p.at(RBRACE) {
		p.parseModelMember(m)
	}
	p.expect(RBRACE)
	return m
}

var memberStarters = map[TokenKind]bool{
	IDENT: true, AT: true, ELLIPSIS: true, RBRACE: true,
}

func (p *Parser) parseModelMember(m *ModelDecl) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(parseError); !ok {
				panic(r)
			}
			for !memberStarters[p.cur.Kind] {
				p.advance()
			}
		}
	}()

	switch p.cur.Kind {
	case ELLIPSIS:
		p.advance()
		m.AllowUnknownFields = true
	case AT:
		m.Rules = append(m.Rules, p.parseRule())
	case IDENT:
		m.Fields = append(m.Fields, p.parseField())
	default:
		p.fail(p.cur.Pos, "expected a field, @rule, or '...', found %s", describeToken(p.cur))
	}
}

func (p *Parser) parseField() *FieldDecl {
	pos := p.cur.Pos
	name := p.expect(IDENT).Text
	optional := false
	if p.at(QUESTION) {
		p.advance()
		optional = true
	}
	p.expect(COLON)
	typ := p.parseTypeExpr()

	var annotations []*Annotation
	for p.at(AT) && !p.atRuleStart() {
		annotations = append(annotations, p.parseAnnotation())
	}

	var def Expr
	if p.at(EQUAL) {
		p.advance()
		def = p.parseExpr()
		if !optional {
			p.errorf(pos, "field %q has a default value but is not optional; only \"%s?\" fields may have defaults", name, name)
		}
	}

	return &FieldDecl{Name: name, Optional: optional, Type: typ, Annotations: annotations, Default: def, Pos: pos}
}

var scalarKeywords = map[TokenKind]string{
	KW_STRING: "string", KW_INT: "int", KW_DECIMAL: "decimal", KW_BOOL: "bool",
	KW_DATE: "date", KW_DATETIME: "datetime", KW_JSON: "json",
}

func (p *Parser) parseTypeExpr() *TypeExpr {
	pos := p.cur.Pos
	var scalar string
	if name, ok := scalarKeywords[p.cur.Kind]; ok {
		scalar = name
		p.advance()
	} else if p.at(IDENT) {
		scalar = p.cur.Text
		p.advance()
	} else {
		p.fail(pos, "expected a type, found %s", describeToken(p.cur))
	}

	t := &TypeExpr{Scalar: scalar, Pos: pos}

	if scalar == "json" && p.at(LBRACE) {
		t.JSONShape = p.parseJSONShape()
	}

	if p.at(LBRACKET) {
		p.advance()
		t.IsArray = true
		for p.at(AT) {
			t.ElemAnnotations = append(t.ElemAnnotations, p.parseAnnotation())
		}
		p.expect(RBRACKET)
	}

	return t
}

func (p *Parser) parseJSONShape() *JSONShape {
	pos := p.cur.Pos
	p.expect(LBRACE)
	shape := &JSONShape{Pos: pos}
	for !p.at(RBRACE) {
		if p.at(ELLIPSIS) {
			p.advance()
			shape.AllowUnknown = true
			continue
		}
		shape.Fields = append(shape.Fields, p.parseField())
	}
	p.expect(RBRACE)
	return shape
}

func (p *Parser) parseAnnotation() *Annotation {
	pos := p.cur.Pos
	p.expect(AT)
	name := p.expect(IDENT).Text
	ann := &Annotation{Name: name, Pos: pos}
	if p.at(LPAREN) {
		p.advance()
		if !p.at(RPAREN) {
			ann.Args = append(ann.Args, p.parseExpr())
			for p.at(COMMA) {
				p.advance()
				ann.Args = append(ann.Args, p.parseExpr())
			}
		}
		p.expect(RPAREN)
	}
	return ann
}

func (p *Parser) parseRule() *RuleDecl {
	pos := p.cur.Pos
	p.expect(AT)
	kw := p.expect(IDENT)
	if kw.Text != "rule" {
		p.fail(kw.Pos, "expected 'rule', found %q", kw.Text)
	}
	p.expect(LPAREN)
	expr := p.parseExpr()
	p.expect(COMMA)
	msg := p.expect(STRING_LIT).Text
	p.expect(RPAREN)

	rule := &RuleDecl{Expr: expr, Message: msg, Pos: pos}
	if p.at(ON) {
		p.advance()
		rule.On = p.expect(IDENT).Text
	}
	return rule
}

func (p *Parser) parseView() *ViewDecl {
	pos := p.cur.Pos
	p.expect(VIEW)
	name := p.expect(IDENT).Text
	p.expect(EQUAL)

	v := &ViewDecl{Name: name, Pos: pos}
	if p.at(PARTIAL) {
		p.advance()
		v.Partial = true
		v.Base = p.expect(IDENT).Text
		return v
	}

	v.Base = p.expect(IDENT).Text
	if p.at(LBRACE) {
		p.advance()
		for !p.at(RBRACE) {
			v.Fields = append(v.Fields, p.expect(IDENT).Text)
			if p.at(COMMA) {
				p.advance()
			} else {
				break
			}
		}
		p.expect(RBRACE)
	}
	return v
}

// ---- Expressions ----
// Precedence, loosest to tightest:
//   implies (right-assoc) > || > && > ! > == != < <= > >= > + - > * / % > unary - > postfix

func (p *Parser) parseExpr() Expr { return p.parseImplies() }

func (p *Parser) parseImplies() Expr {
	x := p.parseOr()
	if p.at(IMPLIES) {
		pos := p.cur.Pos
		p.advance()
		y := p.parseImplies() // right-associative
		return &BinaryExpr{Op: IMPLIES, X: x, Y: y, Pos: pos}
	}
	return x
}

func (p *Parser) parseOr() Expr {
	x := p.parseAnd()
	for p.at(OR) {
		pos := p.cur.Pos
		p.advance()
		x = &BinaryExpr{Op: OR, X: x, Y: p.parseAnd(), Pos: pos}
	}
	return x
}

func (p *Parser) parseAnd() Expr {
	x := p.parseNot()
	for p.at(AND) {
		pos := p.cur.Pos
		p.advance()
		x = &BinaryExpr{Op: AND, X: x, Y: p.parseNot(), Pos: pos}
	}
	return x
}

func (p *Parser) parseNot() Expr {
	if p.at(NOT) {
		pos := p.cur.Pos
		p.advance()
		return &UnaryExpr{Op: NOT, X: p.parseCompare(), Pos: pos}
	}
	return p.parseCompare()
}

var compareOps = map[TokenKind]bool{EQ: true, NEQ: true, LT: true, LTE: true, GT: true, GTE: true}

func (p *Parser) parseCompare() Expr {
	x := p.parseAdd()
	if compareOps[p.cur.Kind] {
		op := p.cur.Kind
		pos := p.cur.Pos
		p.advance()
		return &BinaryExpr{Op: op, X: x, Y: p.parseAdd(), Pos: pos}
	}
	return x
}

func (p *Parser) parseAdd() Expr {
	x := p.parseMul()
	for p.at(PLUS) || p.at(MINUS) {
		op := p.cur.Kind
		pos := p.cur.Pos
		p.advance()
		x = &BinaryExpr{Op: op, X: x, Y: p.parseMul(), Pos: pos}
	}
	return x
}

func (p *Parser) parseMul() Expr {
	x := p.parseUnary()
	for p.at(STAR) || p.at(SLASH) || p.at(PERCENT) {
		op := p.cur.Kind
		pos := p.cur.Pos
		p.advance()
		x = &BinaryExpr{Op: op, X: x, Y: p.parseUnary(), Pos: pos}
	}
	return x
}

func (p *Parser) parseUnary() Expr {
	if p.at(MINUS) {
		pos := p.cur.Pos
		p.advance()
		return &UnaryExpr{Op: MINUS, X: p.parsePostfix(), Pos: pos}
	}
	return p.parsePostfix()
}

func (p *Parser) parsePostfix() Expr {
	x := p.parsePrimary()
	for {
		switch p.cur.Kind {
		case DOUBLE_COLON:
			pos := p.cur.Pos
			p.advance()
			fn := p.expect(IDENT).Text
			p.expect(LPAREN)
			var args []Expr
			if !p.at(RPAREN) {
				args = append(args, p.parseExpr())
				for p.at(COMMA) {
					p.advance()
					args = append(args, p.parseExpr())
				}
			}
			p.expect(RPAREN)
			x = &CapabilityCallExpr{X: x, Func: fn, Args: args, Pos: pos}
		case DOT:
			pos := p.cur.Pos
			p.advance()
			field := p.expect(IDENT).Text
			switch field {
			case "isNull":
				x = &BinaryExpr{Op: EQ, X: x, Y: &NullLitExpr{Pos: pos}, Pos: pos}
			case "isPresent":
				x = &BinaryExpr{Op: NEQ, X: x, Y: &NullLitExpr{Pos: pos}, Pos: pos}
			default:
				x = &SelectorExpr{X: x, Field: field, Pos: pos}
			}
		default:
			return x
		}
	}
}

func (p *Parser) parsePrimary() Expr {
	tok := p.cur
	switch tok.Kind {
	case INT_LIT:
		p.advance()
		return &IntLitExpr{Value: tok.Text, Pos: tok.Pos}
	case DECIMAL_LIT:
		p.advance()
		return &DecimalLitExpr{Value: tok.Text, Pos: tok.Pos}
	case STRING_LIT:
		p.advance()
		return &StringLitExpr{Value: tok.Text, Pos: tok.Pos}
	case TRUE:
		p.advance()
		return &BoolLitExpr{Value: true, Pos: tok.Pos}
	case FALSE:
		p.advance()
		return &BoolLitExpr{Value: false, Pos: tok.Pos}
	case NULL:
		p.advance()
		return &NullLitExpr{Pos: tok.Pos}
	case IDENT:
		p.advance()
		return &IdentExpr{Name: tok.Text, Pos: tok.Pos}
	case LPAREN:
		p.advance()
		x := p.parseExpr()
		p.expect(RPAREN)
		return x
	default:
		p.fail(tok.Pos, "expected an expression, found %s", describeToken(tok))
		return nil // unreachable: fail panics
	}
}
