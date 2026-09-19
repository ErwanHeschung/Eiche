package core

import "testing"

func TestCheckNamingCleanProgramPasses(t *testing.T) {
	prog := resolveOK(t, MapLoader{"main.eiche": briefJobOffer}, "main.eiche")
	if diags := CheckNaming(prog); len(diags) != 0 {
		t.Fatalf("unexpected naming diagnostics: %v", diags)
	}
}

func TestCheckNamingRejectsJavaOnlyKeywordField(t *testing.T) {
	// "goto" is Java-reserved but not a TypeScript reserved word.
	prog := resolveOK(t, MapLoader{"m.eiche": `model M { goto: string }`}, "m.eiche")
	diags := CheckNaming(prog)
	if len(diags) != 1 {
		t.Fatalf("want 1 diagnostic, got %d: %v", len(diags), diags)
	}
}

func TestCheckNamingRejectsTSOnlyKeywordField(t *testing.T) {
	// "let" is TypeScript-reserved but not a Java reserved word.
	prog := resolveOK(t, MapLoader{"m.eiche": `model M { let: string }`}, "m.eiche")
	diags := CheckNaming(prog)
	if len(diags) != 1 {
		t.Fatalf("want 1 diagnostic, got %d: %v", len(diags), diags)
	}
}

func TestCheckNamingRejectsWordReservedInBothLanguages(t *testing.T) {
	// "class" is reserved in both Java and TypeScript: one diagnostic per
	// language, since each is independently actionable.
	prog := resolveOK(t, MapLoader{"m.eiche": `model class { a: string }`}, "m.eiche")
	diags := CheckNaming(prog)
	if len(diags) != 2 {
		t.Fatalf("want 2 diagnostics, got %d: %v", len(diags), diags)
	}
}

func TestCheckNamingRejectsReservedEnumMember(t *testing.T) {
	prog := resolveOK(t, MapLoader{"m.eiche": `enum E { class, X }`}, "m.eiche")
	diags := CheckNaming(prog)
	if len(diags) != 2 {
		t.Fatalf("want 2 diagnostics, got %d: %v", len(diags), diags)
	}
}

func TestCheckNamingRejectsNestedJSONShapeField(t *testing.T) {
	prog := resolveOK(t, MapLoader{"m.eiche": `model M {
  answers: json {
    goto: string
  }
}`}, "m.eiche")
	diags := CheckNaming(prog)
	if len(diags) != 1 {
		t.Fatalf("want 1 diagnostic, got %d: %v", len(diags), diags)
	}
}
