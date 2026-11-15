package formula

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateAccepts(t *testing.T) {
	for _, s := range []string{
		"1", " SUM( A1 , B2 ) ", "-2^-2%", "IF(A1,,B1)", "F()", "F(,)", "A1:B2 B1:C3",
		"(A1,B1)", "Sheet1!$A$1:$B$2,Sheet1!$D$1", "SUM((A1,B1))", "{1,2;3,4}", "{-1;\"x\";TRUE;#N/A}",
		"A1:OFFSET(A1,1,1)", "'My Sheet'!A1&\"x\"", "Sales[[#This Row],[Qty]]*2", "T[]", "T[#All]",
		"T[[#Headers],[A]:[B]]", "_xlfn.SINGLE(A1:A3)", "_xlfn.ANCHORARRAY(A1)", "[1]Sheet1!A1+[1]!Rate",
		"Sheet2!#REF!", "#REF!", "A1=B1", "A1<>B1", "1E+5*.5", "\"a\"\"b\"", "_xlfn.LET(_xlpm.x,1,_xlpm.x+1)",
		"A1\n+B1", "TRUE", "Sheet1:Sheet3!A1", "!A1",
	} {
		if err := Validate(s); err != nil {
			t.Errorf("Validate(%q) = %v", s, err)
		}
	}
}

func TestValidateRefuses(t *testing.T) {
	for s, reason := range map[string]string{
		"":           "empty formula",
		"   ":        "empty formula",
		"=A1":        "expected an operand",
		"A1 +":       "end of formula",
		"SUM(A1":     "missing",
		"(A1":        "missing",
		"A1)":        "unexpected",
		"A1 B1 C1 (": "end of formula",
		"1 2":        "",
		"A1B1(":      "",
		"\"abc":      "unterminated string",
		"'x'":        "quoted sheet name",
		"{1,2;3}":    "differ in length",
		"{1,,3}":     "not a constant",
		"{A1}":       "not a constant",
		"{}":         "not a constant",
		"A1#":        "display form",
		"@A1":        "display form",
		"T[@Qty]":    "display form",
		"T[#Bogus]":  "unknown table keyword",
		"T[[A],]":    "ends without an item",
		"1 % 2":      "",
		"A1++":       "end of formula",
		"SUM(1,2))":  "unexpected",
		"Sheet1!":    "",
		"*1":         "expected an operand",
	} {
		err := Validate(s)
		var r *Refusal
		if !errors.As(err, &r) {
			t.Errorf("Validate(%q) = %v, want a refusal", s, err)
			continue
		}
		if !strings.Contains(r.Reason, reason) {
			t.Errorf("Validate(%q) reason %q, want it to contain %q", s, r.Reason, reason)
		}
	}
}

// TestValidateDepth checks that deep nesting is refused with a reason and
// bounded recursion, for parentheses and calls alike, and that nesting up
// to the limit is accepted.
func TestValidateDepth(t *testing.T) {
	ok := strings.Repeat("(", MaxDepth) + "1" + strings.Repeat(")", MaxDepth)
	if err := Validate(ok); err != nil {
		t.Fatalf("depth %d refused: %v", MaxDepth, err)
	}
	for _, s := range []string{
		strings.Repeat("(", 1_000_000) + "1" + strings.Repeat(")", 1_000_000),
		strings.Repeat("F(", 1_000_000) + "1" + strings.Repeat(")", 1_000_000),
		strings.Repeat("(", MaxDepth+1) + "1" + strings.Repeat(")", MaxDepth+1),
		strings.Repeat("-", 1_000_000) + "1",
	} {
		err := Validate(s)
		if strings.HasPrefix(s, "-") {
			if err != nil {
				t.Errorf("unary chain refused: %v", err)
			}
			continue
		}
		var r *Refusal
		if !errors.As(err, &r) || !strings.Contains(r.Reason, "nesting deeper") {
			t.Errorf("deep nesting: %v", err)
		}
	}
}
