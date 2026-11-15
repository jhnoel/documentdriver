package edit

import (
	"errors"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

// order lists the ids of a container's objects (and "ac" for
// mc:AlternateContent) in document order.
func order(c *tree.Node) string {
	var s []string
	for _, e := range c.Elements() {
		switch {
		case isAC(e):
			s = append(s, "ac")
		case isObject(e):
			s = append(s, objectID(e))
		default:
			s = append(s, e.Local())
		}
	}
	return strings.Join(s, " ")
}

func spTreeOf(t *testing.T, w *Workspace) *tree.Node {
	t.Helper()
	_, st, err := w.spTree(slide1)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

var frag = []byte(`<p:sp><p:nvSpPr><p:cNvPr id="0" name="n"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr/></p:sp>`)
var fragNS = map[string]string{"p": derive.PML[0], "a": derive.DML[0]}

func TestInsertObjectAnchors(t *testing.T) {
	w := workspace(t, testdeck.Diagram(t))
	st := spTreeOf(t, w)
	r10, _ := w.Resolve("slide1#10")
	r21, _ := w.Resolve("slide1#21")
	for _, c := range []struct {
		at   Anchor
		want string // the neighbours of the new object
	}{
		{Front(), "71 NEW"},
		{Back(), "grpSpPr NEW 2"},
		{Before(r10), "3 NEW 10"},
		{After(r10), "10 NEW 11"},
		{After(r21), "21 NEW 22"}, // into the group
	} {
		ref, err := w.InsertObject(slide1, frag, fragNS, c.at)
		if err != nil {
			t.Fatal(err)
		}
		e, err := w.find(ref.part, ref.id, ref.ord)
		if err != nil {
			t.Fatal(err)
		}
		got := strings.ReplaceAll(order(e.Parent()), " "+ref.id+" ", " NEW ")
		got = strings.TrimSuffix(got, " "+ref.id) + map[bool]string{true: " NEW", false: ""}[strings.HasSuffix(got, " "+ref.id)]
		if !strings.Contains(got, c.want) {
			t.Errorf("%+v: order %s, want %q", c.at, got, c.want)
		}
	}
	if !strings.Contains(order(st), "grpSpPr") {
		t.Fatal("shape tree lost its properties")
	}
	wantVerify(t, w, "")
}

func TestInsertObjectNewIDs(t *testing.T) {
	w := workspace(t, testdeck.Diagram(t))
	a, _ := w.InsertObject(slide1, frag, fragNS, Front())
	b, _ := w.InsertObject(slide1, frag, fragNS, Front())
	if a.ID() != "1000" || b.ID() != "1001" {
		t.Fatalf("ids %s %s; want 1000 1001 (the fixture's dangling glue names 999)", a.ID(), b.ID())
	}
	if _, err := w.InsertObject(slide1, []byte(`<a:off x="0" y="0"/>`), fragNS, Front()); err == nil {
		t.Fatal("inserted a non-object")
	}
}

func TestMoveObject(t *testing.T) {
	w := workspace(t, testdeck.Diagram(t))
	st := spTreeOf(t, w)
	before := order(st)
	r10, _ := w.Resolve("slide1#10")
	r11, _ := w.Resolve("slide1#11")
	// Already there: nothing changes.
	if err := w.MoveObject(r10, Before(r11)); err != nil {
		t.Fatal(err)
	}
	if st.Modified() {
		t.Fatal("a move to the same place modified the tree")
	}
	if err := w.MoveObject(r10, Front()); err != nil {
		t.Fatal(err)
	}
	if got := order(st); !strings.HasSuffix(got, "71 10") {
		t.Fatalf("front: %s", got)
	}
	if err := w.MoveObject(r10, After(r11)); err != nil {
		t.Fatal(err)
	}
	if got := order(st); got != strings.Replace(before, "10 11", "11 10", 1) {
		t.Fatalf("after: %s", got)
	}
	r21, _ := w.Resolve("slide1#21")
	if err := w.MoveObject(r21, After(r10)); err == nil {
		t.Fatal("moved an object out of its group")
	}
	if err := w.MoveObject(r21, Front()); err != nil { // front of its group
		t.Fatal(err)
	}
	g, _ := w.Resolve("slide1#20")
	ge, _ := w.find(g.part, g.id, g.ord)
	if got := order(ge); !strings.HasSuffix(got, "24 21") {
		t.Fatalf("group front: %s", got)
	}
	wantVerify(t, w, "")
}

func TestRemoveAndDuplicateInAlternateContent(t *testing.T) {
	w := workspace(t, testdeck.Diagram(t))
	st := spTreeOf(t, w)
	r30, _ := w.Resolve("slide1#30")
	cp, err := w.DuplicateObject(r30, func() string { return "{00000000-0000-4000-8000-000000000000}" })
	if err != nil {
		t.Fatal(err)
	}
	if got := order(st); !strings.Contains(got, "26 25 ac ac ac") && !strings.Contains(got, "ac ac ac") {
		t.Fatalf("duplicate of an mc object: %s", got)
	}
	// Both branches of the copy carry the new id.
	c, err := w.Target(cp)
	if err != nil {
		t.Fatal(err)
	}
	if c.Branches() != 2 {
		t.Fatalf("copy has %d branches", c.Branches())
	}
	if err := w.RemoveObject(r30); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Resolve("slide1#30"); !errors.Is(err, ErrNoObject) {
		t.Fatalf("s30 still resolves: %v", err)
	}
	wantVerify(t, w, "")
}

func TestDuplicateRemapsInnerGlue(t *testing.T) {
	w := workspace(t, testdeck.Diagram(t))
	r20, _ := w.Resolve("slide1#20")
	cp, err := w.DuplicateObject(r20, func() string { return "{G}" })
	if err != nil {
		t.Fatal(err)
	}
	e, _ := w.find(cp.part, cp.id, cp.ord)
	s := string(e.Bytes())
	// Group 20 holds 21, 22, 23 (glued 21:3 -> 22:2) and 24: ids 1000..1004.
	for _, want := range []string{`id="1000" name="Flipped group"`, `id="1001" name="Left"`, `<a:stCxn id="1001" idx="3"/><a:endCxn id="1002" idx="2"/>`} {
		if !strings.Contains(s, want) {
			t.Errorf("copy lacks %s:\n%s", want, s)
		}
	}
	wantVerify(t, w, "")
}

func TestAppendEach(t *testing.T) {
	w := workspace(t, testdeck.Diagram(t))
	h := target(t, w, "slide1#13") // bentConnector3 with adj1
	av := Path{P("spPr"), A("prstGeom"), A("avLst")}
	if err := h.AppendEach(av, A("gd"), [][2]string{{"name", "adj2"}, {"fmla", "val 1"}}); err != nil {
		t.Fatal(err)
	}
	if got := string(h.Find(av).Bytes()); got != `<a:avLst><a:gd name="adj1" fmla="val 25000"/><a:gd name="adj2" fmla="val 1"/></a:avLst>` {
		t.Fatalf("avLst: %s", got)
	}
	if err := h.AppendEach(Path{P("spPr")}, A("ln"), nil); err == nil {
		t.Fatal("AppendEach placed a single child")
	}
	// Colour modifiers: a repeated choice inside schemeClr.
	h11 := target(t, w, "slide1#11")
	clr := Path{P("spPr"), A("solidFill"), A("schemeClr")}
	if err := h11.AppendEach(clr, A("alpha"), [][2]string{{"val", "50000"}}); err != nil {
		t.Fatal(err)
	}
	if got := string(h11.Find(clr).Bytes()); !strings.HasSuffix(got, `<a:lumOff val="25000"/><a:alpha val="50000"/></a:schemeClr>`) {
		t.Fatalf("schemeClr: %s", got)
	}
	wantVerify(t, w, "")
}
