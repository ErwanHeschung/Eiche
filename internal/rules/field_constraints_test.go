package rules

import (
	"testing"

	"eiche/internal/core"
	"eiche/internal/rules/bytecode"
)

func compileModel(t *testing.T, src string) *bytecode.Program {
	t.Helper()
	prog, diags := core.ResolveImports(core.MapLoader{"m.eiche": src}, "m.eiche")
	if len(diags) != 0 {
		t.Fatalf("resolve: %v", diags)
	}
	if diags := core.CheckTypes(prog); len(diags) != 0 {
		t.Fatalf("CheckTypes: %v", diags)
	}
	if diags := core.CheckExpressions(prog, nil); len(diags) != 0 {
		t.Fatalf("CheckExpressions: %v", diags)
	}
	ir := core.BuildIR(prog)
	bc, err := Compile(&ir.Models[0])
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return bc
}

func TestRequiredFieldConstraint(t *testing.T) {
	prog := compileModel(t, `model M {
  title: string
  nickname?: string
}`)

	errs, err := bytecode.Eval(prog, map[string]any{"title": "hi"}, nil)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("title present, nickname absent (optional): want no errors, got %+v", errs)
	}

	errs, err = bytecode.Eval(prog, map[string]any{}, nil)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(errs) != 1 || errs[0].Path != "title" || errs[0].Code != "required" {
		t.Fatalf("title absent: want exactly 1 required error, got %+v", errs)
	}
}

func TestMinMaxLengthConstraint(t *testing.T) {
	prog := compileModel(t, `model M {
  title: string @minLength(3) @maxLength(10)
}`)

	cases := []struct {
		title   string
		wantErr string
	}{
		{"ab", "minLength"},
		{"just right", ""},
		{"way too long a title", "maxLength"},
	}
	for _, c := range cases {
		errs, err := bytecode.Eval(prog, map[string]any{"title": c.title}, nil)
		if err != nil {
			t.Fatalf("Eval(%q): %v", c.title, err)
		}
		if c.wantErr == "" {
			if len(errs) != 0 {
				t.Fatalf("title=%q: want no errors, got %+v", c.title, errs)
			}
			continue
		}
		if len(errs) != 1 || errs[0].Code != c.wantErr {
			t.Fatalf("title=%q: want exactly 1 %s error, got %+v", c.title, c.wantErr, errs)
		}
	}
}

func TestGraphemeAwareMinLength(t *testing.T) {
	// "🇫🇷🇫🇷🇫🇷" is 3 grapheme clusters (flag emoji) but 6 runes / 12
	// UTF-16 code units — @minLength(3) must pass on grapheme count.
	prog := compileModel(t, `model M {
  title: string @minLength(3)
}`)
	errs, err := bytecode.Eval(prog, map[string]any{"title": "🇫🇷🇫🇷🇫🇷"}, nil)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("want minLength(3) satisfied by 3 grapheme clusters, got %+v", errs)
	}
}

func TestMinMaxNumericConstraint(t *testing.T) {
	prog := compileModel(t, `model M {
  salary: int @min(20000) @max(200000)
}`)
	cases := []struct {
		salary  int
		wantErr string
	}{
		{10000, "min"},
		{50000, ""},
		{300000, "max"},
	}
	for _, c := range cases {
		errs, err := bytecode.Eval(prog, map[string]any{"salary": c.salary}, nil)
		if err != nil {
			t.Fatalf("Eval(%d): %v", c.salary, err)
		}
		if c.wantErr == "" {
			if len(errs) != 0 {
				t.Fatalf("salary=%d: want no errors, got %+v", c.salary, errs)
			}
			continue
		}
		if len(errs) != 1 || errs[0].Code != c.wantErr {
			t.Fatalf("salary=%d: want exactly 1 %s error, got %+v", c.salary, c.wantErr, errs)
		}
	}
}

func TestRangeConstraintOnDecimalField(t *testing.T) {
	prog := compileModel(t, `model M {
  hours: decimal @range(10, 48)
}`)
	input, err := bytecode.ParseInput([]byte(`{"hours": "55.5"}`))
	if err != nil {
		t.Fatalf("ParseInput: %v", err)
	}
	errs, err := bytecode.Eval(prog, input, nil)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(errs) != 1 || errs[0].Code != "range" {
		t.Fatalf("want exactly 1 range error, got %+v", errs)
	}
	if errs[0].Params["min"] != "10" || errs[0].Params["max"] != "48" {
		t.Fatalf("want Params to carry the range bounds, got %+v", errs[0].Params)
	}

	input, _ = bytecode.ParseInput([]byte(`{"hours": "37.5"}`))
	errs, err = bytecode.Eval(prog, input, nil)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("37.5 is within [10,48]: want no errors, got %+v", errs)
	}
}

func TestMinItemsConstraint(t *testing.T) {
	prog := compileModel(t, `model M {
  skills: string[] @minItems(1)
}`)
	input, _ := bytecode.ParseInput([]byte(`{"skills": []}`))
	errs, err := bytecode.Eval(prog, input, nil)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(errs) != 1 || errs[0].Code != "minItems" {
		t.Fatalf("empty array: want exactly 1 minItems error, got %+v", errs)
	}

	input, _ = bytecode.ParseInput([]byte(`{"skills": ["Go"]}`))
	errs, err = bytecode.Eval(prog, input, nil)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("non-empty array: want no errors, got %+v", errs)
	}
}

// TestFieldConstraintOrderingWithCrossFieldRule combines a field
// constraint and an explicit @rule referencing the same field: the
// field-constraint failure must suppress the @rule, per the brief's
// stated ordering ("field constraints run first; any @rule referencing a
// field that already has an error is skipped").
func TestFieldConstraintOrderingWithCrossFieldRule(t *testing.T) {
	prog := compileModel(t, `model M {
  salaryMin: int @min(20000)
  salaryMax: int

  @rule(salaryMax >= salaryMin, "salary.range") on salaryMax
}`)
	errs, err := bytecode.Eval(prog, map[string]any{"salaryMin": 5000, "salaryMax": 1000}, nil)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(errs) != 1 || errs[0].Code != "min" {
		t.Fatalf("want only the @min constraint error (cross-field rule suppressed), got %+v", errs)
	}
}

func TestUnknownAnnotationCompilesAsNoOp(t *testing.T) {
	// @format isn't compiled yet — it must not be a compile error.
	prog := compileModel(t, `model M {
  email: string @format(email)
}`)
	errs, err := bytecode.Eval(prog, map[string]any{"email": "anything"}, nil)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("unrecognized annotation should be a no-op, got %+v", errs)
	}
}
