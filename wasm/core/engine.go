package main

import (
	"encoding/json"
	"fmt"

	"eiche/internal/rules/bytecode"
)

// SupportedABI is the bytecode.Program.ABI this build accepts. A mismatch
// fails loadProgram outright rather than risk misreading an incompatible
// instruction set.
const SupportedABI = 1

var (
	buffers           = map[uint32][]byte{}
	programs          = map[uint32]*bytecode.Program{}
	nextHandle uint32 = 1
)

// decodeAndStoreProgram is loadProgram's logic without the memory
// marshaling: decode a JSON-encoded bytecode.Program and register it
// under a new handle, or return 0 on any failure (bad JSON, ABI
// mismatch).
func decodeAndStoreProgram(data []byte) uint32 {
	var prog bytecode.Program
	if err := json.Unmarshal(data, &prog); err != nil {
		return 0
	}
	if prog.ABI != SupportedABI {
		return 0
	}

	handle := nextHandle
	nextHandle++
	programs[handle] = &prog
	return handle
}

// runValidate is validate's logic without the memory marshaling: look up
// the Program by handle and evaluate jsonData against it.
func runValidate(handle uint32, jsonData []byte) Result {
	prog, ok := programs[handle]
	if !ok {
		return resultError(fmt.Sprintf("unknown program handle %d", handle))
	}

	input, err := bytecode.ParseInput(jsonData)
	if err != nil {
		return resultError("invalid JSON input: " + err.Error())
	}

	errs, err := bytecode.Eval(prog, input, unlinkedCapabilities{})
	if err != nil {
		return resultError(err.Error())
	}
	return resultOK(errs)
}

// unlinkedCapabilities is the placeholder CapabilityInvoker until
// per-project capability linking exists. Every "::" call fails with a
// clear, specific error rather than silently returning false (which
// would look like a normal validation failure instead of a build
// problem).
type unlinkedCapabilities struct{}

func (unlinkedCapabilities) Call(capability, fn string, args []bytecode.Value) (bool, error) {
	return false, fmt.Errorf("capability %q::%s is not linked into this build", capability, fn)
}

// Result is the JSON shape validate() writes back: either the list of
// {path, code} validation errors (frozen shape from .context/CLAUDE.md,
// Params omitted here — see internal/rules' scope note), or a message
// explaining why validation itself couldn't run (bad handle, bad JSON, an
// unlinked capability).
type Result struct {
	OK      bool                       `json:"ok"`
	Errors  []bytecode.ValidationError `json:"errors,omitempty"`
	Message string                     `json:"message,omitempty"`
}

func resultOK(errs []bytecode.ValidationError) Result {
	return Result{OK: true, Errors: errs}
}

func resultError(message string) Result {
	return Result{OK: false, Message: message}
}

func encodeResult(r Result) []byte {
	data, err := json.Marshal(r)
	if err != nil {
		data, _ = json.Marshal(Result{OK: false, Message: "internal error encoding result"})
	}
	return data
}
