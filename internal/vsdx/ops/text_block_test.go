package ops_test

import (
	"encoding/json"
	"math"
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

// TestTextBoxPlacements checks text_block box on the three placements
// patent figures use, and an angle, written on a blank page (11 in
// high): read shows each box as written, writing it back changes
// nothing, and the cells are Visio's text transform cells in the
// shape's local inches (y up from its bottom left, along the line for
// a connector), derived by hand from the geometry:
//   - a region title on the top border of the rect [60, 80, 300, 160]:
//     box [110, 72, 200, 16], centre (210, 80) is local (150, 160) pt
//     = TxtPinX 2.0833333333333335, TxtPinY 2.2222222222222223 in;
//   - an arrow label 12 pt above the connector (60, 320)-(360, 320):
//     box [160, 300, 100, 16], centre (210, 308) is 150 pt along the
//     line and 12 pt to its left = TxtPinX 2.0833333333333335,
//     TxtPinY 0.16666666666666696 in;
//   - a numeral at the free end of the leader (420, 140)-(480, 100),
//     an open polyline whose box is [420, 100, 60, 40]: box
//     [482, 86, 30, 18], centre (497, 95) is local (77, 45) pt =
//     TxtPinX 1.0694444444444444, TxtPinY 0.625 in;
//   - every placed block TxtWidth and TxtHeight its size over 72,
//     TxtLocPinX and TxtLocPinY half of them with Visio's
//     TxtWidth*0.5 and TxtHeight*0.5, TxtAngle 0.
func TestTextBoxPlacements(t *testing.T) {
	out, _ := refusalOut(t, vsdxtest.Blank(t), `{"ops":[
		{"op":"object.add","at":"page0","geom":"rect","box":[60,80,300,160],"html":"<p>Region 300</p>","fill":{"pattern":"hatch_up","bg":"#FFFFFF"},"text_block":{"box":[110,72,200,16],"background":"#FFFFFF"}},
		{"op":"object.add","at":"page0","kind":"connector","from":[60,320],"to":[360,320],"html":"<p>signal 310</p>","text_block":{"box":[160,300,100,16]}},
		{"op":"object.add","at":"page0","geom":"polyline","points":[[420,140],[480,100]],"html":"<p>312</p>","text_block":{"box":[482,86,30,18],"margins":0}},
		{"op":"object.add","at":"page0","geom":"rect","box":[400,250,120,80],"html":"<p>turned</p>","text_block":{"box":[400,280,120,20],"angle":30}}]}`)
	for id, want := range map[string]string{
		"page0/s1": `{"box":[110,72,200,16],"valign":"middle","margins":[4,4,4,4],"background":{"hex":"#FFFFFF"}}`,
		"page0/s2": `{"box":[160,300,100,16],"valign":"middle","margins":[4,4,4,4]}`,
		"page0/s3": `{"box":[482,86,30,18],"valign":"middle","margins":[0,0,0,0]}`,
		"page0/s4": `{"box":[400,280,120,20],"angle":30,"valign":"middle","margins":[4,4,4,4]}`,
	} {
		if got := string(readObj(t, out, id)["text_block"]); got != want {
			t.Errorf("%s text_block = %s, want %s", id, got, want)
		}
	}
	for _, c := range []struct {
		id         uint32
		pinX, pinY string
		w, h       string
	}{
		{1, "2.0833333333333335", "2.2222222222222223", "2.7777777777777777", "0.2222222222222222"},
		{2, "2.0833333333333335", "0.16666666666666696", "1.3888888888888888", "0.2222222222222222"},
		{3, "1.0694444444444444", "0.625", "0.4166666666666667", "0.25"},
	} {
		cells := shapeCells(t, out, c.id)
		num := func(v string) float64 {
			f, err := strconv.ParseFloat(v, 64)
			if err != nil {
				t.Fatalf("s%d: %q is not a number", c.id, v)
			}
			return f
		}
		for name, want := range map[string]string{
			"TxtPinX": c.pinX, "TxtPinY": c.pinY, "TxtWidth": c.w, "TxtHeight": c.h, "TxtAngle": "0",
			"TxtLocPinX": strconv.FormatFloat(num(c.w)/2, 'g', -1, 64), "TxtLocPinY": strconv.FormatFloat(num(c.h)/2, 'g', -1, 64),
		} {
			got, ok := cells[name]
			if !ok || math.Abs(num(got[0])-num(want)) > 1e-12 || got[1] != "" {
				t.Errorf("s%d %s = %q, want %s in", c.id, name, got, want)
			}
		}
		if cells["TxtLocPinX"][2] != "TxtWidth*0.5" || cells["TxtLocPinY"][2] != "TxtHeight*0.5" || cells["TxtPinX"][2] != "" {
			t.Errorf("s%d formulas: LocPinX %q LocPinY %q PinX %q, want Visio's TxtWidth*0.5, TxtHeight*0.5 and none", c.id, cells["TxtLocPinX"][2], cells["TxtLocPinY"][2], cells["TxtPinX"][2])
		}
	}
	if _, changed := refusalOut(t, out, `{"ops":[
		{"op":"object.set","at":"page0/s1","text_block":{"box":[110,72,200,16]}},
		{"op":"object.set","at":"page0/s2","text_block":{"box":[160,300,100,16]}},
		{"op":"object.set","at":"page0/s3","text_block":{"box":[482,86,30,18]}},
		{"op":"object.set","at":"page0/s4","text_block":{"box":[400,280,120,20],"angle":30}}]}`); len(changed) != 0 {
		t.Errorf("writing back read's boxes: changed = %v, want none", changed)
	}
	// angle alone turns the block about its centre.
	turned, _ := refusalOut(t, out, `{"ops":[{"op":"object.set","at":"page0/s1","text_block":{"angle":90}}]}`)
	if got := string(readObj(t, turned, "page0/s1")["text_block"]); !strings.HasPrefix(got, `{"box":[110,72,200,16],"angle":90,`) {
		t.Errorf("after angle 90: text_block = %s", got)
	}
}

// TestTextBoxPlanErrors checks the plan-shape faults of box and angle.
func TestTextBoxPlanErrors(t *testing.T) {
	for _, c := range []struct{ tb, code, path string }{
		{`{"box":[0,0,10]}`, "invalid_operation", "ops[0].text_block.box"},
		{`{"box":[0,0,0,10]}`, "invalid_operation", "ops[0].text_block.box"},
		{`{"box":[0,0,10,-1]}`, "invalid_operation", "ops[0].text_block.box"},
		{`{"angle":"30"}`, "invalid_plan", "ops[0].text_block.angle"},
	} {
		_, errs := ops.ParsePlan([]byte(`{"ops":[{"op":"object.set","at":"page0/s1","text_block":` + c.tb + `}]}`))
		if len(errs) != 1 || errs[0].Code != c.code || errs[0].Path != c.path {
			t.Errorf("text_block %s: errs = %v, want one %s at %s", c.tb, errs, c.code, c.path)
		}
	}
}

// TestTextBoxDynamicConnector checks moving the text of a Visio-drawn
// Dynamic connector (dave-howard-vsdx test9_rect_and_line.vsdx s3,
// whose master sets TxtPinX and TxtPinY from Controls.TextPosition):
// the move writes the control's X and Y, with the pins and XDyn, YDyn
// as F="Inh" caches, as Visio stores moved connector text (the corpus's
// Dynamic connectors with moved text); read shows the new box; writing
// it back changes nothing; and a new size, which the master's
// TEXTHEIGHT formula owns, is refused at text_block.
func TestTextBoxDynamicConnector(t *testing.T) {
	src := corpusFile(t, "dave-howard-vsdx/tests/test9_rect_and_line.vsdx")
	if got := string(readObj(t, src, "page0/s3")["text_block"]); !strings.HasPrefix(got, `{"box":[135.91,181.12,40,17.6]`) {
		t.Fatalf("source s3 text_block = %s", got)
	}
	out, changed := refusalOut(t, src, `{"ops":[{"op":"object.set","at":"page0/s3","text_block":{"box":[135.91,140,40,17.6]}}]}`)
	if len(changed) != 1 {
		t.Fatalf("changed = %v, want s3", changed)
	}
	if got := string(readObj(t, out, "page0/s3")["text_block"]); !strings.HasPrefix(got, `{"box":[135.91,140,40,17.6]`) {
		t.Errorf("moved s3 text_block = %s", got)
	}
	cells := shapeCells(t, out, 3)
	if cells["TxtPinX"][2] != "Inh" || cells["TxtPinY"][2] != "Inh" {
		t.Errorf("TxtPinX %q TxtPinY %q, want F=Inh caches", cells["TxtPinX"], cells["TxtPinY"])
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var ctrl map[string][3]string
	for sec := range textSetShapeNode(t, data, 3).ElementsSeq() {
		if n, _ := sec.Attr("N"); sec.Local() != vsd.ElemSection || n != "Controls" {
			continue
		}
		for row := range sec.ElementsSeq() {
			if n, _ := row.Attr("N"); n != "TextPosition" {
				continue
			}
			ctrl = map[string][3]string{}
			for c := range row.ElementsSeq() {
				n, _ := c.Attr("N")
				v, _ := c.Attr("V")
				f, _ := c.Attr("F")
				ctrl[n] = [3]string{v, "", f}
			}
		}
	}
	if ctrl == nil || ctrl["X"][0] != cells["TxtPinX"][0] || ctrl["Y"][0] != cells["TxtPinY"][0] || ctrl["X"][2] != "" || ctrl["YDyn"] != [3]string{cells["TxtPinY"][0], "", "Inh"} {
		t.Errorf("Controls.TextPosition = %v, want X and Y the pins (no formula), XDyn and YDyn their F=Inh caches", ctrl)
	}
	if _, changed := refusalOut(t, out, `{"ops":[{"op":"object.set","at":"page0/s3","text_block":{"box":[135.91,140,40,17.6]}}]}`); len(changed) != 0 {
		t.Errorf("identity: changed = %v", changed)
	}
	expectRefusal(t, out, `{"ops":[{"op":"object.set","at":"page0/s3","text_block":{"box":[135.91,140,40,30]}}]}`, "text_block", "TxtHeight is bound to a formula")
}
