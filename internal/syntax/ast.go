package syntax

// File is the root of a parsed .eiche source file.
type File struct {
	Imports []*ImportDecl
	Decls   []Decl
}

type ImportDecl struct {
	Path string
	Pos  Position
}

// Decl is any top-level declaration: capability, enum, model, or view.
type Decl interface {
	declNode()
	Position() Position
}

type CapabilityDecl struct {
	Name string
	From string
	Pos  Position
}

func (*CapabilityDecl) declNode()            {}
func (d *CapabilityDecl) Position() Position { return d.Pos }

type EnumDecl struct {
	Name    string
	Members []string
	Pos     Position
}

func (*EnumDecl) declNode()            {}
func (d *EnumDecl) Position() Position { return d.Pos }

type ModelDecl struct {
	Name               string
	Fields             []*FieldDecl
	Rules              []*RuleDecl
	AllowUnknownFields bool // set when the model body contains a bare "..."
	Pos                Position
}

func (*ModelDecl) declNode()            {}
func (d *ModelDecl) Position() Position { return d.Pos }

// ViewDecl covers both forms of view derivation:
//
//	view JobOfferPatch  = partial JobOffer      (Partial=true,  Fields=nil)
//	view JobOfferSummary = JobOffer { a, b, c }  (Partial=false, Fields=[a,b,c])
//	view JobOfferCreate  = JobOffer              (Partial=false, Fields=nil -> full projection)
type ViewDecl struct {
	Name    string
	Base    string
	Partial bool
	Fields  []string
	Pos     Position
}

func (*ViewDecl) declNode()            {}
func (d *ViewDecl) Position() Position { return d.Pos }

// FieldDecl is a model or json-shape member.
type FieldDecl struct {
	Name        string
	Optional    bool
	Type        *TypeExpr
	Annotations []*Annotation
	Default     Expr // nil if the field has no default; only valid when Optional
	Pos         Position
}

// TypeExpr is a field's type. Scalar holds either a builtin keyword
// ("string", "int", ...) or a reference to a user-defined enum/model/view.
type TypeExpr struct {
	Scalar          string
	IsArray         bool
	ElemAnnotations []*Annotation // constraints inside T[@constraint...]; only valid when IsArray
	JSONShape       *JSONShape    // non-nil only when Scalar == "json" and an inline shape was given
	Pos             Position
}

type JSONShape struct {
	Fields       []*FieldDecl
	AllowUnknown bool
	Pos          Position
}

type Annotation struct {
	Name string
	Args []Expr
	Pos  Position
}

// RuleDecl is a @rule(...) cross-field validation attached to a model.
// On is empty when the source omitted "on <field>"; the type checker
// resolves it to the first field referenced by Expr.
type RuleDecl struct {
	Expr    Expr
	Message string
	On      string
	Pos     Position
}

// Expr is a node in the @rule expression sub-language.
type Expr interface {
	exprNode()
	Position() Position
}

type IdentExpr struct {
	Name string
	Pos  Position
}

func (*IdentExpr) exprNode()            {}
func (e *IdentExpr) Position() Position { return e.Pos }

type IntLitExpr struct {
	Value string
	Pos   Position
}

func (*IntLitExpr) exprNode()            {}
func (e *IntLitExpr) Position() Position { return e.Pos }

type DecimalLitExpr struct {
	Value string
	Pos   Position
}

func (*DecimalLitExpr) exprNode()            {}
func (e *DecimalLitExpr) Position() Position { return e.Pos }

type StringLitExpr struct {
	Value string
	Pos   Position
}

func (*StringLitExpr) exprNode()            {}
func (e *StringLitExpr) Position() Position { return e.Pos }

type BoolLitExpr struct {
	Value bool
	Pos   Position
}

func (*BoolLitExpr) exprNode()            {}
func (e *BoolLitExpr) Position() Position { return e.Pos }

type NullLitExpr struct {
	Pos Position
}

func (*NullLitExpr) exprNode()            {}
func (e *NullLitExpr) Position() Position { return e.Pos }

// UnaryExpr covers prefix "!" and "-".
type UnaryExpr struct {
	Op  TokenKind
	X   Expr
	Pos Position
}

func (*UnaryExpr) exprNode()            {}
func (e *UnaryExpr) Position() Position { return e.Pos }

// BinaryExpr covers every infix operator including "implies", which is
// right-associative and lowest-precedence per the grammar.
type BinaryExpr struct {
	Op  TokenKind
	X   Expr
	Y   Expr
	Pos Position
}

func (*BinaryExpr) exprNode()            {}
func (e *BinaryExpr) Position() Position { return e.Pos }

// SelectorExpr is field access: X.Field
type SelectorExpr struct {
	X     Expr
	Field string
	Pos   Position
}

func (*SelectorExpr) exprNode()            {}
func (e *SelectorExpr) Position() Position { return e.Pos }

// CapabilityCallExpr is X::Func(Args...), e.g. phone::isValid(contactPhone, "FR").
// X is almost always an *IdentExpr naming the imported capability.
type CapabilityCallExpr struct {
	X    Expr
	Func string
	Args []Expr
	Pos  Position
}

func (*CapabilityCallExpr) exprNode()            {}
func (e *CapabilityCallExpr) Position() Position { return e.Pos }
