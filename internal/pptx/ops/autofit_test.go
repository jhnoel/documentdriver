package ops

import (
	"os"
	"strings"
	"testing"
)

// bodyPrOf returns the API box's a:bodyPr element in the slide XML.
func bodyPrOf(t *testing.T, path string) string {
	t.Helper()
	s := slideXML(t, path)
	s = s[strings.Index(s, `name="API"`):]
	s = s[strings.Index(s, "<a:bodyPr"):]
	if i := strings.Index(s, "</a:bodyPr>"); i >= 0 && i < strings.Index(s, "<a:p>") {
		return s[:i+len("</a:bodyPr>")]
	}
	return s[:strings.Index(s, "/>")+2]
}

// TestAutofit: the autofit choice is written to the shape's own
// a:bodyPr; a new a:normAutofit holds only the scales the plan gives, an
// existing one keeps its scale while the mode stays shrink, and a change
// of mode drops it.
func TestAutofit(t *testing.T) {
	path, _, _ := diagram(t)
	run := func(op string) {
		t.Helper()
		if err := Apply(path, compile(t, `{"ops": [`+op+`]}`)...); err != nil {
			t.Fatal(err)
		}
	}
	run(`{"op": "object.set", "at": "slide1/s10", "text": {"autofit": "shrink"}}`)
	if b := bodyPrOf(t, path); !strings.Contains(b, "<a:normAutofit/>") {
		t.Fatalf("shrink: %s", b)
	}
	run(`{"op": "object.set", "at": "slide1/s10", "text": {"font_scale": 62.5, "ln_spc_reduction": 20}}`)
	if b := bodyPrOf(t, path); !strings.Contains(b, `<a:normAutofit fontScale="62500" lnSpcReduction="20000"/>`) {
		t.Fatalf("scale: %s", b)
	}
	// Other body properties leave the stored scale alone.
	run(`{"op": "object.set", "at": "slide1/s10", "text": {"vert": "vert270", "num_col": 2, "spc_col": 6, "rtl_col": true}}`)
	b := bodyPrOf(t, path)
	for _, want := range []string{`<a:normAutofit fontScale="62500" lnSpcReduction="20000"/>`, `vert="vert270"`, `numCol="2"`, `spcCol="76200"`, `rtlCol="1"`} {
		if !strings.Contains(b, want) {
			t.Errorf("missing %s in %s", want, b)
		}
	}
	// Written back as the view shows it: nothing changes.
	before, _ := os.ReadFile(path)
	st, _ := os.Stat(path)
	run(`{"op": "object.set", "at": "slide1/s10", "text": {"autofit": "shrink", "font_scale": 62.5, "ln_spc_reduction": 20, "vert": "vert270", "num_col": 2, "spc_col": 6, "rtl_col": true, "anchor_ctr": false}}`)
	assertUntouched(t, path, before, st.ModTime())
	// Another mode replaces the element, scales and all.
	run(`{"op": "object.set", "at": "slide1/s10", "text": {"autofit": "shape"}}`)
	if b := bodyPrOf(t, path); !strings.Contains(b, "<a:spAutoFit/>") || strings.Contains(b, "normAutofit") {
		t.Fatalf("shape: %s", b)
	}
}

func TestAutofitRejects(t *testing.T) {
	for _, op := range []struct{ op, want string }{
		{`{"op": "object.set", "at": "slide1/s10", "text": {"autofit": "fit"}}`, "want none, shape"},
		{`{"op": "object.set", "at": "slide1/s10", "text": {"autofit": "none", "font_scale": 50}}`, "only with autofit shrink"},
		{`{"op": "object.set", "at": "slide1/s10", "text": {"font_scale": 0.5}}`, "a:ST_TextFontScalePercent"},
		{`{"op": "object.set", "at": "slide1/s10", "text": {"num_col": 17}}`, "a:ST_TextColumnCount"},
		{`{"op": "object.set", "at": "slide1/s10", "text": {"vert": "sideways"}}`, "a:ST_TextVerticalType"},
		{`{"op": "object.set", "at": "slide1/s10", "text": {"spc_col": -1}}`, "a:ST_PositiveCoordinate32"},
		{`{"op": "object.add", "at": "slide1", "kind": "connector", "name": "x", "geom": "line", "from": [0, 0], "to": [1, 1], "fill": {"hex": "#FF0000"}}`, "only \"none\""},
		{`{"op": "object.add", "at": "slide1", "kind": "connector", "name": "x", "geom": "line", "from": [0, 0], "to": [1, 1], "tx_box": true}`, "tx_box is for shapes"},
	} {
		p, err := DecodePlan(strings.NewReader(`{"ops": [` + op.op + `]}`))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Compile(); err == nil || !strings.Contains(err.Error(), op.want) {
			t.Errorf("%s: %v, want %q", op.op, err, op.want)
		}
	}
}

// TestNonVisual: descr, title, hidden and txBox written to cNvPr and
// cNvSpPr; "" and false remove them; a connector takes fill none.
func TestNonVisual(t *testing.T) {
	path, _, _ := diagram(t)
	run := func(op string) {
		t.Helper()
		if err := Apply(path, compile(t, `{"ops": [`+op+`]}`)...); err != nil {
			t.Fatal(err)
		}
	}
	run(`{"op": "object.set", "at": "slide1/s10", "descr": "Alt", "title": "T", "hidden": true, "tx_box": true}`)
	s := slideXML(t, path)
	s = s[strings.Index(s, `name="API"`)-40:]
	for _, want := range []string{`descr="Alt"`, `title="T"`, `hidden="1"`, `<p:cNvSpPr txBox="1"`} {
		if !strings.Contains(s[:strings.Index(s, "</p:nvSpPr>")], want) {
			t.Errorf("missing %s in %s", want, s[:strings.Index(s, "</p:nvSpPr>")])
		}
	}
	run(`{"op": "object.set", "at": "slide1/s10", "descr": "", "title": "", "hidden": false, "tx_box": false}`)
	s = slideXML(t, path)
	nv := s[strings.Index(s, `name="API"`)-40:]
	nv = nv[:strings.Index(nv, "</p:nvSpPr>")]
	for _, gone := range []string{"descr=", "title=", "hidden=", "txBox="} {
		if strings.Contains(nv, gone) {
			t.Errorf("%s left in %s", gone, nv)
		}
	}
	run(`{"op": "object.set", "at": "slide1/s12", "fill": "none"}`)
	s = slideXML(t, path)
	if !strings.Contains(s, "<a:noFill/>") {
		t.Errorf("connector fill none not written")
	}
}
