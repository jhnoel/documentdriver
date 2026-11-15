package tree

import (
	"slices"
	"testing"
)

func TestIterators(t *testing.T) {
	doc := mustParse(t, slide)
	doc.Node().Walk(func(n *Node) bool {
		if got := slices.Collect(n.ChildrenSeq()); !slices.Equal(got, n.Children()) {
			t.Fatalf("ChildrenSeq of %s differs from Children", n.Name())
		}
		if got := slices.Collect(n.ElementsSeq()); !slices.Equal(got, n.Elements()) {
			t.Fatalf("ElementsSeq of %s differs from Elements", n.Name())
		}
		if n.HasElements() != (len(n.Elements()) > 0) || n.FirstElement() != first(n.Elements()) {
			t.Fatalf("HasElements or FirstElement of %s wrong", n.Name())
		}
		var attrs []Attribute
		var names []string
		for k, v := range n.AttrsSeq() {
			attrs = append(attrs, Attribute{k, v})
		}
		for k := range n.AttrNamesSeq() {
			names = append(names, k)
		}
		want := n.Attrs()
		if !slices.Equal(attrs, want) && len(want)+len(attrs) > 0 || n.HasAttrs() != (len(want) > 0) || len(names) != len(want) {
			t.Fatalf("AttrsSeq of %s = %v, want %v", n.Name(), attrs, want)
		}
		return true
	})
}

func first(ns []*Node) *Node {
	if len(ns) == 0 {
		return nil
	}
	return ns[0]
}

func TestIteratorsDoNotAllocate(t *testing.T) {
	doc := mustParse(t, slide)
	root := doc.Root()
	sp := mustSelect1(t, doc, "//p:sp[1]")
	allocs := testing.AllocsPerRun(100, func() {
		k := 0
		for c := range root.ChildrenSeq() {
			k += int(c.Kind())
		}
		for e := range sp.ElementsSeq() {
			k += len(e.Name())
		}
		for name := range sp.AttrNamesSeq() {
			k += len(name)
		}
		if k < 0 || !sp.HasElements() || sp.FirstElement() == nil {
			t.Fatal("unreachable")
		}
	})
	if allocs != 0 {
		t.Fatalf("iterators allocate %.1f times per run", allocs)
	}
}

// TestIteratorEdits checks what a child iterator visits when its body
// edits the children.
func TestIteratorEdits(t *testing.T) {
	names := func(n *Node, edit func(c *Node)) []string {
		var out []string
		for c := range n.ElementsSeq() {
			out = append(out, c.Name())
			edit(c)
		}
		return out
	}
	parse := func() *Node { return mustParse(t, `<r><a/>t<b/><c/><d/></r>`).Root() }
	cases := []struct {
		name string
		edit func(r, c *Node)
		want []string
		left string
	}{
		{"none", func(_, _ *Node) {}, []string{"a", "b", "c", "d"}, "<r><a/>t<b/><c/><d/></r>"},
		{"remove each", func(_, c *Node) { must(t, c.Remove()) }, []string{"a", "b", "c", "d"}, "<r>t</r>"},
		{"remove b's next", func(r, c *Node) {
			if c.Name() == "b" {
				must(t, r.RemoveChild(r.Elements()[2]))
			}
		}, []string{"a", "b"}, "<r><a/>t<b/><d/></r>"},
		{"remove a and its next", func(r, c *Node) {
			if c.Name() == "a" {
				must(t, c.Remove())
				must(t, r.Children()[0].Remove())
			}
		}, []string{"a"}, "<r><b/><c/><d/></r>"},
		{"insert after", func(r, c *Node) {
			if c.Name() == "b" {
				e, err := NewElement("", "", "x")
				must(t, err)
				must(t, r.InsertAfter(e, c))
			}
		}, []string{"a", "b", "c", "d"}, "<r><a/>t<b/><x/><c/><d/></r>"},
		{"insert before", func(r, c *Node) {
			if c.Name() == "c" {
				e, err := NewElement("", "", "x")
				must(t, err)
				must(t, r.InsertBefore(e, c))
			}
		}, []string{"a", "b", "c", "d"}, "<r><a/>t<b/><x/><c/><d/></r>"},
		{"move to end", func(r, c *Node) {
			if c.Name() == "a" {
				must(t, c.Remove())
				must(t, r.AppendChild(c))
			}
		}, []string{"a", "b", "c", "d", "a"}, "<r>t<b/><c/><d/><a/></r>"},
	}
	for _, c := range cases {
		r := parse()
		got := names(r, func(n *Node) { c.edit(r, n) })
		if !slices.Equal(got, c.want) || string(r.Bytes()) != c.left {
			t.Errorf("%s: visited %v, left %s; want %v, %s", c.name, got, r.Bytes(), c.want, c.left)
		}
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
