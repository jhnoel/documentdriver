package apply

import (
	"strings"
	"testing"
)

// tableXML returns the document's first table's markup.
func tableXML(t *testing.T, path string) string {
	t.Helper()
	x := mainXML(t, path)
	i, j := strings.Index(x, "<w:tbl>"), strings.LastIndex(x, "</w:tbl>")
	if i < 0 || j < 0 {
		return ""
	}
	return x[i : j+len("</w:tbl>")]
}

// TestTableStructureOps: each structural operation's output in the modes
// it runs in.
func TestTableStructureOps(t *testing.T) {
	for _, c := range []struct {
		name, mode, op string
		want, not      []string
	}{
		{"row insert after, tracked", "track", `[{"op":"row.insert","at":"table:1/row:1","position":"after","count":2,"as":"r"},{"op":"cell.format","at":"$r/row:2/cell:2","format":{"fill":"#00FF00"}}]`,
			[]string{`</w:tr><w:tr><w:trPr><w:cantSplit/><w:ins w:id="1" w:author="Ann"`, `<w:shd w:val="clear" w:fill="00FF00"/>`, `<w:t>A2</w:t>`},
			nil},
		{"row insert before, direct", "direct", `[{"op":"row.insert","at":"table:1/row:1","position":"before","count":1}]`,
			[]string{`<w:tblGrid><w:gridCol w:w="2000"/><w:gridCol w:w="3000"/></w:tblGrid><w:tr><w:trPr><w:cantSplit/></w:trPr><w:tc><w:tcPr><w:tcW w:w="2000" w:type="dxa"/></w:tcPr><w:p w14:paraId=`},
			[]string{"w:ins"}},
		{"row delete, tracked", "track", `[{"op":"row.delete","at":"table:1/row:2","count":1}]`,
			[]string{`<w:trPr><w:del w:id=`, `<w:delText>A2</w:delText>`},
			nil},
		{"row delete, direct", "direct", `[{"op":"row.delete","at":"table:1/row:1","count":1}]`,
			[]string{`<w:t>A2</w:t>`},
			[]string{"A1", "w:del"}},
		{"own inserted row deleted, tracked", "track", `[{"op":"row.insert","at":"table:1/row:2","position":"after","count":1},{"op":"row.delete","at":"table:1/row:3","count":1}]`,
			[]string{`<w:t>B2</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`},
			[]string{"w:ins", "w:del"}},
		{"column insert", "direct", `[{"op":"column.insert","at":"table:1/column:1","position":"after","count":1}]`,
			[]string{`<w:gridCol w:w="2000"/><w:gridCol w:w="2000"/><w:gridCol w:w="3000"/>`, `A1</w:t></w:r></w:p></w:tc><w:tc><w:tcPr><w:tcW w:w="2000" w:type="dxa"/></w:tcPr><w:p w14:paraId=`},
			nil},
		{"column delete", "direct", `[{"op":"column.delete","at":"table:1/column:1","count":1}]`,
			[]string{`<w:tblGrid><w:gridCol w:w="3000"/></w:tblGrid>`, "B1", "B2"},
			[]string{"A1", "A2"}},
		{"merge across", "direct", `[{"op":"cell.merge","at":"table:1/row:1/cell:1","row_span":1,"column_span":2}]`,
			[]string{`<w:tcPr><w:tcW w:w="0" w:type="auto"/><w:gridSpan w:val="2"/></w:tcPr><w:p w14:paraId="00000011"><w:r><w:t>A1</w:t></w:r></w:p><w:p w14:paraId="00000012"><w:r><w:t>B1</w:t></w:r></w:p></w:tc></w:tr>`},
			nil},
		{"merge down", "direct", `[{"op":"cell.merge","at":"table:1/row:1/cell:2","row_span":2,"column_span":1}]`,
			[]string{`<w:tcPr><w:vMerge w:val="restart"/></w:tcPr><w:p w14:paraId="00000012"><w:r><w:t>B1</w:t></w:r></w:p><w:p w14:paraId="00000022"><w:r><w:t>B2</w:t></w:r></w:p></w:tc>`, `<w:tc><w:tcPr><w:vMerge w:val="continue"/></w:tcPr><w:p w14:paraId=`},
			nil},
		{"merge then split", "direct", `[{"op":"cell.merge","at":"table:1/row:1/cell:1","row_span":2,"column_span":2},{"op":"cell.split","at":"table:1/row:1/cell:1"}]`,
			[]string{`A1`, `B2`},
			[]string{"gridSpan", "vMerge"}},
		{"table delete, tracked", "track", `[{"op":"table.delete","at":"table:1"}]`,
			[]string{`<w:delText>A1</w:delText>`, `<w:trPr><w:cantSplit/><w:del w:id=`},
			nil},
		{"table delete, direct", "direct", `[{"op":"table.delete","at":"table:1"}]`,
			[]string{`<w:p w14:paraId="0000000A"/><w:p w14:paraId="0000000B"/>`},
			[]string{"w:tbl"}},
	} {
		path := writeDocx(t, gridBody)
		r := commit(t, path, opsText(t, path, c.mode, c.op))
		if r.Outcome != Committed {
			t.Errorf("%s: %+v", c.name, r.Diagnostic)
			continue
		}
		got := mainXML(t, path)
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: no %s in\n%s", c.name, w, tableXML(t, path))
			}
		}
		for _, w := range c.not {
			if strings.Contains(tableXML(t, path), w) {
				t.Errorf("%s: %s left in\n%s", c.name, w, tableXML(t, path))
			}
		}
	}
}

// TestTableStructureRefusals: the guards refuse at the operation, before
// anything changes, the same in both modes except the track refusal of
// the edits Word does not track.
func TestTableStructureRefusals(t *testing.T) {
	merged := strings.Replace(gridBody, `<w:tc><w:p w14:paraId="00000021">`, `<w:tc><w:tcPr><w:gridSpan w:val="2"/></w:tcPr><w:p w14:paraId="00000021">`, 1)
	merged = strings.Replace(merged, `<w:tc><w:p w14:paraId="00000022"><w:r><w:t>B2</w:t></w:r></w:p></w:tc>`, "", 1)
	foreign := strings.Replace(gridBody, `<w:r><w:t>B2</w:t></w:r>`, `<w:ins w:id="9" w:author="Bob" w:date="2020-01-01T00:00:00Z"><w:r><w:t>B2</w:t></w:r></w:ins>`, 1)
	for _, c := range []struct {
		name, body, op string
		modes          []string
		code, path     string
	}{
		{"column tracked", gridBody, `[{"op":"column.insert","at":"table:1/column:1","position":"after","count":1}]`, []string{"track"}, "refused_in_mode", "ops[0]"},
		{"merge tracked", gridBody, `[{"op":"cell.merge","at":"table:1/row:1/cell:1","row_span":1,"column_span":2}]`, []string{"track"}, "refused_in_mode", "ops[0]"},
		{"split tracked", merged, `[{"op":"cell.split","at":"table:1/row:2/cell:1"}]`, []string{"track"}, "refused_in_mode", "ops[0]"},
		{"row insert in a merged table", merged, `[{"op":"row.insert","at":"table:1/row:1","position":"after","count":1}]`, []string{"track", "direct"}, "operation_failed", "ops[0].at"},
		{"foreign revision", foreign, `[{"op":"row.delete","at":"table:1/row:1","count":1}]`, []string{"track", "direct"}, "operation_failed", "ops[0].at"},
		{"every row", gridBody, `[{"op":"row.delete","at":"table:1/row:1","count":2}]`, []string{"track", "direct"}, "operation_failed", "ops[0].count"},
		{"every column", gridBody, `[{"op":"column.delete","at":"table:1/column:1","count":2}]`, []string{"direct"}, "operation_failed", "ops[0].count"},
		{"no column", gridBody, `[{"op":"column.delete","at":"table:1/column:3","count":1}]`, []string{"direct"}, "not_found", "ops[0].at"},
		{"merge past the table", gridBody, `[{"op":"cell.merge","at":"table:1/row:2/cell:1","row_span":2,"column_span":1}]`, []string{"direct"}, "operation_failed", "ops[0].row_span"},
		{"split an unmerged cell", gridBody, `[{"op":"cell.split","at":"table:1/row:1/cell:1"}]`, []string{"direct"}, "operation_failed", "ops[0].at"},
		{"no row", gridBody, `[{"op":"row.insert","at":"table:1/row:3","position":"after","count":1}]`, []string{"track", "direct"}, "not_found", "ops[0].at"},
		{"unbound alias", gridBody, `[{"op":"row.delete","at":"$x/row:1","count":1}]`, []string{"direct"}, "not_found", "ops[0].at"},
	} {
		for _, mode := range c.modes {
			path := writeDocx(t, c.body)
			r, err := runText(path, opsText(t, path, mode, c.op), Options{})
			if err != nil {
				t.Fatal(err)
			}
			if r.Outcome != Rejected || r.Diagnostic.Code != c.code || r.Diagnostic.Path != c.path {
				t.Errorf("%s %s: %s %+v, want %s at %s", c.name, mode, r.Outcome, r.Diagnostic, c.code, c.path)
			}
		}
	}
}

// TestDeleteTableInTextBox: a table that is a text box's only block is
// part of its paragraph's own content; deleting it leaves the text box a
// closing paragraph (tracked: an inserted one, which rejecting removes),
// and a table without rows is refused as unreadable in both modes.
func TestDeleteTableInTextBox(t *testing.T) {
	box := `<w:p w14:paraId="0000000A"><w:r><w:pict><v:shape xmlns:v="urn:schemas-microsoft-com:vml"><v:textbox><w:txbxContent>` +
		`<w:tbl><w:tblPr/><w:tblGrid><w:gridCol w:w="2000"/></w:tblGrid><w:tr><w:tc><w:p w14:paraId="00000011"><w:r><w:t>in a box</w:t></w:r></w:p></w:tc></w:tr></w:tbl>` +
		`</w:txbxContent></v:textbox></v:shape></w:pict></w:r></w:p>`
	op := `[{"op":"table.delete","at":"table:1"}]`
	for mode, want := range map[string]string{
		"direct": `<w:txbxContent><w:p w14:paraId=`,
		"track":  `<w:delText>in a box</w:delText>`,
	} {
		path := writeDocx(t, box)
		r := commit(t, path, opsText(t, path, mode, op))
		if r.Outcome != Committed {
			t.Fatalf("%s: %+v", mode, r.Diagnostic)
		}
		got := mainXML(t, path)
		if !strings.Contains(got, want) {
			t.Errorf("%s: no %s in %s", mode, want, got)
		}
		if mode == "track" && !strings.Contains(got, `</w:tbl><w:p w14:paraId=`) {
			t.Errorf("track: no closing paragraph after the table: %s", got)
		}
	}
	rowless := `<w:tbl><w:tblPr/><w:tblGrid><w:gridCol w:w="2000"/></w:tblGrid></w:tbl><w:p w14:paraId="0000000A"/>`
	for _, mode := range []string{"track", "direct"} {
		path := writeDocx(t, rowless)
		r, err := runText(path, opsText(t, path, mode, op), Options{})
		if err != nil {
			t.Fatal(err)
		}
		if r.Outcome != Rejected || r.Diagnostic.Code != "unreadable_target" || r.Diagnostic.Path != "ops[0].at" {
			t.Errorf("rowless %s: %s %+v", mode, r.Outcome, r.Diagnostic)
		}
	}
}
