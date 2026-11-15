package edit

import (
	"errors"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/plan"
)

func TestRangeJoinsParagraphs(t *testing.T) {
	w, body := open(t)
	ps := paras(body)
	h, err := w.Range(docPart, "ps", ps[0], ps[1])
	if err != nil {
		t.Fatal(err)
	}
	// Join: the first paragraph's run moves to the front of the second's
	// content, and the first paragraph goes.
	run := ps[0].Elements()[0]
	if err := h.PutAfter(run, ps[1].Elements()[0]); err != nil {
		t.Fatal(err)
	}
	if err := h.RemoveNode(ps[0]); err != nil {
		t.Fatal(err)
	}
	if got := text(body); got != "onetwo" {
		t.Fatalf("text %q", got)
	}
	if err := w.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestRegionRefusesOutside(t *testing.T) {
	w, body := open(t)
	ps := paras(body)
	h, err := w.Range(docPart, "p0", ps[0], ps[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := h.RemoveNode(ps[1]); !errors.Is(err, ErrOutside) {
		t.Fatalf("RemoveNode outside = %v", err)
	}
	if err := h.Append(ps[1], ps[0].Elements()[0]); !errors.Is(err, ErrOutside) {
		t.Fatalf("Append into outside = %v", err)
	}
	if _, err := w.Range(docPart, "x", ps[0], body); !errors.Is(err, ErrRegion) {
		t.Fatalf("Range over a non-sibling = %v", err)
	}
	// A change outside the region by any other means fails Verify.
	if err := ps[1].SetAttr("w:rsidR", "1"); err != nil {
		t.Fatal(err)
	}
	if err := w.Verify(); !errors.Is(err, plan.ErrUndeclared) {
		t.Fatalf("Verify = %v", err)
	}
}

func TestStoryAndPrivate(t *testing.T) {
	w, body := open(t)
	h := w.Story(docPart, "body", body)
	ps := paras(body)
	if err := h.Prepend(body, ps[1]); err != nil {
		t.Fatal(err)
	}
	if _, err := h.EnsureIn(ps[0], W("pPr")); err != nil {
		t.Fatal(err)
	}
	if err := w.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := h.PutBefore(body, ps[0]); !errors.Is(err, ErrOutside) {
		t.Fatalf("moving the story root = %v", err)
	}
	d, err := tree.Parse([]byte(document))
	if err != nil {
		t.Fatal(err)
	}
	pb := d.Root().Elements()[0]
	ph := Private(wml, pb)
	if err := ph.RemoveNode(paras(pb)[0]); err != nil {
		t.Fatal(err)
	}
	if len(paras(pb)) != 1 {
		t.Fatal("private edit did not apply")
	}
}

func TestPlacementWithUnknownSiblings(t *testing.T) {
	w, body := open(t)
	p := paras(body)[0]
	h := w.Story(docPart, "body", body)
	// A paragraph whose only elements are unknown extensions: the new
	// pPr goes first, ahead of them; nothing is refused or moved.
	ext, err := w.Parse(p, []byte(`<x:ext xmlns:x="urn:x"/>`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.RemoveNode(p.Elements()[0]); err != nil {
		t.Fatal(err)
	}
	pos := Into(p)
	if err := w.Insert(docPart, pos, ext[0], nil); err != nil {
		t.Fatal(err)
	}
	if _, err := h.EnsureIn(p, W("pPr")); err != nil {
		t.Fatal(err)
	}
	if got := string(p.Bytes()); got != `<w:p><w:pPr/><x:ext xmlns:x="urn:x"/></w:p>` {
		t.Fatalf("got %s", got)
	}
	// SlotEnd and AppendEach place runs after the known pPr and the
	// unknown element attached to it.
	if err := w.Target(docPart, "p", []*tree.Node{p}).AppendEach(nil, W("r"), nil); err != nil {
		t.Fatal(err)
	}
	if got := string(p.Bytes()); got != `<w:p><w:pPr/><x:ext xmlns:x="urn:x"/><w:r/></w:p>` {
		t.Fatalf("got %s", got)
	}
	if _, err := h.EnsureIn(p, W("tbl")); !errors.Is(err, ErrNotAChild) {
		t.Fatalf("a child the schema does not allow = %v", err)
	}
}
