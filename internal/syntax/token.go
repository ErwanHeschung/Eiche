package syntax

type TokenKind int

const (
	EOF TokenKind = iota
	ILLEGAL

	IDENT
	INT_LIT
	DECIMAL_LIT
	STRING_LIT

	// keywords
	IMPORT
	CAPABILITY
	FROM
	ENUM
	MODEL
	VIEW
	PARTIAL
	ON
	IMPLIES
	TRUE
	FALSE
	NULL

	// scalar type keywords
	KW_STRING
	KW_INT
	KW_DECIMAL
	KW_BOOL
	KW_DATE
	KW_DATETIME
	KW_JSON

	// punctuation
	LBRACE       // {
	RBRACE       // }
	LPAREN       // (
	RPAREN       // )
	LBRACKET     // [
	RBRACKET     // ]
	COMMA        // ,
	COLON        // :
	SEMI         // ;
	EQUAL        // =
	QUESTION     // ?
	DOT          // .
	ELLIPSIS     // ...
	DOUBLE_COLON // ::
	AT           // @

	// operators
	PLUS
	MINUS
	STAR
	SLASH
	PERCENT
	EQ  // ==
	NEQ // !=
	LT
	LTE
	GT
	GTE
	AND // &&
	OR  // ||
	NOT // !
)

var keywords = map[string]TokenKind{
	"import":     IMPORT,
	"capability": CAPABILITY,
	"from":       FROM,
	"enum":       ENUM,
	"model":      MODEL,
	"view":       VIEW,
	"partial":    PARTIAL,
	"on":         ON,
	"implies":    IMPLIES,
	"true":       TRUE,
	"false":      FALSE,
	"null":       NULL,
	"string":     KW_STRING,
	"int":        KW_INT,
	"decimal":    KW_DECIMAL,
	"bool":       KW_BOOL,
	"date":       KW_DATE,
	"datetime":   KW_DATETIME,
	"json":       KW_JSON,
}

func LookupIdent(s string) TokenKind {
	if kind, ok := keywords[s]; ok {
		return kind
	}
	return IDENT
}

type Token struct {
	Kind TokenKind
	Text string // literal source text (identifier name, unescaped-free string content, etc.)
	Pos  Position
}

func (k TokenKind) String() string {
	if s, ok := tokenNames[k]; ok {
		return s
	}
	return "UNKNOWN"
}

var tokenNames = map[TokenKind]string{
	EOF:          "EOF",
	ILLEGAL:      "ILLEGAL",
	IDENT:        "IDENT",
	INT_LIT:      "INT_LIT",
	DECIMAL_LIT:  "DECIMAL_LIT",
	STRING_LIT:   "STRING_LIT",
	IMPORT:       "import",
	CAPABILITY:   "capability",
	FROM:         "from",
	ENUM:         "enum",
	MODEL:        "model",
	VIEW:         "view",
	PARTIAL:      "partial",
	ON:           "on",
	IMPLIES:      "implies",
	TRUE:         "true",
	FALSE:        "false",
	NULL:         "null",
	KW_STRING:    "string",
	KW_INT:       "int",
	KW_DECIMAL:   "decimal",
	KW_BOOL:      "bool",
	KW_DATE:      "date",
	KW_DATETIME:  "datetime",
	KW_JSON:      "json",
	LBRACE:       "{",
	RBRACE:       "}",
	LPAREN:       "(",
	RPAREN:       ")",
	LBRACKET:     "[",
	RBRACKET:     "]",
	COMMA:        ",",
	COLON:        ":",
	SEMI:         ";",
	EQUAL:        "=",
	QUESTION:     "?",
	DOT:          ".",
	ELLIPSIS:     "...",
	DOUBLE_COLON: "::",
	AT:           "@",
	PLUS:         "+",
	MINUS:        "-",
	STAR:         "*",
	SLASH:        "/",
	PERCENT:      "%",
	EQ:           "==",
	NEQ:          "!=",
	LT:           "<",
	LTE:          "<=",
	GT:           ">",
	GTE:          ">=",
	AND:          "&&",
	OR:           "||",
	NOT:          "!",
}
