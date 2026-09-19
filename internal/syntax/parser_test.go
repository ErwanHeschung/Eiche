package syntax

import "testing"

func mustParse(t *testing.T, src string) *File {
	t.Helper()
	f, errs := ParseFile("test.eiche", src)
	if len(errs) != 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}
	return f
}

func TestParseBriefExample(t *testing.T) {
	src := `import "./common.eiche"
capability phone   from "eiche-phone@1.2"
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

  @rule(salaryMax >= salaryMin, "salary.range") on salaryMax
  @rule(contractType == CDD implies endDate != null, "cdd.endDate") on endDate
  @rule(phone::isValid(contactPhone, "FR"), "phone.notFrench") on contactPhone
  @rule(payroll::isCoherent(contractType, salaryMin, salaryMax, hours),
        "payroll.incoherent") on salaryMin
}`
	f := mustParse(t, src)

	if len(f.Imports) != 1 || f.Imports[0].Path != "./common.eiche" {
		t.Fatalf("imports = %+v", f.Imports)
	}
	// 2 capabilities + 1 enum + 1 model
	if len(f.Decls) != 4 {
		t.Fatalf("want 4 decls, got %d: %+v", len(f.Decls), f.Decls)
	}
	model, ok := f.Decls[3].(*ModelDecl)
	if !ok || model.Name != "JobOffer" {
		t.Fatalf("decl 3 = %+v, want ModelDecl JobOffer", f.Decls[3])
	}
	if len(model.Fields) != 9 {
		t.Fatalf("want 9 fields, got %d: %+v", len(model.Fields), model.Fields)
	}
	if len(model.Rules) != 4 {
		t.Fatalf("want 4 rules, got %d", len(model.Rules))
	}
	for i, want := range []string{"salaryMax", "endDate", "contactPhone", "salaryMin"} {
		if model.Rules[i].On != want {
			t.Fatalf("rule %d attaches to %q, want %q", i, model.Rules[i].On, want)
		}
	}
}

func TestParseDecls(t *testing.T) {
	src := `capability phone from "eiche-phone@1.2"

enum ContractType { CDI, CDD }

model JobOffer {
  title: string @minLength(3)
  endDate?: date
  skills: string[@minLength(2)] @minItems(1)
  metadata: json

  @rule(title != null, "title.required")
}

view JobOfferPatch = partial JobOffer
`
	f := mustParse(t, src)
	if len(f.Decls) != 4 {
		t.Fatalf("want 4 decls, got %d: %+v", len(f.Decls), f.Decls)
	}

	cap, ok := f.Decls[0].(*CapabilityDecl)
	if !ok || cap.Name != "phone" || cap.From != "eiche-phone@1.2" {
		t.Fatalf("capability = %+v", f.Decls[0])
	}

	enum, ok := f.Decls[1].(*EnumDecl)
	if !ok || enum.Name != "ContractType" || len(enum.Members) != 2 {
		t.Fatalf("enum = %+v", f.Decls[1])
	}

	model, ok := f.Decls[2].(*ModelDecl)
	if !ok {
		t.Fatalf("decl 2 is not a ModelDecl: %+v", f.Decls[2])
	}
	if len(model.Fields) != 4 {
		t.Fatalf("want 4 fields, got %d: %+v", len(model.Fields), model.Fields)
	}
	if len(model.Rules) != 1 {
		t.Fatalf("want 1 rule, got %d", len(model.Rules))
	}
	if model.Fields[1].Name != "endDate" || !model.Fields[1].Optional {
		t.Fatalf("endDate field = %+v", model.Fields[1])
	}
	skills := model.Fields[2]
	if !skills.Type.IsArray || len(skills.Type.ElemAnnotations) != 1 || skills.Type.ElemAnnotations[0].Name != "minLength" {
		t.Fatalf("skills type = %+v", skills.Type)
	}
	if len(skills.Annotations) != 1 || skills.Annotations[0].Name != "minItems" {
		t.Fatalf("skills annotations = %+v", skills.Annotations)
	}

	if _, ok := f.Decls[3].(*ViewDecl); !ok {
		t.Fatalf("decl 3 = %+v, want ViewDecl", f.Decls[3])
	}
}

func TestParseViews(t *testing.T) {
	f := mustParse(t, `model M { a: string }
view P = partial M
view S = M { a }
view F = M`)
	p := f.Decls[1].(*ViewDecl)
	if !p.Partial || p.Base != "M" {
		t.Fatalf("partial view = %+v", p)
	}
	s := f.Decls[2].(*ViewDecl)
	if s.Partial || s.Base != "M" || len(s.Fields) != 1 || s.Fields[0] != "a" {
		t.Fatalf("projection view = %+v", s)
	}
	full := f.Decls[3].(*ViewDecl)
	if full.Partial || full.Base != "M" || len(full.Fields) != 0 {
		t.Fatalf("full view = %+v", full)
	}
}

func TestParseUnknownFieldsMarker(t *testing.T) {
	f := mustParse(t, `model M {
  a: string
  ...
}`)
	m := f.Decls[0].(*ModelDecl)
	if !m.AllowUnknownFields {
		t.Fatal("AllowUnknownFields = false, want true")
	}
	if len(m.Fields) != 1 {
		t.Fatalf("fields = %+v", m.Fields)
	}
}

func TestParseJSONShape(t *testing.T) {
	f := mustParse(t, `model M {
  answers: json {
    source: string
    consent: bool
    ...
  }
}`)
	m := f.Decls[0].(*ModelDecl)
	shape := m.Fields[0].Type.JSONShape
	if shape == nil {
		t.Fatal("JSONShape is nil")
	}
	if len(shape.Fields) != 2 || !shape.AllowUnknown {
		t.Fatalf("shape = %+v", shape)
	}
}

func TestParseDefaultValue(t *testing.T) {
	f := mustParse(t, `enum Status { DRAFT, PUBLISHED }
model M {
  status?: Status = DRAFT
}`)
	m := f.Decls[1].(*ModelDecl)
	field := m.Fields[0]
	if field.Default == nil {
		t.Fatal("Default is nil")
	}
	id, ok := field.Default.(*IdentExpr)
	if !ok || id.Name != "DRAFT" {
		t.Fatalf("default = %+v", field.Default)
	}
}

func TestDefaultOnRequiredFieldIsAnError(t *testing.T) {
	_, errs := ParseFile("t.eiche", `model M { status: string = "x" }`)
	if len(errs) != 1 {
		t.Fatalf("want 1 error, got %d: %v", len(errs), errs)
	}
}

func TestImpliesIsRightAssociativeAndLowestPrecedence(t *testing.T) {
	f := mustParse(t, `model M {
  a: bool
  @rule(a == true implies a != false, "r")
}`)
	rule := f.Decls[0].(*ModelDecl).Rules[0]
	top, ok := rule.Expr.(*BinaryExpr)
	if !ok || top.Op != IMPLIES {
		t.Fatalf("top expr = %+v, want IMPLIES", rule.Expr)
	}
	if _, ok := top.X.(*BinaryExpr); !ok {
		t.Fatalf("implies.X = %+v, want a BinaryExpr (a == true)", top.X)
	}
	if _, ok := top.Y.(*BinaryExpr); !ok {
		t.Fatalf("implies.Y = %+v, want a BinaryExpr (a != false)", top.Y)
	}
}

func TestNullSugar(t *testing.T) {
	f := mustParse(t, `model M {
  a: string
  @rule(a.isNull, "r1")
  @rule(a.isPresent, "r2")
}`)
	rules := f.Decls[0].(*ModelDecl).Rules
	isNull := rules[0].Expr.(*BinaryExpr)
	if isNull.Op != EQ {
		t.Fatalf("isNull desugars to %v, want EQ", isNull.Op)
	}
	if _, ok := isNull.Y.(*NullLitExpr); !ok {
		t.Fatalf("isNull.Y = %+v, want NullLitExpr", isNull.Y)
	}
	isPresent := rules[1].Expr.(*BinaryExpr)
	if isPresent.Op != NEQ {
		t.Fatalf("isPresent desugars to %v, want NEQ", isPresent.Op)
	}
}

func TestCapabilityCallExpr(t *testing.T) {
	f := mustParse(t, `capability phone from "p@1"
model M {
  contactPhone: string
  @rule(phone::isValid(contactPhone, "FR"), "r")
}`)
	rule := f.Decls[1].(*ModelDecl).Rules[0]
	call, ok := rule.Expr.(*CapabilityCallExpr)
	if !ok {
		t.Fatalf("rule expr = %+v, want CapabilityCallExpr", rule.Expr)
	}
	if call.Func != "isValid" || len(call.Args) != 2 {
		t.Fatalf("call = %+v", call)
	}
	recv, ok := call.X.(*IdentExpr)
	if !ok || recv.Name != "phone" {
		t.Fatalf("call.X = %+v", call.X)
	}
}

// TestErrorRecovery checks that a syntax error inside one declaration does
// not prevent later, independent declarations from parsing successfully —
// exactly the property the brief calls out as worth budgeting real time for.
func TestErrorRecovery(t *testing.T) {
	src := `model Broken {
  a: @@@ string
}

model Fine {
  b: string
}`
	f, errs := ParseFile("t.eiche", src)
	if len(errs) == 0 {
		t.Fatal("expected at least one error from the broken model")
	}
	if len(f.Decls) != 2 {
		t.Fatalf("want both decls recovered, got %d: %+v", len(f.Decls), f.Decls)
	}
	fine, ok := f.Decls[1].(*ModelDecl)
	if !ok || fine.Name != "Fine" || len(fine.Fields) != 1 {
		t.Fatalf("second model not recovered cleanly: %+v", f.Decls[1])
	}
}

func TestDiagnosticRender(t *testing.T) {
	src := "model Broken {\n  a: @@@ string\n}\n"
	_, errs := ParseFile("t.eiche", src)
	if len(errs) == 0 {
		t.Fatal("expected an error")
	}
	rendered := errs[0].Render(src)
	if rendered == "" {
		t.Fatal("Render returned empty string")
	}
}
