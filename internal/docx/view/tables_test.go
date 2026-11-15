package view

import (
	"fmt"
	"testing"
)

// TestGridColumnCountsGridBefore checks that a cell's grid column counts
// the columns its row skips (w:gridBefore) and the spans before it, as
// the table operations' column addresses do.
func TestGridColumnCountsGridBefore(t *testing.T) {
	cell := func(span string) string {
		if span == "" {
			return `<w:tc><w:p/></w:tc>`
		}
		return `<w:tc><w:tcPr><w:gridSpan w:val="` + span + `"/></w:tcPr><w:p/></w:tc>`
	}
	body := `<w:tbl><w:tblGrid><w:gridCol/><w:gridCol/><w:gridCol/><w:gridCol/><w:gridCol/></w:tblGrid>` +
		`<w:tr><w:trPr><w:gridBefore w:val="2"/></w:trPr>` + cell("2") + cell("") + `</w:tr>` +
		`<w:tr>` + cell("") + cell("") + `</w:tr>` +
		`<w:tr><w:trPr><w:gridBefore w:val="x"/></w:trPr>` + cell("") + `</w:tr></w:tbl>`
	d := build(t, map[string]string{"word/document.xml": docHead + body + `<w:sectPr/></w:body></w:document>`})
	tables := TableInventory(d.XML)
	rows := tables[0].(map[string]any)["rows"].([]any)
	var got []string
	for _, r := range rows {
		for _, c := range r.(map[string]any)["cells"].([]any) {
			got = append(got, fmt.Sprint(c.(map[string]any)["grid_column"]))
		}
	}
	if want := "[3 5 1 2 1]"; fmt.Sprint(got) != want {
		t.Errorf("grid columns %v, want %s", got, want)
	}
}
