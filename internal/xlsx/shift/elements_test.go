package shift

import (
	"errors"
	"testing"

	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// K8a, K8b, K8c: Element, ShiftCols, ShiftSqref and the CF and DV
// rewrites, pure (designer rows; frozen). K8a also adds
// TestElementTableComplete in its own elements_chunk_test.go: every
// x:CT_Worksheet child but sheetData is classified in elementRules.

func TestElement(t *testing.T) {
	ins := formula.Edit{Sheet: "Budget", Kind: formula.InsertRows, At: 2, Count: 1}
	del := formula.Edit{Sheet: "Budget", Kind: formula.DeleteRows, At: 2, Count: 1}
	for _, tc := range []struct {
		local, in, want string
		e               formula.Edit
	}{
		{"dimension", `<dimension ref="A1:C5"/>`, `<dimension ref="A1:C6"/>`, ins},
		{"mergeCells", `<mergeCells count="2"><mergeCell ref="A1:B1"/><mergeCell ref="A2:B2"/></mergeCells>`, `<mergeCells count="1"><mergeCell ref="A1:B1"/></mergeCells>`, del},
		{"mergeCells", `<mergeCells count="1"><mergeCell ref="A2:B2"/></mergeCells>`, ``, del},
		{"conditionalFormatting", `<conditionalFormatting sqref="A1:A5"><cfRule type="expression" priority="1"><formula>A3&gt;0</formula></cfRule></conditionalFormatting>`,
			`<conditionalFormatting sqref="A1:A6"><cfRule type="expression" priority="1"><formula>A4&gt;0</formula></cfRule></conditionalFormatting>`, ins},
		{"dataValidations", `<dataValidations count="1"><dataValidation sqref="B3"><formula1>$A$1:$A$9</formula1></dataValidation></dataValidations>`,
			`<dataValidations count="1"><dataValidation sqref="B4"><formula1>$A$1:$A$10</formula1></dataValidation></dataValidations>`, ins},
		{"hyperlinks", `<hyperlinks><hyperlink ref="A3" r:id="rId1"/></hyperlinks>`, `<hyperlinks><hyperlink ref="A4" r:id="rId1"/></hyperlinks>`, ins},
		{"autoFilter", `<autoFilter ref="A1:C9"/>`, `<autoFilter ref="A1:C8"/>`, del},
		{"sheetViews", `<sheetViews><sheetView workbookViewId="0"><selection activeCell="B3" sqref="B3"/></sheetView></sheetViews>`,
			`<sheetViews><sheetView workbookViewId="0"><selection activeCell="B4" sqref="B4"/></sheetView></sheetViews>`, ins},
		{"rowBreaks", `<rowBreaks count="1" manualBreakCount="1"><brk id="5" max="16383" man="1"/></rowBreaks>`,
			`<rowBreaks count="1" manualBreakCount="1"><brk id="6" max="16383" man="1"/></rowBreaks>`, ins},
		{"pageMargins", `<pageMargins left="0.7" right="0.7" top="0.75" bottom="0.75" header="0.3" footer="0.3"/>`,
			`<pageMargins left="0.7" right="0.7" top="0.75" bottom="0.75" header="0.3" footer="0.3"/>`, ins},
	} {
		t.Run(tc.local, func(t *testing.T) {
			xlsxtest.Skip(t, func() {
				got, _, err := Element(tc.local, tc.in, own, tc.e)
				if err != nil || got != tc.want {
					t.Errorf("Element(%s):\n got  %s %v\n want %s", tc.local, got, err, tc.want)
				}
			})
		})
	}
}

func TestElementMore(t *testing.T) {
	insC := formula.Edit{Sheet: "Budget", Kind: formula.InsertCols, At: 2, Count: 1}
	delC := formula.Edit{Sheet: "Budget", Kind: formula.DeleteCols, At: 2, Count: 1}
	ins1 := formula.Edit{Sheet: "Budget", Kind: formula.InsertRows, At: 1, Count: 1}
	del3 := formula.Edit{Sheet: "Budget", Kind: formula.DeleteRows, At: 3, Count: 1}
	for _, tc := range []struct {
		name, local, in, want string
		e                     formula.Edit
	}{
		{"cols grow", "cols", `<cols><col min="1" max="3" width="9"/></cols>`, `<cols><col min="1" max="4" width="9"/></cols>`, insC},
		{"cols move", "cols", `<cols><col min="3" max="3" width="9"/></cols>`, `<cols><col min="4" max="4" width="9"/></cols>`, insC},
		{"cols shrink", "cols", `<cols><col min="1" max="3" width="9"/></cols>`, `<cols><col min="1" max="2" width="9"/></cols>`, delC},
		{"cols removed", "cols", `<cols><col min="2" max="2" width="9"/></cols>`, ``, delC},
		{"cols on a row edit", "cols", `<cols><col min="2" max="2" width="9"/></cols>`, `<cols><col min="2" max="2" width="9"/></cols>`, ins1},
		{"colBreaks", "colBreaks", `<colBreaks count="1" manualBreakCount="1"><brk id="3" max="1048575" man="1"/></colBreaks>`,
			`<colBreaks count="1" manualBreakCount="1"><brk id="4" max="1048575" man="1"/></colBreaks>`, insC},
		{"pane", "sheetViews", `<sheetViews><sheetView workbookViewId="0"><pane ySplit="2" topLeftCell="A3" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews>`,
			`<sheetViews><sheetView workbookViewId="0"><pane ySplit="2" topLeftCell="A4" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews>`, ins1},
		{"sqref cell removed", "conditionalFormatting", `<conditionalFormatting sqref="A1 A3 B2:B4"><cfRule type="expression" priority="1"><formula>TRUE</formula></cfRule></conditionalFormatting>`,
			`<conditionalFormatting sqref="A1 B2:B3"><cfRule type="expression" priority="1"><formula>TRUE</formula></cfRule></conditionalFormatting>`, del3},
		{"sqref emptied", "conditionalFormatting", `<conditionalFormatting sqref="A3"><cfRule type="expression" priority="1"><formula>TRUE</formula></cfRule></conditionalFormatting>`, ``, del3},
		{"dv removed, count", "dataValidations", `<dataValidations count="2"><dataValidation sqref="A3"/><dataValidation sqref="B5"/></dataValidations>`,
			`<dataValidations count="1"><dataValidation sqref="B4"/></dataValidations>`, del3},
		{"protectedRanges before", "protectedRanges", `<protectedRanges><protectedRange sqref="A1" name="p"/></protectedRanges>`,
			`<protectedRanges><protectedRange sqref="A1" name="p"/></protectedRanges>`, del3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			xlsxtest.Skip(t, func() {
				got, _, err := Element(tc.local, tc.in, own, tc.e)
				if err != nil || got != tc.want {
					t.Errorf("Element(%s):\n got  %s %v\n want %s", tc.local, got, err, tc.want)
				}
			})
		})
	}
	x14 := `<extLst><ext uri="{78C0D931-6437-407d-A8EE-F0AAD7539E65}" xmlns:x14="http://schemas.microsoft.com/office/spreadsheetml/2009/9/main">` +
		`<x14:conditionalFormattings><x14:conditionalFormatting xmlns:xm="http://schemas.microsoft.com/office/excel/2006/main"><xm:sqref>A5</xm:sqref></x14:conditionalFormatting></x14:conditionalFormattings></ext></extLst>`
	for _, tc := range []struct{ local, in string }{
		{"extLst", x14},
		{"protectedRanges", `<protectedRanges><protectedRange sqref="A5" name="p"/></protectedRanges>`},
	} {
		xlsxtest.Skip(t, func() {
			_, _, err := Element(tc.local, tc.in, own, ins1)
			var r *Refused
			if !errors.As(err, &r) {
				t.Errorf("%s at or after the point: %v, want *Refused", tc.local, err)
			}
		})
	}
	xlsxtest.Skip(t, func() {
		if got, _, err := Element("extLst", x14, own, formula.Edit{Sheet: "Budget", Kind: formula.InsertRows, At: 9, Count: 1}); err != nil || got != x14 {
			t.Errorf("extLst before the point: %v", err)
		}
	})
}

func TestShiftColsAndSqref(t *testing.T) {
	xlsxtest.Skip(t, func() {
		if got, err := ShiftCols(`<cols><col min="2" max="5" width="9"/></cols>`, formula.Edit{Sheet: "Budget", Kind: formula.DeleteCols, At: 3, Count: 2}); err != nil || got != `<cols><col min="2" max="3" width="9"/></cols>` {
			t.Errorf("ShiftCols = %s %v", got, err)
		}
	})
	xlsxtest.Skip(t, func() {
		for _, tc := range []struct {
			in, want string
			e        formula.Edit
		}{
			{"A1 A3 B2:B4", "A1 B2:B3", formula.Edit{Sheet: "Budget", Kind: formula.DeleteRows, At: 3, Count: 1}},
			{"A1:A9", "A1:A10", formula.Edit{Sheet: "Budget", Kind: formula.InsertRows, At: 5, Count: 1}},
			{"C1", "", formula.Edit{Sheet: "Budget", Kind: formula.DeleteCols, At: 3, Count: 1}},
		} {
			if got, err := ShiftSqref(tc.in, tc.e); err != nil || got != tc.want {
				t.Errorf("ShiftSqref(%q) = %q %v, want %q", tc.in, got, err, tc.want)
			}
		}
	})
}
