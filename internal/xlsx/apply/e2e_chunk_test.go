package apply

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
	"github.com/jhnoel/documentdriver/internal/xlsx/read"
	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// End to end, verification chunk V: every op through Run with the real
// gate (integrity.GateScoped) on one workbook, each held to the laws:
// untouched parts byte for byte, the recalculation policy, the result's
// changed units equal to read of the output, atomicity under injected
// save faults, dry run equal to the real run, determinism. Then identity
// writes, sequencing and aliases, and --recalc with LibreOffice.

// v2Book is the e2e workbook. Budget: shared strings, an inline string,
// formulas with cached values, a column width, a merge. Other: a formula
// naming Budget. Two defined names, a calcChain.
func v2Book() xlsxtest.Book {
	return xlsxtest.Book{
		CalcPr:       `<calcPr calcId="191029"/>`,
		CalcChain:    `<c r="C1" i="1"/><c r="A4" i="1"/><c r="A1" i="2"/>`,
		SST:          []string{"<t>Item</t>", "<t>Cost</t>", "<t>apple pie</t>"},
		DefinedNames: `<definedName name="Rate">0.2</definedName><definedName name="Costs">Budget!$B$2:$B$3</definedName>`,
		Sheets: []xlsxtest.Sheet{
			{Name: "Budget", Dimension: "A1:E4",
				Head: `<cols><col min="1" max="1" width="20" customWidth="1"/></cols>`,
				Rows: `<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c><c r="C1"><f>SUM(B2:B3)</f><v>30</v></c></row>` +
					`<row r="2"><c r="A2" t="s"><v>2</v></c><c r="B2"><v>10</v></c></row>` +
					`<row r="3"><c r="A3" t="inlineStr"><is><t>apple tart</t></is></c><c r="B3"><v>20</v></c></row>` +
					`<row r="4"><c r="A4"><f>B2*2</f><v>20</v></c></row>`,
				Tail: `<mergeCells count="1"><mergeCell ref="D1:E1"/></mergeCells>`},
			{Name: "Other", Dimension: "A1:B1",
				Rows: `<row r="1"><c r="A1"><f>Budget!B2+1</f><v>11</v></c><c r="B1"><v>5</v></c></row>`},
		}}
}

func v2Write(t *testing.T) (string, []byte) {
	t.Helper()
	p, err := v2Book().Write(t.TempDir(), "in.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p) //nolint:gosec // test input
	if err != nil {
		t.Fatal(err)
	}
	return p, data
}

// cellsOf is read --range rng in view, as JSON ("" when the read fails).
func cellsOf(t *testing.T, data []byte, rng string, view model.View) string {
	t.Helper()
	c, err := read.Cells(data, read.CellsRequest{Range: rng, View: view}, read.Options{})
	if err != nil {
		return "error: " + err.Error()
	}
	b, err := cli.Marshal(c.Cells)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// sheetFacts is read --range rng's merges and hidden lists.
func sheetFacts(t *testing.T, data []byte, rng string) string {
	t.Helper()
	c, err := read.Cells(data, read.CellsRequest{Range: rng}, read.Options{})
	if err != nil {
		return "error: " + err.Error()
	}
	return "merges " + strings.Join(c.Merges, ",") + "; hidden " + strings.Join(c.Hidden, ",")
}

// bookFacts is the summary's sheet names and defined names.
func bookFacts(t *testing.T, data []byte) string {
	t.Helper()
	wb, err := read.Workbook(data, read.Options{})
	if err != nil {
		return "error: " + err.Error()
	}
	var out []string
	for _, s := range wb.Sheets {
		out = append(out, s.Name)
	}
	for _, n := range wb.Names {
		out = append(out, n.Name+"="+n.RefersTo)
	}
	return strings.Join(out, ",")
}

type v2Case struct {
	name string
	plan *ops.Plan
	// reads are what read shows on the output: "cells R" (cells view),
	// "values R", "formulas R", "facts R" (merges and hidden), "book"
	// (sheets and names), "part P" (substrings that part must hold),
	// each to its expected text.
	reads map[string]string
	// stale: the save asks Excel to recalculate (fullCalcOnLoad and the
	// stale-values hazard); chainGone: the calcChain is removed; adjusts:
	// a structural op or rename rewrites formulas it did not write,
	// which keep their cached values.
	stale, chainGone, adjusts bool
	// hazards are the other hazard kinds expected, in order.
	hazards []string
}

func v2Cases() []v2Case {
	return []v2Case{
		{name: "cell.set text", plan: ops.NewPlan(&ops.CellSet{At: ops.AtCell("Budget!D2"), Value: ops.Str("a<b&c")}),
			reads: map[string]string{"cells Budget!C1:D2": `{"C1":{"f":"=SUM(B2:B3)","v":30},"D2":"a<b&c"}`}, stale: true},
		{name: "cell.set formula", plan: ops.NewPlan(&ops.CellSet{At: ops.AtCell("Budget!D3"), Value: ops.Fx("=B3*3")}),
			reads: map[string]string{"cells Budget!C1:D3": `{"C1":{"f":"=SUM(B2:B3)","v":30},"D3":{"f":"=B3*3"}}`, "cells Other!A1": `{"A1":{"f":"=Budget!B2+1","v":11}}`},
			stale: true, chainGone: true},
		{name: "cell.set over a formula", plan: ops.NewPlan(&ops.CellSet{At: ops.AtCell("Budget!A4"), Value: ops.Num(7)}),
			reads: map[string]string{"cells Budget!A4": `{"A4":7}`}, stale: true, chainGone: true},
		{name: "cell.set date", plan: ops.NewPlan(&ops.CellSet{At: ops.AtCell("Budget!D4"), Value: ops.DateOf("2024-03-01")}),
			reads: map[string]string{"cells Budget!D4": `{"D4":{"date":"2024-03-01"}}`}, stale: true},
		{name: "range.set", plan: ops.NewPlan(&ops.RangeSet{At: ops.AtRange("Budget!D2:E3"), Values: ops.Grid{{ops.Str("x"), ops.Null()}, {ops.Num(1), ops.Str("")}}}),
			reads: map[string]string{"cells Budget!D2:E3": `{"D2":"x","D3":1,"E3":""}`}, stale: true},
		{name: "range.clear", plan: ops.NewPlan(&ops.RangeClear{At: ops.AtRange("Budget!A2:B2")}),
			reads: map[string]string{"cells Budget!A1:C2": `{"A1":"Item","B1":"Cost","C1":{"f":"=SUM(B2:B3)","v":30}}`}, stale: true},
		{name: "range.clear formula", plan: ops.NewPlan(&ops.RangeClear{At: ops.AtRange("Budget!A4")}),
			reads: map[string]string{"cells Budget!A4": `{}`}, stale: true, chainGone: true},
		{name: "text.replace", plan: ops.NewPlan(&ops.TextReplace{At: ops.AtText("sheet:Budget"), Select: "apple", With: "pear", All: true}),
			reads: map[string]string{"cells Budget!A1:A3": `{"A1":"Item","A2":"pear pie","A3":"pear tart"}`}, stale: true},
		{name: "row.insert", plan: ops.NewPlan(&ops.RowInsert{At: ops.AtRows("Budget!2:2"), Position: ops.Before}),
			reads: map[string]string{
				"formulas Budget!A1:C5": `{"C1":"=SUM(B3:B4)","A5":"=B3*2"}`,
				"values Budget!A2:B3":   `{"A3":"apple pie","B3":10}`,
				"formulas Other!A1":     `{"A1":"=Budget!B3+1"}`,
				"facts Budget!A1:E5":    "merges Budget!D1:E1; hidden ",
				"book":                  "Budget,Other,Rate==0.2,Costs==Budget!$B$3:$B$4",
			}, adjusts: true, stale: true, chainGone: true},
		{name: "row.delete", plan: ops.NewPlan(&ops.RowDelete{At: ops.AtRows("Budget!2:2")}),
			reads: map[string]string{
				"formulas Budget!A1:C3": `{"C1":"=SUM(B2:B2)","A3":"=#REF!*2"}`,
				"formulas Other!A1":     `{"A1":"=Budget!#REF!+1"}`,
				"book":                  "Budget,Other,Rate==0.2,Costs==Budget!$B$2:$B$2",
			}, adjusts: true, stale: true, chainGone: true, hazards: []string{"ref-error", "ref-error"}},
		{name: "column.insert", plan: ops.NewPlan(&ops.ColumnInsert{At: ops.AtCols("Budget!B:B"), Position: ops.Before}),
			reads: map[string]string{
				"formulas Budget!A1:D4": `{"D1":"=SUM(C2:C3)","A4":"=C2*2"}`,
				"values Budget!B1:C1":   `{"C1":"Cost"}`,
				"formulas Other!A1":     `{"A1":"=Budget!C2+1"}`,
				"facts Budget!A1:F4":    "merges Budget!E1:F1; hidden ",
			}, adjusts: true, stale: true, chainGone: true},
		{name: "column.delete", plan: ops.NewPlan(&ops.ColumnDelete{At: ops.AtCols("Budget!C:C")}),
			reads:   map[string]string{"values Budget!A1:C1": `{"A1":"Item","B1":"Cost"}`, "facts Budget!A1:E4": "merges Budget!C1:D1; hidden "},
			adjusts: true, stale: true, chainGone: true},
		{name: "row.format", plan: ops.NewPlan(&ops.RowFormat{At: ops.AtRows("Budget!2:3"), Height: ops.Ptr(30.0), Hidden: ops.Ptr(true)}),
			reads: map[string]string{"facts Budget!A1:E4": "merges Budget!D1:E1; hidden Budget!2:3"}},
		{name: "column.format", plan: ops.NewPlan(&ops.ColumnFormat{At: ops.AtCols("Budget!C:D"), Width: ops.Ptr(15.0), Hidden: ops.Ptr(true)}),
			reads: map[string]string{"facts Budget!A1:E4": "merges Budget!D1:E1; hidden Budget!C:D"}},
		{name: "range.format", plan: ops.NewPlan(&ops.RangeFormat{At: ops.AtRange("Budget!A1:B1"), Bold: ops.Ptr(true), NumberFormat: ops.Ptr("0.000")}),
			reads: map[string]string{"cells Budget!A1:B1": `{"A1":"Item","B1":"Cost"}`, "part xl/styles.xml": `formatCode="0.000"`}},
		{name: "cell.merge", plan: ops.NewPlan(&ops.CellMerge{At: ops.AtRange("Budget!A6:B6")}),
			reads: map[string]string{"facts Budget!A1:E6": "merges Budget!D1:E1,Budget!A6:B6; hidden "}},
		{name: "cell.split", plan: ops.NewPlan(&ops.CellSplit{At: ops.AtRange("Budget!D1:E1")}),
			reads: map[string]string{"facts Budget!A1:E4": "merges ; hidden "}},
		{name: "sheet.add", plan: ops.NewPlan(&ops.SheetAdd{Name: "Notes"}),
			reads: map[string]string{"book": "Budget,Other,Notes,Rate==0.2,Costs==Budget!$B$2:$B$3", "cells Notes!A1:B2": `{}`}},
		{name: "sheet.rename", plan: ops.NewPlan(&ops.SheetRename{At: ops.AtSheet("sheet:Budget"), Name: "Plan 2"}),
			reads: map[string]string{
				"book":              "Plan 2,Other,Rate==0.2,Costs=='Plan 2'!$B$2:$B$3",
				"formulas Other!A1": `{"A1":"='Plan 2'!B2+1"}`,
			}, stale: true, adjusts: true},
		{name: "name.add", plan: ops.NewPlan(&ops.NameAdd{Name: "Tax", RefersTo: "=Budget!$C$1*2"}),
			reads: map[string]string{"book": "Budget,Other,Rate==0.2,Costs==Budget!$B$2:$B$3,Tax==Budget!$C$1*2"}, stale: true},
		{name: "name.set", plan: ops.NewPlan(&ops.NameSet{At: ops.AtName("name:Rate"), RefersTo: "=0.25"}),
			reads: map[string]string{"book": "Budget,Other,Rate==0.25,Costs==Budget!$B$2:$B$3"}, stale: true},
		{name: "name.delete", plan: ops.NewPlan(&ops.NameDelete{At: ops.AtName("name:Rate")}),
			reads: map[string]string{"book": "Budget,Other,Costs==Budget!$B$2:$B$3"}, stale: true},
	}
}

// checkReads holds data to c.reads.
func checkReads(t *testing.T, data []byte, reads map[string]string) {
	t.Helper()
	keys := slices.Sorted(func(yield func(string) bool) {
		for k := range reads {
			if !yield(k) {
				return
			}
		}
	})
	for _, k := range keys {
		want := reads[k]
		kind, arg, _ := strings.Cut(k, " ")
		var got string
		switch kind {
		case "cells":
			got = cellsOf(t, data, arg, model.ViewCells)
		case "values":
			got = cellsOf(t, data, arg, model.ViewValues)
		case "formulas":
			got = cellsOf(t, data, arg, model.ViewFormulas)
		case "facts":
			got = sheetFacts(t, data, arg)
		case "book":
			got = bookFacts(t, data)
		case "part":
			parts, err := xlsxtest.Parts(data)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(parts[arg]), want) {
				t.Errorf("%s lacks %s", arg, want)
			}
			continue
		default:
			t.Fatalf("bad read key %q", k)
		}
		if got != want {
			t.Errorf("%s:\n got %s\nwant %s", k, got, want)
		}
	}
}

// TestE2EEveryOp: every op through Run with the real gate, held to the
// laws.
func TestE2EEveryOp(t *testing.T) {
	xlsxtest.Skip(t, func() {
		for _, c := range v2Cases() {
			t.Run(c.name, func(t *testing.T) {
				in, before := v2Write(t)
				out := filepath.Join(t.TempDir(), "out.xlsx")
				res, err := Run(in, c.plan, Options{Out: out})
				if err != nil {
					t.Fatal(err)
				}
				written, err := os.ReadFile(out) //nolint:gosec // test output
				if err != nil {
					t.Fatal(err)
				}
				if res.NewBase != cli.Hash(written) || res.Base != cli.Hash(before) || res.Saved != out {
					t.Errorf("bases %s %s saved %s", res.Base, res.NewBase, res.Saved)
				}
				checkUntouched(t, before, written, res.Parts)
				checkRecalcPolicy(t, before, written, res, c)
				checkReads(t, written, c.reads)
				checkUnitsRoundTrip(t, written, res)
				if got, err := os.ReadFile(in); err != nil || !bytes.Equal(got, before) { //nolint:gosec // test input
					t.Error("-o changed the input")
				}

				// Dry run: the same result, nothing written.
				dry, err := Run(in, c.plan, Options{DryRun: true})
				if err != nil {
					t.Fatal(err)
				}
				if dry.Saved != "" || dry.NewBase != "" || !slices.Equal(dry.Parts, res.Parts) || !bytes.Equal(dry.Output, res.Output) || !sameChanged(dry.Changed, res.Changed) {
					t.Errorf("dry run differs: %+v", dry)
				}
				// Deterministic: the same input and plan give the same bytes.
				again := filepath.Join(t.TempDir(), "again.xlsx")
				if _, err := Run(in, c.plan, Options{Out: again}); err != nil {
					t.Fatal(err)
				}
				if b, _ := os.ReadFile(again); !bytes.Equal(b, written) { //nolint:gosec // test output
					t.Error("a second run wrote different bytes")
				}
				checkAtomic(t, in, before, c.plan)
			})
		}
	})
}

// checkUntouched: every part outside res.Parts is byte for byte the
// input's, and every part in it differs (changed, added or deleted).
func checkUntouched(t *testing.T, before, after []byte, changed []string) {
	t.Helper()
	a, err := xlsxtest.Parts(before)
	if err != nil {
		t.Fatal(err)
	}
	b, err := xlsxtest.Parts(after)
	if err != nil {
		t.Fatal(err)
	}
	in := map[string]bool{}
	for _, p := range changed {
		in[strings.TrimPrefix(p, "/")] = true
	}
	for name, data := range a {
		if in[name] {
			if nb, ok := b[name]; ok && bytes.Equal(nb, data) {
				t.Errorf("%s is listed changed but is the same", name)
			}
			continue
		}
		if !bytes.Equal(b[name], data) {
			t.Errorf("%s changed but is not listed", name)
		}
	}
	for name := range b {
		if _, ok := a[name]; !ok && !in[name] {
			t.Errorf("%s added but not listed", name)
		}
	}
}

// checkRecalcPolicy: a save that changes a value, a formula or a sheet's
// structure sets fullCalcOnLoad and reports stale-values; the calcChain
// goes when a formula cell is added, removed or moved; a written formula
// has no <v>; the other hazards are the case's.
func checkRecalcPolicy(t *testing.T, before, after []byte, res *Result, c v2Case) {
	t.Helper()
	parts, err := xlsxtest.Parts(after)
	if err != nil {
		t.Fatal(err)
	}
	full := strings.Contains(string(parts["xl/workbook.xml"]), `fullCalcOnLoad="1"`)
	var kinds []string
	staleHazard := false
	for _, h := range res.Hazards {
		if h.Kind == "stale-values" {
			staleHazard = true
			if h.ID != "workbook" || !h.New {
				t.Errorf("stale-values hazard %+v", h)
			}
			continue
		}
		kinds = append(kinds, h.Kind)
	}
	if full != c.stale || staleHazard != c.stale {
		t.Errorf("fullCalcOnLoad %v, stale-values %v, want %v", full, staleHazard, c.stale)
	}
	if _, kept := parts["xl/calcChain.xml"]; kept == c.chainGone {
		t.Errorf("calcChain kept %v, want removed %v", kept, c.chainGone)
	}
	if c.chainGone {
		rels := string(parts["xl/_rels/workbook.xml.rels"]) + string(parts["[Content_Types].xml"])
		if strings.Contains(rels, "calcChain") {
			t.Error("calcChain still named in the relationships or content types")
		}
	}
	if !slices.Equal(kinds, c.hazards) {
		t.Errorf("hazards %v, want %v", kinds, c.hazards)
	}
	// Formulas the plan wrote carry no cached value (adjusted ones keep
	// theirs).
	inParts, err := xlsxtest.Parts(before)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range parts {
		if !strings.HasPrefix(name, "xl/worksheets/sheet") {
			continue
		}
		for _, cell := range strings.SplitAfter(string(data), "</c>") {
			i := strings.Index(cell, "<c ")
			if i < 0 || !strings.Contains(cell, "<f") {
				continue
			}
			cell = cell[i:]
			if !strings.Contains(string(inParts[name]), cell) && strings.Contains(cell, "<v>") && !c.adjusts {
				t.Errorf("%s: a written formula has a cached value: %s", name, cell)
			}
		}
	}
}

// checkUnitsRoundTrip: each changed range unit is read --range of the
// output at its id, and the JSON is not HTML-escaped.
func checkUnitsRoundTrip(t *testing.T, data []byte, res *Result) {
	t.Helper()
	for _, raw := range res.Changed {
		if bytes.Contains(raw, []byte(`\u003c`)) || bytes.Contains(raw, []byte(`\u0026`)) {
			t.Errorf("changed unit HTML-escaped: %s", raw)
		}
		var u struct {
			ID    string          `json:"id"`
			Cells json.RawMessage `json:"cells"`
		}
		if err := json.Unmarshal(raw, &u); err != nil {
			t.Fatal(err)
		}
		if u.Cells == nil {
			continue
		}
		if got := cellsOf(t, data, u.ID, model.ViewCells); got != string(u.Cells) {
			t.Errorf("unit %s: changed %s, read %s", u.ID, u.Cells, got)
		}
	}
}

func sameChanged(a, b []json.RawMessage) bool {
	return slices.EqualFunc(a, b, func(x, y json.RawMessage) bool { return bytes.Equal(x, y) })
}

// checkAtomic: a save that fails at any stage before the rename leaves
// the input as it was, and the plan's error is io.
func checkAtomic(t *testing.T, in string, before []byte, p *ops.Plan) {
	t.Helper()
	for _, stage := range []string{opc.StageCreate, opc.StageWrite, opc.StageSync, opc.StageClose, opc.StageRename} {
		restore := opc.SetFaultHookFor(in, func(s, _ string) error {
			if s == stage {
				return errors.New("injected fault")
			}
			return nil
		})
		_, err := Run(in, p, Options{})
		restore()
		if err == nil {
			t.Errorf("%s: a failing save succeeded", stage)
		}
		if got, rerr := os.ReadFile(in); rerr != nil || !bytes.Equal(got, before) { //nolint:gosec // test input
			t.Errorf("%s: a failed save changed the file", stage)
		}
	}
	// No temporary file is left beside the input.
	entries, err := os.ReadDir(filepath.Dir(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("left behind: %v", names)
	}
}

// TestE2EIdentityWrites: writes that leave the workbook as it was write
// nothing, report no part and changed: [].
func TestE2EIdentityWrites(t *testing.T) {
	xlsxtest.Skip(t, func() {
		for _, c := range []struct {
			name string
			plan *ops.Plan
		}{
			{"cell.set number", ops.NewPlan(&ops.CellSet{At: ops.AtCell("Budget!B2"), Value: ops.Num(10)})},
			{"cell.set inline string", ops.NewPlan(&ops.CellSet{At: ops.AtCell("Budget!A3"), Value: ops.Str("apple tart")})},
			{"range.set", ops.NewPlan(&ops.RangeSet{At: ops.AtRange("Other!B1"), Values: ops.Grid{{ops.Num(5)}}})},
			{"range.set nulls", ops.NewPlan(&ops.RangeSet{At: ops.AtRange("Budget!D2:E3"), Values: ops.Grid{{ops.Null(), ops.Null()}, {ops.Null(), ops.Null()}}})},
			{"range.clear empty", ops.NewPlan(&ops.RangeClear{At: ops.AtRange("Budget!F10:G12")})},
			{"text.replace same", ops.NewPlan(&ops.TextReplace{At: ops.AtText("Budget!A3"), Select: "tart", With: "tart"})},
			{"row.format visible", ops.NewPlan(&ops.RowFormat{At: ops.AtRows("Budget!2:2"), Hidden: ops.Ptr(false)})},
			{"column.format same width", ops.NewPlan(&ops.ColumnFormat{At: ops.AtCols("Budget!A:A"), Width: ops.Ptr(20.0)})},
			{"name.set same", ops.NewPlan(&ops.NameSet{At: ops.AtName("name:Rate"), RefersTo: "=0.2"})},
			{"set then restore", ops.NewPlan(
				&ops.CellSet{At: ops.AtCell("Budget!B2"), Value: ops.Num(99)},
				&ops.CellSet{At: ops.AtCell("Budget!B2"), Value: ops.Num(10)})},
		} {
			t.Run(c.name, func(t *testing.T) {
				in, before := v2Write(t)
				out := filepath.Join(t.TempDir(), "out.xlsx")
				res, err := Run(in, c.plan, Options{Out: out})
				if err != nil {
					t.Fatal(err)
				}
				if res.Saved != "" || len(res.Parts) != 0 || len(res.Changed) != 0 || res.NewBase != res.Base {
					t.Errorf("identity write: saved %q parts %v changed %s", res.Saved, res.Parts, res.Changed)
				}
				if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
					t.Error("an identity write wrote a file")
				}
				if got, _ := os.ReadFile(in); !bytes.Equal(got, before) { //nolint:gosec // test input
					t.Error("the input changed")
				}
			})
		}
	})
}

// TestE2ESequencingAndAliases: each op sees the workbook as the ops
// before it left it; units and aliases carry final addresses; alias
// misuse is refused at its op.
func TestE2ESequencingAndAliases(t *testing.T) {
	xlsxtest.Skip(t, func() {
		in, _ := v2Write(t)
		out := filepath.Join(t.TempDir(), "out.xlsx")
		res, err := Run(in, ops.NewPlan(
			&ops.SheetAdd{Name: "Draft", As: ops.Ptr("d")},
			&ops.CellSet{At: ops.AtCell("Draft!A1"), Value: ops.Fx("=Budget!B2*2")},
			&ops.CellSet{At: ops.AtCell("Budget!D2"), Value: ops.Num(1)},
			&ops.RowInsert{At: ops.AtRows("Budget!1:1"), Position: ops.Before, Count: ops.Ptr(2)},
			&ops.SheetRename{At: ops.AtSheet("$d"), Name: "Final"},
			&ops.SheetRename{At: ops.AtSheet("sheet:Budget"), Name: "Plan"},
			&ops.NameAdd{Name: "Base", RefersTo: "=Final!$A$1"},
		), Options{Out: out})
		if err != nil {
			t.Fatal(err)
		}
		written, err := os.ReadFile(out) //nolint:gosec // test output
		if err != nil {
			t.Fatal(err)
		}
		if res.Aliases["d"] != "sheet:Final" {
			t.Errorf("aliases %v", res.Aliases)
		}
		checkReads(t, written, map[string]string{
			"formulas Final!A1": `{"A1":"=Plan!B4*2"}`,
			"values Plan!D4":    `{"D4":1}`,
			"book":              "Plan,Other,Final,Rate==0.2,Costs==Plan!$B$4:$B$5,Base==Final!$A$1",
		})
		var ids []string
		for _, raw := range res.Changed {
			var u struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(raw, &u); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, u.ID)
		}
		for _, want := range []string{"sheet:Final", "Final!A1", "Plan!D4", "name:Base"} {
			if !slices.Contains(ids, want) {
				t.Errorf("changed ids %v lack %s", ids, want)
			}
		}
		checkUnitsRoundTrip(t, written, res)

		for _, c := range []struct {
			name string
			plan *ops.Plan
			code string
			path string
		}{
			{"undefined alias", ops.NewPlan(&ops.SheetRename{At: ops.AtSheet("$nope"), Name: "X"}), cli.CodeNotFound, "ops[0].at"},
			{"alias twice", ops.NewPlan(&ops.SheetAdd{Name: "A1x", As: ops.Ptr("a")}, &ops.SheetAdd{Name: "B1x", As: ops.Ptr("a")}), cli.CodeOperationFailed, "ops[1].as"},
			{"later op refused", ops.NewPlan(&ops.CellSet{At: ops.AtCell("Budget!A1"), Value: ops.Num(1)}, &ops.CellSplit{At: ops.AtRange("Budget!A1:B1")}), cli.CodeNotFound, "ops[1].at"},
		} {
			_, err := Run(in, c.plan, Options{DryRun: true})
			var e *cli.Error
			if !errors.As(err, &e) || e.Code != c.code || e.Path != c.path {
				t.Errorf("%s: got %v, want %s at %s", c.name, err, c.code, c.path)
			}
		}
	})
}

// TestE2ERecalc: --recalc with LibreOffice computes the written
// formula's value and splices it, the rest as without it: fullCalcOnLoad
// stays and stale-values is reported. Needs DD_SOFFICE or soffice on
// PATH.
func TestE2ERecalc(t *testing.T) {
	bin := os.Getenv("DD_SOFFICE")
	if bin == "" {
		bin = "soffice"
	}
	if _, err := exec.LookPath(bin); err != nil {
		t.Skip("no LibreOffice (set DD_SOFFICE)")
	}
	xlsxtest.Skip(t, func() {
		in, before := v2Write(t)
		out := filepath.Join(t.TempDir(), "out.xlsx")
		p := ops.NewPlan(
			&ops.CellSet{At: ops.AtCell("Budget!D3"), Value: ops.Fx("=B3*3")},
			&ops.CellSet{At: ops.AtCell("Budget!B2"), Value: ops.Num(100)})
		res, err := Run(in, p, Options{Out: out, Recalc: &RecalcOptions{}})
		if err != nil {
			t.Fatal(err)
		}
		written, err := os.ReadFile(out) //nolint:gosec // test output
		if err != nil {
			t.Fatal(err)
		}
		checkUntouched(t, before, written, res.Parts)
		checkReads(t, written, map[string]string{
			"cells Budget!B1:D3":   `{"B1":"Cost","C1":{"f":"=SUM(B2:B3)","v":120},"B2":100,"B3":20,"D3":{"f":"=B3*3","v":60}}`,
			"cells Other!A1":       `{"A1":{"f":"=Budget!B2+1","v":101}}`,
			"part xl/workbook.xml": `fullCalcOnLoad="1"`,
		})
		if len(res.Hazards) == 0 || res.Hazards[0].Kind != "stale-values" {
			t.Errorf("hazards %+v", res.Hazards)
		}
		checkUnitsRoundTrip(t, written, res)
	})
}

// TestE2EEmptySheetCellOrder: cells written into a sheet with an empty
// sheetData come out in column order, the same on every run (they were
// written in map order: the gate refused it as worksheet.cell-order on
// 166 corpus workbooks, and the bytes varied run to run).
func TestE2EEmptySheetCellOrder(t *testing.T) {
	xlsxtest.Skip(t, func() {
		for _, rows := range []string{"", `<row r="9"><c r="A9"><v>1</v></c></row>`} {
			b := xlsxtest.Book{Sheets: []xlsxtest.Sheet{{Name: "S", Rows: rows, EmptySheetData: true}}}
			in, err := b.Write(t.TempDir(), "in.xlsx")
			if err != nil {
				t.Fatal(err)
			}
			var first []byte
			for range 8 {
				res, err := Run(in, ops.NewPlan(&ops.RangeFormat{At: ops.AtRange("S!A1:E3"), Bold: ops.Ptr(true)}), Options{DryRun: true})
				if err != nil {
					t.Fatalf("rows %q: %v", rows, err)
				}
				if first == nil {
					first = res.Output
				} else if !bytes.Equal(first, res.Output) {
					t.Fatalf("rows %q: output differs run to run", rows)
				}
			}
		}
	})
}

// TestE2EImplicitPositions: cells and rows stored without r (positions
// implied by order, valid OOXML) can be formatted and set in place (a
// range.format over them was unreadable_target on 7 corpus files).
func TestE2EImplicitPositions(t *testing.T) {
	xlsxtest.Skip(t, func() {
		b := xlsxtest.Book{Sheets: []xlsxtest.Sheet{{Name: "S", Rows: `<row><c><v>1</v></c><c><v>2</v></c></row><row><c><v>3</v></c><c t="inlineStr"><is><t>x</t></is></c></row>`}}}
		in, err := b.Write(t.TempDir(), "in.xlsx")
		if err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(t.TempDir(), "out.xlsx")
		if _, err := Run(in, ops.NewPlan(
			&ops.RangeFormat{At: ops.AtRange("S!A1:B2"), Bold: ops.Ptr(true)},
			&ops.CellSet{At: ops.AtCell("S!B2"), Value: ops.Str("y")},
		), Options{Out: out}); err != nil {
			t.Fatal(err)
		}
		written, err := os.ReadFile(out) //nolint:gosec // test output
		if err != nil {
			t.Fatal(err)
		}
		if got := cellsOf(t, written, "S!A1:B2", model.ViewValues); got != `{"A1":1,"B1":2,"A2":3,"B2":"y"}` {
			t.Errorf("got %s", got)
		}
	})
}
