package shift

import (
	"errors"
	"testing"

	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// K9a, K9b, K9c: OtherSheet, DefinedName, Table, Guard, pure (designer
// rows; frozen).

var insRow2 = formula.Edit{Sheet: "Budget", Kind: formula.InsertRows, At: 2, Count: 1}

func TestOtherSheet(t *testing.T) {
	xlsxtest.Skip(t, func() {
		src := `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>` +
			`<row r="1"><c r="A1"><f>Budget!A3*2</f><v>1</v></c><c r="B1"><f>A3</f></c><c r="C1"><f>'budget'!A1</f></c></row></sheetData></worksheet>`
		edits, st, err := OtherSheet(src, formula.Sheets{Own: "Other", Order: []string{"Budget", "Other"}}, insRow2)
		if err != nil || len(edits) != 1 || edits[0].Text != "Budget!A4*2" || st.Adjusted != 1 {
			t.Errorf("OtherSheet = %+v %+v %v", edits, st, err)
		}
		none, _, err := OtherSheet(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData/></worksheet>`, formula.Sheets{Own: "Other"}, insRow2)
		if err != nil || len(none) != 0 {
			t.Errorf("empty sheet: %v %v", none, err)
		}
	})
}

func TestDefinedName(t *testing.T) {
	xlsxtest.Skip(t, func() {
		got, refErr, err := DefinedName("Budget!$A$2:$A$9", formula.Sheets{Order: []string{"Budget"}}, insRow2)
		if err != nil || refErr || got != "Budget!$A$3:$A$10" {
			t.Errorf("DefinedName = %q %v %v", got, refErr, err)
		}
		_, refErr, _ = DefinedName("Budget!$A$2", formula.Sheets{Order: []string{"Budget"}}, insRow2.Inverse())
		if !refErr {
			t.Error("a deleted reference did not report #REF!")
		}
	})
}

func TestTable(t *testing.T) {
	xlsxtest.Skip(t, func() {
		tbl := `<table xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" id="1" name="T" displayName="T" ref="A1:B5"><autoFilter ref="A1:B5"/></table>`
		got, err := Table(tbl, insRow2)
		if err != nil || got != `<table xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" id="1" name="T" displayName="T" ref="A1:B6"><autoFilter ref="A1:B6"/></table>` {
			t.Errorf("Table = %s %v", got, err)
		}
		for _, e := range []formula.Edit{
			{Sheet: "Budget", Kind: formula.InsertCols, At: 2, Count: 1},
			{Sheet: "Budget", Kind: formula.DeleteRows, At: 1, Count: 1},
			{Sheet: "Budget", Kind: formula.DeleteRows, At: 2, Count: 4},
		} {
			var r *Refused
			if _, err := Table(tbl, e); !errors.As(err, &r) {
				t.Errorf("Table %+v: %v, want *Refused", e, err)
			}
		}
	})
}

func TestGuard(t *testing.T) {
	xlsxtest.Skip(t, func() {
		drawing := `<xdr:wsDr xmlns:xdr="http://schemas.openxmlformats.org/drawingml/2006/spreadsheetDrawing"><xdr:twoCellAnchor>` +
			`<xdr:from><xdr:col>0</xdr:col><xdr:colOff>0</xdr:colOff><xdr:row>4</xdr:row><xdr:rowOff>0</xdr:rowOff></xdr:from>` +
			`<xdr:to><xdr:col>3</xdr:col><xdr:colOff>0</xdr:colOff><xdr:row>9</xdr:row><xdr:rowOff>0</xdr:rowOff></xdr:to></xdr:twoCellAnchor></xdr:wsDr>`
		var r *Refused
		if err := Guard(Parts{Drawing: drawing}, "Budget", insRow2); !errors.As(err, &r) {
			t.Errorf("a drawing after the point: %v", err)
		}
		if err := Guard(Parts{Drawing: drawing}, "Budget", formula.Edit{Sheet: "Budget", Kind: formula.InsertRows, At: 20, Count: 1}); err != nil {
			t.Errorf("a drawing before the point: %v", err)
		}
		if err := Guard(Parts{SheetRelTypes: []string{"http://schemas.openxmlformats.org/officeDocument/2006/relationships/pivotTable"}}, "Budget", insRow2); !errors.As(err, &r) {
			t.Errorf("a pivot table: %v", err)
		}
	})
}
