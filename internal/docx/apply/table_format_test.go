package apply

import (
	"strconv"
	"strings"
	"testing"
)

// tableBody is a 2x3 table: a header row, a vertical merge in column 2
// and a row another author inserted.
const tableBody = `<w:tbl><w:tblPr><w:tblW w:w="0" w:type="auto"/></w:tblPr>` +
	`<w:tblGrid><w:gridCol w:w="2000"/><w:gridCol w:w="2000"/><w:gridCol w:w="2000"/></w:tblGrid>` +
	`<w:tr><w:tc><w:p w14:paraId="00000011"><w:r><w:t>a</w:t></w:r></w:p></w:tc><w:tc><w:tcPr><w:vMerge w:val="restart"/></w:tcPr><w:p w14:paraId="00000012"/></w:tc><w:tc><w:p w14:paraId="00000013"/></w:tc></w:tr>` +
	`<w:tr><w:trPr><w:ins w:id="5" w:author="Bob" w:date="2020-01-01T00:00:00Z"/></w:trPr><w:tc><w:p w14:paraId="00000021"/></w:tc><w:tc><w:tcPr><w:vMerge/></w:tcPr><w:p w14:paraId="00000022"/></w:tc><w:tc><w:p w14:paraId="00000023"/></w:tc></w:tr>` +
	`</w:tbl><w:p w14:paraId="0000000A"/>`

// TestFormatTableOps: each format records its container's change in
// track mode and none in direct mode, the grid change carries no author
// or date, a vertical merge is formatted on all its cells, and the
// guards refuse at the operation.
func TestFormatTableOps(t *testing.T) {
	cases := []struct {
		name, op string
		track    []string
		direct   []string
	}{
		{"table", `[{"op":"table.format","at":"table:1","format":{"width":{"unit":"points","value":300},"alignment":"center","column_widths":[50,60,70]}}]`,
			[]string{`<w:tblW w:w="6000" w:type="dxa"/>`, `<w:jc w:val="center"/>`, `<w:tblPrChange w:id="6" w:author="Ann" w:date="2026-01-01T00:00:00Z"><w:tblPr><w:tblW w:w="0" w:type="auto"/></w:tblPr></w:tblPrChange>`,
				`<w:gridCol w:w="1000"/><w:gridCol w:w="1200"/><w:gridCol w:w="1400"/><w:tblGridChange w:id="7"><w:tblGrid>`},
			[]string{`<w:tblPr><w:tblW w:w="6000" w:type="dxa"/><w:jc w:val="center"/></w:tblPr><w:tblGrid><w:gridCol w:w="1000"/><w:gridCol w:w="1200"/><w:gridCol w:w="1400"/></w:tblGrid>`}},
		{"row", `[{"op":"row.format","at":"table:1/row:1","format":{"height":24,"height_rule":"exact","repeating_header":true,"allow_split":false}}]`,
			[]string{`<w:trPr><w:cantSplit/><w:trHeight w:val="480" w:hRule="exact"/><w:tblHeader/><w:trPrChange w:id="6" w:author="Ann" w:date="2026-01-01T00:00:00Z"><w:trPr></w:trPr></w:trPrChange></w:trPr>`},
			[]string{`<w:tr><w:trPr><w:cantSplit/><w:trHeight w:val="480" w:hRule="exact"/><w:tblHeader/></w:trPr>`}},
		{"merged cell", `[{"op":"cell.format","at":"table:1/row:1/cell:2","format":{"fill":"#ABCDEF"}}]`,
			[]string{`<w:tcPr><w:vMerge w:val="restart"/><w:shd w:val="clear" w:fill="ABCDEF"/><w:tcPrChange w:id="6"`, `<w:tcPr><w:vMerge/><w:shd w:val="clear" w:fill="ABCDEF"/><w:tcPrChange w:id="7"`},
			[]string{`<w:tcPr><w:vMerge w:val="restart"/><w:shd w:val="clear" w:fill="ABCDEF"/></w:tcPr>`, `<w:tcPr><w:vMerge/><w:shd w:val="clear" w:fill="ABCDEF"/></w:tcPr>`}},
	}
	for _, c := range cases {
		for mode, want := range map[string][]string{"track": c.track, "direct": c.direct} {
			path := writeDocx(t, tableBody)
			r := commit(t, path, opsText(t, path, mode, c.op))
			if r.Outcome != Committed {
				t.Fatalf("%s %s: %+v", c.name, mode, r.Diagnostic)
			}
			got := mainXML(t, path)
			for _, w := range want {
				if !strings.Contains(got, w) {
					t.Errorf("%s %s: no %s in\n%s", c.name, mode, w, got)
				}
			}
			if mode == "direct" && strings.Contains(got, "Change") {
				t.Errorf("%s direct: a property change is left: %s", c.name, got)
			}
		}
	}
	for _, c := range []struct{ name, op, code, path string }{
		{"continuation", `[{"op":"cell.format","at":"table:1/row:2/cell:2","format":{"fill":"#ABCDEF"}}]`, "operation_failed", "ops[0].at"},
		{"foreign row", `[{"op":"row.format","at":"table:1/row:2","format":{"height":20}}]`, "operation_failed", "ops[0].at"},
		{"no table", `[{"op":"table.format","at":"table:2","format":{"layout":"fixed"}}]`, "not_found", "ops[0].at"},
		{"no row", `[{"op":"row.format","at":"table:1/row:3","format":{"height":20}}]`, "not_found", "ops[0].at"},
		{"no cell", `[{"op":"cell.format","at":"table:1/row:1/cell:4","format":{"fill":"#ABCDEF"}}]`, "not_found", "ops[0].at"},
		{"grid count", `[{"op":"table.format","at":"table:1","format":{"column_widths":[50]}}]`, "operation_failed", "ops[0].format.column_widths"},
		{"style", `[{"op":"table.format","at":"table:1","format":{"style":"Nope"}}]`, "not_found", "ops[0].format.style"},
	} {
		for _, mode := range []string{"track", "direct"} {
			path := writeDocx(t, tableBody)
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

// midTable is the address of the table near the middle paragraph mid of
// largeBody (a table follows every hundredth paragraph), for the scaling
// cases of the table operations.
func midTable(mid string) string {
	v, err := strconv.ParseUint(mid, 16, 32)
	if err != nil || v < 100 {
		return "table:1"
	}
	return "table:" + strconv.FormatUint(v/100, 10)
}

// gridBody is a plain 2x2 table between two paragraphs.
const gridBody = `<w:p w14:paraId="0000000A"/><w:tbl><w:tblPr><w:tblW w:w="0" w:type="auto"/></w:tblPr>` +
	`<w:tblGrid><w:gridCol w:w="2000"/><w:gridCol w:w="3000"/></w:tblGrid>` +
	`<w:tr><w:trPr><w:cantSplit/></w:trPr><w:tc><w:tcPr><w:tcW w:w="2000" w:type="dxa"/></w:tcPr><w:p w14:paraId="00000011"><w:r><w:t>A1</w:t></w:r></w:p></w:tc><w:tc><w:p w14:paraId="00000012"><w:r><w:t>B1</w:t></w:r></w:p></w:tc></w:tr>` +
	`<w:tr><w:tc><w:p w14:paraId="00000021"><w:r><w:t>A2</w:t></w:r></w:p></w:tc><w:tc><w:p w14:paraId="00000022"><w:r><w:t>B2</w:t></w:r></w:p></w:tc></w:tr>` +
	`</w:tbl><w:p w14:paraId="0000000B"/>`
