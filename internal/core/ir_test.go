package core

import (
	"encoding/json"
	"testing"
)

const briefJobOffer = `capability phone   from "eiche-phone@1.2"
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
}

view JobOfferSummary = JobOffer { title, contractType, salaryMin }
`

func buildIR(t *testing.T) *IR {
	t.Helper()
	prog := resolveOK(t, MapLoader{"main.eiche": briefJobOffer}, "main.eiche")
	if diags := CheckTypes(prog); len(diags) != 0 {
		t.Fatalf("CheckTypes: %v", diags)
	}
	if diags := CheckExpressions(prog, nil); len(diags) != 0 {
		t.Fatalf("CheckExpressions: %v", diags)
	}
	return BuildIR(prog)
}

func TestBuildIRStructure(t *testing.T) {
	ir := buildIR(t)

	if ir.Version != IRVersion {
		t.Fatalf("Version = %d, want %d", ir.Version, IRVersion)
	}
	if len(ir.Capabilities) != 2 || ir.Capabilities[0].Name != "payroll" || ir.Capabilities[1].Name != "phone" {
		t.Fatalf("Capabilities = %+v, want sorted [payroll, phone]", ir.Capabilities)
	}
	if len(ir.Enums) != 1 || ir.Enums[0].Name != "ContractType" || len(ir.Enums[0].Members) != 4 {
		t.Fatalf("Enums = %+v", ir.Enums)
	}
	// Enum member order is declaration order, not sorted.
	want := []string{"CDI", "CDD", "ALTERNANCE", "STAGE"}
	for i, m := range want {
		if ir.Enums[0].Members[i] != m {
			t.Fatalf("member %d = %q, want %q", i, ir.Enums[0].Members[i], m)
		}
	}

	if len(ir.Models) != 1 {
		t.Fatalf("Models = %+v", ir.Models)
	}
	model := ir.Models[0]
	if model.Name != "JobOffer" || len(model.Fields) != 10 || len(model.Rules) != 3 {
		t.Fatalf("model = %+v", model)
	}

	skills := model.Fields[7]
	if skills.Name != "skills" || !skills.Type.IsArray {
		t.Fatalf("skills = %+v", skills)
	}
	if len(skills.Type.ElemConstraints) != 1 || skills.Type.ElemConstraints[0].Name != "minLength" {
		t.Fatalf("skills elem constraints = %+v", skills.Type.ElemConstraints)
	}
	if len(skills.Annotations) != 1 || skills.Annotations[0].Name != "minItems" {
		t.Fatalf("skills annotations = %+v", skills.Annotations)
	}

	rule := model.Rules[0]
	if rule.On != "salaryMax" || rule.Message != "salary.range" {
		t.Fatalf("rule = %+v", rule)
	}
	if rule.Expr.Kind != ExprBinary || rule.Expr.Op != ">=" {
		t.Fatalf("rule.Expr = %+v", rule.Expr)
	}

	capCall := model.Rules[2].Expr
	if capCall.Kind != ExprCapabilityCall || capCall.Name != "isValid" || len(capCall.Args) != 2 {
		t.Fatalf("capability call expr = %+v", capCall)
	}
	if capCall.X.Kind != ExprIdent || capCall.X.Name != "phone" {
		t.Fatalf("capability call receiver = %+v", capCall.X)
	}

	if len(ir.Views) != 1 || ir.Views[0].Name != "JobOfferSummary" || len(ir.Views[0].Fields) != 3 {
		t.Fatalf("Views = %+v", ir.Views)
	}
}

func TestIRJSONRoundTrip(t *testing.T) {
	ir := buildIR(t)

	data, err := json.Marshal(ir)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var back IR
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	data2, err := json.Marshal(&back)
	if err != nil {
		t.Fatalf("re-Marshal: %v", err)
	}
	if string(data) != string(data2) {
		t.Fatalf("round-trip not stable:\nfirst:  %s\nsecond: %s", data, data2)
	}
}

func TestIRBuildIsDeterministic(t *testing.T) {
	prog := resolveOK(t, MapLoader{"main.eiche": briefJobOffer}, "main.eiche")
	CheckTypes(prog)
	CheckExpressions(prog, nil)

	a, err := json.Marshal(BuildIR(prog))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for i := 0; i < 5; i++ {
		b, err := json.Marshal(BuildIR(prog))
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if string(a) != string(b) {
			t.Fatalf("BuildIR is not deterministic across repeated calls (map iteration order leaking through)")
		}
	}
}
