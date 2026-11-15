package splice

import (
	"errors"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

const sheet = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:x14ac="urn:x14ac"><dimension ref="A1:B2"/><sheetData><row r="1"><c r="A1"><v>1</v></c><c r="B1" t="s"><v>0</v></c></row><row r="2" x14ac:dyDescent="0.25"><c r="A2"><f>A1+1</f><v>2</v></c></row></sheetData><!-- c --></worksheet>`

// span returns the span of the first occurrence of s in sheet.
func span(t *testing.T, s string) (int, int) {
	t.Helper()
	i := strings.Index(sheet, s)
	if i < 0 {
		t.Fatalf("%q not in the fixture", s)
	}
	return i, i + len(s)
}

func TestReplace(t *testing.T) {
	p := New(sheet)
	if !p.Empty() || p.Len() != len(sheet) || p.String() != sheet {
		t.Fatal("new patch is not empty")
	}
	s, e := span(t, `<c r="A1"><v>1</v></c>`)
	if err := p.Replace(s, e, `<c r="A1"><v>1</v></c>`); err != nil || !p.Empty() {
		t.Fatalf("identity replacement kept: %v, empty %v", err, p.Empty())
	}
	if err := p.Replace(s, e, `<c r="A1"><v>42</v></c>`); err != nil {
		t.Fatal(err)
	}
	s2, e2 := span(t, `<row r="2" x14ac:dyDescent="0.25"><c r="A2"><f>A1+1</f><v>2</v></c></row>`)
	if err := p.Replace(s2, e2, ""); err != nil {
		t.Fatal(err)
	}
	// Given out of order: an insertion before the first edit.
	ins := strings.Index(sheet, `<row r="1">`)
	if err := p.Replace(ins, ins, `<row r="0"/>`); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(sheet, `<c r="A1"><v>1</v></c>`, `<c r="A1"><v>42</v></c>`, 1)
	want = strings.Replace(want, `<row r="2" x14ac:dyDescent="0.25"><c r="A2"><f>A1+1</f><v>2</v></c></row>`, "", 1)
	want = strings.Replace(want, `<row r="1">`, `<row r="0"/><row r="1">`, 1)
	if got := p.String(); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if p.Len() != len(want) {
		t.Fatalf("Len %d, want %d", p.Len(), len(want))
	}
	var b strings.Builder
	if n, err := p.WriteTo(&b); err != nil || int(n) != len(want) || b.String() != want {
		t.Fatalf("WriteTo: %d, %v", n, err)
	}
	if got := p.Spans(); len(got) != 3 || got[0] != (Span{ins, ins}) || got[1] != (Span{s, e}) || got[2] != (Span{s2, e2}) {
		t.Fatalf("Spans %v", got)
	}
	if err := p.Check(); err != nil {
		t.Fatal(err)
	}
	if err := tree.Check([]byte(p.String())); err != nil {
		t.Fatal(err)
	}
}

func TestReplaceOverlap(t *testing.T) {
	p := New(sheet)
	s, e := span(t, `<c r="A1"><v>1</v></c>`)
	if err := p.Replace(s, e, `<c r="A1"/>`); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name       string
		start, end int
		ok         bool
	}{
		{"same span", s, e, false},
		{"inside", s + 1, e - 1, false},
		{"straddles start", s - 1, s + 1, false},
		{"straddles end", e - 1, e + 1, false},
		{"covers", s - 1, e + 1, false},
		{"insertion inside", s + 3, s + 3, false},
		{"insertion at start", s, s, true},
		{"second insertion at start", s, s, true},
		{"insertion at end", e, e, true},
		{"adjacent after", e, e + 1, true},
		{"adjacent before", s - 1, s, true},
	} {
		err := p.Replace(c.start, c.end, "<x/>")
		if (err == nil) != c.ok {
			t.Errorf("%s: err %v, want ok %v", c.name, err, c.ok)
		}
		if err != nil && !errors.Is(err, ErrOverlap) {
			t.Errorf("%s: %v is not ErrOverlap", c.name, err)
		}
	}
	if err := p.Replace(-1, 0, ""); err == nil {
		t.Error("negative start accepted")
	}
	if err := p.Replace(0, len(sheet)+1, ""); err == nil {
		t.Error("end past the original accepted")
	}
	// Insertions at one point keep call order and precede a span
	// starting there.
	q := New("<a><b/></a>")
	for _, r := range []string{"<x/>", "<y/>"} {
		if err := q.Replace(3, 3, r); err != nil {
			t.Fatal(err)
		}
	}
	if err := q.Replace(3, 7, "<c/>"); err != nil {
		t.Fatal(err)
	}
	if err := q.Replace(3, 3, "<z/>"); err != nil {
		t.Fatal(err)
	}
	if got, want := q.String(), "<a><x/><y/><z/><c/></a>"; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestCheckSpans(t *testing.T) {
	cells := strings.Index(sheet, `<c r="A1">`)
	v1 := strings.Index(sheet, `<v>1</v>`)
	sd := strings.Index(sheet, `<sheetData>`) + len(`<sheetData>`)
	sdEnd := strings.Index(sheet, `</sheetData>`)
	row1End := strings.Index(sheet, `</row>`)
	comment := strings.Index(sheet, `<!-- c -->`)
	root := strings.Index(sheet, `<worksheet`)
	formula := strings.Index(sheet, `A1+1`)
	for _, c := range []struct {
		name       string
		start, end int
		repl       string
		want       error
	}{
		{"one element", cells, cells + len(`<c r="A1"><v>1</v></c>`), `<c r="A1"/>`, nil},
		{"run of siblings", cells, row1End, "", nil},
		{"text node", v1 + 3, v1 + 4, "7", nil},
		{"insertion at content start", sd, sd, `<row r="0"/>`, nil},
		{"insertion at content end", sdEnd, sdEnd, `<row r="9"/>`, nil},
		{"insertion between children", cells + len(`<c r="A1"><v>1</v></c>`), cells + len(`<c r="A1"><v>1</v></c>`), `<c r="Z1"/>`, nil},
		{"comment node", comment, comment + len(`<!-- c -->`), "", nil},
		{"root's content", strings.Index(sheet, `<dimension`), strings.Index(sheet, `</worksheet>`), "<sheetData/>", nil},
		{"prefixed in scope", cells, cells, `<x14ac:e/>`, nil},
		{"declared inside", cells, cells, `<p:e xmlns:p="urn:p"/>`, nil},
		{"inside a start tag", cells + 2, cells + 4, "", ErrSpan},
		{"insertion at text start", v1 + 3, v1 + 3, "<x/>", nil},
		{"inside text", formula + 2, formula + 2, "<x/>", ErrSpan},
		{"part of text", formula, formula + 2, "", ErrSpan},
		{"inside a comment", comment + 5, comment + 5, "<x/>", ErrSpan},
		{"crosses parents", cells, sdEnd, "", ErrSpan},
		{"start tag only", cells, cells + len(`<c r="A1">`), "", ErrSpan},
		{"the root", root, len(sheet), "<w/>", ErrSpan},
		{"the prolog", 0, 0, "<!-- -->", ErrSpan},
		{"undeclared prefix", cells, cells, `<q:e/>`, ErrReplacement},
		{"unbalanced", cells, cells, `<c>`, ErrReplacement},
		{"stray end tag", cells, cells, `</c>`, ErrReplacement},
		{"bad entity", cells, cells, `&nbsp;`, ErrReplacement},
		{"xml declaration", cells, cells, `<?xml version="1.0"?>`, ErrReplacement},
	} {
		p := New(sheet)
		if err := p.Replace(c.start, c.end, c.repl); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		err := p.Check()
		if c.want == nil {
			if err != nil {
				t.Errorf("%s: %v", c.name, err)
			} else if werr := tree.Check([]byte(p.String())); werr != nil {
				t.Errorf("%s: Check passed but the result is not well formed: %v", c.name, werr)
			}
			continue
		}
		if !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
}

func TestCheckMalformedOriginal(t *testing.T) {
	p := New("<a><b></a>")
	if err := p.Replace(3, 3, "<c/>"); err != nil {
		t.Fatal(err)
	}
	if err := p.Check(); err == nil {
		t.Fatal("malformed original passed")
	}
}

// TestCheckJunction: "]]>" made where text of the original meets a
// replacement's, which neither piece has alone.
func TestCheckJunction(t *testing.T) {
	for _, c := range []struct {
		orig, repl string
		start, end int
	}{
		{"<a>]]<b/>></a>", "", 5, 9},     // a deletion joins "]]" and ">"
		{"<a>]]<b/></a>", ">", 5, 9},     // raw text after "]]"
		{"<a><b/>></a>", "]]", 3, 7},     // text before ">"
		{"<a>]<b/>></a>", "]", 4, 8},     // three pieces
		{"<a>]<b/><c/>></a>", "]", 4, 8}, // with an empty piece beside
	} {
		p := New(c.orig)
		if err := p.Replace(c.start, c.end, c.repl); err != nil {
			t.Fatal(err)
		}
		if c.orig == "<a>]<b/><c/>></a>" {
			if err := p.Replace(8, 12, ""); err != nil {
				t.Fatal(err)
			}
		}
		if tree.Check([]byte(p.String())) == nil {
			t.Fatalf("%q: the case is well formed", p.String())
		}
		if err := p.Check(); !errors.Is(err, ErrReplacement) {
			t.Errorf("%q: %v, want ErrReplacement", p.String(), err)
		}
	}
	// "]]>" inside a piece's CDATA is markup, not a junction fault.
	p := New("<a><![CDATA[x]]><b/></a>")
	if err := p.Replace(16, 20, "<c/>"); err != nil {
		t.Fatal(err)
	}
	if err := p.Check(); err != nil {
		t.Fatal(err)
	}
}

func TestCheckRemembered(t *testing.T) {
	p := New(sheet)
	s, e := span(t, `<v>1</v>`)
	if err := p.Replace(s, e, `<v>2</v>`); err != nil {
		t.Fatal(err)
	}
	if err := p.Check(); err != nil || !p.checked {
		t.Fatalf("%v, checked %v", err, p.checked)
	}
	s, e = span(t, `<!-- c -->`)
	if err := p.Replace(s, e, `<bad`); err != nil {
		t.Fatal(err)
	}
	if p.checked {
		t.Fatal("check remembered across a change")
	}
	if err := p.Check(); !errors.Is(err, ErrReplacement) {
		t.Fatalf("%v, want ErrReplacement", err)
	}
}

func TestWriter(t *testing.T) {
	var w Writer
	w.Start("c")
	w.Attr("r", `A"1<&>`+"\t\n\r")
	w.Start("is")
	w.Start("t")
	w.Attr("xml:space", "preserve")
	w.Text(" a<b>&c\r ")
	w.End()
	w.End()
	w.Start("v")
	w.End()
	w.End()
	got, err := w.String()
	if err != nil {
		t.Fatal(err)
	}
	want := `<c r="A&quot;1&lt;&amp;&gt;&#9;&#10;&#13;"><is><t xml:space="preserve"> a&lt;b&gt;&amp;c&#13; </t></is><v/></c>`
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if err := tree.Check([]byte("<r>" + got + "</r>")); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		f    func(w *Writer)
	}{
		{"bad element name", func(w *Writer) { w.Start("1c") }},
		{"two colons", func(w *Writer) { w.Start("a:b:c") }},
		{"bad attribute name", func(w *Writer) { w.Start("c"); w.Attr("r r", "") }},
		{"attribute after content", func(w *Writer) { w.Start("c"); w.Text("x"); w.Attr("r", "") }},
		{"control character", func(w *Writer) { w.Start("c"); w.Text("\x01") }},
		{"invalid UTF-8", func(w *Writer) { w.Start("c"); w.Attr("r", "\xff") }},
		{"unclosed", func(w *Writer) { w.Start("c") }},
		{"extra End", func(w *Writer) { w.End() }},
	} {
		var w Writer
		c.f(&w)
		if _, err := w.String(); err == nil {
			t.Errorf("%s: no error", c.name)
		}
	}
	w.Reset()
	w.Text("x")
	if s, err := w.String(); err != nil || s != "x" {
		t.Fatalf("after Reset: %q, %v", s, err)
	}
}

// TestCheckWellFormedStops: over a proved original, Check stops after the
// last span, so a fault past it is not seen (the caller's proof covers
// it); New's Check scans the whole original and sees it.
func TestCheckWellFormedStops(t *testing.T) {
	orig := "<a><b/><c></a>" // malformed after <b/>
	for _, c := range []struct {
		p      *Patch
		wantOK bool
	}{{New(orig), false}, {NewWellFormed(orig), true}} {
		if err := c.p.Replace(3, 7, "<d/>"); err != nil {
			t.Fatal(err)
		}
		if err := c.p.Check(); (err == nil) != c.wantOK {
			t.Errorf("wellFormed=%v: Check %v", c.p.wellFormed, err)
		}
	}
	// Span faults before the stop are found as by New.
	p := NewWellFormed(sheet)
	cells := strings.Index(sheet, `<c r="A1">`)
	if err := p.Replace(cells+2, cells+4, ""); err != nil {
		t.Fatal(err)
	}
	if err := p.Check(); !errors.Is(err, ErrSpan) {
		t.Fatalf("got %v, want ErrSpan", err)
	}
}
