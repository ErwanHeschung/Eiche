package core

import (
	"testing"

	"eiche/internal/manifest"
)

func resolveOK(t *testing.T, loader MapLoader, entry string) *Program {
	t.Helper()
	prog, diags := ResolveImports(loader, entry)
	if len(diags) != 0 {
		t.Fatalf("unexpected resolve diagnostics: %v", diags)
	}
	return prog
}

func TestCheckTypesBriefExample(t *testing.T) {
	src := `capability phone   from "eiche-phone@1.2"
capability payroll from "eiche-payroll@1.0"

enum ContractType { CDI, CDD, ALTERNANCE, STAGE }

model JobOffer {
  title:        string @minLength(3) @maxLength(120)
  contractType: ContractType
  startDate:    date @after(today)
  endDate?:     date
  salaryMin:    int @min(20000)
  salaryMax:    int
  hours:        decimal @range(10, 48)
  skills:       string[@minLength(2)] @minItems(1)
  metadata:     json
  contactPhone: string

  @rule(salaryMax >= salaryMin, "salary.range") on salaryMax
  @rule(contractType == CDD implies endDate != null, "cdd.endDate") on endDate
  @rule(phone::isValid(contactPhone, "FR"), "phone.notFrench") on contactPhone
  @rule(payroll::isCoherent(contractType, salaryMin, salaryMax, hours),
        "payroll.incoherent") on salaryMin
}

view JobOfferPatch   = partial JobOffer
view JobOfferSummary = JobOffer { title, contractType, salaryMin }
`
	prog := resolveOK(t, MapLoader{"main.eiche": src}, "main.eiche")

	if diags := CheckTypes(prog); len(diags) != 0 {
		t.Fatalf("CheckTypes: %v", diags)
	}

	manifests := map[string]*manifest.Manifest{
		"phone": {
			Name: "eiche-phone",
			Exports: map[string]manifest.Export{
				"isValid": {Args: []string{"string", "string"}, Returns: "bool"},
			},
		},
		"payroll": {
			Name: "eiche-payroll",
			Exports: map[string]manifest.Export{
				"isCoherent": {Args: []string{"string", "int", "int", "float"}, Returns: "bool"},
			},
		},
	}
	if diags := CheckExpressions(prog, manifests); len(diags) != 0 {
		t.Fatalf("CheckExpressions: %v", diags)
	}
}

func TestCheckUndefinedFieldType(t *testing.T) {
	prog := resolveOK(t, MapLoader{"m.eiche": `model M { a: NoSuchType }`}, "m.eiche")
	diags := CheckTypes(prog)
	if len(diags) != 1 {
		t.Fatalf("want 1 diagnostic, got %d: %v", len(diags), diags)
	}
}

func TestCheckDefaultMustBeOptional(t *testing.T) {
	// The parser already rejects a default on a non-optional field
	// syntactically; this exercises the type-checker's independent check
	// on default *value* compatibility once imports are merged.
	prog := resolveOK(t, MapLoader{"m.eiche": `model M { a?: int = "not a number" }`}, "m.eiche")
	diags := CheckTypes(prog)
	if len(diags) != 1 {
		t.Fatalf("want 1 diagnostic, got %d: %v", len(diags), diags)
	}
}

func TestCheckEnumDefault(t *testing.T) {
	good := resolveOK(t, MapLoader{"m.eiche": `enum Status { DRAFT, PUBLISHED }
model M { status?: Status = DRAFT }`}, "m.eiche")
	if diags := CheckTypes(good); len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}

	bad := resolveOK(t, MapLoader{"m.eiche": `enum Status { DRAFT, PUBLISHED }
model M { status?: Status = ARCHIVED }`}, "m.eiche")
	if diags := CheckTypes(bad); len(diags) != 1 {
		t.Fatalf("want 1 diagnostic, got %d: %v", len(diags), diags)
	}
}

func TestCheckModelTypedFieldCannotDefault(t *testing.T) {
	prog := resolveOK(t, MapLoader{"m.eiche": `model Inner { a: string }
model Outer { inner?: Inner = null }`}, "m.eiche")
	diags := CheckTypes(prog)
	if len(diags) != 1 {
		t.Fatalf("want 1 diagnostic, got %d: %v", len(diags), diags)
	}
}

func TestCheckViewBase(t *testing.T) {
	prog := resolveOK(t, MapLoader{"m.eiche": `model M { a: string }
view V1 = partial NoSuchModel
view V2 = M { noSuchField }
enum E { X }
view V3 = partial E
`}, "m.eiche")
	diags := CheckTypes(prog)
	if len(diags) != 3 {
		t.Fatalf("want 3 diagnostics, got %d: %v", len(diags), diags)
	}
}

func TestCheckExprUndefinedIdentifier(t *testing.T) {
	prog := resolveOK(t, MapLoader{"m.eiche": `model M {
  a: string
  @rule(b == a, "r") on a
}`}, "m.eiche")
	diags := CheckExpressions(prog, nil)
	if len(diags) != 1 {
		t.Fatalf("want 1 diagnostic, got %d: %v", len(diags), diags)
	}
}

func TestCheckExprAmbiguousEnumMember(t *testing.T) {
	prog := resolveOK(t, MapLoader{"m.eiche": `enum A { SHARED, X }
enum B { SHARED, Y }
model M {
  a: A
  @rule(a == SHARED, "r") on a
}`}, "m.eiche")
	diags := CheckExpressions(prog, nil)
	if len(diags) != 1 {
		t.Fatalf("want 1 diagnostic, got %d: %v", len(diags), diags)
	}
}

func TestCheckExprUndefinedCapability(t *testing.T) {
	prog := resolveOK(t, MapLoader{"m.eiche": `model M {
  a: string
  @rule(ghost::isValid(a), "r") on a
}`}, "m.eiche")
	diags := CheckExpressions(prog, nil)
	if len(diags) != 1 {
		t.Fatalf("want 1 diagnostic, got %d: %v", len(diags), diags)
	}
}

func TestCheckExprCapabilityArityAgainstManifest(t *testing.T) {
	loader := MapLoader{"m.eiche": `capability phone from "eiche-phone@1.2"
model M {
  a: string
  @rule(phone::isValid(a, a, a), "r") on a
}`}
	prog := resolveOK(t, loader, "m.eiche")
	manifests := map[string]*manifest.Manifest{
		"phone": {Name: "eiche-phone", Exports: map[string]manifest.Export{
			"isValid": {Args: []string{"string", "string"}, Returns: "bool"},
		}},
	}
	diags := CheckExpressions(prog, manifests)
	if len(diags) != 1 {
		t.Fatalf("want 1 diagnostic (arity mismatch), got %d: %v", len(diags), diags)
	}
}

func TestCheckExprCapabilityUnknownFunction(t *testing.T) {
	loader := MapLoader{"m.eiche": `capability phone from "eiche-phone@1.2"
model M {
  a: string
  @rule(phone::lineType(a), "r") on a
}`}
	prog := resolveOK(t, loader, "m.eiche")
	manifests := map[string]*manifest.Manifest{
		"phone": {Name: "eiche-phone", Exports: map[string]manifest.Export{
			"isValid": {Args: []string{"string", "string"}, Returns: "bool"},
		}},
	}
	diags := CheckExpressions(prog, manifests)
	if len(diags) != 1 {
		t.Fatalf("want 1 diagnostic (unknown function), got %d: %v", len(diags), diags)
	}
}

func TestCheckExprInfersOnField(t *testing.T) {
	prog := resolveOK(t, MapLoader{"m.eiche": `model M {
  a: int
  b: int
  @rule(a >= b, "r")
}`}, "m.eiche")
	diags := CheckExpressions(prog, nil)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	rule := prog.Models["M"].Rules[0]
	if rule.On != "a" {
		t.Fatalf("inferred On = %q, want %q", rule.On, "a")
	}
}

func TestCheckExprOnFieldMustExist(t *testing.T) {
	prog := resolveOK(t, MapLoader{"m.eiche": `model M {
  a: int
  @rule(a > 0, "r") on doesNotExist
}`}, "m.eiche")
	diags := CheckExpressions(prog, nil)
	if len(diags) != 1 {
		t.Fatalf("want 1 diagnostic, got %d: %v", len(diags), diags)
	}
}

func TestCheckExprNoFieldReferencedIsAnError(t *testing.T) {
	prog := resolveOK(t, MapLoader{"m.eiche": `enum E { X }
model M {
  a: E
  @rule(X == X, "always true, references no field")
}`}, "m.eiche")
	diags := CheckExpressions(prog, nil)
	if len(diags) != 1 {
		t.Fatalf("want 1 diagnostic, got %d: %v", len(diags), diags)
	}
}
