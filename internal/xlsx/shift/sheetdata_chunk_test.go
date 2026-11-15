package shift

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
)

func TestSheetDataChunkPreservesBytes(t *testing.T) {
	// Unknown children and attributes are kept; only r and formula text move.
	in := `<sheetData><row r="1" ht="9" spans="1:1"><c r="A1" s="3"><v>1</v></c><c r="B1"><f>A2</f></c></row></sheetData>`
	got, st, err := SheetData(in, own, formula.Edit{Sheet: "Budget", Kind: formula.InsertRows, At: 2, Count: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `ht="9"`) || !strings.Contains(got, `spans="1:1"`) || !strings.Contains(got, `s="3"`) {
		t.Fatalf("attrs lost: %s", got)
	}
	// A2 is referenced from B1; insert at 2 moves it to A3.
	if !strings.Contains(got, `<f>A3</f>`) {
		t.Fatalf("ref not shifted: %s", got)
	}
	if st.Adjusted != 1 {
		t.Fatalf("adjusted %d", st.Adjusted)
	}
}

func TestSheetDataChunkColumnDeleteDropsCell(t *testing.T) {
	in := `<sheetData><row r="1"><c r="A1"><v>1</v></c><c r="B1"><v>2</v></c><c r="C1"><v>3</v></c></row></sheetData>`
	got, _, err := SheetData(in, own, formula.Edit{Sheet: "Budget", Kind: formula.DeleteCols, At: 2, Count: 1})
	if err != nil {
		t.Fatal(err)
	}
	want := `<sheetData><row r="1"><c r="A1"><v>1</v></c><c r="B1"><v>3</v></c></row></sheetData>`
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestSheetDataChunkEmpty(t *testing.T) {
	got, _, err := SheetData(`<sheetData/>`, own, formula.Edit{Sheet: "Budget", Kind: formula.InsertRows, At: 1, Count: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got != `<sheetData/>` {
		t.Fatalf("got %s", got)
	}
}

func TestSheetDataChunkImplicitCoords(t *testing.T) {
	// Rows and cells without r take the next free number. Both move
	// here, so both gain an explicit r (single-quoted: added by
	// SheetData, see insertR) and the formula (which Shift rewrites)
	// moves with them.
	in := `<sheetData><row><c><f>A2</f></c></row></sheetData>`
	got, _, err := SheetData(in, own, formula.Edit{Sheet: "Budget", Kind: formula.InsertRows, At: 1, Count: 1})
	if err != nil {
		t.Fatal(err)
	}
	want := `<sheetData><row r='2'><c r='A2'><f>A3</f></c></row></sheetData>`
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestSheetDataChunkImplicitCellsAndRows(t *testing.T) {
	// Implicit cells (A, B, C in one row) and implicit rows (1, 2, 3,
	// one cell each): an edit before, at, or after a position leaves
	// it implicit only when it does not move; a moved one gains r.
	cells := `<sheetData><row><c><v>1</v></c><c><v>2</v></c><c><v>3</v></c></row></sheetData>`
	rows := `<sheetData><row><c><v>1</v></c></row><row><c><v>2</v></c></row><row><c><v>3</v></c></row></sheetData>`
	edit := func(kind formula.EditKind, at int) formula.Edit {
		return formula.Edit{Sheet: "Budget", Kind: kind, At: at, Count: 1}
	}
	for _, tc := range []struct {
		name, in, want string
		e              formula.Edit
	}{
		{"insert cols at first cell", cells,
			`<sheetData><row><c r='B1'><v>1</v></c><c r='C1'><v>2</v></c><c r='D1'><v>3</v></c></row></sheetData>`, edit(formula.InsertCols, 1)},
		{"insert cols at second cell", cells,
			`<sheetData><row><c><v>1</v></c><c r='C1'><v>2</v></c><c r='D1'><v>3</v></c></row></sheetData>`, edit(formula.InsertCols, 2)},
		{"insert cols after last cell", cells, cells, edit(formula.InsertCols, 4)},
		{"delete cols at first cell", cells,
			`<sheetData><row><c r='A1'><v>2</v></c><c r='B1'><v>3</v></c></row></sheetData>`, edit(formula.DeleteCols, 1)},
		{"delete cols at second cell", cells,
			`<sheetData><row><c><v>1</v></c><c r='B1'><v>3</v></c></row></sheetData>`, edit(formula.DeleteCols, 2)},
		{"delete cols after last cell", cells, cells, edit(formula.DeleteCols, 4)},
		{"insert rows at first row", rows,
			`<sheetData><row r='2'><c r='A2'><v>1</v></c></row><row r='3'><c r='A3'><v>2</v></c></row><row r='4'><c r='A4'><v>3</v></c></row></sheetData>`, edit(formula.InsertRows, 1)},
		{"insert rows at second row", rows,
			`<sheetData><row><c><v>1</v></c></row><row r='3'><c r='A3'><v>2</v></c></row><row r='4'><c r='A4'><v>3</v></c></row></sheetData>`, edit(formula.InsertRows, 2)},
		{"insert rows after last row", rows, rows, edit(formula.InsertRows, 4)},
		{"delete rows at first row", rows,
			`<sheetData><row r='1'><c r='A1'><v>2</v></c></row><row r='2'><c r='A2'><v>3</v></c></row></sheetData>`, edit(formula.DeleteRows, 1)},
		{"delete rows at second row", rows,
			`<sheetData><row><c><v>1</v></c></row><row r='2'><c r='A2'><v>3</v></c></row></sheetData>`, edit(formula.DeleteRows, 2)},
		{"delete rows after last row", rows, rows, edit(formula.DeleteRows, 4)},
		{"implicit cell keeps its attributes", `<sheetData><row><c s="1"><v>1</v></c></row></sheetData>`,
			`<sheetData><row><c r='B1' s="1"><v>1</v></c></row></sheetData>`, edit(formula.InsertCols, 1)},
		{"implicit empty cell and row", `<sheetData><row><c/></row></sheetData>`,
			`<sheetData><row r='2'><c r='A2'/></row></sheetData>`, edit(formula.InsertRows, 1)},
		{"prefixed implicit cell", `<x:sheetData><x:row><x:c><v>1</v></x:c></x:row></x:sheetData>`,
			`<x:sheetData><x:row><x:c r='B1'><v>1</v></x:c></x:row></x:sheetData>`, edit(formula.InsertCols, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, _, err := SheetData(tc.in, own, tc.e)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
	// Insert followed by its inverse delete restores implicit rows
	// and cells, including the saved fuzz seed's shape.
	for _, tc := range []struct {
		name, in string
		e        formula.Edit
	}{
		{"cols at 2", `<sheetData><row><c></c><c></c></row></sheetData>`, edit(formula.InsertCols, 2)},
		{"cols at 1", `<sheetData><row><c></c><c></c></row></sheetData>`, edit(formula.InsertCols, 1)},
		{"rows at 1", rows, edit(formula.InsertRows, 1)},
		{"rows at 2", rows, edit(formula.InsertRows, 2)},
	} {
		t.Run("round trip "+tc.name, func(t *testing.T) {
			out, _, err := SheetData(tc.in, own, tc.e)
			if err != nil {
				t.Fatal(err)
			}
			back, _, err := SheetData(out, own, tc.e.Inverse())
			if err != nil {
				t.Fatal(err)
			}
			if back != tc.in {
				t.Fatalf("got %s want %s (via %s)", back, tc.in, out)
			}
		})
	}
}

func TestSheetDataChunkAbsoluteRefAndCDATA(t *testing.T) {
	// A moved cell keeps its $-flags; a CDATA formula is shifted and
	// written back as text.
	in := `<sheetData><row r="1"><c r="$A$1"><f><![CDATA[$A$2+1]]></f></c></row></sheetData>`
	got, _, err := SheetData(in, own, formula.Edit{Sheet: "Budget", Kind: formula.InsertRows, At: 1, Count: 1})
	if err != nil {
		t.Fatal(err)
	}
	want := `<sheetData><row r="2"><c r="$A$2"><f>$A$3+1</f></c></row></sheetData>`
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestSheetDataChunkInlineStringKept(t *testing.T) {
	in := `<sheetData><row r="1"><c r="A1"><is><t>x &amp; y</t></is></c><c r="B1"><v>2</v></c></row></sheetData>`
	got, _, err := SheetData(in, own, formula.Edit{Sheet: "Budget", Kind: formula.InsertCols, At: 1, Count: 1})
	if err != nil {
		t.Fatal(err)
	}
	want := `<sheetData><row r="1"><c r="B1"><is><t>x &amp; y</t></is></c><c r="C1"><v>2</v></c></row></sheetData>`
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestSheetDataChunkArrayColumnCut(t *testing.T) {
	in := `<sheetData><row r="1"><c r="A1"><f t="array" ref="A1:C1">B1:C1+1</f></c></row></sheetData>`
	_, _, err := SheetData(in, own, formula.Edit{Sheet: "Budget", Kind: formula.InsertCols, At: 2, Count: 1})
	if err == nil {
		t.Fatal("want refusal for array cut")
	}
}
