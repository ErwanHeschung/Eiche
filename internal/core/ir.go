package core

import (
	"slices"

	"eiche/internal/syntax"
)

// IRVersion is the IR schema version. Bump it whenever a change to these
// types would change the meaning of already-serialized IR, so consumers
// (eichec breaking, emitters, the rule compiler) can detect a skew instead
// of silently misreading an old artifact.
const IRVersion = 1

// IR is the stable, serializable artifact produced after import
// resolution and checking. Per .context/CLAUDE.md's architecture:
// "emitters never touch the AST" — the Java/TS emitters and the rule
// compiler consume only this, disjoint views of the same tree, so a
// grammar change can't ripple into codegen. It is also what
// `eichec breaking` will diff between two versions of a contract, so
// field order and naming here are part of that future contract.
//
// Slices are sorted by name (except where declaration order is
// semantically meaningful — enum members, field order, rule order,
// annotation args) so two builds of the same Program serialize
// byte-for-byte identically.
type IR struct {
	Version      int            `json:"version"`
	Capabilities []IRCapability `json:"capabilities,omitempty"`
	Enums        []IREnum       `json:"enums,omitempty"`
	Models       []IRModel      `json:"models,omitempty"`
	Views        []IRView       `json:"views,omitempty"`
}

type IRCapability struct {
	Name string `json:"name"`
	From string `json:"from"`
}

type IREnum struct {
	Name    string   `json:"name"`
	Members []string `json:"members"`
}

type IRModel struct {
	Name               string    `json:"name"`
	AllowUnknownFields bool      `json:"allowUnknownFields,omitempty"`
	Fields             []IRField `json:"fields"`
	Rules              []IRRule  `json:"rules,omitempty"`
}

type IRView struct {
	Name    string   `json:"name"`
	Base    string   `json:"base"`
	Partial bool     `json:"partial,omitempty"`
	Fields  []string `json:"fields,omitempty"`
}

type IRField struct {
	Name        string         `json:"name"`
	Optional    bool           `json:"optional,omitempty"`
	Type        IRType         `json:"type"`
	Annotations []IRAnnotation `json:"annotations,omitempty"`
	Default     *IRExpr        `json:"default,omitempty"`
}

type IRType struct {
	Scalar          string         `json:"scalar"`
	IsArray         bool           `json:"isArray,omitempty"`
	ElemConstraints []IRAnnotation `json:"elemConstraints,omitempty"`
	JSONShape       *IRJSONShape   `json:"jsonShape,omitempty"`
}

type IRJSONShape struct {
	Fields       []IRField `json:"fields"`
	AllowUnknown bool      `json:"allowUnknown,omitempty"`
}

type IRAnnotation struct {
	Name string   `json:"name"`
	Args []IRExpr `json:"args,omitempty"`
}

type IRRule struct {
	Expr    IRExpr `json:"expr"`
	Message string `json:"message"`
	On      string `json:"on"`
}

// IRExprKind discriminates IRExpr, Go having no native sum types. Encoding
// as one flat struct with an omitempty field set per kind keeps the JSON
// simple to read and diff, at the cost of most fields being optional.
type IRExprKind string

const (
	ExprIdent          IRExprKind = "ident"
	ExprIntLit         IRExprKind = "intLit"
	ExprDecimalLit     IRExprKind = "decimalLit"
	ExprStringLit      IRExprKind = "stringLit"
	ExprBoolLit        IRExprKind = "boolLit"
	ExprNullLit        IRExprKind = "nullLit"
	ExprUnary          IRExprKind = "unary"
	ExprBinary         IRExprKind = "binary"
	ExprSelector       IRExprKind = "selector"
	ExprCapabilityCall IRExprKind = "capabilityCall"
)

type IRExpr struct {
	Kind IRExprKind `json:"kind"`

	// Ident / selector field / capability function name.
	Name string `json:"name,omitempty"`
	// Literal text: int/decimal digits, string contents, "true"/"false".
	Value string `json:"value,omitempty"`
	// Operator token text, for Unary and Binary ("!", "-", "+", "==", "implies", ...).
	Op string `json:"op,omitempty"`

	X    *IRExpr  `json:"x,omitempty"`    // operand (unary), lhs (binary), base (selector/call)
	Y    *IRExpr  `json:"y,omitempty"`    // rhs (binary)
	Args []IRExpr `json:"args,omitempty"` // capability call arguments, in source order
}

// BuildIR lowers a resolved, checked Program into the stable IR. Callers
// should run CheckTypes and CheckExpressions first and lower only a
// diagnostic-free Program — BuildIR itself does not re-validate anything.
func BuildIR(prog *Program) *IR {
	ir := &IR{Version: IRVersion}

	for _, name := range sortedKeys(prog.Capabilities) {
		d := prog.Capabilities[name]
		ir.Capabilities = append(ir.Capabilities, IRCapability{Name: d.Name, From: d.From})
	}

	for _, name := range sortedKeys(prog.Enums) {
		d := prog.Enums[name]
		members := append([]string{}, d.Members...)
		ir.Enums = append(ir.Enums, IREnum{Name: d.Name, Members: members})
	}

	for _, name := range sortedKeys(prog.Models) {
		ir.Models = append(ir.Models, lowerModel(prog.Models[name]))
	}

	for _, name := range sortedKeys(prog.Views) {
		d := prog.Views[name]
		fields := append([]string{}, d.Fields...)
		ir.Views = append(ir.Views, IRView{Name: d.Name, Base: d.Base, Partial: d.Partial, Fields: fields})
	}

	return ir
}

func lowerModel(m *syntax.ModelDecl) IRModel {
	im := IRModel{Name: m.Name, AllowUnknownFields: m.AllowUnknownFields}
	for _, f := range m.Fields {
		im.Fields = append(im.Fields, lowerField(f))
	}
	for _, r := range m.Rules {
		im.Rules = append(im.Rules, IRRule{Expr: lowerExpr(r.Expr), Message: r.Message, On: r.On})
	}
	return im
}

func lowerField(f *syntax.FieldDecl) IRField {
	field := IRField{Name: f.Name, Optional: f.Optional, Type: lowerType(f.Type)}
	for _, a := range f.Annotations {
		field.Annotations = append(field.Annotations, lowerAnnotation(a))
	}
	if f.Default != nil {
		d := lowerExpr(f.Default)
		field.Default = &d
	}
	return field
}

func lowerType(t *syntax.TypeExpr) IRType {
	it := IRType{Scalar: t.Scalar, IsArray: t.IsArray}
	for _, a := range t.ElemAnnotations {
		it.ElemConstraints = append(it.ElemConstraints, lowerAnnotation(a))
	}
	if t.JSONShape != nil {
		shape := &IRJSONShape{AllowUnknown: t.JSONShape.AllowUnknown}
		for _, f := range t.JSONShape.Fields {
			shape.Fields = append(shape.Fields, lowerField(f))
		}
		it.JSONShape = shape
	}
	return it
}

func lowerAnnotation(a *syntax.Annotation) IRAnnotation {
	ann := IRAnnotation{Name: a.Name}
	for _, arg := range a.Args {
		ann.Args = append(ann.Args, lowerExpr(arg))
	}
	return ann
}

func lowerExpr(e syntax.Expr) IRExpr {
	switch n := e.(type) {
	case *syntax.IdentExpr:
		return IRExpr{Kind: ExprIdent, Name: n.Name}
	case *syntax.IntLitExpr:
		return IRExpr{Kind: ExprIntLit, Value: n.Value}
	case *syntax.DecimalLitExpr:
		return IRExpr{Kind: ExprDecimalLit, Value: n.Value}
	case *syntax.StringLitExpr:
		return IRExpr{Kind: ExprStringLit, Value: n.Value}
	case *syntax.BoolLitExpr:
		v := "false"
		if n.Value {
			v = "true"
		}
		return IRExpr{Kind: ExprBoolLit, Value: v}
	case *syntax.NullLitExpr:
		return IRExpr{Kind: ExprNullLit}
	case *syntax.UnaryExpr:
		x := lowerExpr(n.X)
		return IRExpr{Kind: ExprUnary, Op: n.Op.String(), X: &x}
	case *syntax.BinaryExpr:
		x := lowerExpr(n.X)
		y := lowerExpr(n.Y)
		return IRExpr{Kind: ExprBinary, Op: n.Op.String(), X: &x, Y: &y}
	case *syntax.SelectorExpr:
		x := lowerExpr(n.X)
		return IRExpr{Kind: ExprSelector, Name: n.Field, X: &x}
	case *syntax.CapabilityCallExpr:
		x := lowerExpr(n.X)
		ir := IRExpr{Kind: ExprCapabilityCall, Name: n.Func, X: &x}
		for _, a := range n.Args {
			ir.Args = append(ir.Args, lowerExpr(a))
		}
		return ir
	default:
		panic("core: lowerExpr: unhandled expression node")
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
