package edit

import (
	"errors"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

// childNames lists the qualified names of e's element children.
func childNames(e *tree.Node) string {
	var s []string
	for _, c := range e.Elements() {
		s = append(s, c.Name())
	}
	return strings.Join(s, " ")
}

var spPr = Path{P("spPr")}

func TestEnsurePlacesBySchemaOrder(t *testing.T) {
	w := workspace(t, testdeck.Diagram(t))
	h := target(t, w, "slide1#11") // spPr: xfrm prstGeom
	before := childNames(h.Find(spPr))
	if err := h.Ensure(spPr, A("ln")); err != nil {
		t.Fatal(err)
	}
	if err := h.Ensure(spPr, A("solidFill")); err != nil { // fill goes between geometry and line
		t.Fatal(err)
	}
	if err := h.Ensure(spPr, A("extLst")); err != nil {
		t.Fatal(err)
	}
	if err := h.Ensure(spPr, A("effectLst")); err != nil { // between ln and extLst
		t.Fatal(err)
	}
	got := childNames(h.Find(spPr))
	if !strings.HasPrefix(before, "a:xfrm a:prstGeom") || got != "a:xfrm a:prstGeom a:solidFill a:ln a:effectLst a:extLst" {
		t.Fatalf("spPr children %q -> %q", before, got)
	}
	// Ensuring an existing child changes nothing.
	b := h.Elem(0).Bytes()
	if err := h.Ensure(spPr, A("ln")); err != nil {
		t.Fatal(err)
	}
	if string(b) != string(h.Elem(0).Bytes()) {
		t.Fatal("ensuring an existing child changed the object")
	}
	wantVerify(t, w, "")
}

func TestEnsureReplacesWithinChoice(t *testing.T) {
	w := workspace(t, testdeck.Diagram(t))
	h := target(t, w, "slide1#50") // custGeom
	if err := h.Ensure(spPr, A("prstGeom")); err != nil {
		t.Fatal(err)
	}
	got := childNames(h.Find(spPr))
	if strings.Contains(got, "custGeom") || !strings.Contains(got, "a:prstGeom") {
		t.Fatalf("geometry not replaced: %s", got)
	}
	if n := h.Find(Path{P("spPr"), A("prstGeom")}); len(n.Elements()) != 0 || len(n.Attrs()) != 0 {
		t.Fatalf("replacement is not empty: %s", n.Bytes())
	}
}

func TestEnsureRejects(t *testing.T) {
	w := workspace(t, testdeck.Diagram(t))
	h := target(t, w, "slide1#10")
	g := target(t, w, "slide1#20")
	cases := []struct {
		h      *Handle
		parent Path
		child  Name
		err    error
	}{
		{h, Path{P("txBody")}, A("p"), ErrRepeated},
		{h, spPr, A("chOff"), ErrNotAChild},
		{h, Path{P("spPr"), A("xfrm")}, A("chOff"), ErrNotAChild},
		{h, Path{P("nvSpPr"), P("nvPr")}, P("ph"), ErrUnknownParent},
		{h, Path{P("spPr"), A("nope")}, A("x"), ErrBranch},
		{g, Path{}, P("sp"), ErrRepeated},
	}
	for _, c := range cases {
		if err := c.h.Ensure(c.parent, c.child); !errors.Is(err, c.err) {
			t.Errorf("Ensure(%s, %s) err = %v, want %v", c.parent, c.child, err, c.err)
		}
	}
	// A group's transform has a child space; a shape's does not.
	if err := g.Ensure(Path{P("grpSpPr"), A("xfrm")}, A("chOff")); err != nil {
		t.Errorf("group chOff: %v", err)
	}
}

// TestEnsureBesideUnknown: a parent whose only elements are unknown to
// the schema table takes the new child first, ahead of them (extensions
// come last in every content model); the unknown element is kept.
func TestEnsureBesideUnknown(t *testing.T) {
	w := workspace(t, testdeck.Diagram(t))
	h := target(t, w, "slide1#10")
	sp := h.Find(spPr)
	for _, c := range sp.Elements() {
		if err := c.Remove(); err != nil {
			t.Fatal(err)
		}
	}
	unknown, err := tree.NewElement("urn:x", "x", "thing")
	if err != nil {
		t.Fatal(err)
	}
	if err := sp.AppendChild(unknown); err != nil {
		t.Fatal(err)
	}
	if err := h.Ensure(spPr, A("xfrm")); err != nil {
		t.Fatal(err)
	}
	if els := sp.Elements(); len(els) != 2 || els[0].Local() != "xfrm" || els[1] != unknown {
		t.Fatalf("spPr holds %v", els)
	}
}

func TestEnsureEveryBranch(t *testing.T) {
	w := workspace(t, testdeck.Diagram(t))
	h := target(t, w, "slide1#30")
	if err := h.Ensure(spPr, A("ln")); err != nil {
		t.Fatal(err)
	}
	for i := range h.Branches() {
		if h.FindIn(i, Path{P("spPr"), A("ln")}) == nil {
			t.Fatalf("branch %d has no a:ln", i)
		}
	}
	wantVerify(t, w, "")
}
