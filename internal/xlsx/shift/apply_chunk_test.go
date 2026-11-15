package shift

import (
	"errors"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/xlsx/edit"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

func TestApplyChunkMissingSheet(t *testing.T) {
	b := xlsxtest.Book{Sheets: []xlsxtest.Sheet{{Name: "S", Rows: `<row r="1"><c r="A1"><v>1</v></c></row>`}}}
	path, err := b.Write(t.TempDir(), "in.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := opc.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := pkg.Close(); err != nil {
			t.Error(err)
		}
	})
	ws, err := edit.New(pkg)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Apply(ws, formula.Edit{Sheet: "missing", Kind: formula.InsertRows, At: 1, Count: 1})
	if !errors.Is(err, edit.ErrNotFound) {
		t.Fatalf("Apply: %v", err)
	}
	if len(ws.Touched()) != 0 {
		t.Fatalf("unexpected writes: %v", ws.Touched())
	}

	rels, err := pkg.Rels("/xl/worksheets/sheet1.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rels.Add(ctypes.RelPivotTable, "../pivotTables/pivot1.xml", false); err != nil {
		t.Fatal(err)
	}
	_, err = Apply(ws, formula.Edit{Sheet: "s", Kind: formula.InsertRows, At: 1, Count: 1})
	var refused *Refused
	if !errors.As(err, &refused) || refused.Construct != "a pivot table on S" {
		t.Fatalf("Apply guard: %v", err)
	}
	if len(ws.Touched()) != 0 || ws.RecalcRequested() {
		t.Fatalf("refusal wrote sheet: %v", ws.Touched())
	}
}
