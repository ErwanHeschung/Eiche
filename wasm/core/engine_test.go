package main

import (
	"encoding/json"
	"testing"

	"eiche/internal/rules/bytecode"
)

// These test decodeAndStoreProgram/runValidate directly — the portable
// logic — rather than the alloc/free/wasmexport shim in main.go. See
// main.go's doc comment for why: that shim's "pointer" only round-trips
// correctly under real 32-bit WASM linear memory, not a native 64-bit
// test process, so it's verified separately via spike/m2-wasm-core-smoke
// against an actual TinyGo build instead.

func salaryRangeProgramJSON(t *testing.T) []byte {
	t.Helper()
	prog := bytecode.Program{
		ABI:        SupportedABI,
		Fields:     []string{"salaryMin", "salaryMax"},
		FieldTypes: []string{"int", "int"},
		Checks: []bytecode.Check{{
			Code: []bytecode.Instr{
				{Op: bytecode.OpPushField, Operand: 1},
				{Op: bytecode.OpPushField, Operand: 0},
				{Op: bytecode.OpGte},
			},
			Fields:  []int{0, 1},
			On:      "salaryMax",
			Message: "salary.range",
		}},
	}
	data, err := json.Marshal(prog)
	if err != nil {
		t.Fatalf("marshal program: %v", err)
	}
	return data
}

func TestFullLifecycle(t *testing.T) {
	handle := decodeAndStoreProgram(salaryRangeProgramJSON(t))
	if handle == 0 {
		t.Fatal("decodeAndStoreProgram returned 0 (failure)")
	}
	defer delete(programs, handle)

	r := runValidate(handle, []byte(`{"salaryMin": 40000, "salaryMax": 60000}`))
	if !r.OK {
		t.Fatalf("want ok, got %+v", r)
	}
	if len(r.Errors) != 0 {
		t.Fatalf("valid input: unexpected errors %+v", r.Errors)
	}

	r = runValidate(handle, []byte(`{"salaryMin": 90000, "salaryMax": 40000}`))
	if !r.OK {
		t.Fatalf("want ok=true (validation ran) with 1 reported error, got %+v", r)
	}
	if len(r.Errors) != 1 || r.Errors[0].Path != "salaryMax" || r.Errors[0].Code != "salary.range" {
		t.Fatalf("want exactly the salary.range error, got %+v", r.Errors)
	}
}

func TestDecodeProgramRejectsBadABI(t *testing.T) {
	prog := bytecode.Program{ABI: SupportedABI + 1, Fields: []string{}, FieldTypes: []string{}}
	data, _ := json.Marshal(prog)

	handle := decodeAndStoreProgram(data)
	if handle != 0 {
		t.Fatalf("want 0 (rejected) for an ABI mismatch, got handle %d", handle)
	}
}

func TestDecodeProgramRejectsGarbage(t *testing.T) {
	handle := decodeAndStoreProgram([]byte("not json at all"))
	if handle != 0 {
		t.Fatalf("want 0 (rejected) for garbage input, got handle %d", handle)
	}
}

func TestValidateUnknownHandle(t *testing.T) {
	r := runValidate(999, []byte(`{}`))
	if r.OK {
		t.Fatalf("want ok=false for an unknown handle, got %+v", r)
	}
}

func TestValidateUnlinkedCapabilityFailsClearly(t *testing.T) {
	prog := bytecode.Program{
		ABI:        SupportedABI,
		Fields:     []string{"contactPhone"},
		FieldTypes: []string{"string"},
		Calls:      []bytecode.CapabilityCall{{Capability: "phone", Func: "isValid", ArgCount: 1}},
		Checks: []bytecode.Check{{
			Code: []bytecode.Instr{
				{Op: bytecode.OpPushField, Operand: 0},
				{Op: bytecode.OpCallCapability, Operand: 0},
			},
			Fields:  []int{0},
			On:      "contactPhone",
			Message: "phone.invalid",
		}},
	}
	data, _ := json.Marshal(prog)
	handle := decodeAndStoreProgram(data)
	if handle == 0 {
		t.Fatal("decodeAndStoreProgram failed")
	}
	defer delete(programs, handle)

	r := runValidate(handle, []byte(`{"contactPhone": "+33612345678"}`))
	if r.OK {
		t.Fatalf("want ok=false (unlinked capability), got %+v", r)
	}
	if r.Message == "" {
		t.Fatal("want a non-empty message explaining the unlinked capability")
	}
}

func TestEncodeResultRoundTrip(t *testing.T) {
	r := resultOK([]bytecode.ValidationError{{Path: "a", Code: "b"}})
	data := encodeResult(r)

	var back Result
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !back.OK || len(back.Errors) != 1 || back.Errors[0].Path != "a" {
		t.Fatalf("round-trip mismatch: %+v", back)
	}
}
