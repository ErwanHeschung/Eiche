// Command core is the compiled rule engine: a single, host-agnostic WASM
// binary built with `tinygo build -target=wasm-unknown`, run identically
// under Chicory on the JVM and in a browser. It is stateless per the
// brief's architecture: alloc a buffer, load a compiled rule Program into
// it once, then validate any number of JSON payloads against that same
// handle.
//
// ABI (all sizes/pointers are uint32, addressing this module's own
// exported linear memory):
//
//	alloc(size) -> ptr                      caller writes size bytes at ptr
//	free(ptr)                               releases a buffer from alloc or the result of validate
//	loadProgram(ptr, len) -> handle          decodes a JSON-encoded bytecode.Program; 0 on failure
//	unloadProgram(handle)                    releases a loaded Program
//	validate(handle, jsonPtr, jsonLen) -> ptr
//	    always succeeds in the ABI sense (never traps on bad input): the
//	    result at ptr is a 4-byte little-endian length prefix followed by
//	    that many bytes of JSON, an encoded Result — {"ok":true,"errors":[...]}
//	    or {"ok":false,"message":"..."}. The caller must free(ptr) when done.
//
// Capability calls ("::") are not linked into this build yet — every
// call fails with a clear error surfaced through the normal Result.
// Per-project capability linking (matching each declared "capability ...
// from ..." to an actual imported WASM function) is a separate,
// not-yet-built step; see .context/CLAUDE.md's M2 checklist.
//
// File layout: this file is a thin ABI shim — memory marshaling only, no
// logic — over engine.go, which is ordinary portable Go with no
// unsafe.Pointer. That split exists because alloc()'s "pointer" is a real
// pointer truncated to uint32, which only round-trips correctly under a
// genuine 32-bit WASM linear memory; compiled natively for `go test` on a
// 64-bit host, that truncation silently reconstructs the wrong address.
// So engine.go's logic is tested natively (see engine_test.go), and only
// the memory-marshaling glue here needs the real TinyGo/WASM environment
// to verify — see spike/m2-wasm-core-smoke.
package main

import (
	"encoding/binary"
	"unsafe"
)

//go:wasmexport alloc
func alloc(size uint32) uint32 {
	if size == 0 {
		return 0
	}
	buf := make([]byte, size)
	ptr := ptrOf(buf)
	buffers[ptr] = buf
	return ptr
}

//go:wasmexport free
func free(ptr uint32) {
	delete(buffers, ptr)
}

//go:wasmexport loadProgram
func loadProgram(ptr, length uint32) uint32 {
	return decodeAndStoreProgram(readMemory(ptr, length))
}

//go:wasmexport unloadProgram
func unloadProgram(handle uint32) {
	delete(programs, handle)
}

//go:wasmexport validate
func validate(handle, jsonPtr, jsonLen uint32) uint32 {
	r := runValidate(handle, readMemory(jsonPtr, jsonLen))
	return writeResult(r)
}

func writeResult(r Result) uint32 {
	data := encodeResult(r)
	buf := make([]byte, 4+len(data))
	binary.LittleEndian.PutUint32(buf, uint32(len(data)))
	copy(buf[4:], data)

	ptr := ptrOf(buf)
	buffers[ptr] = buf
	return ptr
}

func ptrOf(buf []byte) uint32 {
	if len(buf) == 0 {
		return 0
	}
	return uint32(uintptr(unsafe.Pointer(&buf[0])))
}

func readMemory(ptr, length uint32) []byte {
	if length == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), length)
}

func main() {}
