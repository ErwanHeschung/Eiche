// Package core resolves imports into one flat namespace, then type-checks
// and expression-checks the merged program — the "internal/core" stage
// from .context/CLAUDE.md's architecture diagram, sitting between the
// parser and the emitters/rule compiler.
package core

import (
	"fmt"
	"path/filepath"

	"eiche/internal/syntax"
)

// SourceLoader abstracts reading a .eiche file by path, so import
// resolution is testable without touching the filesystem.
type SourceLoader interface {
	Load(path string) (string, error)
}

// MapLoader is a SourceLoader backed by an in-memory map, keyed by the
// same path strings ResolveImports would otherwise pass to the OS.
type MapLoader map[string]string

func (m MapLoader) Load(path string) (string, error) {
	src, ok := m[path]
	if !ok {
		return "", fmt.Errorf("no such file: %s", path)
	}
	return src, nil
}

// Program is the flat, resolved set of declarations across an entry file
// and everything it transitively imports. The namespace is flat per the
// settled decision in .context/CLAUDE.md: every capability/enum/model/view
// name is visible from every file, regardless of which one declared it.
type Program struct {
	Capabilities map[string]*syntax.CapabilityDecl
	Enums        map[string]*syntax.EnumDecl
	Models       map[string]*syntax.ModelDecl
	Views        map[string]*syntax.ViewDecl

	// DeclFile maps a declaration back to the file it came from, so
	// later stages (type checker, expression checker) can attach a
	// correct File to their diagnostics even though the Program itself
	// is a flat merge across every imported file.
	DeclFile map[syntax.Decl]string
}

func newProgram() *Program {
	return &Program{
		Capabilities: map[string]*syntax.CapabilityDecl{},
		Enums:        map[string]*syntax.EnumDecl{},
		Models:       map[string]*syntax.ModelDecl{},
		Views:        map[string]*syntax.ViewDecl{},
		DeclFile:     map[syntax.Decl]string{},
	}
}

// ResolveImports parses entry and every file it transitively imports,
// merging all declarations into one flat Program. It always returns a
// non-nil *Program; check the returned diagnostics (parse errors, import
// cycles, missing files, duplicate names) to know whether it's usable.
func ResolveImports(loader SourceLoader, entry string) (*Program, []syntax.Diagnostic) {
	prog := newProgram()
	var diags []syntax.Diagnostic

	visiting := map[string]bool{} // on the current import stack: cycle detection
	visited := map[string]bool{}  // fully processed: import-once

	var visit func(path string, pos Position, fromFile string)
	visit = func(path string, pos Position, fromFile string) {
		if visited[path] {
			return
		}
		if visiting[path] {
			diags = append(diags, syntax.Diagnostic{
				File: fromFile, Pos: pos,
				Message: fmt.Sprintf("import cycle: %q is imported while it is still being resolved", path),
			})
			return
		}
		visiting[path] = true
		defer func() { visiting[path] = false; visited[path] = true }()

		src, err := loader.Load(path)
		if err != nil {
			diags = append(diags, syntax.Diagnostic{
				File: fromFile, Pos: pos,
				Message: fmt.Sprintf("cannot import %q: %v", path, err),
			})
			return
		}

		file, errs := syntax.ParseFile(path, src)
		diags = append(diags, errs...)

		dir := filepath.Dir(path)
		for _, imp := range file.Imports {
			visit(resolveImportPath(dir, imp.Path), imp.Pos, path)
		}

		for _, decl := range file.Decls {
			mergeDecl(prog, path, decl, &diags)
		}
	}

	visit(entry, syntax.Position{}, entry)
	return prog, diags
}

// Position is an alias kept local to core so callers of ResolveImports
// don't need to import syntax just to pass a zero position for the entry
// file's synthetic "import".
type Position = syntax.Position

func resolveImportPath(dir, importPath string) string {
	if filepath.IsAbs(importPath) {
		return filepath.Clean(importPath)
	}
	return filepath.Clean(filepath.Join(dir, importPath))
}

// typeNamePos returns the position at which name was already declared as
// an enum, model, or view — the three declaration kinds that share one
// type-reference namespace, since a field's TypeExpr or a view's Base can
// resolve to any of them by plain identifier. Capabilities are looked up
// separately (only ever referenced via "name::func(...)"), so they don't
// collide with this namespace.
func typeNamePos(prog *Program, name string) (syntax.Position, bool) {
	if d, ok := prog.Enums[name]; ok {
		return d.Pos, true
	}
	if d, ok := prog.Models[name]; ok {
		return d.Pos, true
	}
	if d, ok := prog.Views[name]; ok {
		return d.Pos, true
	}
	return syntax.Position{}, false
}

func mergeDecl(prog *Program, file string, decl syntax.Decl, diags *[]syntax.Diagnostic) {
	switch d := decl.(type) {
	case *syntax.CapabilityDecl:
		if existing, ok := prog.Capabilities[d.Name]; ok {
			*diags = append(*diags, dupDiag(file, d.Name, d.Pos, existing.Pos))
			return
		}
		prog.Capabilities[d.Name] = d

	case *syntax.EnumDecl:
		if pos, ok := typeNamePos(prog, d.Name); ok {
			*diags = append(*diags, dupDiag(file, d.Name, d.Pos, pos))
			return
		}
		prog.Enums[d.Name] = d

	case *syntax.ModelDecl:
		if pos, ok := typeNamePos(prog, d.Name); ok {
			*diags = append(*diags, dupDiag(file, d.Name, d.Pos, pos))
			return
		}
		prog.Models[d.Name] = d

	case *syntax.ViewDecl:
		if pos, ok := typeNamePos(prog, d.Name); ok {
			*diags = append(*diags, dupDiag(file, d.Name, d.Pos, pos))
			return
		}
		prog.Views[d.Name] = d
	}
	prog.DeclFile[decl] = file
}

func dupDiag(file, name string, pos, firstPos syntax.Position) syntax.Diagnostic {
	return syntax.Diagnostic{
		File: file, Pos: pos,
		Message: fmt.Sprintf("%q is already declared (first declared at line %d, column %d) — names are flat across all imported files", name, firstPos.Line, firstPos.Column),
	}
}
