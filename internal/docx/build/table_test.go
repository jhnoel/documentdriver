package build

import (
	"errors"
	"strings"
	"testing"
)

// TestNewTable: a new table is placed as one block with its grid and one
// empty paragraph per cell, in row-major order, and its cells take inline
// content; a shape outside 1..100 is refused.
func TestNewTable(t *testing.T) {
	h, p, _ := fixture(t)
	body := p.Parent()
	tbl, paras, err := NewTable(h, body, 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.PlaceAt(body, tbl, p); err != nil {
		t.Fatal(err)
	}
	if len(paras) != 6 {
		t.Fatalf("%d cell paragraphs, want 6", len(paras))
	}
	if _, err := Inline(h, paras[4], nil, atoms(t, "<b>x</b>"), Options{}); err != nil {
		t.Fatal(err)
	}
	got := string(tbl.Bytes())
	want := `<w:tbl><w:tblPr><w:tblW w:w="0" w:type="auto"/></w:tblPr><w:tblGrid><w:gridCol w:w="2400"/><w:gridCol w:w="2400"/><w:gridCol w:w="2400"/></w:tblGrid>` +
		`<w:tr><w:tc><w:p/></w:tc><w:tc><w:p/></w:tc><w:tc><w:p/></w:tc></w:tr>` +
		`<w:tr><w:tc><w:p/></w:tc><w:tc><w:p><w:r><w:rPr><w:b/></w:rPr><w:t>x</w:t></w:r></w:p></w:tc><w:tc><w:p/></w:tc></w:tr></w:tbl>`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if !strings.Contains(string(body.Bytes()), "</w:tbl><w:p/>") {
		t.Errorf("the table is not before its anchor: %s", body.Bytes())
	}
	for _, shape := range [][2]int{{0, 1}, {1, 0}, {101, 1}, {1, 101}} {
		if _, _, err := NewTable(h, body, shape[0], shape[1]); !errors.Is(err, ErrTableSize) {
			t.Errorf("%v: %v, want ErrTableSize", shape, err)
		}
	}
}
