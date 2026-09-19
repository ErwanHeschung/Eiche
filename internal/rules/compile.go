// Package rules compiles a checked model's @rule expressions into the
// bytecode format defined in internal/rules/bytecode. This package (not
// bytecode itself) depends on internal/core, since it reads core.IRModel
// — it is native-Go only, run by eichec, and never imported by wasm/core.
package rules

import (
	"fmt"
	"sort"
	"strconv"

	"eiche/internal/core"
	"eiche/internal/rules/bytecode"
)

// ProgramABI is the bytecode ABI version this compiler emits.
const ProgramABI = 1

// Compile lowers one checked model's @rule list into a bytecode.Program.
// Callers must have already run core.CheckTypes, core.CheckExpressions,
// and core.CheckNaming on the enclosing Program and confirmed they're
// diagnostic-free — Compile does not re-validate field references or
// capability calls.
//
// Scope cuts for this first version of the compiler, made explicit rather
// than silently handled:
//   - Field-level constraint annotations (@minLength, @min, @range, ...)
//     are not compiled into checks yet — only explicit @rule blocks are.
//     Both are meant to go through the same engine per the brief ("no
//     Bean Validation annotations... everything goes through the WASM
//     engine"), so this is a real gap, not a permanent design choice —
//     it needs a registry of built-in annotation semantics, deliberately
//     deferred to keep this change reviewable.
//   - SelectorExpr (nested "." access, e.g. into a json-typed field) is
//     rejected with a compile error. It isn't exercised by any rule in
//     the brief's own examples, and doing it properly needs a coherent
//     "dynamic value" design in the VM that hasn't been built.
func Compile(m *core.IRModel) (*bytecode.Program, error) {
	c := &compiler{fieldIdx: map[string]int{}}
	for _, f := range m.Fields {
		c.registerField(f.Name, f.Type.Scalar)
	}

	checks := make([]bytecode.Check, 0, len(m.Rules))
	for _, rule := range m.Rules {
		code, err := c.compileExpr(rule.Expr)
		if err != nil {
			return nil, fmt.Errorf("model %s, rule %q: %w", m.Name, rule.Message, err)
		}

		refs := &fieldRefs{requireNonNull: map[int]bool{}}
		c.markFieldUsage(rule.Expr, false, refs)
		fieldIdxs := make([]int, 0, len(refs.requireNonNull))
		for idx := range refs.requireNonNull {
			fieldIdxs = append(fieldIdxs, idx)
		}
		sort.Ints(fieldIdxs)

		checks = append(checks, bytecode.Check{
			Code:    code,
			Fields:  fieldIdxs,
			On:      rule.On,
			Message: rule.Message,
		})
	}

	return &bytecode.Program{
		ABI:        ProgramABI,
		Fields:     c.fields,
		FieldTypes: c.fieldTypes,
		Constants:  c.constants,
		Calls:      c.calls,
		Checks:     checks,
	}, nil
}

type compiler struct {
	fields     []string
	fieldTypes []string
	fieldIdx   map[string]int
	constants  []bytecode.Value
	calls      []bytecode.CapabilityCall
}

func (c *compiler) registerField(name, scalar string) int {
	if idx, ok := c.fieldIdx[name]; ok {
		return idx
	}
	idx := len(c.fields)
	c.fields = append(c.fields, name)
	c.fieldTypes = append(c.fieldTypes, scalar)
	c.fieldIdx[name] = idx
	return idx
}

func (c *compiler) constIndex(v bytecode.Value) int32 {
	for i, existing := range c.constants {
		if existing == v {
			return int32(i)
		}
	}
	c.constants = append(c.constants, v)
	return int32(len(c.constants) - 1)
}

func (c *compiler) callIndex(call bytecode.CapabilityCall) int32 {
	for i, existing := range c.calls {
		if existing == call {
			return int32(i)
		}
	}
	c.calls = append(c.calls, call)
	return int32(len(c.calls) - 1)
}

var binaryOps = map[string]bytecode.Op{
	"==": bytecode.OpEq, "!=": bytecode.OpNeq,
	"<": bytecode.OpLt, "<=": bytecode.OpLte,
	">": bytecode.OpGt, ">=": bytecode.OpGte,
	"+": bytecode.OpAdd, "-": bytecode.OpSub,
	"*": bytecode.OpMul, "/": bytecode.OpDiv, "%": bytecode.OpMod,
	"&&": bytecode.OpAnd, "||": bytecode.OpOr,
}

// compileExpr emits e's bytecode. It doesn't track field usage — see
// markFieldUsage, a separate pass, for that; keeping the two concerns
// apart means the null-check exemption (below) can't accidentally skew
// what code gets emitted.
func (c *compiler) compileExpr(e core.IRExpr) ([]bytecode.Instr, error) {
	switch e.Kind {
	case core.ExprIdent:
		if idx, ok := c.fieldIdx[e.Name]; ok {
			return []bytecode.Instr{{Op: bytecode.OpPushField, Operand: int32(idx)}}, nil
		}
		// Not a field: core.CheckExpressions already guarantees every
		// non-field bare identifier here is an unambiguous enum member
		// (Compile's precondition is that checking already passed), so
		// it compiles to a constant, wire-encoded as its member name —
		// matching how the codegen emitters represent enums.
		idx := c.constIndex(bytecode.Value{Kind: bytecode.KindString, Str: e.Name})
		return []bytecode.Instr{{Op: bytecode.OpPushConst, Operand: idx}}, nil

	case core.ExprIntLit:
		v, err := strconv.ParseInt(e.Value, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("bad int literal %q: %w", e.Value, err)
		}
		idx := c.constIndex(bytecode.Value{Kind: bytecode.KindInt, Int: v})
		return []bytecode.Instr{{Op: bytecode.OpPushConst, Operand: idx}}, nil

	case core.ExprDecimalLit:
		idx := c.constIndex(bytecode.Value{Kind: bytecode.KindDecimal, Str: e.Value})
		return []bytecode.Instr{{Op: bytecode.OpPushConst, Operand: idx}}, nil

	case core.ExprStringLit:
		idx := c.constIndex(bytecode.Value{Kind: bytecode.KindString, Str: e.Value})
		return []bytecode.Instr{{Op: bytecode.OpPushConst, Operand: idx}}, nil

	case core.ExprBoolLit:
		idx := c.constIndex(bytecode.Value{Kind: bytecode.KindBool, Bool: e.Value == "true"})
		return []bytecode.Instr{{Op: bytecode.OpPushConst, Operand: idx}}, nil

	case core.ExprNullLit:
		idx := c.constIndex(bytecode.Value{Kind: bytecode.KindNull})
		return []bytecode.Instr{{Op: bytecode.OpPushConst, Operand: idx}}, nil

	case core.ExprUnary:
		x, err := c.compileExpr(*e.X)
		if err != nil {
			return nil, err
		}
		var op bytecode.Op
		switch e.Op {
		case "!":
			op = bytecode.OpNot
		case "-":
			op = bytecode.OpNeg
		default:
			return nil, fmt.Errorf("unsupported unary operator %q", e.Op)
		}
		return append(x, bytecode.Instr{Op: op}), nil

	case core.ExprBinary:
		if e.Op == "implies" {
			// A implies B == !A || B. No dedicated VM opcode: there are
			// no side effects to worry about, so eager evaluation of
			// both operands is exactly as correct as short-circuiting.
			x, err := c.compileExpr(*e.X)
			if err != nil {
				return nil, err
			}
			y, err := c.compileExpr(*e.Y)
			if err != nil {
				return nil, err
			}
			code := append(x, bytecode.Instr{Op: bytecode.OpNot})
			code = append(code, y...)
			code = append(code, bytecode.Instr{Op: bytecode.OpOr})
			return code, nil
		}

		op, ok := binaryOps[e.Op]
		if !ok {
			return nil, fmt.Errorf("unsupported binary operator %q", e.Op)
		}
		x, err := c.compileExpr(*e.X)
		if err != nil {
			return nil, err
		}
		y, err := c.compileExpr(*e.Y)
		if err != nil {
			return nil, err
		}
		code := append(x, y...)
		return append(code, bytecode.Instr{Op: op}), nil

	case core.ExprSelector:
		return nil, fmt.Errorf("nested field access (%q.%s) is not yet supported by the rule compiler", exprHint(e.X), e.Name)

	case core.ExprCapabilityCall:
		if e.X == nil || e.X.Kind != core.ExprIdent {
			return nil, fmt.Errorf("capability call receiver must be a plain capability name")
		}
		var code []bytecode.Instr
		for _, arg := range e.Args {
			argCode, err := c.compileExpr(arg)
			if err != nil {
				return nil, err
			}
			code = append(code, argCode...)
		}
		callIdx := c.callIndex(bytecode.CapabilityCall{
			Capability: e.X.Name,
			Func:       e.Name,
			ArgCount:   len(e.Args),
		})
		return append(code, bytecode.Instr{Op: bytecode.OpCallCapability, Operand: callIdx}), nil

	default:
		return nil, fmt.Errorf("unhandled expression kind %q", e.Kind)
	}
}

func exprHint(e *core.IRExpr) string {
	if e == nil {
		return "?"
	}
	if e.Name != "" {
		return e.Name
	}
	return string(e.Kind)
}

// fieldRefs accumulates, for one rule, which fields (by index) must be
// non-null for the check to run.
type fieldRefs struct {
	requireNonNull map[int]bool
}

// markFieldUsage walks e to decide which field references require a
// non-null value. A field referenced as the direct operand of "== null"
// or "!= null" is exempt: that comparison IS the field's null-check, so
// treating "the field is null" as a reason to skip the whole rule would
// make exactly the rules meant to catch that case never fire — e.g.
// "contractType == CDD implies endDate != null" must still run (and
// fail) when endDate is null, which a blanket "skip on any null
// reference" reading of the brief's null-safety rule would prevent. Every
// other reference (arithmetic, capability call args, comparisons against
// a non-null value) requires its field to be non-null to evaluate
// meaningfully, matching the brief's stated intent for that rule.
func (c *compiler) markFieldUsage(e core.IRExpr, nullCheckedHere bool, refs *fieldRefs) {
	switch e.Kind {
	case core.ExprIdent:
		idx, ok := c.fieldIdx[e.Name]
		if !ok {
			return // an enum constant, not a field reference — nothing to require non-null
		}
		if !nullCheckedHere {
			refs.requireNonNull[idx] = true
		}

	case core.ExprBinary:
		if e.Op == "==" || e.Op == "!=" {
			xIsNull := e.X != nil && e.X.Kind == core.ExprNullLit
			yIsNull := e.Y != nil && e.Y.Kind == core.ExprNullLit
			if e.X != nil {
				c.markFieldUsage(*e.X, yIsNull, refs)
			}
			if e.Y != nil {
				c.markFieldUsage(*e.Y, xIsNull, refs)
			}
			return
		}
		if e.X != nil {
			c.markFieldUsage(*e.X, false, refs)
		}
		if e.Y != nil {
			c.markFieldUsage(*e.Y, false, refs)
		}

	case core.ExprUnary, core.ExprSelector:
		if e.X != nil {
			c.markFieldUsage(*e.X, false, refs)
		}

	case core.ExprCapabilityCall:
		for _, arg := range e.Args {
			c.markFieldUsage(arg, false, refs)
		}
	}
}
