package formula

import "testing"

// benchFormula is a mid-sized cell formula of the kind the corpus holds.
const benchFormula = "IF(ISERROR(VLOOKUP($B12,'Price List'!$A$2:$F$500,MATCH(D$1,'Price List'!$A$1:$F$1,0),FALSE)),0,VLOOKUP($B12,'Price List'!$A$2:$F$500,MATCH(D$1,'Price List'!$A$1:$F$1,0),FALSE))*$C12+SUM(E2:E11)"

func BenchmarkLex(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		Lex(benchFormula)
	}
}

func BenchmarkValidate(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		if err := Validate(benchFormula); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkShift(b *testing.B) {
	sh := Sheets{Own: "Sheet1", Order: []string{"Sheet1", "Price List"}}
	e := Edit{Sheet: "Sheet1", Kind: InsertRows, At: 5, Count: 1}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Shift(benchFormula, sh, e); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTranslate(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Translate(benchFormula, Cell{Row: 12, Col: 4}, Cell{Row: 40, Col: 4}); err != nil {
			b.Fatal(err)
		}
	}
}
