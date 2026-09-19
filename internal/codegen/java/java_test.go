package java

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

func TestEmitEnum(t *testing.T) {
	ir := buildCheckedIR(t)
	files := Emit(ir, Options{Package: "com.example.jobs"})

	src, ok := files["ContractType.java"]
	if !ok {
		t.Fatalf("files = %v, missing ContractType.java", keysOf(files))
	}
	for _, want := range []string{
		"package com.example.jobs;",
		"public enum ContractType {",
		"CDI,", "CDD,", "ALTERNANCE,", "STAGE",
		"}",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("ContractType.java missing %q:\n%s", want, src)
		}
	}
}

func TestEmitModelRecord(t *testing.T) {
	ir := buildCheckedIR(t)
	files := Emit(ir, Options{Package: "com.example.jobs"})

	src, ok := files["JobOffer.java"]
	if !ok {
		t.Fatalf("files = %v, missing JobOffer.java", keysOf(files))
	}

	for _, want := range []string{
		"package com.example.jobs;",
		"import com.fasterxml.jackson.annotation.JsonProperty;",
		"import com.fasterxml.jackson.databind.JsonNode;",
		"import jakarta.annotation.Nullable;",
		"import java.math.BigDecimal;",
		"import java.time.LocalDate;",
		"import java.util.List;",
		"public record JobOffer(",
		`@JsonProperty("title") String title`,
		"ContractType contractType", // enum reference: no import, same package
		`@JsonProperty("startDate") LocalDate startDate`,
		`@Nullable @JsonProperty("endDate") LocalDate endDate`,
		"int salaryMin", // required int: primitive, unboxed
		"BigDecimal hours",
		"List<String> skills",
		"JsonNode metadata",
		") {}",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("JobOffer.java missing %q:\n%s", want, src)
		}
	}

	// datetime isn't used by this model but Instant must never be imported
	// unless a datetime field is actually present, to keep unused imports
	// (which some javac configurations reject) out of generated code.
	if strings.Contains(src, "import java.time.Instant;") {
		// jobOfferSrc has no datetime field — Instant must not be imported.
		t.Fatalf("unexpected Instant import with no datetime field:\n%s", src)
	}
}

func TestEmitViewPartial(t *testing.T) {
	ir := buildCheckedIR(t)
	files := Emit(ir, Options{})
	src := files["JobOfferPatch.java"]

	// Every field becomes nullable under "partial", including ones that
	// are required on the base model (e.g. title, salaryMin).
	for _, want := range []string{
		`@Nullable @JsonProperty("title") String title`,
		`@Nullable @JsonProperty("salaryMin") Integer salaryMin`,
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("JobOfferPatch.java missing %q:\n%s", want, src)
		}
	}
}

func TestEmitViewProjection(t *testing.T) {
	ir := buildCheckedIR(t)
	files := Emit(ir, Options{})
	src := files["JobOfferSummary.java"]

	for _, want := range []string{"title", "contractType", "salaryMin"} {
		if !strings.Contains(src, want) {
			t.Fatalf("JobOfferSummary.java missing field %q:\n%s", want, src)
		}
	}
	for _, notWant := range []string{"endDate", "hours", "skills", "metadata", "contactPhone"} {
		if strings.Contains(src, notWant) {
			t.Fatalf("JobOfferSummary.java should not contain projected-out field %q:\n%s", notWant, src)
		}
	}
}

func TestEmitViewFull(t *testing.T) {
	ir := buildCheckedIR(t)
	files := Emit(ir, Options{})
	src := files["JobOfferCreate.java"]

	// A bare "view X = Base" carries every base field at its original
	// optionality: salaryMin required, endDate nullable.
	if !strings.Contains(src, "int salaryMin") {
		t.Fatalf("JobOfferCreate.java: salaryMin should stay required:\n%s", src)
	}
	if !strings.Contains(src, `@Nullable @JsonProperty("endDate")`) {
		t.Fatalf("JobOfferCreate.java: endDate should stay nullable:\n%s", src)
	}
}

func keysOf(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
