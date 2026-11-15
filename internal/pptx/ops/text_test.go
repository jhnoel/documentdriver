package ops

import (
	"errors"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/pptx/ops/edit"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/text"
)

// TestAddWithHTML: a new shape with html gets a p:txBody holding an
// empty a:bodyPr, placed after p:spPr, and paragraphs with only what the
// HTML says.
func TestAddWithHTML(t *testing.T) {
	path, _, _ := diagram(t)
	if err := Apply(path, compile(t, `{"ops": [
	  {"op": "object.add", "at": "slide1", "kind": "shape", "name": "Cache", "geom": "rect", "box": [100, 50, 90, 60],
	   "html": "<p style=\"text-align:center\"><b>Cache</b></p>"}]}`)...); err != nil {
		t.Fatal(err)
	}
	s := slideXML(t, path)
	want := `<a:prstGeom prst="rect"/></p:spPr><p:txBody><a:bodyPr/><a:p><a:pPr algn="ctr"/><a:r><a:rPr b="1"/><a:t>Cache</a:t></a:r></a:p></p:txBody></p:sp></p:spTree>`
	if !strings.Contains(s, want) {
		t.Fatalf("new shape:\n%s", s[strings.LastIndex(s, "<p:sp>"):])
	}
}

// TestTextOps: text.set and text.replace on existing bodies, and html on
// object.set; written as shown is a no-op.
func TestTextOps(t *testing.T) {
	path, _, _ := diagram(t)
	if err := Apply(path, compile(t, `{"ops": [
	  {"op": "text.replace", "at": "slide1/s10", "select": "Gateway", "with": "Proxy"},
	  {"op": "object.set", "at": "slide1/s11", "html": "<p>DB</p>"},
	  {"op": "text.set", "at": "slide1/s2", "html": "<p lang=\"en-US\">Overview</p>"}]}`)...); err != nil {
		t.Fatal(err)
	}
	s := slideXML(t, path)
	for _, want := range []string{
		`<a:t> Proxy &amp; co</a:t>`,
		`<a:prstGeom prst="ellipse"><a:avLst></a:avLst></a:prstGeom><a:solidFill><a:schemeClr val="accent1"><a:lumMod val="75000"/><a:lumOff val="25000"/></a:schemeClr></a:solidFill></p:spPr><p:txBody><a:bodyPr/><a:p><a:r><a:t>DB</a:t></a:r></a:p></p:txBody>`,
		`<p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:r><a:rPr lang="en-US"/><a:t>Overview</a:t></a:r></a:p></p:txBody>`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s", want)
		}
	}
	// The sets again: every body already shows them, so nothing changes.
	if err := Apply(path, compile(t, `{"ops": [
	  {"op": "object.set", "at": "slide1/s11", "html": "<p>DB</p>"},
	  {"op": "text.set", "at": "slide1/s2", "html": "<p lang=\"en-US\">Overview</p>"}]}`)...); err != nil {
		t.Fatal(err)
	}
	if got := slideXML(t, path); got != s {
		t.Error("writing the shown text changed the slide")
	}
}

// TestTextRejects: refusals leave the file untouched.
func TestTextRejects(t *testing.T) {
	for _, c := range []struct {
		op   string
		want error
		msg  string
	}{
		{`{"op": "text.set", "at": "slide1/s12", "html": "x"}`, edit.ErrNotAChild, "txBody"},
		{`{"op": "text.replace", "at": "slide1/s10", "select": "nowhere", "with": "x"}`, text.ErrNoMatch, "nowhere"},
		{`{"op": "text.replace", "at": "slide1/s3", "select": "o", "with": "0"}`, text.ErrManyMatches, "2 times"},
		{`{"op": "text.replace", "at": "slide1/s10", "select": "API Gateway", "with": "x"}`, nil, ""},
		{`{"op": "text.replace", "at": "slide1/s11", "select": "x", "with": "y"}`, nil, "no text body"},
	} {
		path, before, mtime := diagram(t)
		err := Apply(path, compile(t, `{"ops": [`+c.op+`]}`)...)
		if c.msg == "" && c.want == nil {
			// Crossing run boundaries is allowed: merged into the first run.
			if err != nil {
				t.Errorf("%s: %v", c.op, err)
			}
			continue
		}
		if err == nil || (c.want != nil && !errors.Is(err, c.want)) || !strings.Contains(err.Error(), c.msg) {
			t.Errorf("%s: err = %v, want %v naming %q", c.op, err, c.want, c.msg)
		}
		assertUntouched(t, path, before, mtime)
	}
	// HTML outside the grammar is refused at compile, before the deck is
	// opened.
	for _, op := range []string{
		`{"op": "text.set", "at": "slide1/s10", "html": "<div>x</div>"}`,
		`{"op": "object.set", "at": "slide1/s10", "html": "<div>x</div>"}`,
		`{"op": "object.add", "at": "slide1", "kind": "shape", "name": "x", "geom": "rect", "box": [0, 0, 1, 1], "html": "<div>x</div>"}`,
	} {
		p, err := DecodePlan(strings.NewReader(`{"ops": [` + op + `]}`))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Compile(); !errors.Is(err, text.ErrGrammar) || !strings.Contains(err.Error(), "div") {
			t.Errorf("%s: compile err = %v", op, err)
		}
	}
	// html on a connector is refused at compile.
	p, err := DecodePlan(strings.NewReader(`{"ops": [{"op": "object.add", "at": "slide1", "kind": "connector",
	  "name": "x", "geom": "line", "from": [0, 0], "to": [1, 1], "html": "x"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Compile(); err == nil || !strings.Contains(err.Error(), "connectors hold no text") {
		t.Errorf("connector html: %v", err)
	}
}

// TestReplaceAll: all replaces every match.
func TestReplaceAll(t *testing.T) {
	path, _, _ := diagram(t)
	if err := Apply(path, compile(t, `{"ops": [
	  {"op": "text.replace", "at": "slide1/s3", "select": "o", "with": "0", "all": true}]}`)...); err != nil {
		t.Fatal(err)
	}
	s := slideXML(t, path)
	if !strings.Contains(s, `<a:t>0ne</a:t>`) || !strings.Contains(s, `<a:t>tw0</a:t>`) {
		t.Fatal(s)
	}
}
