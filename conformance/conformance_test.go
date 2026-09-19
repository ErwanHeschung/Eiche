// Package conformance runs every *.json fixture in this directory
// through the real pipeline: parse -> resolve -> check -> build IR ->
// compile to bytecode -> evaluate. Per .context/CLAUDE.md's convention,
// fixtures are data (JSON), not test code, specifically so a future
// language binding (Java, TypeScript) can run the exact same fixtures
// against its own runtime without anyone reviewing a second evaluator —
// this file is the Go-side runner, not the source of truth.
//
// Fixture shape:
//
//	{
//	  "name": "...",              // for humans; the filename is what actually identifies it
//	  "schema": "model M { ... }", // .eiche source, as a single string
//	  "model": "M",                // which model in schema to validate against
//	  "input": { ... },            // the JSON payload to validate
//	  "expected": [                // every error that must be produced, in any order
//	    { "path": "field", "code": "code", "params": { "min": "3" } }
//	  ],
//	  "notes": "..."                // optional: why this case exists, not read by the runner
//	}
//
// "expected": [] asserts the payload is valid. "params" is optional per
// expected error — omit it to only check path/code and ignore whatever
// params (if any) the real error carries.
package conformance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"eiche/internal/core"
	"eiche/internal/rules"
	"eiche/internal/rules/bytecode"
)

type Fixture struct {
	Name     string          `json:"name"`
	Schema   string          `json:"schema"`
	Model    string          `json:"model"`
	Input    json.RawMessage `json:"input"`
	Expected []ExpectedError `json:"expected"`
}

type ExpectedError struct {
	Path   string            `json:"path"`
	Code   string            `json:"code"`
	Params map[string]string `json:"params,omitempty"`
}

func TestConformance(t *testing.T) {
	files, err := filepath.Glob("*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no conformance fixtures found (*.json in this directory)")
	}

	for _, path := range files {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			runFixture(t, path)
		})
	}
}

func runFixture(t *testing.T, path string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fx Fixture
	if err := json.Unmarshal(data, &fx); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}

	prog, diags := core.ResolveImports(core.MapLoader{"fixture.eiche": fx.Schema}, "fixture.eiche")
	if len(diags) != 0 {
		t.Fatalf("resolve: %v", diags)
	}
	if diags := core.CheckTypes(prog); len(diags) != 0 {
		t.Fatalf("CheckTypes: %v", diags)
	}
	if diags := core.CheckExpressions(prog, nil); len(diags) != 0 {
		t.Fatalf("CheckExpressions: %v", diags)
	}
	if diags := core.CheckNaming(prog); len(diags) != 0 {
		t.Fatalf("CheckNaming: %v", diags)
	}
	if _, ok := prog.Models[fx.Model]; !ok {
		t.Fatalf("fixture names model %q, not declared in its schema", fx.Model)
	}

	ir := core.BuildIR(prog)
	var irModel *core.IRModel
	for i := range ir.Models {
		if ir.Models[i].Name == fx.Model {
			irModel = &ir.Models[i]
		}
	}
	if irModel == nil {
		t.Fatalf("model %q missing from built IR (internal error)", fx.Model)
	}

	bc, err := rules.Compile(irModel)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	input, err := bytecode.ParseInput(fx.Input)
	if err != nil {
		t.Fatalf("parse fixture input: %v", err)
	}

	errs, err := bytecode.Eval(bc, input, nil)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}

	assertErrorsMatch(t, fx.Expected, errs)
}

func assertErrorsMatch(t *testing.T, want []ExpectedError, got []bytecode.ValidationError) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("got %d error(s), want %d\n got: %+v\nwant: %+v", len(got), len(want), got, want)
	}

	remaining := append([]bytecode.ValidationError{}, got...)
	for _, w := range want {
		idx := -1
		for i, g := range remaining {
			if g.Path == w.Path && g.Code == w.Code && paramsMatch(w.Params, g.Params) {
				idx = i
				break
			}
		}
		if idx == -1 {
			t.Fatalf("expected error {path:%q code:%q params:%v} not found among actual errors %+v", w.Path, w.Code, w.Params, got)
		}
		remaining = append(remaining[:idx], remaining[idx+1:]...)
	}
}

// paramsMatch: a fixture that omits "params" doesn't care what the real
// error's Params contains; a fixture that specifies it must match exactly.
func paramsMatch(want, got map[string]string) bool {
	if want == nil {
		return true
	}
	if len(want) != len(got) {
		return false
	}
	for k, v := range want {
		if got[k] != v {
			return false
		}
	}
	return true
}
