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

// Compile lowers one checked model's field constraints and @rule list
// into a bytecode.Program. Callers must have already run
// core.CheckTypes, core.CheckExpressions, and core.CheckNaming on the
// enclosing Program and confirmed they're diagnostic-free — Compile does
// not re-validate field references or capability calls.
//
// Field constraints (required-presence for non-optional fields, plus
// @minLength/@maxLength/@min/@max/@range/@minItems/@maxItems) compile
// first, in field declaration order, ahead of @rule checks — required so
// the brief's ordering rule ("field constraints run first; any @rule
// referencing a field that already has an error is skipped") has
// something to gate against; see bytecode.Check.IsFieldConstraint.
//
// Scope cuts for this version of the compiler, made explicit rather than
// silently handled:
//   - @format, @after, @before, @each (element-level array constraints),
//     and any other annotation aren't compiled into checks — they're
//     silently accepted as no-ops. @format/@after/@before need a
//     format-string registry and a "now" validation-context parameter
//     that don't exist yet; @each needs either a VM loop construct or
//     per-element unrolling, and the array-element story more generally
//     (OpGetProp on array members) isn't designed yet either.
//   - Annotation arguments must be integer literals (@min(20000),
//     @range(10, 48)) — matches every example in the brief's own DSL.
//     A decimal-literal argument (@min(20000.50)) is rejected.
//   - SelectorExpr (nested "." access, e.g. into a json-typed field) is
//     rejected with a compile error, for the same reason as above: it
//     isn't exercised by any rule in the brief's own examples, and doing
//     it properly needs a coherent "dynamic value" design in the VM.
func Compile(m *core.IRModel) (*bytecode.Program, error) {
	c := &compiler{fieldIdx: map[string]int{}}
	for _, f := range m.Fields {
		c.registerField(f.Name, f.Type.Scalar, f.Type.IsArray)
	}

	var checks []bytecode.Check
	for _, f := range m.Fields {
		fieldChecks, err := c.compileFieldConstraints(f)
		if err != nil {
			return nil, fmt.Errorf("model %s: %w", m.Name, err)
		}
		checks = append(checks, fieldChecks...)
	}

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
		ABI:          ProgramABI,
		Fields:       c.fields,
		FieldTypes:   c.fieldTypes,
		FieldIsArray: c.fieldIsArray,
		Constants:    c.constants,
		Calls:        c.calls,
		Checks:       checks,
	}, nil
}

type compiler struct {
	fields       []string
	fieldTypes   []string
	fieldIsArray []bool
	fieldIdx     map[string]int
	constants    []bytecode.Value
	calls        []bytecode.CapabilityCall
}

func (c *compiler) registerField(name, scalar string, isArray bool) int {
	if idx, ok := c.fieldIdx[name]; ok {
		return idx
	}
	idx := len(c.fields)
	c.fields = append(c.fields, name)
	c.fieldTypes = append(c.fieldTypes, scalar)
	c.fieldIsArray = append(c.fieldIsArray, isArray)
	c.fieldIdx[name] = idx
	return idx
}

// compileFieldConstraints synthesizes this field's required-presence
// check (if it isn't declared optional) and every built-in annotation it
// carries. Unrecognized annotations (@format, @after, ...) are silently
// skipped — see Compile's doc comment.
func (c *compiler) compileFieldConstraints(f core.IRField) ([]bytecode.Check, error) {
	idx := c.fieldIdx[f.Name]
	var checks []bytecode.Check

	if !f.Optional {
		checks = append(checks, bytecode.Check{
			Code:              []bytecode.Instr{{Op: bytecode.OpFieldPresent, Operand: int32(idx)}},
			On:                f.Name,
			Message:           "required",
			IsFieldConstraint: true,
		})
	}

	for _, ann := range f.Annotations {
		check, err := c.compileAnnotation(f.Name, idx, ann)
		if err != nil {
			return nil, err
		}
		if check != nil {
			checks = append(checks, *check)
		}
	}

	return checks, nil
}

func (c *compiler) compileAnnotation(fieldName string, idx int, ann core.IRAnnotation) (*bytecode.Check, error) {
	switch ann.Name {
	case "minLength", "minItems":
		return c.compileLengthBound(fieldName, idx, ann, bytecode.OpGte, "min")
	case "maxLength", "maxItems":
		return c.compileLengthBound(fieldName, idx, ann, bytecode.OpLte, "max")
	case "min":
		return c.compileNumericBound(fieldName, idx, ann, bytecode.OpGte, "min")
	case "max":
		return c.compileNumericBound(fieldName, idx, ann, bytecode.OpLte, "max")
	case "range":
		return c.compileRange(fieldName, idx, ann)
	default:
		// Not a built-in this compiler gives runtime semantics to yet —
		// left as a no-op rather than an error, since annotations also
		// serve as codegen/documentation hints independent of validation.
		return nil, nil
	}
}

func (c *compiler) compileLengthBound(fieldName string, idx int, ann core.IRAnnotation, op bytecode.Op, paramKey string) (*bytecode.Check, error) {
	n, err := intArg(ann.Args, 0)
	if err != nil {
		return nil, fmt.Errorf("field %q: @%s: %w", fieldName, ann.Name, err)
	}
	constIdx := c.constIndex(bytecode.Value{Kind: bytecode.KindInt, Int: n})
	return &bytecode.Check{
		Code: []bytecode.Instr{
			{Op: bytecode.OpFieldLen, Operand: int32(idx)},
			{Op: bytecode.OpPushConst, Operand: constIdx},
			{Op: op},
		},
		Fields:            []int{idx},
		On:                fieldName,
		Message:           ann.Name,
		IsFieldConstraint: true,
		Params:            map[string]string{paramKey: strconv.FormatInt(n, 10)},
	}, nil
}

func (c *compiler) compileNumericBound(fieldName string, idx int, ann core.IRAnnotation, op bytecode.Op, paramKey string) (*bytecode.Check, error) {
	n, err := intArg(ann.Args, 0)
	if err != nil {
		return nil, fmt.Errorf("field %q: @%s: %w", fieldName, ann.Name, err)
	}
	constIdx := c.constIndex(bytecode.Value{Kind: bytecode.KindInt, Int: n})
	return &bytecode.Check{
		Code: []bytecode.Instr{
			{Op: bytecode.OpPushField, Operand: int32(idx)},
			{Op: bytecode.OpPushConst, Operand: constIdx},
			{Op: op},
		},
		Fields:            []int{idx},
		On:                fieldName,
		Message:           ann.Name,
		IsFieldConstraint: true,
		Params:            map[string]string{paramKey: strconv.FormatInt(n, 10)},
	}, nil
}

func (c *compiler) compileRange(fieldName string, idx int, ann core.IRAnnotation) (*bytecode.Check, error) {
	if len(ann.Args) != 2 {
		return nil, fmt.Errorf("field %q: @range needs exactly 2 arguments, got %d", fieldName, len(ann.Args))
	}
	lo, err := intArg(ann.Args, 0)
	if err != nil {
		return nil, fmt.Errorf("field %q: @range: %w", fieldName, err)
	}
	hi, err := intArg(ann.Args, 1)
	if err != nil {
		return nil, fmt.Errorf("field %q: @range: %w", fieldName, err)
	}
	loIdx := c.constIndex(bytecode.Value{Kind: bytecode.KindInt, Int: lo})
	hiIdx := c.constIndex(bytecode.Value{Kind: bytecode.KindInt, Int: hi})
	return &bytecode.Check{
		Code: []bytecode.Instr{
			{Op: bytecode.OpPushField, Operand: int32(idx)},
			{Op: bytecode.OpPushConst, Operand: loIdx},
			{Op: bytecode.OpGte},
			{Op: bytecode.OpPushField, Operand: int32(idx)},
			{Op: bytecode.OpPushConst, Operand: hiIdx},
			{Op: bytecode.OpLte},
			{Op: bytecode.OpAnd},
		},
		Fields:            []int{idx},
		On:                fieldName,
		Message:           "range",
		IsFieldConstraint: true,
		Params:            map[string]string{"min": strconv.FormatInt(lo, 10), "max": strconv.FormatInt(hi, 10)},
	}, nil
}

func intArg(args []core.IRExpr, i int) (int64, error) {
	if i >= len(args) {
		return 0, fmt.Errorf("missing argument %d", i)
	}
	a := args[i]
	if a.Kind != core.ExprIntLit {
		return 0, fmt.Errorf("argument %d must be an integer literal, got %s", i, a.Kind)
	}
	return strconv.ParseInt(a.Value, 10, 64)
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
