package ops_test

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// G1, G2, G3: cell.set, range.set, range.clear, through the cell-write
// shell (needs K1, K2, K3, K4, K5, K6) (designer rows; frozen).

func TestCellSetValues(t *testing.T) {
	xlsxtest.Skip(t, func() {
		for _, tc := range []struct {
			v    ops.CellValue
			read string // the cell as read shows it
			xml  string // a substring of the written sheet
		}{
			{ops.Num(1200), `1200`, `<c r="B2"><v>1200</v></c>`},
			{ops.Str("Item"), `"Item"`, `t="inlineStr"`},
			{ops.Str(""), `""`, `t="inlineStr"`},
			{ops.Str("=x"), `{"text":"=x"}`, `<t>=x</t>`},
			{ops.Boolean(false), `false`, `t="b"`},
			{ops.Err("#DIV/0!"), `{"error":"#DIV/0!"}`, `t="e"`},
			{ops.DateOf("2024-03-01"), `{"date":"2024-03-01"}`, `<v>45352</v>`},
			{ops.TimeOf("13:30:00"), `{"time":"13:30:00"}`, `<v>0.5625</v>`},
			{ops.Fx("=A1*2"), `{"f":"=A1*2"}`, `<f>A1*2</f></c>`},
		} {
			r := runOps(t, book(`<row r="1"><c r="A1"><v>2</v></c></row>`), &ops.CellSet{At: ops.AtCell("Budget!B2"), Value: tc.v}).ok()
			if got := r.cells("Budget!B2")["B2"]; got != tc.read {
				t.Errorf("%+v reads %s, want %s", tc.v, got, tc.read)
			}
			if x := r.sheet(1); !strings.Contains(x, tc.xml) || strings.Contains(x, `t="s"`) {
				t.Errorf("%+v wrote %s", tc.v, x)
			}
			if len(r.env[0].Changed) != 1 || r.env[0].Changed[0].ID != "Budget!B2" {
				t.Errorf("changed %+v", r.env[0].Changed)
			}
		}
	})
}

func TestCellSetKeepsStyleAndSST(t *testing.T) {
	xlsxtest.Skip(t, func() {
		b := book(`<row r="1"><c r="A1" s="4" t="s"><v>0</v></c></row>`)
		b.SST = []string{"<t>old</t>"}
		r := runOps(t, b, &ops.CellSet{At: ops.AtCell("Budget!A1"), Value: ops.Num(0.5)}).ok()
		if x := r.sheet(1); !strings.Contains(x, `<c r="A1" s="4"><v>0.5</v></c>`) {
			t.Errorf("style not kept: %s", x)
		}
		if pt, _ := r.pkg.Part("/xl/sharedStrings.xml"); pt.Patch() != nil {
			t.Error("the shared strings were touched (decision 11)")
		}
		// A date into a cell with a date format keeps it; into a
		// percent cell it gets format 14 (decision 9).
		r = runOps(t, book(`<row r="1"><c r="A1" s="4"><v>1</v></c></row>`), &ops.CellSet{At: ops.AtCell("Budget!A1"), Value: ops.DateOf("2024-03-01")}).ok()
		if got := r.cells("Budget!A1")["A1"]; got != `{"date":"2024-03-01"}` {
			t.Errorf("date into a percent cell reads %s", got)
		}
	})
}

func TestCellSetRefusals(t *testing.T) {
	xlsxtest.Skip(t, func() {
		array := `<row r="1"><c r="A1"><f t="array" ref="A1:A2">B1:B2*2</f><v>1</v></c></row><row r="2"><c r="A2"><v>2</v></c></row>`
		table := `<table id="1" name="T" displayName="T" ref="A1:B2"><tableColumns count="2"><tableColumn id="1" name="H1"/><tableColumn id="2" name="H2"/></tableColumns></table>`
		withTable := book(`<row r="1"><c r="A1" t="inlineStr"><is><t>H1</t></is></c><c r="B1" t="inlineStr"><is><t>H2</t></is></c></row>`)
		withTable.Sheets[0].Tables = []string{table}
		for _, tc := range []struct {
			name string
			b    xlsxtest.Book
			spec ops.Spec
			code string
		}{
			{"array member", book(array), &ops.CellSet{At: ops.AtCell("Budget!A2"), Value: ops.Num(1)}, cli.CodeOperationFailed},
			{"array anchor", book(array), &ops.CellSet{At: ops.AtCell("Budget!A1"), Value: ops.Num(1)}, cli.CodeOperationFailed},
			{"table header", withTable, &ops.CellSet{At: ops.AtCell("Budget!B1"), Value: ops.Str("X")}, cli.CodeOperationFailed},
			{"no sheet", book(""), &ops.CellSet{At: ops.AtCell("Nope!A1"), Value: ops.Num(1)}, cli.CodeNotFound},
			{"range over array", book(array), &ops.RangeSet{At: ops.AtRange("Budget!A2:B2"), Values: ops.Grid{{ops.Num(1), ops.Num(2)}}}, cli.CodeOperationFailed},
		} {
			t.Run(tc.name, func(t *testing.T) {
				xlsxtest.Skip(t, func() { runOps(t, tc.b, tc.spec).refused(0, tc.code, "at") })
			})
		}
	})
}

func TestCellSetUnsharesMaster(t *testing.T) {
	xlsxtest.Skip(t, func() {
		rows := `<row r="1"><c r="A1"><v>1</v></c><c r="B1"><f t="shared" ref="B1:B3" si="0">A1*2</f><v>2</v></c></row>` +
			`<row r="2"><c r="A2"><v>2</v></c><c r="B2"><f t="shared" si="0"/><v>4</v></c></row>` +
			`<row r="3"><c r="A3"><v>3</v></c><c r="B3"><f t="shared" si="0"/><v>6</v></c></row>`
		r := runOps(t, book(rows), &ops.CellSet{At: ops.AtCell("Budget!B1"), Value: ops.Num(0)}).ok()
		got := r.cells("Budget!B1:B3")
		if got["B2"] != `{"f":"=A2*2","v":4}` || got["B3"] != `{"f":"=A3*2","v":6}` {
			t.Errorf("children after unsharing: %v", got)
		}
	})
}

func TestRangeSetAndClear(t *testing.T) {
	xlsxtest.Skip(t, func() {
		rows := `<row r="1"><c r="A1" s="1"><v>9</v></c><c r="B1"><v>9</v></c></row>`
		r := runOps(t, book(rows),
			&ops.RangeSet{At: ops.AtRange("Budget!A1"), Values: ops.Grid{{ops.Null(), ops.Str("")}, {ops.Num(3), ops.Fx("=A2+1")}}}).ok()
		got := r.cells("Budget!A1:B2")
		if got["A1"] != "" { // null empties A1's content; its style stays
			t.Errorf("A1 %s", got["A1"])
		}
		if got["B1"] != `""` || got["A2"] != `3` || got["B2"] != `{"f":"=A2+1"}` {
			t.Errorf("range.set read back %v", got)
		}
		r = runOps(t, book(rows), &ops.RangeClear{At: ops.AtRange("Budget!A:B")}).ok()
		if x := r.sheet(1); !strings.Contains(x, `<c r="A1" s="1"/>`) || strings.Contains(x, `r="B1"`) {
			t.Errorf("range.clear: %s", x)
		}
	})
}
