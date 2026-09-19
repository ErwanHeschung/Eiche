package manifest

import "testing"

func TestParseBriefExample(t *testing.T) {
	src := `{
  "name": "eiche-payroll",
  "version": "1.0.0",
  "abi": 1,
  "exports": {
    "isCoherent": { "args": ["string", "int", "int", "float"], "returns": "bool" }
  }
}`
	m, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.Name != "eiche-payroll" || m.Version != "1.0.0" || m.ABI != 1 {
		t.Fatalf("m = %+v", m)
	}
	exp, ok := m.Exports["isCoherent"]
	if !ok {
		t.Fatal("missing export isCoherent")
	}
	if len(exp.Args) != 4 || exp.Returns != "bool" {
		t.Fatalf("export = %+v", exp)
	}
}

func TestParseMissingName(t *testing.T) {
	_, err := Parse([]byte(`{"version": "1.0.0"}`))
	if err == nil {
		t.Fatal("want error for missing name")
	}
}

func TestParseInvalidJSON(t *testing.T) {
	_, err := Parse([]byte(`not json`))
	if err == nil {
		t.Fatal("want error for invalid json")
	}
}
