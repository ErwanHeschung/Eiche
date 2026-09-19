package rules

import (
	"testing"

	"eiche/internal/core"
	"eiche/internal/rules/bytecode"
)

const jobOfferSrc = `capability phone from "eiche-phone@1.2"

enum ContractType { CDI, CDD, ALTERNANCE, STAGE }

model JobOffer {
  title:        string
  contractType: ContractType
  endDate?:     date
  salaryMin:    int
  salaryMax:    int
  hours:        decimal
  contactPhone: string

  @rule(salaryMax >= salaryMin, "salary.range") on salaryMax
  @rule(contractType == CDD implies endDate != null, "cdd.endDate") on endDate
  @rule(phone::isValid(contactPhone, "FR"), "phone.notFrench") on contactPhone
}
`

func compiledJobOffer(t *testing.T) *bytecode.Program {
	t.Helper()
	prog, diags := core.ResolveImports(core.MapLoader{"main.eiche": jobOfferSrc}, "main.eiche")
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

type stubPhoneInvoker struct{}

func (stubPhoneInvoker) Call(capability, fn string, args []bytecode.Value) (bool, error) {
	if capability != "phone" || fn != "isValid" {
		return false, nil
	}
	// A tiny stand-in for the real capability: "valid" iff it starts with "+33".
	num := args[0]
	return len(num.Str) >= 3 && num.Str[:3] == "+33", nil
}

func TestCompileAndEvalValidPayload(t *testing.T) {
	prog := compiledJobOffer(t)
	input, err := bytecode.ParseInput([]byte(`{
		"title": "Backend Engineer",
		"contractType": "CDI",
		"endDate": null,
		"salaryMin": 45000,
		"salaryMax": 60000,
		"hours": "37.5",
		"contactPhone": "+33612345678"
	}`))
	if err != nil {
		t.Fatalf("ParseInput: %v", err)
	}

	errs, err := bytecode.Eval(prog, input, stubPhoneInvoker{})
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("valid payload: unexpected errors %+v", errs)
	}
}

func TestCompileAndEvalSalaryRangeViolation(t *testing.T) {
	prog := compiledJobOffer(t)
	input, err := bytecode.ParseInput([]byte(`{
		"title": "Backend Engineer",
		"contractType": "CDI",
		"endDate": null,
		"salaryMin": 80000,
		"salaryMax": 60000,
		"hours": "37.5",
		"contactPhone": "+33612345678"
	}`))
	if err != nil {
		t.Fatalf("ParseInput: %v", err)
	}

	errs, err := bytecode.Eval(prog, input, stubPhoneInvoker{})
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(errs) != 1 || errs[0].Path != "salaryMax" || errs[0].Code != "salary.range" {
		t.Fatalf("want exactly the salary.range error, got %+v", errs)
	}
}

// TestCDDRequiresEndDate is the case that drove the null-check exemption
// in markFieldUsage: "contractType == CDD implies endDate != null" must
// actually fire when endDate is null, not be skipped because it
// references a null field.
func TestCDDRequiresEndDate(t *testing.T) {
	prog := compiledJobOffer(t)
	input, err := bytecode.ParseInput([]byte(`{
		"title": "Backend Engineer",
		"contractType": "CDD",
		"endDate": null,
		"salaryMin": 45000,
		"salaryMax": 60000,
		"hours": "37.5",
		"contactPhone": "+33612345678"
	}`))
	if err != nil {
		t.Fatalf("ParseInput: %v", err)
	}

	errs, err := bytecode.Eval(prog, input, stubPhoneInvoker{})
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(errs) != 1 || errs[0].Path != "endDate" || errs[0].Code != "cdd.endDate" {
		t.Fatalf("want exactly the cdd.endDate error, got %+v", errs)
	}
}

func TestCDDWithEndDateIsFine(t *testing.T) {
	prog := compiledJobOffer(t)
	input, err := bytecode.ParseInput([]byte(`{
		"title": "Backend Engineer",
		"contractType": "CDD",
		"endDate": "2027-01-01",
		"salaryMin": 45000,
		"salaryMax": 60000,
		"hours": "37.5",
		"contactPhone": "+33612345678"
	}`))
	if err != nil {
		t.Fatalf("ParseInput: %v", err)
	}

	errs, err := bytecode.Eval(prog, input, stubPhoneInvoker{})
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("valid CDD with endDate: unexpected errors %+v", errs)
	}
}

func TestCapabilityCallViolation(t *testing.T) {
	prog := compiledJobOffer(t)
	input, err := bytecode.ParseInput([]byte(`{
		"title": "Backend Engineer",
		"contractType": "CDI",
		"endDate": null,
		"salaryMin": 45000,
		"salaryMax": 60000,
		"hours": "37.5",
		"contactPhone": "+14155552671"
	}`))
	if err != nil {
		t.Fatalf("ParseInput: %v", err)
	}

	errs, err := bytecode.Eval(prog, input, stubPhoneInvoker{})
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(errs) != 1 || errs[0].Path != "contactPhone" || errs[0].Code != "phone.notFrench" {
		t.Fatalf("want exactly the phone.notFrench error, got %+v", errs)
	}
}

func TestMultipleViolationsAllReported(t *testing.T) {
	prog := compiledJobOffer(t)
	input, err := bytecode.ParseInput([]byte(`{
		"title": "Backend Engineer",
		"contractType": "CDD",
		"endDate": null,
		"salaryMin": 80000,
		"salaryMax": 60000,
		"hours": "37.5",
		"contactPhone": "+14155552671"
	}`))
	if err != nil {
		t.Fatalf("ParseInput: %v", err)
	}

	errs, err := bytecode.Eval(prog, input, stubPhoneInvoker{})
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(errs) != 3 {
		t.Fatalf("want all 3 rules to fail independently, got %d: %+v", len(errs), errs)
	}
}

func TestSelectorExprRejectedAtCompileTime(t *testing.T) {
	src := `model M {
  meta: json
  @rule(meta.foo == "x", "r")
}`
	prog, diags := core.ResolveImports(core.MapLoader{"m.eiche": src}, "m.eiche")
	if len(diags) != 0 {
		t.Fatalf("resolve: %v", diags)
	}
	if diags := core.CheckExpressions(prog, nil); len(diags) != 0 {
		t.Fatalf("CheckExpressions: %v", diags)
	}
	ir := core.BuildIR(prog)
	_, err := Compile(&ir.Models[0])
	if err == nil {
		t.Fatal("want a compile error for nested field access")
	}
}
