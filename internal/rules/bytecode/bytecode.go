// Package bytecode defines the instruction set and value representation
// shared between the rule compiler (internal/rules, native Go, part of
// eichec) and the rule VM (wasm/core, built with TinyGo). It has zero
// dependency on internal/core or internal/syntax on purpose: the WASM
// build must not pull in the whole compiler front end just to interpret
// already-compiled bytecode.
package bytecode

// Op is one VM instruction. Every op takes at most one int32 operand,
// which keeps both the compiler and the VM simple — no variable-length
// instruction encoding to get wrong.
type Op byte

const (
	OpPushConst Op = iota // operand: index into Program.Constants
	OpPushField           // operand: index into Program.Fields — reads a top-level field from the input object
	OpGetProp             // operand: index into Program.Constants (a KindString) — pops a value, pushes its named property
	OpEq
	OpNeq
	OpLt
	OpLte
	OpGt
	OpGte
	OpAdd
	OpSub
	OpMul
	OpDiv
	OpMod
	OpAnd
	OpOr
	OpNot
	OpNeg
	OpCallCapability // operand: index into Program.Calls
)

// ValueKind discriminates Value, Go having no sum types.
type ValueKind byte

const (
	KindNull ValueKind = iota
	KindBool
	KindInt
	KindDecimal // Str holds the exact decimal text — never a float64, to preserve precision
	KindString
	KindJSON // an opaque, non-null value from a "json"-typed field; only null-checks are supported on it
)

type Value struct {
	Kind ValueKind
	Bool bool
	Int  int64
	Str  string // decimal text (KindDecimal) or string contents (KindString)
}

// Instr is one bytecode instruction.
type Instr struct {
	Op      Op
	Operand int32
}

// CapabilityCall describes one "::" call site: which imported capability,
// which exported function, and how many stack arguments to pop for it.
// M2 supports bool-returning capability functions only, matching every
// example in the brief's manifest and DSL — "Errors collapse to false."
type CapabilityCall struct {
	Capability string
	Func       string
	ArgCount   int
}

// Check is one compiled rule: a self-contained expression (Code, indexing
// into the Program's shared Constants/Fields/Calls pools) that must
// evaluate true, the field it attaches an error to on failure, and the
// message/code to report. Fields lists (by index into Program.Fields)
// every top-level field the expression references, so the VM can apply
// the frozen null-safety rule: skip this check if any of those fields is
// null in the input, rather than double-reporting past a missing value.
type Check struct {
	Code    []Instr
	Fields  []int
	On      string
	Message string
}

// Program is one model's compiled rule set: everything the VM needs to
// validate one JSON payload, with no further lookups outside these pools.
type Program struct {
	ABI    int
	Fields []string
	// FieldTypes holds each Fields entry's declared IR scalar type
	// ("int", "decimal", "bool", "string", "date", "datetime", "json",
	// or an enum name), parallel to Fields by index. The VM needs this
	// to decode a field's JSON value correctly — notably, a decimal
	// field is wire-encoded as a JSON *string* (to preserve exact
	// precision, matching the TS emitter's decimal -> string mapping),
	// so "is this JSON string a decimal or a real string field" isn't
	// answerable from the JSON alone.
	FieldTypes []string
	Constants  []Value
	Calls      []CapabilityCall
	Checks     []Check
}
