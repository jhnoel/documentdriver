package ops_test

import (
	"encoding/json"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// shapeCells returns the shape-level cells of shape id in the saved
// drawing at path, by name: V, U and F.
func shapeCells(t *testing.T, path string, id uint32) map[string][3]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][3]string{}
	for c := range textSetShapeNode(t, data, id).ElementsSeq() {
		if c.Local() != vsd.ElemCell {
			continue
		}
		n, _ := c.Attr("N")
		v, _ := c.Attr("V")
		u, _ := c.Attr("U")
		f, _ := c.Attr("F")
		out[n] = [3]string{v, u, f}
	}
	return out
}

// TestTextBlockWriteRead checks the text_block rows on object.add and
// object.set: valign, margins and background store Visio's cells
// (VerticalAlign 0/1/2; margins in inches with U="PT"; TextBkgnd
// #RRGGBB or 0, TextBkgndTrans a fraction), read shows them back in
// the plan's form, writing back what read shows changes nothing, and a
// single margins number sets all four sides. Expected inches are the
// points divided by 72 (20 pt = 0.2777777777777778 in, 12 pt =
// 0.16666666666666666 in, 4 pt = 0.05555555555555555 in, Visio's 4 pt
// default as the corpus stores it).
func TestTextBlockWriteRead(t *testing.T) {
	out, changed := refusalOut(t, vsdxtest.Blank(t), `{"ops":[
		{"op":"object.add","at":"page0","geom":"rect","box":[40,40,160,100],"html":"<p>a</p>","text_block":{"valign":"bottom","margins":[20,4,4,12],"background":{"hex":"#ffff00","transparency":25}}},
		{"op":"object.add","at":"page0","geom":"rect","box":[220,40,160,100],"html":"<p>b</p>","text_block":{"valign":"top","margins":0,"background":"none"}},
		{"op":"object.add","at":"page0","geom":"rect","box":[400,40,160,100],"html":"<p>c</p>"}]}`)
	if len(changed) != 3 {
		t.Fatalf("changed = %v, want three shapes", changed)
	}
	cells := shapeCells(t, out, 1)
	for name, want := range map[string][3]string{
		"VerticalAlign":  {"2", "", ""},
		"LeftMargin":     {"0.2777777777777778", "PT", ""},
		"BottomMargin":   {"0.16666666666666666", "PT", ""},
		"TextBkgnd":      {"#FFFF00", "", ""},
		"TextBkgndTrans": {"0.25", "", ""},
	} {
		if got := cells[name]; got != want {
			t.Errorf("s1 %s = %q, want %q", name, got, want)
		}
	}
	// TopMargin and RightMargin equal the style's 4 pt: not written.
	for _, name := range []string{"TopMargin", "RightMargin"} {
		if got, ok := cells[name]; ok {
			t.Errorf("s1 %s = %q, want none (the style's value)", name, got)
		}
	}
	for id, want := range map[string]string{
		"page0/s1": `{"valign":"bottom","margins":[20,4,4,12],"background":{"hex":"#FFFF00","transparency":25}}`,
		"page0/s2": `{"valign":"top","margins":[0,0,0,0],"background":"none"}`,
	} {
		if got := string(readObj(t, out, id)["text_block"]); got != want {
			t.Errorf("%s text_block = %s, want %s", id, got, want)
		}
	}
	if got := readObj(t, out, "page0/s3")["text_block"]; got != nil {
		t.Errorf("s3 text_block = %s, want none (only the style sheets' values)", got)
	}
	if got := shapeCells(t, out, 2)["TextBkgnd"]; got != [3]string{"0", "", ""} {
		t.Errorf("s2 TextBkgnd = %q, want 0", got)
	}
	if _, changed := refusalOut(t, out, `{"ops":[
		{"op":"object.set","at":"page0/s1","text_block":{"valign":"bottom","margins":[20,4,4,12],"background":{"hex":"#FFFF00","transparency":25}}},
		{"op":"object.set","at":"page0/s2","text_block":{"valign":"top","margins":[0,0,0,0],"background":"none"}},
		{"op":"object.set","at":"page0/s3","text_block":{"valign":"middle","margins":4}}]}`); len(changed) != 0 {
		t.Errorf("writing back read's text_block: changed = %v, want none", changed)
	}
	set, _ := refusalOut(t, out, `{"ops":[{"op":"object.set","at":"page0/s3","text_block":{"valign":"top"}}]}`)
	if got := string(readObj(t, set, "page0/s3")["text_block"]); got != `{"valign":"top","margins":[4,4,4,4]}` {
		t.Errorf("s3 after valign top: text_block = %s, want valign top and the style's margins", got)
	}
}

// TestTextBlockValign checks each valign value's stored VerticalAlign.
func TestTextBlockValign(t *testing.T) {
	for valign, want := range map[string]string{"top": "0", "middle": "1", "bottom": "2"} {
		// The style stores 1, so middle on a new shape writes nothing.
		out, _ := refusalOut(t, vsdxtest.Blank(t), `{"ops":[{"op":"object.add","at":"page0","geom":"rect","box":[0,0,10,10],"text_block":{"valign":"`+valign+`"}}]}`)
		got, ok := shapeCells(t, out, 1)["VerticalAlign"]
		if valign == "middle" {
			if ok {
				t.Errorf("middle: VerticalAlign = %q, want none (the style's 1)", got)
			}
			continue
		}
		if got[0] != want {
			t.Errorf("%s: VerticalAlign = %q, want %s", valign, got[0], want)
		}
	}
}

// TestTextBlockUnits checks margins in a plan with units in.
func TestTextBlockUnits(t *testing.T) {
	out, _ := refusalOut(t, vsdxtest.Blank(t), `{"units":"in","ops":[{"op":"object.add","at":"page0","geom":"rect","box":[1,1,2,1],"text_block":{"margins":[0.5,0,0,0]}}]}`)
	if got := shapeCells(t, out, 1)["LeftMargin"]; got != [3]string{"0.5", "PT", ""} {
		t.Errorf("LeftMargin = %q, want 0.5 in", got)
	}
}

// TestTextBlockPlanErrors checks the plan-shape faults of text_block.
func TestTextBlockPlanErrors(t *testing.T) {
	for _, c := range []struct {
		tb, code, path string
	}{
		{`{"valign":"center"}`, "invalid_operation", "ops[0].text_block.valign"},
		{`{"margins":[1,2]}`, "invalid_operation", "ops[0].text_block.margins"},
		{`{"margins":[-1,0,0,0]}`, "invalid_operation", "ops[0].text_block.margins"},
		{`{"margins":"4"}`, "invalid_plan", "ops[0].text_block.margins"},
		{`{"background":"#12345"}`, "invalid_operation", "ops[0].text_block.background"},
		{`{"background":{"hex":"#FFFFFF","transparency":120}}`, "invalid_operation", "ops[0].text_block.background.transparency"},
		{`{"background":{"transparency":10}}`, "invalid_plan", "ops[0].text_block.background.hex"},
		{`{"background":{"hex":"#FFFFFF","alpha":1}}`, "invalid_plan", "ops[0].text_block.background.alpha"},
		{`{"padding":1}`, "invalid_plan", "ops[0].text_block.padding"},
		{`{}`, "invalid_plan", "ops[0].text_block"},
		{`"top"`, "invalid_plan", "ops[0].text_block"},
	} {
		_, errs := ops.ParsePlan([]byte(`{"ops":[{"op":"object.set","at":"page0/s1","text_block":` + c.tb + `}]}`))
		if len(errs) != 1 || errs[0].Code != c.code || errs[0].Path != c.path {
			t.Errorf("text_block %s: errs = %v, want one %s at %s", c.tb, errs, c.code, c.path)
		}
	}
	_, errs := ops.ParsePlan([]byte(`{"ops":[{"op":"object.add","at":"page0","kind":"picture","image":"x.png","box":[0,0,1,1],"text_block":{"valign":"top"}}]}`))
	if len(errs) != 1 || errs[0].Path != "ops[0].text_block" {
		t.Errorf("picture with text_block: errs = %v, want one fault at ops[0].text_block", errs)
	}
}

// TestTextBlockPolygon checks that a closed polyline (a diamond) holds
// its own label with a text block, as read shows it.
func TestTextBlockPolygon(t *testing.T) {
	out, _ := refusalOut(t, vsdxtest.Blank(t), `{"ops":[{"op":"object.add","at":"page0","geom":"polyline","closed":true,"points":[[150,250],[250,320],[150,390],[50,320]],"html":"<p>decide</p>","text_block":{"valign":"top","background":"#FFFFFF"}}]}`)
	o := readObj(t, out, "page0/s1")
	if got := string(o["html"]); got != `"<p>decide</p>"` {
		t.Errorf("html = %s, want the label", got)
	}
	if got := string(o["text_block"]); got != `{"valign":"top","margins":[4,4,4,4],"background":{"hex":"#FFFFFF"}}` {
		t.Errorf("text_block = %s", got)
	}
}

// textMarkers returns the IX values of the marker elements local (cp
// or pp) in the Text of shape node, in text order.
func textMarkers(shape *tree.Node, local string) []int {
	var out []int
	for c := range shape.ElementsSeq() {
		if c.Local() != vsd.ElemText {
			continue
		}
		for m := range c.ElementsSeq() {
			if m.Local() == local {
				ix, _ := m.Attr("IX")
				n, _ := strconv.Atoi(ix)
				out = append(out, n)
			}
		}
	}
	return out
}

// sectionRowCells returns, for each row of section in shape node, its
// cell names in order.
func sectionRowCells(shape *tree.Node, section string) [][]string {
	var out [][]string
	for c := range shape.ElementsSeq() {
		if n, _ := c.Attr("N"); c.Local() != vsd.ElemSection || n != section {
			continue
		}
		for r := range c.ElementsSeq() {
			var names []string
			for cell := range r.ElementsSeq() {
				n, _ := cell.Attr("N")
				names = append(names, n)
			}
			out = append(out, names)
		}
	}
	return out
}

// TestTextRunsVisioForm reproduces the run markup LibreOffice misread
// (an empty Row IX='0', a row used for two stretches, rows out of text
// order) and checks the form Visio writes (every Visio-authored text
// of the corpus): cp and pp markers numbered 0, 1, 2... in text order,
// each once, one row per marker, every Character row with the same
// cells (Font, Color, Style, Size, Strikethru) and every Paragraph row
// with the same cells, and read's html unchanged. The rows per case
// are the stretches of one format: "Bold plain italic plain" is four,
// "big small big" three; the label case two of each (two sizes, two
// paragraph formats); the bullet case one Character row and two
// Paragraph rows.
func TestTextRunsVisioForm(t *testing.T) {
	cases := []struct {
		html        string
		chars, pars int
	}{
		{`<p style="margin-bottom:6pt"><span style="font-size:11pt">label</span></p><p><span style="font-size:23pt"> </span></p>`, 2, 2},
		{`<p><b>Bold</b> plain <i>italic</i> plain</p>`, 4, 1},
		{`<ul><li>bullet</li></ul><p>not a bullet</p>`, 1, 2},
		{`<p><span style="font-size:20pt">big</span> small <span style="font-size:20pt">big</span></p>`, 3, 1},
	}
	var plan []string
	for i, c := range cases {
		h, err := json.Marshal(c.html)
		if err != nil {
			t.Fatal(err)
		}
		plan = append(plan, `{"op":"object.add","at":"page0","geom":"rect","box":[0,`+strconv.Itoa(i*80)+`,200,60],"html":`+string(h)+`}`)
	}
	out, _ := refusalOut(t, vsdxtest.Blank(t), `{"ops":[`+strings.Join(plan, ",")+`]}`)
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range cases {
		id := uint32(i + 1)
		shape := textSetShapeNode(t, data, id)
		for _, m := range []struct {
			local, section string
			want           int
			cells          []string
		}{
			{vsd.ElemCharMark, vsd.SectionCharacter, c.chars, []string{"Font", "Color", "Style", "Size", "Strikethru"}},
			{vsd.ElemParaMark, vsd.SectionParagraph, c.pars, []string{"IndFirst", "IndLeft", "IndRight", "SpLine", "SpBefore", "SpAfter", "HorzAlign", "Bullet", "BulletStr"}},
		} {
			got := textMarkers(shape, m.local)
			want := make([]int, m.want)
			for k := range want {
				want[k] = k
			}
			if !slices.Equal(got, want) {
				t.Errorf("case %d: %s markers = %v, want %v (in text order, each once)", i, m.local, got, want)
			}
			rows := sectionRowCells(shape, m.section)
			if len(rows) != m.want {
				t.Errorf("case %d: %d %s rows, want %d", i, len(rows), m.section, m.want)
			}
			for r, names := range rows {
				if !slices.Equal(names, m.cells) {
					t.Errorf("case %d: %s row %d cells = %v, want %v", i, m.section, r, names, m.cells)
				}
			}
		}
		var got string
		if err := json.Unmarshal(readObj(t, out, "page0/s"+strconv.Itoa(i+1))["html"], &got); err != nil {
			t.Fatal(err)
		}
		if got != c.html {
			t.Errorf("case %d: read html = %s, want %s", i, got, c.html)
		}
	}
	// The plain run after a bold one stores Style 0 (inherited), not
	// nothing: LibreOffice takes a cell a row leaves out from row 0.
	shape := textSetShapeNode(t, data, 2)
	var styles []string
	for c := range shape.ElementsSeq() {
		if n, _ := c.Attr("N"); c.Local() != vsd.ElemSection || n != vsd.SectionCharacter {
			continue
		}
		for r := range c.ElementsSeq() {
			for cell := range r.ElementsSeq() {
				if n, _ := cell.Attr("N"); n == "Style" {
					v, _ := cell.Attr("V")
					styles = append(styles, v)
				}
			}
		}
	}
	if !slices.Equal(styles, []string{"1", "0", "2", "0"}) {
		t.Errorf("Style per row = %v, want [1 0 2 0]", styles)
	}
}
