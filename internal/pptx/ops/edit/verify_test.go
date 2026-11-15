package edit

import (
	"errors"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

var (
	pathOff = Path{P("spPr"), A("xfrm"), A("off")}
	pathXf  = Path{P("spPr"), A("xfrm")}
)

func target(t *testing.T, w *Workspace, addr string) *Handle {
	t.Helper()
	r, err := w.Resolve(addr)
	if err != nil {
		t.Fatal(err)
	}
	h, err := w.Target(r)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// raw returns the projected element of addr for deliberate, undeclared
// mutation.
func raw(t *testing.T, w *Workspace, addr string) *tree.Node {
	t.Helper()
	r, err := w.Resolve(addr)
	if err != nil {
		t.Fatal(err)
	}
	e, err := w.find(r.part, r.id, r.ord)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func wantVerify(t *testing.T, w *Workspace, want string) {
	t.Helper()
	err := w.Verify()
	if want == "" {
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		return
	}
	if !errors.Is(err, ErrUndeclared) || !strings.Contains(err.Error(), want) {
		t.Fatalf("Verify = %v, want %q", err, want)
	}
}

func TestVerifyAcceptsDeclaredEdits(t *testing.T) {
	w := workspace(t, testdeck.Diagram(t))
	h := target(t, w, "slide1#10")
	if err := h.SetAttr(pathOff, "x", "1"); err != nil {
		t.Fatal(err)
	}
	g := target(t, w, "slide1#20") // a group's own transform
	if err := g.SetAttr(Path{P("grpSpPr"), A("xfrm"), A("off")}, "x", "1"); err != nil {
		t.Fatal(err)
	}
	wantVerify(t, w, "")
}

func TestVerifyRejectsUntargetedObject(t *testing.T) {
	w := workspace(t, testdeck.Diagram(t))
	h := target(t, w, "slide1#10")
	if err := h.SetAttr(pathOff, "x", "1"); err != nil {
		t.Fatal(err)
	}
	// A sneaky edit of a neighbour, nested in a group.
	n, _ := resolve(raw(t, w, "slide1#21"), pathOff)
	if err := n.SetAttr("x", "7"); err != nil {
		t.Fatal(err)
	}
	wantVerify(t, w, "object s21")
}

func TestVerifyRejectsUntargetedAfterSetBack(t *testing.T) {
	// A value set and set back is not a change: bytes are compared.
	w := workspace(t, testdeck.Diagram(t))
	target(t, w, "slide1#10")
	n, _ := resolve(raw(t, w, "slide1#11"), pathOff)
	for _, v := range []string{"7", "3810000"} {
		if err := n.SetAttr("x", v); err != nil {
			t.Fatal(err)
		}
	}
	wantVerify(t, w, "")
}

func TestVerifyGroupTargetDoesNotCoverChildren(t *testing.T) {
	w := workspace(t, testdeck.Diagram(t))
	target(t, w, "slide1#20")
	child := raw(t, w, "slide1#21")
	if err := child.Remove(); err != nil {
		t.Fatal(err)
	}
	wantVerify(t, w, "object s21 at byte")
}

func TestVerifyRemovalNeedsContainer(t *testing.T) {
	w := workspace(t, testdeck.Diagram(t))
	target(t, w, "slide1#40~2")
	if err := raw(t, w, "slide1#40~2").Remove(); err != nil {
		t.Fatal(err)
	}
	wantVerify(t, w, "p:spTree")
	if err := w.SlideContainer(slide1); err != nil {
		t.Fatal(err)
	}
	wantVerify(t, w, "")
}

func TestVerifyContainerOwnContent(t *testing.T) {
	w := workspace(t, testdeck.Diagram(t))
	if err := w.SlideContainer(slide1); err != nil {
		t.Fatal(err)
	}
	_, st, _ := w.spTree(slide1)
	off := st.Find(A("off").NS().URIs[0], "off")
	if err := off.SetAttr("x", "5"); err != nil {
		t.Fatal(err)
	}
	wantVerify(t, w, "declared container but its own content changed")
}

func TestVerifyRejectsUndeclaredPart(t *testing.T) {
	w := workspace(t, testdeck.Diagram(t))
	doc, err := w.tree("/ppt/presentation.xml")
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Root().SetAttr("saveSubsetFonts", "1"); err != nil {
		t.Fatal(err)
	}
	wantVerify(t, w, "part /ppt/presentation.xml changed")
	w.DeclarePart("/ppt/presentation.xml")
	wantVerify(t, w, "")
}

func TestVerifyRejectsSlideRootChange(t *testing.T) {
	w := workspace(t, testdeck.Diagram(t))
	target(t, w, "slide1#10")
	doc, _ := w.tree(slide1)
	if err := doc.Root().SetAttr("show", "0"); err != nil {
		t.Fatal(err)
	}
	wantVerify(t, w, "p:sld")
}

func TestHandleAppliesToEveryBranch(t *testing.T) {
	w := workspace(t, testdeck.Diagram(t))
	h := target(t, w, "slide1#30")
	if h.Branches() != 2 || h.Elem(0).Parent().Local() != "Choice" || h.Elem(1).Parent().Local() != "Fallback" {
		t.Fatalf("branches: %d", h.Branches())
	}
	if err := h.SetAttr(pathOff, "x", "1"); err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		if v, _ := h.FindIn(i, pathOff).Attr("x"); v != "1" {
			t.Fatalf("branch %d x = %s", i, v)
		}
	}
	wantVerify(t, w, "")
	// The projected branch of an unsupported Choice is the Fallback.
	h31 := target(t, w, "slide1#31")
	if h31.Elem(0).Parent().Local() != "Fallback" || h31.Branches() != 2 {
		t.Fatal("s31 projected branch is not the Fallback")
	}
}

func TestHandleFailsWhenABranchLacksThePath(t *testing.T) {
	w := workspace(t, testdeck.Diagram(t))
	h := target(t, w, "slide1#30")
	// Remove the Fallback's transform behind the handle's back.
	if err := h.FindIn(1, pathXf).Remove(); err != nil {
		t.Fatal(err)
	}
	if err := h.SetAttr(pathOff, "x", "1"); !errors.Is(err, ErrBranch) {
		t.Fatalf("SetAttr err = %v", err)
	}
	if v, _ := h.Find(pathOff).Attr("x"); v == "1" {
		t.Fatal("projected branch changed although another branch could not be")
	}
}

func TestPathDoesNotEnterOtherObjects(t *testing.T) {
	w := workspace(t, testdeck.Diagram(t))
	g := target(t, w, "slide1#20")
	if err := g.SetAttr(Path{P("sp"), P("spPr")}, "bwMode", "auto"); err == nil {
		t.Fatal("path stepped into a child object")
	}
}

func TestParentMap(t *testing.T) {
	w := workspace(t, testdeck.Diagram(t))
	h := target(t, w, "slide1#21")
	pm, err := h.ParentMap(0)
	if err != nil {
		t.Fatal(err)
	}
	if !pm.FlipH || pm.FlipV {
		t.Fatalf("parent flips = %v %v", pm.FlipH, pm.FlipV)
	}
}

// resolve returns the element at path p under e, for deliberate,
// undeclared mutation.
func resolve(e *tree.Node, p Path) (*tree.Node, bool) {
	for _, n := range p {
		var next *tree.Node
		for _, c := range e.Elements() {
			if n.Matches(c) {
				next = c
				break
			}
		}
		if next == nil {
			return nil, false
		}
		e = next
	}
	return e, true
}
