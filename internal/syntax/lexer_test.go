package syntax

import "testing"

func lexAll(t *testing.T, src string) ([]Token, *Lexer) {
	t.Helper()
	l := NewLexer("test.eiche", src)
	var toks []Token
	for {
		tok := l.Next()
		toks = append(toks, tok)
		if tok.Kind == EOF {
			break
		}
	}
	return toks, l
}

func kinds(toks []Token) []TokenKind {
	ks := make([]TokenKind, len(toks))
	for i, t := range toks {
		ks[i] = t.Kind
	}
	return ks
}

func assertKinds(t *testing.T, src string, want ...TokenKind) {
	t.Helper()
	toks, l := lexAll(t, src)
	if len(l.Errors) != 0 {
		t.Fatalf("unexpected lex errors for %q: %v", src, l.Errors)
	}
	got := kinds(toks)
	want = append(want, EOF)
	if len(got) != len(want) {
		t.Fatalf("%q: got %d tokens %v, want %d %v", src, len(got), got, len(want), want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%q: token %d = %v, want %v", src, i, got[i], want[i])
		}
	}
}

func TestPunctuationAndOperators(t *testing.T) {
	assertKinds(t, "{}()[],:;=?.@", LBRACE, RBRACE, LPAREN, RPAREN, LBRACKET, RBRACKET, COMMA, COLON, SEMI, EQUAL, QUESTION, DOT, AT)
	assertKinds(t, "...", ELLIPSIS)
	assertKinds(t, "::", DOUBLE_COLON)
	assertKinds(t, "== != < <= > >= && || !", EQ, NEQ, LT, LTE, GT, GTE, AND, OR, NOT)
	assertKinds(t, "+ - * / %", PLUS, MINUS, STAR, SLASH, PERCENT)
}

func TestKeywordsVsIdents(t *testing.T) {
	assertKinds(t, "model MyModel", MODEL, IDENT)
	assertKinds(t, "view partial on implies true false null", VIEW, PARTIAL, ON, IMPLIES, TRUE, FALSE, NULL)
	assertKinds(t, "string int decimal bool date datetime json", KW_STRING, KW_INT, KW_DECIMAL, KW_BOOL, KW_DATE, KW_DATETIME, KW_JSON)
	// "stringly" must not be mistaken for the keyword "string" (word-boundary check).
	assertKinds(t, "stringly", IDENT)
}

func TestNumbers(t *testing.T) {
	toks, l := lexAll(t, "20000 10.5 3")
	if len(l.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors)
	}
	want := []struct {
		kind TokenKind
		text string
	}{
		{INT_LIT, "20000"},
		{DECIMAL_LIT, "10.5"},
		{INT_LIT, "3"},
	}
	for i, w := range want {
		if toks[i].Kind != w.kind || toks[i].Text != w.text {
			t.Fatalf("token %d = %v %q, want %v %q", i, toks[i].Kind, toks[i].Text, w.kind, w.text)
		}
	}
}

func TestStringEscapes(t *testing.T) {
	toks, l := lexAll(t, `"salary.range" "line1\nline2" "quote\"inside"`)
	if len(l.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", l.Errors)
	}
	want := []string{"salary.range", "line1\nline2", "quote\"inside"}
	for i, w := range want {
		if toks[i].Text != w {
			t.Fatalf("string %d = %q, want %q", i, toks[i].Text, w)
		}
	}
}

func TestUnterminatedString(t *testing.T) {
	_, l := lexAll(t, `"never closed`)
	if len(l.Errors) != 1 {
		t.Fatalf("want 1 error, got %d: %v", len(l.Errors), l.Errors)
	}
	if l.Errors[0].Pos.Line != 1 || l.Errors[0].Pos.Column != 1 {
		t.Fatalf("error position = %v, want line 1 col 1", l.Errors[0].Pos)
	}
}

func TestLineComments(t *testing.T) {
	assertKinds(t, "model X // this is dropped\n{ }", MODEL, IDENT, LBRACE, RBRACE)
}

func TestIllegalCharacterRecovers(t *testing.T) {
	toks, l := lexAll(t, "model # X")
	if len(l.Errors) != 1 {
		t.Fatalf("want 1 error, got %d: %v", len(l.Errors), l.Errors)
	}
	got := kinds(toks)
	want := []TokenKind{MODEL, ILLEGAL, IDENT, EOF}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("token %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestPositions(t *testing.T) {
	toks, _ := lexAll(t, "model X {\n  title: string\n}")
	// "title" starts on line 2, column 3.
	var title Token
	for _, tok := range toks {
		if tok.Kind == IDENT && tok.Text == "title" {
			title = tok
		}
	}
	if title.Pos.Line != 2 || title.Pos.Column != 3 {
		t.Fatalf("title position = %+v, want line 2 col 3", title.Pos)
	}
}

// TestBriefExample lexes the exact DSL example from .context/CLAUDE.md
// end-to-end without producing any lexer errors, catching regressions
// against the language's own reference example.
func TestBriefExample(t *testing.T) {
	src := `import "./common.eiche"
capability phone   from "eiche-phone@1.2"
capability payroll from "eiche-payroll@1.0"

enum ContractType { CDI, CDD, ALTERNANCE, STAGE }

model JobOffer {
  title:        string @minLength(3) @maxLength(120)
  contractType: ContractType
  startDate:    date @after(today)
  endDate?:     date
  salaryMin:    int @min(20000)
  salaryMax:    int
  hours:        decimal @range(10, 48)
  skills:       string[@minLength(2)] @minItems(1)
  metadata:     json

  @rule(salaryMax >= salaryMin, "salary.range") on salaryMax
  @rule(contractType == CDD implies endDate != null, "cdd.endDate") on endDate
  @rule(phone::isValid(contactPhone, "FR"), "phone.notFrench") on contactPhone
  @rule(payroll::isCoherent(contractType, salaryMin, salaryMax, hours),
        "payroll.incoherent") on salaryMin
}`
	_, l := lexAll(t, src)
	if len(l.Errors) != 0 {
		t.Fatalf("unexpected lex errors on brief example: %v", l.Errors)
	}
}
