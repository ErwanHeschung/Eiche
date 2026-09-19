package core

import "testing"

func TestResolveSingleFile(t *testing.T) {
	loader := MapLoader{
		"main.eiche": `model M { a: string }`,
	}
	prog, diags := ResolveImports(loader, "main.eiche")
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if _, ok := prog.Models["M"]; !ok {
		t.Fatalf("Models = %+v, want M present", prog.Models)
	}
}

func TestResolveMergesImports(t *testing.T) {
	loader := MapLoader{
		"main.eiche":   `import "./common.eiche"` + "\n" + `model JobOffer { status: Status }`,
		"common.eiche": `enum Status { DRAFT, PUBLISHED }`,
	}
	prog, diags := ResolveImports(loader, "main.eiche")
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if _, ok := prog.Enums["Status"]; !ok {
		t.Fatalf("Enums = %+v, want Status present", prog.Enums)
	}
	if _, ok := prog.Models["JobOffer"]; !ok {
		t.Fatalf("Models = %+v, want JobOffer present", prog.Models)
	}
}

func TestResolveImportOnce(t *testing.T) {
	// diamond import: main -> a, main -> b, a -> shared, b -> shared.
	// shared must be merged exactly once, not twice (which would otherwise
	// trip the duplicate-name check on its own declarations).
	loader := MapLoader{
		"main.eiche":   `import "./a.eiche"` + "\n" + `import "./b.eiche"` + "\n" + `model M { a: string }`,
		"a.eiche":      `import "./shared.eiche"`,
		"b.eiche":      `import "./shared.eiche"`,
		"shared.eiche": `enum Status { DRAFT }`,
	}
	prog, diags := ResolveImports(loader, "main.eiche")
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if len(prog.Enums) != 1 {
		t.Fatalf("Enums = %+v, want exactly 1", prog.Enums)
	}
}

func TestResolveImportCycle(t *testing.T) {
	loader := MapLoader{
		"a.eiche": `import "./b.eiche"`,
		"b.eiche": `import "./a.eiche"`,
	}
	_, diags := ResolveImports(loader, "a.eiche")
	if len(diags) != 1 {
		t.Fatalf("want 1 diagnostic (import cycle), got %d: %v", len(diags), diags)
	}
}

func TestResolveMissingImport(t *testing.T) {
	loader := MapLoader{
		"main.eiche": `import "./missing.eiche"`,
	}
	_, diags := ResolveImports(loader, "main.eiche")
	if len(diags) != 1 {
		t.Fatalf("want 1 diagnostic (missing file), got %d: %v", len(diags), diags)
	}
}

func TestResolveDuplicateNameAcrossFiles(t *testing.T) {
	loader := MapLoader{
		"main.eiche":  `import "./other.eiche"` + "\n" + `model M { a: string }`,
		"other.eiche": `model M { b: int }`,
	}
	_, diags := ResolveImports(loader, "main.eiche")
	if len(diags) != 1 {
		t.Fatalf("want 1 diagnostic (duplicate M), got %d: %v", len(diags), diags)
	}
}

func TestResolveDuplicateAcrossKinds(t *testing.T) {
	// A model and a view sharing a name collide too: both live in the
	// type-reference namespace (either can be the target of a TypeExpr or
	// a view's Base).
	loader := MapLoader{
		"main.eiche": `model M { a: string }
view M = partial M`,
	}
	_, diags := ResolveImports(loader, "main.eiche")
	if len(diags) != 1 {
		t.Fatalf("want 1 diagnostic (M as both model and view), got %d: %v", len(diags), diags)
	}
}
