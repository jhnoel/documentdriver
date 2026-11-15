package edit

import (
	"strings"
	"testing"
)

func TestStyleKernelChunk(t *testing.T) {
	tbl := StyleTable{NumFmts: map[int]string{170: "custom"}, Fonts: []string{`<font><b/><i/><color rgb="FF000000"/><name val="X"/></font>`}, XFs: []string{`<xf fontId="0" numFmtId="0"><alignment vertical="top"/><protection locked="1"/></xf>`, `<xf applyNumberFormat="1" numFmtId="14" fontId="0"><alignment vertical="top"/><protection locked="1"/></xf>`}}
	if p, err := PlanNumFmt(tbl, 0, 14); err != nil || p.XF != 1 || p.NewXF != "" {
		t.Fatalf("intern: %+v %v", p, err)
	}
	no := false
	color := "abcdef"
	align := "center"
	wrap := true
	code := `a"&b`
	p, err := PlanFormat(tbl, 0, CellFormat{Bold: &no, Color: &color, Align: &align, Wrap: &wrap, NumberFormat: &code})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{`numFmtId="171"`, `formatCode="a&quot;&amp;b"`} {
		if !strings.Contains(p.NewNumFmt, s) {
			t.Errorf("numfmt %q", p.NewNumFmt)
		}
	}
	if strings.Contains(p.NewFont, "<b/>") || !strings.Contains(p.NewFont, `rgb="FFABCDEF"`) {
		t.Errorf("font %q", p.NewFont)
	}
	if !strings.Contains(p.NewXF, `<alignment vertical="top" horizontal="center" wrapText="1"/>`) || !strings.Contains(p.NewXF, `applyAlignment="1"`) {
		t.Errorf("xf %q", p.NewXF)
	}
	bad := "xx0000"
	if p, err := PlanFormat(tbl, 0, CellFormat{Fill: &bad}); err == nil || p != (StylePlan{}) {
		t.Errorf("bad color: %+v %v", p, err)
	}
	if _, _, ok := NumFmtOf(StyleTable{XFs: []string{`<xf/>`}}, 0); ok {
		t.Error("missing numFmtId accepted")
	}
	if _, err := PlanNumFmt(tbl, -1, 14); err == nil {
		t.Error("negative xf accepted")
	}
}

func TestStyleKeepsSpelling(t *testing.T) {
	// Single quotes, a double space and a newline before an attribute:
	// kept when another attribute of the same tag is edited.
	xf := "<xf  fontId='0'\nnumFmtId=\"0\"  applyNumberFormat=\"0\"/>"
	tbl := StyleTable{XFs: []string{xf}}
	p, err := PlanNumFmt(tbl, 0, 14)
	if err != nil {
		t.Fatal(err)
	}
	want := "<xf  fontId='0'\nnumFmtId=\"14\"  applyNumberFormat=\"1\"/>"
	if p.NewXF != want {
		t.Errorf("NewXF = %q, want %q", p.NewXF, want)
	}
	// equalStyle compares decoded values: spelling differences alone
	// do not make two elements unequal.
	a, err := parseStyle("<xf fontId='0' numFmtId=\"14\"/>")
	if err != nil {
		t.Fatal(err)
	}
	b, err := parseStyle(`<xf  numFmtId="14"   fontId="0"  />`)
	if err != nil {
		t.Fatal(err)
	}
	if !equalStyle(a, b) {
		t.Error("same attributes in different spellings compare unequal")
	}
}
