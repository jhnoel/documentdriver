package integrity

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// The scoped check and gate (designer-owned; these pass). The corpus
// half of TestGateMatchesFullCheck is verification chunk V's.

func scopedBook(t *testing.T, sheet2 string) []byte {
	t.Helper()
	b := xlsxtest.Book{Sheets: []xlsxtest.Sheet{
		{Name: "A", Rows: `<row r="1"><c r="A1" s="5"><v>1</v></c></row>`}, // a pre-existing style fault
		{Name: "B", Rows: sheet2},
	}}
	data, err := b.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

var bothSheets = Scope{Parts: []string{"/xl/worksheets/sheet1.xml", "/xl/worksheets/sheet2.xml"}}

func TestGateMatchesFullCheck(t *testing.T) {
	xlsxtest.Skip(t, func() {
		for _, data := range [][]byte{
			scopedBook(t, `<row r="1"><c r="A1"><v>1</v></c></row>`),
			scopedBook(t, `<row r="2"/><row r="1"/>`),
		} {
			full, scoped := Check(data), CheckScoped(data, bothSheets)
			if !slices.Equal(full.Kinds(), scoped.Kinds()) {
				t.Errorf("full %v, scoped %v", full.Kinds(), scoped.Kinds())
			}
			one := CheckScoped(data, Scope{Parts: []string{"/xl/worksheets/sheet2.xml"}})
			for _, k := range one.Kinds() {
				if k.Part == "/xl/worksheets/sheet1.xml" {
					t.Errorf("an out-of-scope sheet was checked: %v", k)
				}
				if full.Count(k) != one.Count(k) {
					t.Errorf("%v: full %d, scoped %d", k, full.Count(k), one.Count(k))
				}
			}
		}
		// Every fixture of this package's own tests, if any are on disk.
		files, _ := filepath.Glob(filepath.Join("testdata", "*.xlsx"))
		for _, f := range files {
			data, err := os.ReadFile(f) //nolint:gosec // test fixture
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(Check(data).Kinds(), CheckScoped(data, everything(t, data)).Kinds()) {
				t.Errorf("%s: scoped check with every part differs from Check", f)
			}
		}
	})
}

// everything is a scope holding every part of data.
func everything(t *testing.T, data []byte) Scope {
	t.Helper()
	parts, err := xlsxtest.Parts(data)
	if err != nil {
		t.Fatal(err)
	}
	var s Scope
	for name := range parts {
		s.Parts = append(s.Parts, "/"+name)
	}
	return s
}

func TestGateScoped(t *testing.T) {
	xlsxtest.Skip(t, func() {
		in := scopedBook(t, `<row r="1"><c r="A1"><v>1</v></c></row>`)
		// The pre-existing fault in sheet A never blocks an edit of B.
		ok := scopedBook(t, `<row r="1"><c r="A1"><v>2</v></c></row>`)
		after, err := GateScoped(in, ok, Scope{Parts: []string{"/xl/worksheets/sheet2.xml"}})
		if err != nil || after == nil {
			t.Fatalf("gate refused a clean edit: %v", err)
		}
		// A fault introduced in B is refused.
		bad := scopedBook(t, `<row r="1"><c r="A1" s="7"><v>2</v></c></row>`)
		_, err = GateScoped(in, bad, Scope{Parts: []string{"/xl/worksheets/sheet2.xml"}})
		var g *GateFailure
		if !errors.As(err, &g) || len(g.Introduced) != 1 || g.Introduced[0].Kind != WorksheetStyle {
			t.Errorf("gate: %v", err)
		}
	})
}

// TestScopedTableBringsSheet: a table in scope is checked with its sheet.
func TestScopedTableBringsSheet(t *testing.T) {
	b := xlsxtest.Book{Sheets: []xlsxtest.Sheet{{Name: "A",
		Rows:   `<row r="1"><c r="A1" t="inlineStr"><is><t>X</t></is></c></row>`,
		Tables: []string{`<table id="1" name="T" displayName="T" ref="A1:A2"><tableColumns count="1"><tableColumn id="1" name="Y"/></tableColumns></table>`}}}}
	data, err := b.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	got := CheckScoped(data, Scope{Parts: []string{"/xl/tables/table1.xml"}})
	if !got.HasKind(TableHeaderMismatch) {
		t.Errorf("table in scope, its header mismatch not found: %v", got.Kinds())
	}
}
