package ts

import (
	"strings"
	"testing"

	"eiche/internal/core"
)

const jobOfferSrc = `capability phone from "eiche-phone@1.2"

enum ContractType { CDI, CDD, ALTERNANCE, STAGE }

model JobOffer {
  title:        string @minLength(3)
  contractType: ContractType
  startDate:    date
  endDate?:     date
  salaryMin:    int
  salaryMax:    int
  hours:        decimal
  skills:       string[@minLength(2)]
  metadata:     json
  contactPhone: string

  @rule(salaryMax >= salaryMin, "salary.range") on salaryMax
}

view JobOfferPatch   = partial JobOffer
view JobOfferSummary = JobOffer { title, contractType, salaryMin }
view JobOfferCreate  = JobOffer
`

func buildCheckedIR(t *testing.T) *core.IR {
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
	if diags := core.CheckNaming(prog); len(diags) != 0 {
		t.Fatalf("CheckNaming: %v", diags)
	}
	return core.BuildIR(prog)
}

func TestEmitEnumUnionType(t *testing.T) {
	ir := buildCheckedIR(t)
	files := Emit(ir)

	src, ok := files["ContractType.ts"]
	if !ok {
		t.Fatalf("files = %v, missing ContractType.ts", keysOf(files))
	}
	for _, want := range []string{
		"export type ContractType =",
		`"CDI" |`, `"CDD" |`, `"ALTERNANCE" |`, `"STAGE";`,
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("ContractType.ts missing %q:\n%s", want, src)
		}
	}
}

func TestEmitInterface(t *testing.T) {
	ir := buildCheckedIR(t)
	files := Emit(ir)

	src, ok := files["JobOffer.ts"]
	if !ok {
		t.Fatalf("files = %v, missing JobOffer.ts", keysOf(files))
	}

	for _, want := range []string{
		`import type { ContractType } from "./ContractType";`,
		"export interface JobOffer {",
		"readonly title: string;",
		"readonly contractType: ContractType;",
		"readonly startDate: string;",
		"readonly endDate?: string;",
		"readonly salaryMin: number;",
		"readonly hours: string;", // decimal -> string, preserves precision
		"readonly skills: readonly string[];",
		"readonly metadata: unknown;",
		"}",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("JobOffer.ts missing %q:\n%s", want, src)
		}
	}
}

func TestEmitViewPartialAllOptional(t *testing.T) {
	ir := buildCheckedIR(t)
	files := Emit(ir)
	src := files["JobOfferPatch.ts"]

	for _, want := range []string{"readonly title?: string;", "readonly salaryMin?: number;"} {
		if !strings.Contains(src, want) {
			t.Fatalf("JobOfferPatch.ts missing %q:\n%s", want, src)
		}
	}
}

func TestEmitViewProjection(t *testing.T) {
	ir := buildCheckedIR(t)
	files := Emit(ir)
	src := files["JobOfferSummary.ts"]

	for _, want := range []string{"title", "contractType", "salaryMin"} {
		if !strings.Contains(src, want) {
			t.Fatalf("JobOfferSummary.ts missing field %q:\n%s", want, src)
		}
	}
	for _, notWant := range []string{"endDate", "hours", "skills", "metadata", "contactPhone"} {
		if strings.Contains(src, notWant) {
			t.Fatalf("JobOfferSummary.ts should not contain projected-out field %q:\n%s", notWant, src)
		}
	}
}

func TestEmitBarrelFile(t *testing.T) {
	ir := buildCheckedIR(t)
	files := Emit(ir)
	src, ok := files["index.ts"]
	if !ok {
		t.Fatalf("files = %v, missing index.ts", keysOf(files))
	}
	for _, want := range []string{
		`export * from "./ContractType";`,
		`export * from "./JobOffer";`,
		`export * from "./JobOfferPatch";`,
		`export * from "./JobOfferSummary";`,
		`export * from "./JobOfferCreate";`,
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("index.ts missing %q:\n%s", want, src)
		}
	}
}

func keysOf(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
