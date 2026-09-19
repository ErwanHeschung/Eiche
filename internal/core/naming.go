package core

import "eiche/internal/syntax"

// javaReserved is every word a generated identifier must not collide with:
// the 51 reserved keywords plus the contextual keywords that matter for
// the specific constructs the Java emitter generates (records, sealed
// interfaces later). Contextual keywords are technically legal as plain
// identifiers in general Java, but not safe as a record component name or
// type name, so they're rejected here too rather than gambling on context.
var javaReserved = wordSet(
	"abstract", "assert", "boolean", "break", "byte", "case", "catch", "char",
	"class", "const", "continue", "default", "do", "double", "else", "enum",
	"extends", "final", "finally", "float", "for", "goto", "if", "implements",
	"import", "instanceof", "int", "interface", "long", "native", "new",
	"package", "private", "protected", "public", "return", "short", "static",
	"strictfp", "super", "switch", "synchronized", "this", "throw", "throws",
	"transient", "try", "void", "volatile", "while",
	"true", "false", "null",
	// contextual, but unsafe for record components / generated type names
	"var", "record", "yield", "sealed", "permits", "non-sealed",
)

// tsReserved is TypeScript/JavaScript's reserved words plus a short list
// of interface-breaking global names ("Object", "Array", ...) that would
// shadow a built-in if used as a generated interface name.
var tsReserved = wordSet(
	"break", "case", "catch", "class", "const", "continue", "debugger",
	"default", "delete", "do", "else", "enum", "export", "extends", "false",
	"finally", "for", "function", "if", "import", "in", "instanceof", "new",
	"null", "return", "super", "switch", "this", "throw", "true", "try",
	"typeof", "var", "void", "while", "with", "as", "implements", "interface",
	"let", "package", "private", "protected", "public", "static", "yield",
	"any", "boolean", "declare", "get", "module", "require", "number",
	"set", "string", "symbol", "type", "from", "of",
)

func wordSet(words ...string) map[string]bool {
	set := make(map[string]bool, len(words))
	for _, w := range words {
		set[w] = true
	}
	return set
}

// CheckNaming rejects any declared name — model, enum, enum member, view,
// or field (including nested json-shape fields) — that would collide with
// a Java or TypeScript reserved word. Per the M1 brief: "Reject reserved
// words at compile time with a clear message; never silently rename."
// Silently renaming would break the frozen error-path contract, since
// {path, code, params} paths are built from the DSL's own field names.
func CheckNaming(prog *Program) []syntax.Diagnostic {
	var diags []syntax.Diagnostic

	checkName := func(file string, pos syntax.Position, kind, name string) {
		if javaReserved[name] {
			diags = append(diags, err(file, pos, "%s %q is a reserved word in Java and can't be used as a generated identifier", kind, name))
		}
		if tsReserved[name] {
			diags = append(diags, err(file, pos, "%s %q is a reserved word in TypeScript and can't be used as a generated identifier", kind, name))
		}
	}

	for _, e := range prog.Enums {
		file := prog.DeclFile[e]
		checkName(file, e.Pos, "enum", e.Name)
		for _, m := range e.Members {
			checkName(file, e.Pos, "enum member", m)
		}
	}

	for _, m := range prog.Models {
		file := prog.DeclFile[m]
		checkName(file, m.Pos, "model", m.Name)
		checkFieldNames(file, m.Fields, checkName)
	}

	for _, v := range prog.Views {
		file := prog.DeclFile[v]
		checkName(file, v.Pos, "view", v.Name)
	}

	return diags
}

func checkFieldNames(file string, fields []*syntax.FieldDecl, checkName func(file string, pos syntax.Position, kind, name string)) {
	for _, f := range fields {
		checkName(file, f.Pos, "field", f.Name)
		if f.Type.JSONShape != nil {
			checkFieldNames(file, f.Type.JSONShape.Fields, checkName)
		}
	}
}
