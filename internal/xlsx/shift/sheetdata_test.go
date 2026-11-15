package shift

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/sheet"
	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// K7a and K7b: SheetData and PlanShared, pure (designer rows; frozen).

var own = formula.Sheets{Own: "Budget", Order: []string{"Budget", "Other"}}

func TestSheetData(t *testing.T) {
	xlsxtest.Skip(t, func() {
		for _, tc := range []struct {
			name, in, want string
			e              formula.Edit
			adjusted       int
		}{
			{"insert rows",
				`<sheetData><row r="1"><c r="A1"><f>A2+1</f></c></row><row r="2" ht="9"><c r="A2"><v>5</v></c></row></sheetData>`,
				`<sheetData><row r="1"><c r="A1"><f>A4+1</f></c></row><row r="4" ht="9"><c r="A4"><v>5</v></c></row></sheetData>`,
				formula.Edit{Sheet: "Budget", Kind: formula.InsertRows, At: 2, Count: 2}, 1},
			{"delete row",
				`<sheetData><row r="1"><c r="A1"><f>SUM(A2:A3)</f></c></row><row r="2"><c r="A2"><v>5</v></c></row><row r="3"><c r="A3"><v>6</v></c></row></sheetData>`,
				`<sheetData><row r="1"><c r="A1"><f>SUM(A2:A2)</f></c></row><row r="2"><c r="A2"><v>6</v></c></row></sheetData>`,
				formula.Edit{Sheet: "Budget", Kind: formula.DeleteRows, At: 2, Count: 1}, 1},
			{"insert column",
				`<sheetData><row r="1"><c r="A1"><v>1</v></c><c r="B1"><f>A1</f></c></row></sheetData>`,
				`<sheetData><row r="1"><c r="A1"><v>1</v></c><c r="C1"><f>A1</f></c></row></sheetData>`,
				formula.Edit{Sheet: "Budget", Kind: formula.InsertCols, At: 2, Count: 1}, 0},
		} {
			got, st, err := SheetData(tc.in, own, tc.e)
			if err != nil || got != tc.want || st.Adjusted != tc.adjusted {
				t.Errorf("%s:\n got  %s %+v %v\n want %s adjusted %d", tc.name, got, st, err, tc.want, tc.adjusted)
			}
		}
	})
}

func TestSheetDataRefusals(t *testing.T) {
	xlsxtest.Skip(t, func() {
		for _, tc := range []struct {
			name, in string
			e        formula.Edit
		}{
			{"cuts an array", `<sheetData><row r="1"><c r="A1"><f t="array" ref="A1:A3">B1:B3</f></c></row></sheetData>`,
				formula.Edit{Sheet: "Budget", Kind: formula.InsertRows, At: 2, Count: 1}},
			{"past the grid", `<sheetData><row r="1048576"><c r="A1048576"><v>1</v></c></row></sheetData>`,
				formula.Edit{Sheet: "Budget", Kind: formula.InsertRows, At: 1, Count: 1}},
		} {
			_, _, err := SheetData(tc.in, own, tc.e)
			var r *Refused
			if !errors.As(err, &r) {
				t.Errorf("%s: %v, want *Refused", tc.name, err)
			}
		}
		_, st, err := SheetData(`<sheetData><row r="1"><c r="A1"><f>A2</f></c></row><row r="2"/></sheetData>`, own,
			formula.Edit{Sheet: "Budget", Kind: formula.DeleteRows, At: 2, Count: 1})
		if err != nil || len(st.RefErrors) != 1 || st.RefErrors[0] != "A1" {
			t.Errorf("ref errors %+v %v", st, err)
		}
	})
}

// FuzzSheetData: an insert then the delete of what it inserted gives the
// input back when no formula gained #REF!, byte for byte up to the
// spelling of references (a moved reference is written in Excel's form:
// uppercase, no leading zeros in the row, so a hand-written "b10" comes
// back "B10" and "A01" comes back "A1"; formula.Shift keeps an unmoved
// reference's spelling, so only moved ones change, and the inverse
// cannot know how one was first spelled; a moved row's r is likewise
// written without leading zeros), and up to XML line-end
// normalisation (a raw CR or CRLF in text reads as LF, so a rewritten
// formula is written with LF); and nothing panics. The law holds for
// well-formed input only: on bytes an XML parser refuses (a bare "&")
// the shifter's lenient decoder may write a different spelling.
func FuzzSheetData(f *testing.F) {
	f.Add(`<sheetData><row r="1"><c r="A1"><f>A2+B3</f></c></row><row r="3"><c r="B3"><v>1</v></c></row></sheetData>`, 2, 1, false)
	f.Add("<sheetData><row><c><f>\rA1</f></c></row></sheetData>", 1, 145, false)
	f.Fuzz(func(t *testing.T, xml string, at, count int, cols bool) {
		xlsxtest.Skip(t, func() {
			if _, err := tree.ParseString(xml); err != nil {
				return
			}
			e := formula.Edit{Sheet: "Budget", Kind: formula.InsertRows, At: at, Count: count}
			if cols {
				e.Kind = formula.InsertCols
			}
			out, st, err := SheetData(xml, own, e)
			if err != nil || len(st.RefErrors) > 0 {
				return
			}
			back, _, err := SheetData(out, own, e.Inverse())
			if err == nil && !strings.EqualFold(lineEnds(refSpelling(back)), lineEnds(refSpelling(xml))) {
				t.Fatalf("insert then delete:\n%s\n%s", xml, back)
			}
		})
	})
}

// lineEnds applies XML line-end normalisation to s: CRLF and a lone CR
// become LF, as an XML processor reads them in text.
func lineEnds(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

// leadingZeros is a row number's leading zeros after a column name or
// at the start of a row's r attribute.
var leadingZeros = regexp.MustCompile(`([A-Za-z]\$?|\br=")0+([1-9])`)

// refSpelling drops the leading zeros of row numbers (A01 is A1, and a
// moved row's r="007" comes back r="7") for
// FuzzSheetData's comparison; text that only looks like a reference is
// changed alike on both sides.
func refSpelling(s string) string { return leadingZeros.ReplaceAllString(s, "$1$2") }

// formulasOf reads a rewritten sheetData back as the reader would: every
// cell's formula text, shared children written out from their master
// (formula.Translate), keyed by A1; and each array anchor's ref.
func formulasOf(t *testing.T, sd string) (map[string]string, map[string]string) {
	t.Helper()
	p := ""
	if strings.HasPrefix(sd, "<x:") {
		p = "x:"
	}
	decl := `xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"`
	if p != "" {
		decl = `xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main"`
	}
	doc := "<" + p + "worksheet " + decl + ">" + sd + "</" + p + "worksheet>"
	f, arrays := map[string]string{}, map[string]string{}
	_, err := sheet.Scan(doc, sheet.ScanOptions{From: 1, To: formula.MaxRows, Row: func(r *sheet.Row) error {
		for _, c := range r.Cells {
			key := formula.Cell{Row: c.Row, Col: c.Col}.String()
			switch {
			case c.F.Present && c.F.Text != "":
				f[key] = c.F.Text
				if c.F.Type == sheet.Array || c.F.Type == sheet.DataTable {
					arrays[key] = c.F.Ref
				}
			case c.F.Present && c.Master > 0:
				m := r.Index.Masters()[c.Master-1]
				text, err := formula.Translate(m.Text, formula.Cell{Row: m.Row, Col: m.Col}, formula.Cell{Row: c.Row, Col: c.Col})
				if err != nil {
					t.Fatalf("%s: %v", key, err)
				}
				f[key] = text
			case c.F.Present:
				f[key] = "(orphan)"
			}
		}
		return nil
	}})
	if err != nil {
		t.Fatalf("output does not scan: %v\n%s", err, sd)
	}
	return f, arrays
}

func TestSheetDataSemantics(t *testing.T) {
	shared := `<sheetData><row r="1"><c r="A1"><v>1</v></c><c r="B1"><f t="shared" ref="B1:B3" si="0">A1*2</f></c></row>` +
		`<row r="2"><c r="A2"><v>2</v></c><c r="B2"><f t="shared" si="0"/></c></row>` +
		`<row r="3"><c r="A3"><v>3</v></c><c r="B3"><f t="shared" si="0"/></c></row></sheetData>`
	below := `<sheetData><row r="1"><c r="B1"><f t="shared" ref="B1:B3" si="0">A2</f></c></row>` +
		`<row r="2"><c r="B2"><f t="shared" si="0"/></c></row><row r="3"><c r="B3"><f t="shared" si="0"/></c></row></sheetData>`
	array := `<sheetData><row r="3"><c r="A3"><f t="array" ref="A3:A4">B3:B4*2</f></c></row><row r="4"><c r="A4"><v>2</v></c></row></sheetData>`
	table := `<sheetData><row r="2"><c r="B2"><f t="dataTable" ref="B2:B3" dt2D="0" dtr="0" r1="A1"/></c></row></sheetData>`
	for _, tc := range []struct {
		name, in string
		e        formula.Edit
		want     map[string]string // cell -> formula text as read
		arrays   map[string]string // anchor -> ref
	}{
		{"shared kept, moved", shared, formula.Edit{Sheet: "Budget", Kind: formula.InsertRows, At: 1, Count: 1},
			map[string]string{"B2": "A2*2", "B3": "A3*2", "B4": "A4*2"}, nil},
		{"shared kept, grown", shared, formula.Edit{Sheet: "Budget", Kind: formula.InsertRows, At: 2, Count: 1},
			map[string]string{"B1": "A1*2", "B3": "A3*2", "B4": "A4*2"}, nil},
		{"shared split", below, formula.Edit{Sheet: "Budget", Kind: formula.InsertRows, At: 2, Count: 1},
			map[string]string{"B1": "A3", "B3": "A4", "B4": "A5"}, nil},
		{"shared master deleted", shared, formula.Edit{Sheet: "Budget", Kind: formula.DeleteRows, At: 1, Count: 1},
			map[string]string{"B1": "A1*2", "B2": "A2*2"}, nil},
		{"shared column delete", shared, formula.Edit{Sheet: "Budget", Kind: formula.DeleteCols, At: 1, Count: 1},
			map[string]string{"A1": "#REF!*2", "A2": "#REF!*2", "A3": "#REF!*2"}, nil},
		{"array moved whole", array, formula.Edit{Sheet: "Budget", Kind: formula.InsertRows, At: 1, Count: 1},
			map[string]string{"A4": "B4:B5*2"}, map[string]string{"A4": "A4:A5"}},
		{"array deleted whole", array, formula.Edit{Sheet: "Budget", Kind: formula.DeleteRows, At: 3, Count: 2},
			map[string]string{}, map[string]string{}},
		{"data table moved", table, formula.Edit{Sheet: "Budget", Kind: formula.InsertRows, At: 1, Count: 1},
			map[string]string{}, map[string]string{}},
		{"column delete clips", `<sheetData><row r="1"><c r="A1"><f>SUM(A2:D2)</f></c><c r="C1"><f>D1</f></c></row></sheetData>`,
			formula.Edit{Sheet: "Budget", Kind: formula.DeleteCols, At: 2, Count: 1},
			map[string]string{"A1": "SUM(A2:C2)", "B1": "C1"}, nil},
		{"prefixed sheet", `<x:sheetData><x:row r="2"><x:c r="A2"><x:f>A3</x:f></x:c></x:row></x:sheetData>`,
			formula.Edit{Sheet: "Budget", Kind: formula.InsertRows, At: 1, Count: 1}, map[string]string{"A3": "A4"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			xlsxtest.Skip(t, func() {
				out, _, err := SheetData(tc.in, own, tc.e)
				if err != nil {
					t.Fatal(err)
				}
				got, arrays := formulasOf(t, out)
				for k, v := range tc.want {
					if got[k] != v {
						t.Errorf("%s = %q, want %q\n%s", k, got[k], v, out)
					}
				}
				if len(got) != len(tc.want) && tc.name != "data table moved" {
					t.Errorf("formulas %v, want %v", got, tc.want)
				}
				for k, v := range tc.arrays {
					if arrays[k] != v {
						t.Errorf("array %s ref %q, want %q", k, arrays[k], v)
					}
				}
				if tc.name == "data table moved" && !strings.Contains(out, `<c r="B3"><f t="dataTable" ref="B3:B4" dt2D="0" dtr="0" r1="A2"/></c>`) {
					t.Errorf("data table: %s", out)
				}
			})
		})
	}
	xlsxtest.Skip(t, func() {
		_, _, err := SheetData(table, own, formula.Edit{Sheet: "Budget", Kind: formula.InsertRows, At: 3, Count: 1})
		var r *Refused
		if !errors.As(err, &r) {
			t.Errorf("a cut data table: %v", err)
		}
	})
}

func TestPlanShared(t *testing.T) {
	c := func(r, col int) formula.Cell { return formula.Cell{Row: r, Col: col} }
	g := SharedGroup{SI: 0, Master: c(1, 2), Text: "A2", Ref: formula.Range{Kind: formula.Cells, From: c(1, 2), To: c(3, 2)},
		Members: []formula.Cell{c(1, 2), c(2, 2), c(3, 2)}}
	xlsxtest.Skip(t, func() {
		got, _, err := PlanShared([]SharedGroup{g}, 5, own, formula.Edit{Sheet: "Budget", Kind: formula.InsertRows, At: 2, Count: 1})
		if err != nil {
			t.Fatal(err)
		}
		if b1 := got[c(1, 2)]; b1.Text != "A3" {
			t.Errorf("B1 %+v", b1)
		}
		b3 := got[c(3, 2)]
		if !b3.Shared || !b3.Master || b3.Text != "A4" || b3.Ref != "B3:B4" {
			t.Errorf("B3 %+v", b3)
		}
		if b4 := got[c(4, 2)]; !b4.Shared || b4.Master || b4.SI != b3.SI {
			t.Errorf("B4 %+v", b4)
		}
		if _, ok := got[c(2, 2)]; ok {
			t.Error("a member at its pre-edit cell")
		}
	})
}
