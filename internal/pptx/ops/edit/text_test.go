package edit

import (
	"errors"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

const (
	nsA = "http://schemas.openxmlformats.org/drawingml/2006/main"
	pA  = `<a:p><a:r><a:t>A</a:t></a:r></a:p>`
	pB  = `<a:p><a:r><a:t>B</a:t></a:r></a:p>`
)

func bodyXML(h *Handle, i int) string {
	return string(h.FindIn(i, Path{P("txBody")}).Bytes())
}

// partBytes returns the slide part's bytes as the workspace would write
// them.
func partBytes(t *testing.T, w *Workspace) string {
	t.Helper()
	d, _, err := w.spTree(slide1)
	if err != nil {
		t.Fatal(err)
	}
	return string(d.Bytes())
}

func TestReplaceParagraphsRange(t *testing.T) {
	w := workspace(t, testdeck.Diagram(t))
	h := target(t, w, "slide1#3")
	before := partBytes(t, w)
	old := bodyXML(h, 0)
	if err := h.ReplaceParagraphs([][]byte{[]byte(pA + "\n" + pB)}); err != nil {
		t.Fatal(err)
	}
	want := `<p:txBody><a:bodyPr/><a:lstStyle/>` + pA + "\n" + pB + `</p:txBody>`
	if got := bodyXML(h, 0); got != want {
		t.Fatalf("body\n%s\nwant\n%s", got, want)
	}
	// Everything outside the paragraph range keeps its bytes.
	if after := partBytes(t, w); strings.Replace(after, want, old, 1) != before {
		t.Fatal("bytes outside the paragraphs changed")
	}
	wantVerify(t, w, "")
}

func TestReplaceParagraphsInsertsWhenNone(t *testing.T) {
	w := workspace(t, testdeck.Diagram(t))
	// A body with a:lstStyle and no paragraph: after a:lstStyle.
	h := target(t, w, "slide1#2")
	body := h.FindIn(0, Path{P("txBody")})
	for _, c := range body.Elements() {
		if derive.Is(c, derive.DML, "p") {
			if err := body.RemoveChild(c); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := h.ReplaceParagraphs([][]byte{[]byte(pA)}); err != nil {
		t.Fatal(err)
	}
	if got, want := bodyXML(h, 0), `<p:txBody><a:bodyPr/><a:lstStyle/>`+pA+`</p:txBody>`; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	// A new body in both mc branches (Ensure): after a:bodyPr, each
	// branch its own fragment.
	h30 := target(t, w, "slide1#30")
	if err := h30.Ensure(nil, P("txBody")); err != nil {
		t.Fatal(err)
	}
	if err := h30.Ensure(Path{P("txBody")}, A("bodyPr")); err != nil {
		t.Fatal(err)
	}
	if err := h30.ReplaceParagraphs([][]byte{[]byte(pA), []byte(pB)}); err != nil {
		t.Fatal(err)
	}
	for i, p := range []string{pA, pB} {
		if got, want := bodyXML(h30, i), `<p:txBody><a:bodyPr/>`+p+`</p:txBody>`; got != want {
			t.Errorf("branch %d: got %s, want %s", i, got, want)
		}
	}
	wantVerify(t, w, "")
}

// TestReplaceParagraphsNamespaces: prefixes in scope at p:txBody, or
// declared on the paragraphs themselves, are accepted as written;
// others, and anything but a:p, are refused.
func TestReplaceParagraphsNamespaces(t *testing.T) {
	w := workspace(t, testdeck.Diagram(t))
	h := target(t, w, "slide1#3")
	own := `<d:p xmlns:d="` + nsA + `"><d:r><d:t>D</d:t></d:r></d:p>`
	if err := h.ReplaceParagraphs([][]byte{[]byte(own)}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(bodyXML(h, 0), own) {
		t.Fatalf("declared prefix not kept: %s", bodyXML(h, 0))
	}
	for _, c := range []struct{ frag, want string }{
		{`<q:p><q:r/></q:p>`, "prefix"},
		{`<a:r><a:t>x</a:t></a:r>`, "a:r"},
		{`<p:sp/>`, "p:sp"},
		{pA + `text`, "text outside"},
		{``, "no a:p"},
		{`<!-- c -->`, "no a:p"},
		{`<a:p>`, "parse"},
		{`<p xmlns="` + nsA + `"/>` + `<x/>`, "x"},
	} {
		before := bodyXML(h, 0)
		err := h.ReplaceParagraphs([][]byte{[]byte(c.frag)})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: err = %v, want %q", c.frag, err, c.want)
		}
		if bodyXML(h, 0) != before {
			t.Errorf("%q: refused but changed the body", c.frag)
		}
	}
	// Whitespace and comments between paragraphs are allowed.
	if err := h.ReplaceParagraphs([][]byte{[]byte(pA + "\n  <!-- c -->" + pB)}); err != nil {
		t.Fatal(err)
	}
	wantVerify(t, w, "")
}

// TestReplaceParagraphsAllOrNothing: one branch that cannot change
// leaves every branch as it was; nil leaves a branch alone.
func TestReplaceParagraphsAllOrNothing(t *testing.T) {
	w := workspace(t, testdeck.Diagram(t))
	h := target(t, w, "slide1#30")
	if err := h.Ensure(nil, P("txBody")); err != nil {
		t.Fatal(err)
	}
	if err := h.Ensure(Path{P("txBody")}, A("bodyPr")); err != nil {
		t.Fatal(err)
	}
	if err := h.ReplaceParagraphs([][]byte{[]byte(pA), []byte(pA)}); err != nil {
		t.Fatal(err)
	}
	snapshot := func() [2]string { return [2]string{bodyXML(h, 0), bodyXML(h, 1)} }
	s0 := snapshot()
	for _, frags := range [][][]byte{
		{[]byte(pB), []byte(`<a:r/>`)},
		{[]byte(pB), []byte(`<q:p/>`)},
		{[]byte(pB), {}},
		{[]byte(pB)}, // one fragment for two branches
	} {
		if err := h.ReplaceParagraphs(frags); err == nil {
			t.Errorf("%q accepted", frags)
		}
		if snapshot() != s0 {
			t.Fatalf("%q: a branch changed although another could not", frags)
		}
	}
	// nil leaves the Fallback alone.
	if err := h.ReplaceParagraphs([][]byte{[]byte(pB), nil}); err != nil {
		t.Fatal(err)
	}
	if s := snapshot(); !strings.Contains(s[0], pB) || s[1] != s0[1] {
		t.Fatalf("nil fragment: %v", s)
	}
	// A branch without a text body fails the whole call.
	fb := h.FindIn(1, Path{P("txBody")})
	if err := fb.Remove(); err != nil {
		t.Fatal(err)
	}
	s1 := bodyXML(h, 0)
	if err := h.ReplaceParagraphs([][]byte{[]byte(pA), []byte(pA)}); !errors.Is(err, ErrBranch) {
		t.Fatalf("err = %v", err)
	}
	if bodyXML(h, 0) != s1 {
		t.Fatal("Choice changed although the Fallback has no text body")
	}
	// A body without a:bodyPr is refused too.
	h3 := target(t, w, "slide1#3")
	b := h3.FindIn(0, Path{P("txBody")})
	if err := b.RemoveChild(derive.Child(b, derive.DML, "bodyPr")); err != nil {
		t.Fatal(err)
	}
	if err := h3.ReplaceParagraphs([][]byte{[]byte(pA)}); !errors.Is(err, ErrBranch) || !strings.Contains(err.Error(), "bodyPr") {
		t.Fatalf("no bodyPr: %v", err)
	}
}

// TestReplaceParagraphsUntargeted: the verifier holds the change to the
// target's own content, as for every Handle mutator; changing another
// object's paragraphs by hand is caught.
func TestReplaceParagraphsUntargeted(t *testing.T) {
	w := workspace(t, testdeck.Diagram(t))
	h := target(t, w, "slide1#10")
	if err := h.ReplaceParagraphs([][]byte{[]byte(pA)}); err != nil {
		t.Fatal(err)
	}
	wantVerify(t, w, "")
	other := derive.Child(raw(t, w, "slide1#3"), derive.PML, "txBody")
	var p *tree.Node
	for _, c := range other.Elements() {
		if derive.Is(c, derive.DML, "p") {
			p = c
		}
	}
	if err := other.RemoveChild(p); err != nil {
		t.Fatal(err)
	}
	wantVerify(t, w, "object s3")
}

func TestChildOrder(t *testing.T) {
	slots, ok := ChildOrder("a:CT_TextParagraphProperties")
	if !ok || len(slots) == 0 {
		t.Fatal("pPr order not tabulated")
	}
	bullets := ""
	for _, s := range slots {
		if len(s) > 0 && s[0] == "a:buNone" {
			bullets = strings.Join(s, ",")
		}
	}
	if bullets != "a:buNone,a:buAutoNum,a:buChar,a:buBlip" {
		t.Errorf("bullet choice = %q", bullets)
	}
	slots[0][0] = "changed"
	if again, _ := ChildOrder("a:CT_TextParagraphProperties"); again[0][0] == "changed" {
		t.Error("ChildOrder hands out the table itself")
	}
	if _, ok := ChildOrder("a:CT_Nothing"); ok {
		t.Error("unknown type tabulated")
	}
}
