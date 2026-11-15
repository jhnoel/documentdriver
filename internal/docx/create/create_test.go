package create

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/integrity"
	"github.com/jhnoel/documentdriver/internal/docx/math"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/read"
)

const sample = `<h1>Title <i>here</i></h1>
<p>Plain <b>bold</b> and <a href="https://example.com/x">a link</a>, area <equation display="inline">x^2</equation>.</p>
<p class="Quote" style="text-align:center">Quoted<br/>line&#9;tab</p>
<h3>Page <page-number></page-number> of <page-count></page-count></h3>
<p><equation display="block">a+b</equation></p>`

// TestCreate: the document passes the created-file gate, reads back with
// its headings, styles, link, equations and fields, and the same input
// gives the same bytes.
func TestCreate(t *testing.T) {
	out, err := Build(sample)
	if err != nil {
		t.Fatal(err)
	}
	if err := integrity.GateCreated(out); err != nil {
		t.Fatalf("gate: %v", err)
	}
	again, err := Build(sample)
	if err != nil || !bytes.Equal(out, again) {
		t.Errorf("not deterministic: %v", err)
	}
	d, err := model.OpenDocument(out)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(d.XML.AllParagraphs()); n != 5 {
		t.Errorf("%d paragraphs, want 5", n)
	}
	if r := d.ParaIDs(); len(r.Repaired) != 0 {
		t.Errorf("paragraphs without a stored id: %d", len(r.Repaired))
	}
	res, err := read.Read(out, "document", "", read.Options{Equation: math.OMMLToLaTeX})
	if err != nil {
		t.Fatal(err)
	}
	s, err := read.Encode(res.Object(), false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<h1 `, `<b>bold</b>`, `https://example.com/x`, `x^{2}`, `Quote`, `<h3 `, `PAGE`, `a+b`} {
		if !strings.Contains(s, want) {
			t.Errorf("read back has no %s:\n%s", want, s)
		}
	}
}

// TestCreateRefusals: what P4 does not build is refused, naming the
// element.
func TestCreateRefusals(t *testing.T) {
	for _, c := range []struct{ in, path string }{
		{`<p><img src="data:image/png;base64,iVBORw0KGgo="/></p>`, "html<img>"},
		{`<p>a<footnote>n</footnote></p>`, "html<footnote>"},
		{`<p class="NoSuchStyle">x</p>`, "html@class"},
		{``, "html"},
	} {
		_, err := Build(c.in)
		var e *Error
		if !errors.As(err, &e) || e.Code != "invalid_operation" || e.Path != c.path {
			t.Errorf("%s: %v (%+v), want invalid_operation at %s", c.in, err, e, c.path)
		}
	}
}

// TestCreateBlockEquationParagraph: a top-level block equation is a
// paragraph of its own between the others; a top-level inline equation
// is refused, as other inline content outside a paragraph is.
func TestCreateBlockEquationParagraph(t *testing.T) {
	out, err := Build("<p>a</p>\n<equation display=\"block\">x^2</equation>\n<p>b</p>")
	if err != nil {
		t.Fatal(err)
	}
	if err := integrity.GateCreated(out); err != nil {
		t.Fatal(err)
	}
	d, err := model.OpenDocument(out)
	if err != nil {
		t.Fatal(err)
	}
	ps := d.XML.AllParagraphs()
	if len(ps) != 3 || len(model.CurrentEquations(ps[1])) != 1 || model.ParagraphText(ps[2], model.All) != "b" {
		t.Fatalf("%d paragraphs", len(ps))
	}
	_, err = Build(`<p>a</p><equation display="inline">x</equation>`)
	var refused *Error
	if !errors.As(err, &refused) || refused.Code != "invalid_operation" || !strings.Contains(refused.Msg, "outside a paragraph") {
		t.Errorf("a top-level inline equation: %v", err)
	}
}
