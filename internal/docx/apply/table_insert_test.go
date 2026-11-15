package apply

import (
	"strings"
	"testing"
)

// TestInsertTable: a new table with filled cells and cell aliases, in
// both modes, inside a cell (direct adds the cell's closing paragraph,
// track refuses), and its refusals.
func TestInsertTable(t *testing.T) {
	op := `[{"op":"table.insert","at":"0000000A","position":"after","rows":2,"columns":2,"cells":[["A1","<b>B1</b>"],["A2"]],"as":"t"},` +
		`{"op":"paragraph.format","at":"$t_r2c2","alignment":"center"},{"op":"cell.format","at":"$t/row:1/cell:2","format":{"fill":"#ABCDEF"}}]`
	for mode, want := range map[string][]string{
		"track":  {`<w:tr><w:trPr><w:ins w:id=`, `<w:ins w:id="`, `<w:t>A1</w:t>`, `<w:b/>`, `<w:jc w:val="center"/>`, `w:fill="ABCDEF"`, `<w:gridCol w:w="2400"/><w:gridCol w:w="2400"/>`},
		"direct": {`<w:tbl><w:tblPr><w:tblW w:w="0" w:type="auto"/></w:tblPr>`, `<w:t>A1</w:t>`, `<w:jc w:val="center"/>`, `w:fill="ABCDEF"`},
	} {
		path := writeDocx(t, gridBody)
		r := commit(t, path, opsText(t, path, mode, op))
		if r.Outcome != Committed {
			t.Fatalf("%s: %+v", mode, r.Diagnostic)
		}
		got := mainXML(t, path)
		for _, w := range want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: no %s in %s", mode, w, got)
			}
		}
		if i, j := strings.Index(got, `w14:paraId="0000000A"`), strings.Index(got, "<w:tbl>"); i < 0 || j < i {
			t.Errorf("%s: the table is not after its anchor", mode)
		}
		if mode == "direct" && strings.Contains(got, "w:ins") {
			t.Errorf("direct: a revision is left: %s", got)
		}
	}
	// In a cell, after its last paragraph.
	inCell := `[{"op":"table.insert","at":"00000022","position":"after","rows":1,"columns":1}]`
	path := writeDocx(t, gridBody)
	if r := commit(t, path, opsText(t, path, "direct", inCell)); r.Outcome != Committed {
		t.Fatalf("in a cell: %+v", r.Diagnostic)
	}
	if got := mainXML(t, path); !strings.Contains(got, `</w:tbl><w:p w14:paraId=`) || strings.Count(got, "<w:tbl>") != 2 {
		t.Errorf("in a cell: %s", got)
	}
	for _, c := range []struct{ name, mode, op, code, path string }{
		{"end of a cell, tracked", "track", inCell, "refused_in_mode", "ops[0]"},
		{"no paragraph", "direct", `[{"op":"table.insert","at":"0000ABCD","position":"after","rows":1,"columns":1}]`, "not_found", "ops[0].at"},
		{"bad cell html", "direct", `[{"op":"table.insert","at":"0000000A","position":"after","rows":1,"columns":1,"cells":[["<table>"]]}]`, "invalid_operation", "ops[0].cells[0][0]"},
	} {
		path := writeDocx(t, gridBody)
		r, err := runText(path, opsText(t, path, c.mode, c.op), Options{})
		if err != nil {
			t.Fatal(err)
		}
		if r.Outcome != Rejected || r.Diagnostic.Code != c.code || r.Diagnostic.Path != c.path {
			t.Errorf("%s: %s %+v, want %s at %s", c.name, r.Outcome, r.Diagnostic, c.code, c.path)
		}
	}
}
