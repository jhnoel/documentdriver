package ops_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

func TestSheetRenameChunkReferences(t *testing.T) {
	b := xlsxtest.Book{DefinedNames: `<definedName name="Local" localSheetId="0">Budget!A1</definedName>`, Sheets: []xlsxtest.Sheet{
		{Name: "Budget", Rows: `<row r="1"><c r="A1"><f t="shared" si="0" ref="A1:A2">Budget!B1</f><v>1</v></c></row>`},
		{Name: "Other", Tail: `<conditionalFormatting sqref="A1"><cfRule type="expression" priority="1"><formula>Budget!A1</formula></cfRule></conditionalFormatting><conditionalFormatting sqref="B1"><cfRule type="expression" priority="2"><formula>Budget!B1</formula></cfRule></conditionalFormatting><dataValidations count="1"><dataValidation sqref="A1"><formula1>Budget!A1</formula1></dataValidation></dataValidations><hyperlinks><hyperlink ref="A1" location="Budget!A1"/></hyperlinks>`},
	}}
	r := runOps(t, b, &ops.SheetRename{At: ops.AtSheet("sheet:budget"), Name: "New Name"}).ok()
	if got := r.sheet(1); !strings.Contains(got, `<f t="shared" si="0" ref="A1:A2">'New Name'!B1</f>`) {
		t.Errorf("shared formula: %s", got)
	}
	got := r.sheet(2)
	if strings.Count(got, "'New Name'!") != 4 {
		t.Errorf("worksheet references: %s", got)
	}
	if len(r.env[0].Renamed) != 1 || r.env[0].Renamed[0] != [2]string{"Budget", "New Name"} {
		t.Errorf("renamed: %v", r.env[0].Renamed)
	}
	if len(r.env[0].Changed) != 2 || r.env[0].Changed[0].FormulasAdjusted != 1 || r.env[0].Changed[1].FormulasAdjusted != 3 {
		t.Errorf("changed: %+v", r.env[0].Changed)
	}
}

func TestSheetRenameChunkGuard(t *testing.T) {
	for _, tc := range []struct{ name, typ, xml string }{
		{"chart", ctypes.CTDrawingmlChart, `<chart><f>Budget!A1</f></chart>`},
		{"pivot", ctypes.CTSpreadsheetmlPivotCacheDefinition, `<pivotCacheDefinition><worksheetSource sheet="Budget"/></pivotCacheDefinition>`},
		{"external", ctypes.CTSpreadsheetmlExternalLink, `<externalLink><sheetNames><sheetName val="Budget"/></sheetNames></externalLink>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := runOps(t, book("")).ok()
			if err := r.ws.AddPart("/xl/guard-"+tc.name+".xml", []byte(tc.xml), tc.typ); err != nil {
				t.Fatal(err)
			}
			env := &ops.Env{WS: r.ws}
			err := (&ops.SheetRename{At: ops.AtSheet("sheet:Budget"), Name: "New"}).Run(env)
			var reject *ops.Rejection
			if !errors.As(err, &reject) || reject.Code != cli.CodeOperationFailed || reject.Detail == "" {
				t.Fatalf("guard: %v", err)
			}
			if _, ok := r.ws.SheetRef("Budget"); !ok || len(env.Renamed) != 0 {
				t.Fatal("guard modified workbook")
			}
		})
	}
}

func TestSheetRenameChunkIdentityAndCase(t *testing.T) {
	b := book(`<row r="1"><c r="A1"><f>Budget!B1</f></c></row>`)
	r := runOps(t, b, &ops.SheetRename{At: ops.AtSheet("sheet:budget"), Name: "Budget"}).ok()
	if len(r.env[0].Renamed) != 0 || len(r.env[0].Changed) != 0 {
		t.Errorf("identity changed: %+v", r.env[0])
	}
	r = runOps(t, b, &ops.SheetRename{At: ops.AtSheet("sheet:Budget"), Name: "budget"}).ok()
	if len(r.env[0].Renamed) != 1 || !strings.Contains(r.sheet(1), "budget!B1") {
		t.Errorf("case rename: %+v %s", r.env[0], r.sheet(1))
	}
}
