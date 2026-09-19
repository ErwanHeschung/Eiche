package bytecode

import "testing"

// salaryRangeProgram hand-builds the bytecode for
// "salaryMax >= salaryMin" without going through the compiler, to test
// the VM in isolation.
func salaryRangeProgram() *Program {
	return &Program{
		ABI:        1,
		Fields:     []string{"salaryMin", "salaryMax"},
		FieldTypes: []string{"int", "int"},
		Checks: []Check{
			{
				Code: []Instr{
					{Op: OpPushField, Operand: 1}, // salaryMax
					{Op: OpPushField, Operand: 0}, // salaryMin
					{Op: OpGte},
				},
				Fields:  []int{0, 1},
				On:      "salaryMax",
				Message: "salary.range",
			},
		},
	}
}

func TestVMSimpleComparisonPassAndFail(t *testing.T) {
	prog := salaryRangeProgram()

	errs, err := Eval(prog, map[string]any{"salaryMin": 40000, "salaryMax": 60000}, nil)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("valid input: got errors %+v", errs)
	}

	errs, err = Eval(prog, map[string]any{"salaryMin": 60000, "salaryMax": 40000}, nil)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(errs) != 1 || errs[0].Path != "salaryMax" || errs[0].Code != "salary.range" {
		t.Fatalf("invalid input: got %+v", errs)
	}
}

func TestVMNullSafetySkipsCheck(t *testing.T) {
	prog := salaryRangeProgram()
	// salaryMax absent entirely: the check references it, so it must be
	// skipped rather than erroring out on a missing field.
	errs, err := Eval(prog, map[string]any{"salaryMin": 60000}, nil)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("want check skipped (no errors), got %+v", errs)
	}
}

func TestVMDecimalComparisonExactPrecision(t *testing.T) {
	// 10.10 + 10.20 == 20.30 exactly in decimal, but not as float64
	// (0.1+0.2 style rounding) — this is the whole reason Value never
	// carries a float64.
	prog := &Program{
		Fields:     []string{"a", "b", "c"},
		FieldTypes: []string{"decimal", "decimal", "decimal"},
		Checks: []Check{{
			Code: []Instr{
				{Op: OpPushField, Operand: 0},
				{Op: OpPushField, Operand: 1},
				{Op: OpAdd},
				{Op: OpPushField, Operand: 2},
				{Op: OpEq},
			},
			On: "a", Message: "sum",
		}},
	}
	input, err := ParseInput([]byte(`{"a": "10.10", "b": "10.20", "c": "20.30"}`))
	if err != nil {
		t.Fatalf("ParseInput: %v", err)
	}
	errs, err := Eval(prog, input, nil)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("exact decimal sum should match: %+v", errs)
	}
}

func TestVMIntDivisionByZero(t *testing.T) {
	prog := &Program{
		Fields:     []string{"a", "b"},
		FieldTypes: []string{"int", "int"},
		Checks: []Check{{
			Code: []Instr{
				{Op: OpPushField, Operand: 0},
				{Op: OpPushField, Operand: 1},
				{Op: OpDiv},
				{Op: OpPushConst, Operand: 0},
				{Op: OpEq},
			},
			Fields: []int{0, 1},
			On:     "a", Message: "div",
			// no null values here, so this check runs and should error
		}},
		Constants: []Value{{Kind: KindInt, Int: 1}},
	}
	_, err := Eval(prog, map[string]any{"a": 1, "b": 0}, nil)
	if err == nil {
		t.Fatal("want an error for division by zero")
	}
}

func TestVMCapabilityCall(t *testing.T) {
	prog := &Program{
		Fields:     []string{"contactPhone"},
		FieldTypes: []string{"string"},
		Calls:      []CapabilityCall{{Capability: "phone", Func: "isValid", ArgCount: 1}},
		Checks: []Check{{
			Code: []Instr{
				{Op: OpPushField, Operand: 0},
				{Op: OpCallCapability, Operand: 0},
			},
			Fields:  []int{0},
			On:      "contactPhone",
			Message: "phone.invalid",
		}},
	}

	invoker := fakeInvoker{result: true}
	errs, err := Eval(prog, map[string]any{"contactPhone": "+33612345678"}, invoker)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("want no errors, got %+v", errs)
	}

	invoker = fakeInvoker{result: false}
	errs, err = Eval(prog, map[string]any{"contactPhone": "garbage"}, invoker)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(errs) != 1 {
		t.Fatalf("want 1 error, got %+v", errs)
	}
}

type fakeInvoker struct {
	result bool
	err    error
}

func (f fakeInvoker) Call(capability, fn string, args []Value) (bool, error) {
	return f.result, f.err
}

func TestVMAndOrNot(t *testing.T) {
	prog := &Program{
		Fields:     []string{"a", "b"},
		FieldTypes: []string{"bool", "bool"},
		Checks: []Check{{
			Code: []Instr{
				{Op: OpPushField, Operand: 0},
				{Op: OpNot},
				{Op: OpPushField, Operand: 1},
				{Op: OpOr},
			},
			On: "a", Message: "implies-lowering",
		}},
	}
	// !a || b, i.e. "a implies b"
	cases := []struct {
		a, b bool
		want bool
	}{
		{true, true, true},
		{true, false, false},
		{false, true, true},
		{false, false, true},
	}
	for _, c := range cases {
		errs, err := Eval(prog, map[string]any{"a": c.a, "b": c.b}, nil)
		if err != nil {
			t.Fatalf("Eval(%v,%v): %v", c.a, c.b, err)
		}
		got := len(errs) == 0
		if got != c.want {
			t.Fatalf("a=%v b=%v: pass=%v, want %v", c.a, c.b, got, c.want)
		}
	}
}
