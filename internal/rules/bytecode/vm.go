package bytecode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/shopspring/decimal"
)

// ValidationError is the frozen error shape from .context/CLAUDE.md:
// {path, code, params}. Params is reserved for future interpolation data
// (e.g. a @min(20000) failure carrying {"min": "20000"}) — M2's compiled
// @rule checks don't populate it yet, since there's no per-annotation
// metadata to carry; see the scope note in internal/rules.
type ValidationError struct {
	Path   string
	Code   string
	Params map[string]string
}

// CapabilityInvoker calls one imported capability function. The real
// implementation (wasm/core) calls a linked WASM import; tests supply a
// fake. M2 only supports bool-returning capability functions.
type CapabilityInvoker interface {
	Call(capability, fn string, args []Value) (bool, error)
}

// ParseInput decodes a JSON object for Eval, preserving exact numeric
// text via json.Number rather than lossy float64 — required for decimal
// fields to compare and round correctly. Every caller (tests, and the
// future WASM ABI layer) should go through this rather than a bare
// json.Unmarshal into map[string]any.
func ParseInput(data []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("parse input: %w", err)
	}
	return m, nil
}

// Eval validates one input object against prog, returning every failed
// check as a ValidationError. A check whose Fields include a null (or
// absent) value in input is skipped — see internal/rules for exactly
// which field references count as "requiring non-null" here, since a
// field being compared directly against null is deliberately exempt.
func Eval(prog *Program, input map[string]any, invoker CapabilityInvoker) ([]ValidationError, error) {
	var errs []ValidationError

	for _, check := range prog.Checks {
		skip := false
		for _, fieldIdx := range check.Fields {
			name := prog.Fields[fieldIdx]
			if isAbsentOrNull(input, name) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}

		result, err := evalCheck(prog, check, input, invoker)
		if err != nil {
			return nil, fmt.Errorf("check %q: %w", check.Message, err)
		}
		if !result {
			errs = append(errs, ValidationError{Path: check.On, Code: check.Message})
		}
	}

	return errs, nil
}

func isAbsentOrNull(input map[string]any, name string) bool {
	v, ok := input[name]
	return !ok || v == nil
}

func evalCheck(prog *Program, check Check, input map[string]any, invoker CapabilityInvoker) (bool, error) {
	var stack []Value
	push := func(v Value) { stack = append(stack, v) }
	pop := func() (Value, error) {
		if len(stack) == 0 {
			return Value{}, fmt.Errorf("stack underflow")
		}
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		return v, nil
	}

	for _, instr := range check.Code {
		switch instr.Op {
		case OpPushConst:
			push(prog.Constants[instr.Operand])

		case OpPushField:
			name := prog.Fields[instr.Operand]
			declaredType := prog.FieldTypes[instr.Operand]
			v, err := fromJSONTyped(input[name], declaredType)
			if err != nil {
				return false, fmt.Errorf("field %q: %w", name, err)
			}
			push(v)

		case OpGetProp:
			// Nested/selector access isn't compiled yet — internal/rules
			// rejects SelectorExpr at compile time, so this should be
			// unreachable. Guarded rather than silently mis-evaluated.
			return false, fmt.Errorf("OpGetProp is not supported by this VM version")

		case OpNot:
			a, err := pop()
			if err != nil {
				return false, err
			}
			b, err := asBool(a)
			if err != nil {
				return false, err
			}
			push(Value{Kind: KindBool, Bool: !b})

		case OpNeg:
			a, err := pop()
			if err != nil {
				return false, err
			}
			switch a.Kind {
			case KindInt:
				push(Value{Kind: KindInt, Int: -a.Int})
			case KindDecimal:
				d, err := decimal.NewFromString(a.Str)
				if err != nil {
					return false, err
				}
				push(Value{Kind: KindDecimal, Str: d.Neg().String()})
			default:
				return false, fmt.Errorf("unary '-' needs a number, got %v", a.Kind)
			}

		case OpAnd, OpOr:
			b, err := pop()
			if err != nil {
				return false, err
			}
			a, err := pop()
			if err != nil {
				return false, err
			}
			ab, err := asBool(a)
			if err != nil {
				return false, err
			}
			bb, err := asBool(b)
			if err != nil {
				return false, err
			}
			if instr.Op == OpAnd {
				push(Value{Kind: KindBool, Bool: ab && bb})
			} else {
				push(Value{Kind: KindBool, Bool: ab || bb})
			}

		case OpEq, OpNeq:
			b, err := pop()
			if err != nil {
				return false, err
			}
			a, err := pop()
			if err != nil {
				return false, err
			}
			eq, err := valuesEqual(a, b)
			if err != nil {
				return false, err
			}
			if instr.Op == OpEq {
				push(Value{Kind: KindBool, Bool: eq})
			} else {
				push(Value{Kind: KindBool, Bool: !eq})
			}

		case OpLt, OpLte, OpGt, OpGte:
			b, err := pop()
			if err != nil {
				return false, err
			}
			a, err := pop()
			if err != nil {
				return false, err
			}
			cmp, err := compareValues(a, b)
			if err != nil {
				return false, err
			}
			var result bool
			switch instr.Op {
			case OpLt:
				result = cmp < 0
			case OpLte:
				result = cmp <= 0
			case OpGt:
				result = cmp > 0
			case OpGte:
				result = cmp >= 0
			}
			push(Value{Kind: KindBool, Bool: result})

		case OpAdd, OpSub, OpMul, OpDiv, OpMod:
			b, err := pop()
			if err != nil {
				return false, err
			}
			a, err := pop()
			if err != nil {
				return false, err
			}
			v, err := arith(instr.Op, a, b)
			if err != nil {
				return false, err
			}
			push(v)

		case OpCallCapability:
			call := prog.Calls[instr.Operand]
			args := make([]Value, call.ArgCount)
			for i := call.ArgCount - 1; i >= 0; i-- {
				v, err := pop()
				if err != nil {
					return false, err
				}
				args[i] = v
			}
			if invoker == nil {
				return false, fmt.Errorf("capability %q::%s called with no invoker configured", call.Capability, call.Func)
			}
			result, err := invoker.Call(call.Capability, call.Func, args)
			if err != nil {
				return false, fmt.Errorf("capability %q::%s: %w", call.Capability, call.Func, err)
			}
			push(Value{Kind: KindBool, Bool: result})

		default:
			return false, fmt.Errorf("unknown opcode %d", instr.Op)
		}
	}

	if len(stack) != 1 {
		return false, fmt.Errorf("check left %d values on the stack, want exactly 1", len(stack))
	}
	return asBool(stack[0])
}

func asBool(v Value) (bool, error) {
	if v.Kind != KindBool {
		return false, fmt.Errorf("expected a boolean, got %v", v.Kind)
	}
	return v.Bool, nil
}

// fromJSONTyped converts one decoded JSON value (from ParseInput, or a
// plain Go literal in a test) into a Value, per the field's declared IR
// scalar type. The declared type is what disambiguates a decimal field
// (wire-encoded as a JSON string, to preserve exact precision — see
// Program.FieldTypes) from an actual string field holding the same JSON
// shape.
func fromJSONTyped(raw any, declaredType string) (Value, error) {
	if raw == nil {
		return Value{Kind: KindNull}, nil
	}

	switch declaredType {
	case "int":
		n, err := toInt64(raw)
		if err != nil {
			return Value{}, err
		}
		return Value{Kind: KindInt, Int: n}, nil

	case "decimal":
		s, err := toDecimalText(raw)
		if err != nil {
			return Value{}, err
		}
		return Value{Kind: KindDecimal, Str: s}, nil

	case "bool":
		b, ok := raw.(bool)
		if !ok {
			return Value{}, fmt.Errorf("expected a bool, got %T", raw)
		}
		return Value{Kind: KindBool, Bool: b}, nil

	case "string", "date", "datetime":
		s, ok := raw.(string)
		if !ok {
			return Value{}, fmt.Errorf("expected a string, got %T", raw)
		}
		return Value{Kind: KindString, Str: s}, nil

	case "json":
		// Opaque on purpose — see KindJSON's doc comment. Any Go shape
		// (map, slice, string, number, bool) becomes the same marker;
		// only its presence/absence is usable in a rule in M2.
		return Value{Kind: KindJSON}, nil

	default:
		// Not a builtin: an enum reference, wire-encoded as its member
		// name.
		s, ok := raw.(string)
		if !ok {
			return Value{}, fmt.Errorf("expected an enum value (string), got %T", raw)
		}
		return Value{Kind: KindString, Str: s}, nil
	}
}

func toInt64(raw any) (int64, error) {
	switch v := raw.(type) {
	case json.Number:
		return v.Int64()
	case int:
		return int64(v), nil
	case int64:
		return v, nil
	case float64:
		return int64(v), nil
	default:
		return 0, fmt.Errorf("expected an integer, got %T", raw)
	}
}

func toDecimalText(raw any) (string, error) {
	switch v := raw.(type) {
	case string:
		// The realistic wire form: a JSON string carrying exact decimal
		// text, matching the TS emitter's decimal -> string mapping.
		return v, nil
	case json.Number:
		return v.String(), nil
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	case int:
		return strconv.Itoa(v), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	default:
		return "", fmt.Errorf("expected a decimal, got %T", raw)
	}
}

func valuesEqual(a, b Value) (bool, error) {
	if a.Kind == KindNull || b.Kind == KindNull {
		return a.Kind == b.Kind, nil
	}
	if isNumeric(a.Kind) && isNumeric(b.Kind) {
		cmp, err := compareValues(a, b)
		return err == nil && cmp == 0, err
	}
	if a.Kind != b.Kind {
		return false, nil
	}
	switch a.Kind {
	case KindBool:
		return a.Bool == b.Bool, nil
	case KindString:
		return a.Str == b.Str, nil
	}
	return false, fmt.Errorf("unsupported comparison between %v and %v", a.Kind, b.Kind)
}

func isNumeric(k ValueKind) bool { return k == KindInt || k == KindDecimal }

func compareValues(a, b Value) (int, error) {
	switch {
	case isNumeric(a.Kind) && isNumeric(b.Kind):
		da, err := toDecimal(a)
		if err != nil {
			return 0, err
		}
		db, err := toDecimal(b)
		if err != nil {
			return 0, err
		}
		return da.Cmp(db), nil
	case a.Kind == KindString && b.Kind == KindString:
		// Correct for ISO-8601 date/datetime strings of matching
		// precision, which sort lexicographically in chronological
		// order — no separate date type needed at the VM level.
		switch {
		case a.Str < b.Str:
			return -1, nil
		case a.Str > b.Str:
			return 1, nil
		default:
			return 0, nil
		}
	default:
		return 0, fmt.Errorf("cannot order-compare %v and %v", a.Kind, b.Kind)
	}
}

func toDecimal(v Value) (decimal.Decimal, error) {
	switch v.Kind {
	case KindInt:
		return decimal.NewFromInt(v.Int), nil
	case KindDecimal:
		return decimal.NewFromString(v.Str)
	default:
		return decimal.Decimal{}, fmt.Errorf("expected a number, got %v", v.Kind)
	}
}

func arith(op Op, a, b Value) (Value, error) {
	if a.Kind == KindInt && b.Kind == KindInt {
		switch op {
		case OpAdd:
			return Value{Kind: KindInt, Int: a.Int + b.Int}, nil
		case OpSub:
			return Value{Kind: KindInt, Int: a.Int - b.Int}, nil
		case OpMul:
			return Value{Kind: KindInt, Int: a.Int * b.Int}, nil
		case OpDiv:
			if b.Int == 0 {
				return Value{}, fmt.Errorf("division by zero")
			}
			return Value{Kind: KindInt, Int: a.Int / b.Int}, nil
		case OpMod:
			if b.Int == 0 {
				return Value{}, fmt.Errorf("modulo by zero")
			}
			return Value{Kind: KindInt, Int: a.Int % b.Int}, nil
		}
	}

	da, err := toDecimal(a)
	if err != nil {
		return Value{}, err
	}
	db, err := toDecimal(b)
	if err != nil {
		return Value{}, err
	}
	switch op {
	case OpAdd:
		return Value{Kind: KindDecimal, Str: da.Add(db).String()}, nil
	case OpSub:
		return Value{Kind: KindDecimal, Str: da.Sub(db).String()}, nil
	case OpMul:
		return Value{Kind: KindDecimal, Str: da.Mul(db).String()}, nil
	case OpDiv:
		if db.IsZero() {
			return Value{}, fmt.Errorf("division by zero")
		}
		return Value{Kind: KindDecimal, Str: da.Div(db).String()}, nil
	case OpMod:
		if db.IsZero() {
			return Value{}, fmt.Errorf("modulo by zero")
		}
		return Value{Kind: KindDecimal, Str: da.Mod(db).String()}, nil
	}
	return Value{}, fmt.Errorf("unsupported arithmetic op %d", op)
}
