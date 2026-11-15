package build

import (
	"errors"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/html"
	"github.com/jhnoel/documentdriver/internal/docx/math"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// blocksOf lowers document HTML to blocks.
func blocksOf(t *testing.T, s string) []Block {
	t.Helper()
	secs, err := html.ParseDocument(s, math.LaTeXToOMML)
	if err != nil {
		t.Fatal(err)
	}
	if secs[0].BodyErr != nil {
		t.Fatal(secs[0].BodyErr)
	}
	b, err := Blocks(secs[0].Body)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// placeAll places a table's cell paragraphs with their inline content.
func placeAll(h *core.Handle, container, at *tree.Node, p Para) error {
	nodes, err := h.Workspace().Parse(container, []byte(`<w:p/>`), nsW)
	if err != nil {
		return err
	}
	if err := h.PlaceAt(container, nodes[0], at); err != nil {
		return err
	}
	_, err = Inline(h, nodes[0], nil, p.Atoms, Options{})
	return err
}

// TestPlaceTableIsTableInsert: a plain grid is the table table.insert
// writes (NewTable filled with Inline), byte for byte.
func TestPlaceTableIsTableInsert(t *testing.T) {
	h, p, _ := fixture(t)
	body := p.Parent()
	b := blocksOf(t, `<table><tr><td></td><td>a</td><td></td></tr><tr><td></td><td><b>x</b></td><td></td></tr></table>`)
	got, err := PlaceTable(h, body, p, b[0].Table, placeAll)
	if err != nil {
		t.Fatal(err)
	}
	want, paras, err := NewTable(h, body, 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.PlaceAt(body, want, p); err != nil {
		t.Fatal(err)
	}
	for i, s := range map[int]string{1: "a", 4: "<b>x</b>"} {
		if _, err := Inline(h, paras[i], nil, atoms(t, s), Options{}); err != nil {
			t.Fatal(err)
		}
	}
	if string(got.Bytes()) != string(want.Bytes()) {
		t.Errorf("got  %s\nwant %s", got.Bytes(), want.Bytes())
	}
}

// TestPlaceTableSpansAndStyle: colspan is w:gridSpan, rowspan a vMerge
// restart over a continuation cell, a short row w:gridAfter, a nested
// table ends its cell with an empty paragraph, and the style read writes
// becomes the properties it describes, in schema order.
func TestPlaceTableSpansAndStyle(t *testing.T) {
	h, p, _ := fixture(t)
	b := blocksOf(t, `<table style="width:50%;margin-left:auto;margin-right:0;border-top:1pt dashed currentColor;padding-left:5.4pt;border-collapse:separate;border-spacing:1pt;table-layout:fixed;background-color:#00ff00">`+
		`<tr style="height:20pt;break-inside:avoid"><td colspan="2" style="vertical-align:middle;width:72pt">a</td><td rowspan="2">b</td></tr>`+
		`<tr><td>c</td><td><table><tr><td>n</td></tr></table></td></tr><tr><td>short</td></tr></table>`)
	tbl, err := PlaceTable(h, p.Parent(), p, b[0].Table, placeAll)
	if err != nil {
		t.Fatal(err)
	}
	got := string(tbl.Bytes())
	for _, want := range []string{
		`<w:tblPr><w:tblW w:w="2500" w:type="pct"/><w:jc w:val="right"/><w:tblCellSpacing w:w="20" w:type="dxa"/><w:tblBorders><w:top w:val="dashed" w:sz="8" w:space="0" w:color="auto"/></w:tblBorders><w:shd w:val="clear" w:color="auto" w:fill="00FF00"/><w:tblLayout w:type="fixed"/><w:tblCellMar><w:left w:w="108" w:type="dxa"/></w:tblCellMar></w:tblPr>`,
		`<w:tblGrid><w:gridCol w:w="2400"/><w:gridCol w:w="2400"/><w:gridCol w:w="2400"/></w:tblGrid>`,
		`<w:trPr><w:cantSplit/><w:trHeight w:val="400"/></w:trPr>`,
		`<w:tcPr><w:tcW w:w="1440" w:type="dxa"/><w:gridSpan w:val="2"/><w:vAlign w:val="center"/></w:tcPr>`,
		`<w:tc><w:tcPr><w:vMerge w:val="restart"/></w:tcPr>`,
		`<w:tc><w:tcPr><w:vMerge/></w:tcPr><w:p/></w:tc></w:tr>`,
		`</w:tbl><w:p/></w:tc>`,
		`<w:tr><w:trPr><w:gridAfter w:val="2"/></w:trPr>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %s in\n%s", want, got)
		}
	}
}

// TestTableRefusals: structure HTML cannot have in a table, styles that
// are not a table's, and grids Word cannot hold are refused as content.
func TestTableRefusals(t *testing.T) {
	for _, c := range []struct{ in, attr string }{
		{`<table style="color:red"><tr><td>a</td></tr></table>`, "style"},
		{`<table style="vertical-align:top"><tr><td>a</td></tr></table>`, "style"},
		{`<table><tr><td style="height:3pt">a</td></tr></table>`, "style"},
		{`<table><tr style="width:3pt"><td>a</td></tr></table>`, "style"},
		{`<table style="width:12px"><tr><td>a</td></tr></table>`, "style"},
		{`<table style="margin-left:3pt;margin-right:auto"><tr><td>a</td></tr></table>`, "style"},
		{`<table><tr><td style="background-color:red">a</td></tr></table>`, "style"},
		{`<table><tr><td style="border-top:1pt groove #000000">a</td></tr></table>`, "style"},
		{`<table><tr><td colspan="64">a</td></tr></table>`, ""},
		{`<table><tr><td colspan="0">a</td></tr></table>`, ""},
		{`<table><tr><td colspan="x">a</td></tr></table>`, ""},
		{`<table><tr><td>a</td><td rowspan="2">b</td></tr><tr></tr></table>`, ""},
	} {
		h, p, _ := fixture(t)
		secs, err := html.ParseDocument(c.in, math.LaTeXToOMML)
		if err == nil && secs[0].BodyErr != nil {
			err = secs[0].BodyErr
		}
		var b []Block
		if err == nil {
			b, err = Blocks(secs[0].Body)
		}
		if err == nil {
			_, err = PlaceTable(h, p.Parent(), p, b[0].Table, placeAll)
		}
		var at *AttributeError
		switch {
		case err == nil:
			t.Errorf("%s: accepted", c.in)
		case c.attr != "" && (!errors.As(err, &at) || at.Attr != c.attr):
			t.Errorf("%s: %v, want a refusal of %s", c.in, err, c.attr)
		case c.attr == "" && !errors.Is(err, ErrContent) && !strings.Contains(err.Error(), "row"):
			t.Errorf("%s: %v, want a refusal of the content", c.in, err)
		}
	}
}

// TestParagraphsRefuseTables: the paragraph lowering the operations use
// still refuses table structure, naming the element.
func TestParagraphsRefuseTables(t *testing.T) {
	secs, err := html.ParseDocument(`<table><tr><td>a</td></tr></table>`, math.LaTeXToOMML)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Paragraphs(secs[0].Body)
	var el *ElementError
	if !errors.As(err, &el) || el.Element != "table" {
		t.Errorf("%v, want <table> refused", err)
	}
}
