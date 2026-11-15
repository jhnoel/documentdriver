package read

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/xlsx/model"
	"github.com/jhnoel/documentdriver/internal/xlsx/sheet"
)

// probeConvert needs a real Book: newConverter may load the parts.
func probeConvert(t *testing.T) func() {
	return func() {
		b, err := openBook(fx{sheets: []fxSheet{{name: "S"}}}.zip(t), Options{})
		if err == nil {
			newConverter(b, "S")
			b.Close()
		}
	}
}

// convertSheet converts every cell of a fixture sheet with the
// converter alone (openBook, SheetText, sheet.Scan, entry), without
// resolve or Cells, so chunk 3 is checked against the same expectation
// tables as the cell reads of chunk 4.
func convertSheet(t *testing.T, f fx, name string, view model.View) (map[string]string, []model.Note) {
	t.Helper()
	b, err := openBook(f.zip(t), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	sh, ok := b.Sheet(name)
	if !ok {
		t.Fatalf("no sheet %s", name)
	}
	src, err := b.SheetText(sh)
	if err != nil {
		t.Fatal(err)
	}
	c := newConverter(b, sh.Name)
	out := map[string]string{}
	_, err = sheet.Scan(src, sheet.ScanOptions{From: 1, To: 1 << 20, Row: func(r *sheet.Row) error {
		c.ix = r.Index
		for i := range r.Cells {
			rc := &r.Cells[i]
			e, ok := c.entry(rc)
			if !ok || !e.Shown(view) {
				continue
			}
			out[model.CellKey(rc.Row, rc.Col)] = string(e.AppendJSON(nil, view))
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	return out, b.Notes.List()
}

func TestConvert(t *testing.T) {
	skipIfUnimplemented(t, probeConvert(t))
	for _, tc := range []struct {
		name  string
		f     fx
		sheet string
		view  model.View
		want  map[string]string
	}{
		{"values", valuesBook(), "Values", model.ViewCells, valuesWant},
		{"formulas", formulasBook(), "Calc", model.ViewCells, formulasWant},
		{"spill cells", spillBook(), "Spill", model.ViewCells, spillWantCells},
		{"spill values", spillBook(), "Spill", model.ViewValues, spillWantValues},
		{"spill formulas", spillBook(), "Spill", model.ViewFormulas, spillWantFormulas},
		{"reused si", reusedSIBook(), "R", model.ViewFormulas, map[string]string{
			"A1": `"=X1+1"`, "B1": `"=Y1+1"`, "C1": `"=Z1*2"`, "D1": `"=AA1*2"`, "A2": `"=X2*2"`,
		}},
	} {
		got, notes := convertSheet(t, tc.f, tc.sheet, tc.view)
		for k, w := range tc.want {
			if got[k] != w {
				t.Errorf("%s: %s = %s, want %s", tc.name, k, got[k], w)
			}
		}
		for k, g := range got {
			if _, ok := tc.want[k]; !ok {
				t.Errorf("%s: unexpected %s = %s", tc.name, k, g)
			}
		}
		if len(notes) == 0 && tc.name != "spill values" && tc.name != "spill formulas" && tc.name != "reused si" {
			t.Errorf("%s: no notes", tc.name)
		}
	}
}

// reusedSIBook reuses si 0 within one row: B1 is a child of A1's
// master, D1 of C1's, and A2 of C1's (the latest before it).
func reusedSIBook() fx {
	return fx{sheets: []fxSheet{{name: "R", rows: `<row r="1"><c r="A1"><f t="shared" ref="A1:B1" si="0">X1+1</f><v>0</v></c><c r="B1"><f t="shared" si="0"/><v>0</v></c>` +
		`<c r="C1"><f t="shared" ref="C1:D1" si="0">Z1*2</f><v>0</v></c><c r="D1"><f t="shared" si="0"/><v>0</v></c></row>` +
		`<row r="2"><c r="A2"><f t="shared" si="0"/><v>0</v></c></row>`}}}
}

// TestHiddenSerialParts: the format picks the form, and a part of the
// stored serial the form leaves out is said in a note (approver ruling,
// 2026-11-14): a time of day under a date format, whole days under a
// time format.
func TestHiddenSerialParts(t *testing.T) {
	skipIfUnimplemented(t, probeConvert(t))
	f := valueStyles(fx{sheets: []fxSheet{{name: "H", rows: fmt.Sprintf(
		`<row r="1"><c r="A1" s="%d"><v>45352.5</v></c><c r="B1" s="%d"><v>1.25</v></c><c r="C1" s="%d"><v>45352</v></c><c r="D1" s="%d"><v>0.25</v></c></row>`,
		xfDate, xfTime, xfDate, xfTime)}}})
	got, notes := convertSheet(t, f, "H", model.ViewCells)
	want := map[string]string{"A1": `{"date":"2024-03-01"}`, "B1": `{"time":"06:00:00"}`, "C1": `{"date":"2024-03-01"}`, "D1": `{"time":"06:00:00"}`}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %s, want %s", k, got[k], w)
		}
	}
	var dateOnly, timeOnly int
	for _, n := range notes {
		switch {
		case strings.Contains(n.Reason, "the format shows the date only"):
			dateOnly++
			if !strings.Contains(n.At, "A1") || strings.Contains(n.At, "C1") {
				t.Errorf("date-only note at %q", n.At)
			}
		case strings.Contains(n.Reason, "the format shows the time only"):
			timeOnly++
			if !strings.Contains(n.At, "B1") || strings.Contains(n.At, "D1") {
				t.Errorf("time-only note at %q", n.At)
			}
		}
	}
	if dateOnly != 1 || timeOnly != 1 {
		t.Errorf("notes %+v", notes)
	}
}
