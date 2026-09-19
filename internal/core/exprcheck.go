package core

import (
	"eiche/internal/manifest"
	"eiche/internal/syntax"
)

// enumMemberEntry records which enum a bare member name belongs to, or
// that the name is ambiguous because two enums share a member — per the
// flat-namespace decision, that's a real hazard worth a dedicated error
// rather than silently picking whichever enum happened to be visited first.
type enumMemberEntry struct {
	enum      string
	ambiguous bool
}

func buildEnumMemberIndex(prog *Program) map[string]enumMemberEntry {
	idx := map[string]enumMemberEntry{}
	for _, e := range prog.Enums {
		for _, member := range e.Members {
			if existing, ok := idx[member]; ok {
				existing.ambiguous = true
				idx[member] = existing
				continue
			}
			idx[member] = enumMemberEntry{enum: e.Name}
		}
	}
	return idx
}

// CheckExpressions type-checks every @rule body in the program: every
// identifier must resolve to a field of the enclosing model or an
// unambiguous enum member, every "::" call's receiver must be a declared
// capability, and — when a manifest for that capability is supplied — the
// called function and argument count must match its export signature.
//
// manifests is keyed by the capability's local alias (the name after
// "capability", not the "name@version" string after "from"): capability
// resolution/fetching is explicitly out of scope until M6 per the brief,
// so callers that have manifests on hand (tests, or eichec once local
// paths are wired up) pass them in; callers that don't can pass nil and
// still get identifier/receiver checking without arity checking.
//
// RuleDecl.On is filled in here when the source omitted "on <field>": it
// resolves to the first field referenced by Expr, per the frozen engine
// semantics in .context/CLAUDE.md. This mutates the parsed AST in place,
// the same way the grammar already documents "on" as checker-resolved.
func CheckExpressions(prog *Program, manifests map[string]*manifest.Manifest) []syntax.Diagnostic {
	var diags []syntax.Diagnostic
	enumIndex := buildEnumMemberIndex(prog)

	for _, m := range prog.Models {
		file := prog.DeclFile[m]
		fields := map[string]bool{}
		for _, f := range m.Fields {
			fields[f.Name] = true
		}

		ctx := &exprCtx{
			prog:      prog,
			file:      file,
			fields:    fields,
			enumIndex: enumIndex,
			manifests: manifests,
			diags:     &diags,
		}

		for _, rule := range m.Rules {
			firstField := ctx.walk(rule.Expr)
			if rule.On == "" {
				if firstField == "" {
					diags = append(diags, err(file, rule.Pos,
						"rule has no explicit 'on' field, and none could be inferred: %q references no field of model %s",
						rule.Message, m.Name))
					continue
				}
				rule.On = firstField
			} else if !fields[rule.On] {
				diags = append(diags, err(file, rule.Pos,
					"rule's 'on' field %q is not a field of model %s", rule.On, m.Name))
			}
		}
	}

	return diags
}

type exprCtx struct {
	prog      *Program
	file      string
	fields    map[string]bool
	enumIndex map[string]enumMemberEntry
	manifests map[string]*manifest.Manifest
	diags     *[]syntax.Diagnostic
}

func (c *exprCtx) errorf(pos syntax.Position, format string, args ...any) {
	*c.diags = append(*c.diags, err(c.file, pos, format, args...))
}

// walk checks e and every subexpression, returning the name of the first
// model field referenced (in source order), or "" if none was — used to
// infer a rule's "on" field when the source didn't specify one.
func (c *exprCtx) walk(e syntax.Expr) string {
	switch n := e.(type) {
	case *syntax.IdentExpr:
		if c.fields[n.Name] {
			return n.Name
		}
		if entry, ok := c.enumIndex[n.Name]; ok {
			if entry.ambiguous {
				c.errorf(n.Pos, "%q is ambiguous: more than one enum declares a member with this name", n.Name)
			}
			return ""
		}
		c.errorf(n.Pos, "undefined identifier %q: not a field of this model, and not a known enum value", n.Name)
		return ""

	case *syntax.IntLitExpr, *syntax.DecimalLitExpr, *syntax.StringLitExpr, *syntax.BoolLitExpr, *syntax.NullLitExpr:
		return ""

	case *syntax.UnaryExpr:
		return c.walk(n.X)

	case *syntax.BinaryExpr:
		first := c.walk(n.X)
		second := c.walk(n.Y)
		if first != "" {
			return first
		}
		return second

	case *syntax.SelectorExpr:
		// M0 doesn't check n.Field against the shape of whatever n.X
		// resolves to (that needs structural typing of nested json/model
		// shapes, not built yet) — only that the base expression itself
		// resolves.
		return c.walk(n.X)

	case *syntax.CapabilityCallExpr:
		return c.walkCapabilityCall(n)

	default:
		return ""
	}
}

func (c *exprCtx) walkCapabilityCall(call *syntax.CapabilityCallExpr) string {
	recv, ok := call.X.(*syntax.IdentExpr)
	if !ok {
		c.errorf(call.Pos, "capability call receiver must be a plain capability name, not an expression")
	} else if _, declared := c.prog.Capabilities[recv.Name]; !declared {
		c.errorf(recv.Pos, "undefined capability %q (missing a \"capability %s from ...\" import?)", recv.Name, recv.Name)
	} else if m, ok := c.manifests[recv.Name]; ok {
		c.checkCallAgainstManifest(call, recv.Name, m)
	}

	var first string
	for _, arg := range call.Args {
		if f := c.walk(arg); first == "" {
			first = f
		}
	}
	return first
}

func (c *exprCtx) checkCallAgainstManifest(call *syntax.CapabilityCallExpr, capName string, m *manifest.Manifest) {
	export, ok := m.Exports[call.Func]
	if !ok {
		c.errorf(call.Pos, "capability %q (%s) has no function %q", capName, m.Name, call.Func)
		return
	}
	if len(export.Args) != len(call.Args) {
		c.errorf(call.Pos, "capability %q::%s expects %d argument(s), got %d",
			capName, call.Func, len(export.Args), len(call.Args))
	}
}
