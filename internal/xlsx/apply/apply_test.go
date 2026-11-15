package apply

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
	"github.com/jhnoel/documentdriver/internal/xlsx/read"
	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// End to end with the real gate and recalculation policy (needs K1-K6
// and G1; sequential cases need G5, G11, G12) (designer rows; frozen).

func e2eBook(t *testing.T) (string, []byte) {
	t.Helper()
	b := xlsxtest.Book{CalcPr: `<calcPr calcId="191029"/>`, CalcChain: `<c r="B1" i="1"/>`, SST: []string{"<t>Item</t>"},
		Sheets: []xlsxtest.Sheet{
			{Name: "Budget", Dimension: "A1:B1", Rows: `<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1"><f>2*2</f><v>4</v></c></row>`},
			{Name: "Other", Rows: `<row r="1"><c r="A1"><v>1</v></c></row>`},
		}}
	p, err := b.Write(t.TempDir(), "in.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p) //nolint:gosec // test input
	return p, data
}

func TestApplyCellSet(t *testing.T) {
	xlsxtest.Skip(t, func() {
		in, before := e2eBook(t)
		out := filepath.Join(t.TempDir(), "out.xlsx")
		res, err := Run(in, ops.NewPlan(&ops.CellSet{At: ops.AtCell("Budget!C1"), Value: ops.Fx("=B1+1")}), Options{Out: out})
		if err != nil {
			t.Fatal(err)
		}
		written, _ := os.ReadFile(out) //nolint:gosec // test output
		parts, _ := xlsxtest.Parts(written)
		inParts, _ := xlsxtest.Parts(before)
		// Untouched parts byte for byte; the sst never changes.
		for _, name := range []string{"xl/worksheets/sheet2.xml", "xl/sharedStrings.xml", "xl/styles.xml"} {
			if !bytes.Equal(parts[name], inParts[name]) {
				t.Errorf("%s changed", name)
			}
		}
		// Recalculation policy: <f> without <v>, fullCalcOnLoad, no calcChain.
		if !strings.Contains(string(parts["xl/worksheets/sheet1.xml"]), `<c r="C1"><f>B1+1</f></c>`) {
			t.Errorf("sheet1: %s", parts["xl/worksheets/sheet1.xml"])
		}
		if _, ok := parts["xl/calcChain.xml"]; ok {
			t.Error("calcChain kept after a formula write")
		}
		if !strings.Contains(string(parts["xl/workbook.xml"]), `fullCalcOnLoad="1"`) {
			t.Error("fullCalcOnLoad not set")
		}
		// Other cells keep their cached values.
		c, err := read.Cells(written, read.CellsRequest{Range: "Budget!A1:C1"}, read.Options{})
		if err != nil || !c.ValuesStale {
			t.Fatalf("read: %+v %v", c, err)
		}
		if len(res.Hazards) == 0 || res.Hazards[0].Kind != "stale-values" || !res.Hazards[0].New {
			t.Errorf("hazards %+v", res.Hazards)
		}
		if strings.Join(res.Parts, ",") != "/[Content_Types].xml,/xl/_rels/workbook.xml.rels,/xl/calcChain.xml,/xl/workbook.xml,/xl/worksheets/sheet1.xml" {
			t.Errorf("parts %v", res.Parts)
		}
	})
}

func TestApplyAtomicity(t *testing.T) {
	xlsxtest.Skip(t, func() {
		in, before := e2eBook(t)
		_, err := Run(in, ops.NewPlan(
			&ops.CellSet{At: ops.AtCell("Budget!C1"), Value: ops.Num(1)},
			&ops.CellSet{At: ops.AtCell("Nope!A1"), Value: ops.Num(1)}), Options{})
		var e *cli.Error
		if !errors.As(err, &e) || e.Code != cli.CodeNotFound || e.Path != "ops[1].at" {
			t.Errorf("got %v", err)
		}
		if got, _ := os.ReadFile(in); !bytes.Equal(got, before) { //nolint:gosec // test input
			t.Error("a failed plan changed the file")
		}
	})
}

func TestApplyIdentityIsNoOp(t *testing.T) {
	xlsxtest.Skip(t, func() {
		in, _ := e2eBook(t)
		out := filepath.Join(t.TempDir(), "out.xlsx")
		res, err := Run(in, ops.NewPlan(&ops.CellSet{At: ops.AtCell("Other!A1"), Value: ops.Num(1)}), Options{Out: out})
		if err != nil {
			t.Fatal(err)
		}
		if res.Saved != "" || len(res.Parts) != 0 {
			t.Errorf("identity write: %+v", res)
		}
		if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
			t.Error("an identity write wrote a file")
		}
	})
}

// TestApplySequential: ops see the document as earlier ops left it: a
// row insert renumbers what follows, a later cell.set at the shifted
// address hits the moved row, and an alias names the added sheet.
func TestApplySequential(t *testing.T) {
	xlsxtest.Skip(t, func() {
		in, _ := e2eBook(t)
		out := filepath.Join(t.TempDir(), "out.xlsx")
		p := ops.NewPlan(
			&ops.RowInsert{At: ops.AtRows("Budget!1:1"), Position: ops.Before},
			&ops.CellSet{At: ops.AtCell("Budget!C2"), Value: ops.Fx("=B2*10")},
			&ops.SheetAdd{Name: "Notes", As: ops.Ptr("n")},
			&ops.SheetRename{At: ops.AtSheet("$n"), Name: "Memo"},
			&ops.CellSet{At: ops.AtCell("Memo!A1"), Value: ops.Str("done")},
		)
		res, err := Run(in, p, Options{Out: out})
		if err != nil {
			t.Fatal(err)
		}
		written, _ := os.ReadFile(out) //nolint:gosec // test output
		c, err := read.Cells(written, read.CellsRequest{Range: "Budget!A2:C2"}, read.Options{})
		if err != nil {
			t.Fatal(err)
		}
		b, _ := cli.Marshal(c.Cells)
		if string(b) != `{"A2":"Item","B2":{"f":"=2*2","v":4},"C2":{"f":"=B2*10"}}` {
			t.Errorf("Budget row 2: %s", b)
		}
		if res.Aliases["n"] != "sheet:Memo" { // an alias follows its sheet through renames
			t.Errorf("aliases %v", res.Aliases)
		}
		m, err := read.Cells(written, read.CellsRequest{Range: "Memo!A1"}, read.Options{})
		if err != nil || m.Cells.Len() != 1 {
			t.Errorf("Memo!A1: %v", err)
		}
	})
}

// TestApplySetThenInsert: a range reported by an earlier op is read back
// at its final address after a later insert moves it.
func TestApplySetThenInsert(t *testing.T) {
	xlsxtest.Skip(t, func() {
		in, _ := e2eBook(t)
		res, err := Run(in, ops.NewPlan(
			&ops.CellSet{At: ops.AtCell("Budget!C1"), Value: ops.Num(7)},
			&ops.RowInsert{At: ops.AtRows("Budget!1:1"), Position: ops.Before, Count: ops.Ptr(2)},
		), Options{DryRun: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Changed) == 0 || !strings.Contains(string(res.Changed[0]), `"id":"Budget!C3","cells":{"C3":7}`) {
			t.Errorf("changed %s", res.Changed)
		}
	})
}

// TestApplyUnshareThenShift: writing over a shared master unshares its
// group; a later insert shifts the written-out children.
func TestApplyUnshareThenShift(t *testing.T) {
	xlsxtest.Skip(t, func() {
		b := xlsxtest.Book{Sheets: []xlsxtest.Sheet{{Name: "Budget",
			Rows: `<row r="1"><c r="A1"><v>1</v></c><c r="B1"><f t="shared" ref="B1:B3" si="0">A1*2</f><v>2</v></c></row>` +
				`<row r="2"><c r="A2"><v>2</v></c><c r="B2"><f t="shared" si="0"/><v>4</v></c></row>` +
				`<row r="3"><c r="A3"><v>3</v></c><c r="B3"><f t="shared" si="0"/><v>6</v></c></row>`}}}
		in, err := b.Write(t.TempDir(), "in.xlsx")
		if err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(t.TempDir(), "out.xlsx")
		if _, err := Run(in, ops.NewPlan(
			&ops.CellSet{At: ops.AtCell("Budget!B1"), Value: ops.Num(0)},
			&ops.RowInsert{At: ops.AtRows("Budget!2:2"), Position: ops.Before},
		), Options{Out: out}); err != nil {
			t.Fatal(err)
		}
		written, _ := os.ReadFile(out) //nolint:gosec // test output
		c, err := read.Cells(written, read.CellsRequest{Range: "Budget!B1:B4", View: model.ViewFormulas}, read.Options{})
		if err != nil {
			t.Fatal(err)
		}
		// The formulas view shows formula cells only (approved shape), so B1,
		// now the number 0, is checked in the values view.
		got, _ := cli.Marshal(c.Cells)
		if string(got) != `{"B3":"=A3*2","B4":"=A4*2"}` {
			t.Errorf("after unshare and insert: %s", got)
		}
		v, err := read.Cells(written, read.CellsRequest{Range: "Budget!B1", View: model.ViewValues}, read.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := cli.Marshal(v.Cells); string(got) != `{"B1":0}` {
			t.Errorf("B1 after cell.set: %s", got)
		}
	})
}
