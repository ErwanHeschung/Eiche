package core

import (
	"fmt"

	"eiche/internal/syntax"
)

var builtinScalars = map[string]bool{
	"string": true, "int": true, "decimal": true, "bool": true,
	"date": true, "datetime": true, "json": true,
}

// typeKind is what a non-builtin TypeExpr.Scalar identifier resolved to.
type typeKind int

const (
	kindUnknown typeKind = iota
	kindBuiltin
	kindEnum
	kindModel
	kindView
)

func resolveTypeName(prog *Program, name string) typeKind {
	if builtinScalars[name] {
		return kindBuiltin
	}
	if _, ok := prog.Enums[name]; ok {
		return kindEnum
	}
	if _, ok := prog.Models[name]; ok {
		return kindModel
	}
	if _, ok := prog.Views[name]; ok {
		return kindView
	}
	return kindUnknown
}

// CheckTypes verifies every field type, view base, and view projection
// resolves to something real, and that default values match their field's
// type. It does not check @rule bodies — see CheckExpressions.
func CheckTypes(prog *Program) []syntax.Diagnostic {
	var diags []syntax.Diagnostic

	for _, m := range prog.Models {
		checkFields(prog, prog.DeclFile[m], m.Fields, &diags)
	}

	for _, v := range prog.Views {
		checkView(prog, prog.DeclFile[v], v, &diags)
	}

	return diags
}

func checkFields(prog *Program, file string, fields []*syntax.FieldDecl, diags *[]syntax.Diagnostic) {
	for _, f := range fields {
		checkFieldType(prog, file, f, diags)
	}
}

func checkFieldType(prog *Program, file string, f *syntax.FieldDecl, diags *[]syntax.Diagnostic) {
	t := f.Type
	kind := resolveTypeName(prog, t.Scalar)
	if kind == kindUnknown {
		*diags = append(*diags, err(file, t.Pos, "undefined type %q (field %q)", t.Scalar, f.Name))
		return
	}
	if kind == kindView {
		*diags = append(*diags, err(file, t.Pos, "field %q: %q is a view, not a type a field can hold — reference the underlying model instead", f.Name, t.Scalar))
	}

	if t.JSONShape != nil {
		checkFields(prog, file, t.JSONShape.Fields, diags)
	}

	if f.Default != nil {
		checkDefault(prog, file, f, kind, diags)
	}
}

func checkDefault(prog *Program, file string, f *syntax.FieldDecl, kind typeKind, diags *[]syntax.Diagnostic) {
	def := f.Default
	scalar := f.Type.Scalar

	switch kind {
	case kindEnum:
		id, ok := def.(*syntax.IdentExpr)
		if !ok {
			*diags = append(*diags, err(file, def.Position(), "field %q: default must be a member of enum %s", f.Name, scalar))
			return
		}
		enum := prog.Enums[scalar]
		if !enumHasMember(enum, id.Name) {
			*diags = append(*diags, err(file, def.Position(), "field %q: %q is not a member of enum %s", f.Name, id.Name, scalar))
		}

	case kindModel:
		*diags = append(*diags, err(file, def.Position(), "field %q: fields of model type %s cannot have a default value", f.Name, scalar))

	case kindBuiltin:
		if !literalMatchesScalar(def, scalar) {
			*diags = append(*diags, err(file, def.Position(), "field %q: default value's type doesn't match declared type %s", f.Name, scalar))
		}
	}
}

func enumHasMember(enum *syntax.EnumDecl, name string) bool {
	for _, m := range enum.Members {
		if m == name {
			return true
		}
	}
	return false
}

func literalMatchesScalar(e syntax.Expr, scalar string) bool {
	switch e.(type) {
	case *syntax.IntLitExpr:
		return scalar == "int"
	case *syntax.DecimalLitExpr:
		return scalar == "decimal" || scalar == "int"
	case *syntax.StringLitExpr:
		return scalar == "string" || scalar == "date" || scalar == "datetime"
	case *syntax.BoolLitExpr:
		return scalar == "bool"
	default:
		// Anything else (an identifier, a capability call, ...) can't be
		// statically checked against a builtin scalar in M0; let it through
		// rather than mis-flagging as an error — codegen-time evaluation
		// isn't built yet.
		return true
	}
}

func checkView(prog *Program, file string, v *syntax.ViewDecl, diags *[]syntax.Diagnostic) {
	base, ok := prog.Models[v.Base]
	if !ok {
		if _, isEnum := prog.Enums[v.Base]; isEnum {
			*diags = append(*diags, err(file, v.Pos, "view %q: %q is an enum, not a model", v.Name, v.Base))
			return
		}
		if _, isView := prog.Views[v.Base]; isView {
			*diags = append(*diags, err(file, v.Pos, "view %q: %q is a view, not a model — views derive from models only", v.Name, v.Base))
			return
		}
		*diags = append(*diags, err(file, v.Pos, "view %q: undefined model %q", v.Name, v.Base))
		return
	}

	for _, name := range v.Fields {
		if !modelHasField(base, name) {
			*diags = append(*diags, err(file, v.Pos, "view %q: %q has no field %q", v.Name, v.Base, name))
		}
	}
}

func modelHasField(m *syntax.ModelDecl, name string) bool {
	for _, f := range m.Fields {
		if f.Name == name {
			return true
		}
	}
	return false
}

func err(file string, pos syntax.Position, format string, args ...any) syntax.Diagnostic {
	return syntax.Diagnostic{File: file, Pos: pos, Message: fmt.Sprintf(format, args...)}
}
