package ops_test

import (
	"errors"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/edit"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/numfmt"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
	"github.com/jhnoel/documentdriver/internal/xlsx/sheet"
	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// The ops kernels' semantics (designer rows; frozen).

func TestPlanCell(t *testing.T) {
	area := func(r1, c1, r2, c2 int) formula.Range {
		return formula.Range{Kind: formula.Cells, From: formula.Cell{Row: r1, Col: c1}, To: formula.Cell{Row: r2, Col: c2}}
	}
	base := ops.CellFacts{Row: 2, Col: 2, Style: 3, NumFmt: numfmt.General, HasStyles: true}
	for _, tc := range []struct {
		name string
		f    func(ops.CellFacts) ops.CellFacts
		v    ops.CellValue
		want ops.CellWrite
		code string
	}{
		{"number", nil, ops.Num(2), ops.CellWrite{Style: 3, Content: edit.Content{Kind: edit.ContentNumber, Num: 2}}, ""},
		{"empty string", nil, ops.Str(""), ops.CellWrite{Style: 3, Content: edit.Content{Kind: edit.ContentString}}, ""},
		{"null clears", nil, ops.Null(), ops.CellWrite{Clear: true, Style: 3}, ""},
		{"date gets 14", nil, ops.DateOf("2024-03-01"), ops.CellWrite{Style: 3, DateFormat: 14, Content: edit.Content{Kind: edit.ContentNumber, Num: 45352}}, ""},
		{"datetime gets 22", nil, ops.DateOf("2024-03-01T12:00:00"), ops.CellWrite{Style: 3, DateFormat: 22, Content: edit.Content{Kind: edit.ContentNumber, Num: 45352.5}}, ""},
		{"time gets 21", nil, ops.TimeOf("06:00:00"), ops.CellWrite{Style: 3, DateFormat: 21, Content: edit.Content{Kind: edit.ContentNumber, Num: 0.25}}, ""},
		{"date keeps a date format", func(f ops.CellFacts) ops.CellFacts { f.NumFmt = numfmt.DateTime; return f },
			ops.DateOf("2024-03-01"), ops.CellWrite{Style: 3, Content: edit.Content{Kind: edit.ContentNumber, Num: 45352}}, ""},
		{"1904", func(f ops.CellFacts) ops.CellFacts { f.Date1904 = true; return f },
			ops.DateOf("1904-01-02"), ops.CellWrite{Style: 3, DateFormat: 14, Content: edit.Content{Kind: edit.ContentNumber, Num: 1}}, ""},
		{"formula stored", nil, ops.Fx("concat(A1,B1)"), ops.CellWrite{Style: 3, Content: edit.Content{Kind: edit.ContentFormula, Formula: "_xlfn.concat(A1,B1)"}}, ""},
		{"shared master", func(f ops.CellFacts) ops.CellFacts {
			f.Exists, f.Raw = true, sheet.RawCell{Row: 2, Col: 2, F: sheet.Formula{Present: true, Type: sheet.Shared, Text: "A2", SIOK: true}}
			return f
		}, ops.Num(1), ops.CellWrite{Style: 3, Unshare: true, Content: edit.Content{Kind: edit.ContentNumber, Num: 1}}, ""},
		{"array member", func(f ops.CellFacts) ops.CellFacts {
			f.Anchors = []sheet.Anchor{{Row: 1, Col: 2, Ref: area(1, 2, 3, 2), Type: sheet.Array}}
			return f
		}, ops.Num(1), ops.CellWrite{}, cli.CodeOperationFailed},
		{"table header", func(f ops.CellFacts) ops.CellFacts {
			f.Tables = []edit.TableRef{{Name: "T", Ref: area(2, 1, 5, 3), HeaderRows: 1}}
			return f
		}, ops.Num(1), ops.CellWrite{}, cli.CodeOperationFailed},
		{"table body", func(f ops.CellFacts) ops.CellFacts {
			f.Tables = []edit.TableRef{{Name: "T", Ref: area(1, 1, 5, 3), HeaderRows: 1}}
			return f
		}, ops.Num(1), ops.CellWrite{Style: 3, Content: edit.Content{Kind: edit.ContentNumber, Num: 1}}, ""},
		{"date without styles", func(f ops.CellFacts) ops.CellFacts { f.HasStyles = false; return f },
			ops.DateOf("2024-03-01"), ops.CellWrite{}, cli.CodeUnreadableTarget},
	} {
		t.Run(tc.name, func(t *testing.T) {
			xlsxtest.Skip(t, func() {
				f := base
				if tc.f != nil {
					f = tc.f(f)
				}
				got, err := ops.PlanCell(f, tc.v)
				if tc.code != "" {
					var r *ops.Rejection
					if !errors.As(err, &r) || r.Code != tc.code || r.Field != "" {
						t.Errorf("got %v, want %s", err, tc.code)
					}
					return
				}
				if err != nil || got != tc.want {
					t.Errorf("got %+v %v, want %+v", got, err, tc.want)
				}
			})
		})
	}
}

func TestSetColumns(t *testing.T) {
	w20, hide, s5 := 20.0, true, 5
	for _, tc := range []struct {
		cols     string
		from, to int
		width    *float64
		hidden   *bool
		style    *int
		want     string
	}{
		{"", 3, 4, &w20, nil, nil, `<cols><col min="3" max="4" width="20" customWidth="1"/></cols>`},
		{`<cols><col min="1" max="5" width="9" customWidth="1"/></cols>`, 3, 4, &w20, nil, nil,
			`<cols><col min="1" max="2" width="9" customWidth="1"/><col min="3" max="4" width="20" customWidth="1"/><col min="5" max="5" width="9" customWidth="1"/></cols>`},
		{`<cols><col min="2" max="2" width="9" style="4"/></cols>`, 2, 2, nil, &hide, nil, `<cols><col min="2" max="2" width="9" style="4" hidden="1"/></cols>`},
		{`<cols><col min="2" max="2" width="9"/></cols>`, 4, 4, nil, &hide, nil, `<cols><col min="2" max="2" width="9"/><col min="4" max="4" hidden="1"/></cols>`},
		{`<cols><col min="1" max="3" width="9"/></cols>`, 2, 2, nil, nil, &s5, `<cols><col min="1" max="1" width="9"/><col min="2" max="2" width="9" style="5"/><col min="3" max="3" width="9"/></cols>`},
	} {
		xlsxtest.Skip(t, func() {
			got, err := ops.SetColumns(tc.cols, "", tc.from, tc.to, tc.width, tc.hidden, tc.style)
			if err != nil || got != tc.want {
				t.Errorf("SetColumns(%s, %d, %d):\n got  %s %v\n want %s", tc.cols, tc.from, tc.to, got, err, tc.want)
			}
		})
	}
}

func TestMergeKernel(t *testing.T) {
	xlsxtest.Skip(t, func() {
		got, err := ops.AddMerge("", "", "A1:C1")
		if err != nil || got != `<mergeCells count="1"><mergeCell ref="A1:C1"/></mergeCells>` {
			t.Errorf("AddMerge to none: %s %v", got, err)
		}
		got, err = ops.AddMerge(got, "", "A2:B3")
		if err != nil || got != `<mergeCells count="2"><mergeCell ref="A1:C1"/><mergeCell ref="A2:B3"/></mergeCells>` {
			t.Errorf("AddMerge: %s %v", got, err)
		}
		var r *ops.Rejection
		if _, err := ops.AddMerge(got, "", "B1:B2"); !errors.As(err, &r) || r.Code != cli.CodeOperationFailed {
			t.Errorf("overlap: %v", err)
		}
		if got, err := ops.RemoveMerge(`<mergeCells count="1"><mergeCell ref="A1:C1"/></mergeCells>`, "A1:C1"); err != nil || got != "" {
			t.Errorf("RemoveMerge to empty: %q %v", got, err)
		}
		if _, err := ops.RemoveMerge(`<mergeCells count="1"><mergeCell ref="A1:C1"/></mergeCells>`, "A1:B1"); !errors.As(err, &r) || r.Code != cli.CodeNotFound {
			t.Errorf("missing merge: %v", err)
		}
	})
}

func TestRenameKernel(t *testing.T) {
	xlsxtest.Skip(t, func() {
		src := `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row r="1">` +
			`<c r="A1"><f>Budget!A1+budget!B2</f></c><c r="B1"><f>"Budget!A1"</f></c><c r="C1"><f>SUM(Other!A1)</f></c></row></sheetData></worksheet>`
		got, err := ops.RenameInSheet(src, "Budget", "Q1 Budget")
		if err != nil || len(got) != 1 || got[0] != (ops.FormulaRename{Row: 1, Col: 1, Text: "'Q1 Budget'!A1+'Q1 Budget'!B2"}) {
			t.Errorf("RenameInSheet = %+v %v", got, err)
		}
		if s := ops.RenameText("Budget!A1", "Budget", "Q1"); s != "'Q1'!A1" { // Q1 looks like a cell, so it is quoted
			t.Errorf("RenameText %q", s)
		}
	})
}

func TestRenameGuardAndElement(t *testing.T) {
	xlsxtest.Skip(t, func() {
		chart := `<c:chartSpace xmlns:c="http://schemas.openxmlformats.org/drawingml/2006/chart"><c:chart><c:plotArea><c:barChart><c:ser><c:val><c:numRef><c:f>Budget!$B$2:$B$5</c:f></c:numRef></c:val></c:ser></c:barChart></c:plotArea></c:chart></c:chartSpace>`
		var r *ops.Rejection
		if err := ops.RenameGuard(ops.RenameParts{Charts: []string{chart}}, "budget"); !errors.As(err, &r) || r.Code != cli.CodeOperationFailed || r.Detail == "" {
			t.Errorf("chart naming the sheet: %v", err)
		}
		if err := ops.RenameGuard(ops.RenameParts{Charts: []string{chart}}, "Other"); err != nil {
			t.Errorf("chart naming another sheet: %v", err)
		}
		pivot := `<pivotCacheDefinition xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><cacheSource type="worksheet"><worksheetSource ref="A1:B9" sheet="Budget"/></cacheSource></pivotCacheDefinition>`
		if err := ops.RenameGuard(ops.RenameParts{PivotCaches: []string{pivot}}, "Budget"); !errors.As(err, &r) {
			t.Errorf("pivot cache: %v", err)
		}
	})
	xlsxtest.Skip(t, func() {
		for _, tc := range []struct{ local, in, want string }{
			{"hyperlinks", `<hyperlinks><hyperlink ref="A1" location="Budget!A1" display="x"/></hyperlinks>`, `<hyperlinks><hyperlink ref="A1" location="'Q1 Budget'!A1" display="x"/></hyperlinks>`},
			{"dataValidations", `<dataValidations count="1"><dataValidation sqref="A1"><formula1>Budget!$A$1:$A$9</formula1></dataValidation></dataValidations>`,
				`<dataValidations count="1"><dataValidation sqref="A1"><formula1>'Q1 Budget'!$A$1:$A$9</formula1></dataValidation></dataValidations>`},
			{"conditionalFormatting", `<conditionalFormatting sqref="A1"><cfRule type="expression" priority="1"><formula>Budget!A1&gt;0</formula></cfRule></conditionalFormatting>`,
				`<conditionalFormatting sqref="A1"><cfRule type="expression" priority="1"><formula>'Q1 Budget'!A1&gt;0</formula></cfRule></conditionalFormatting>`},
			{"mergeCells", `<mergeCells count="1"><mergeCell ref="A1:B1"/></mergeCells>`, `<mergeCells count="1"><mergeCell ref="A1:B1"/></mergeCells>`},
		} {
			got, _, err := ops.RenameElement(tc.local, tc.in, "Budget", "Q1 Budget")
			if err != nil || got != tc.want {
				t.Errorf("RenameElement(%s):\n got  %s %v\n want %s", tc.local, got, err, tc.want)
			}
		}
	})
}
