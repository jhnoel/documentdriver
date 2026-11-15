package tree

import (
	"testing"
)

// TestAttrSpan checks that AttrSpan is the raw value's span of every
// attribute of every node of a parsed and a scanned document, and false
// for built nodes and once the attributes change.
func TestAttrSpan(t *testing.T) {
	const src = `<a xmlns:p="u" p:x="1" y="a&amp;b" z=''>` + "\r\n" +
		`<b p:x="2"/><c/></a>`
	doc, err := ParseString(src)
	if err != nil {
		t.Fatal(err)
	}
	doc.Root().Walk(func(n *Node) bool {
		if n.Kind() != ElementNode {
			return true
		}
		for _, a := range n.Attrs() {
			start, end, ok := n.AttrSpan(a.Name)
			if !ok {
				t.Fatalf("%s: AttrSpan(%s) not ok", n.Name(), a.Name)
			}
			raw, _ := n.AttrRaw(a.Name)
			if got := src[start:end]; got != string(raw) {
				t.Fatalf("%s: src[%d:%d] = %q, AttrRaw = %q", n.Name(), start, end, got, raw)
			}
			if q := src[start-1]; q != '"' && q != '\'' {
				t.Fatalf("%s: src[%d] = %q, not a quote", n.Name(), start-1, q)
			}
		}
		if _, _, ok := n.AttrSpan("nope"); ok {
			t.Fatalf("%s: AttrSpan(nope) ok", n.Name())
		}
		return true
	})

	// In a Visitor, in Start and in End.
	v := &spanVisitor{t: t, src: src}
	if err := ScanString(src, v); err != nil {
		t.Fatal(err)
	}
	if v.n == 0 {
		t.Fatal("visitor saw no attributes")
	}

	// False after SetAttr and for built nodes.
	b := mustSelect1(t, doc, "//b")
	if err := b.SetAttr("p:x", "3"); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := b.AttrSpan("p:x"); ok {
		t.Fatal("AttrSpan ok after SetAttr")
	}
	el, err := NewElement("", "", "e")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := el.AttrSpan("x"); ok {
		t.Fatal("AttrSpan ok for a built node")
	}
	if _, _, ok := NewText("t").AttrSpan("x"); ok {
		t.Fatal("AttrSpan ok for a text node")
	}
}

type spanVisitor struct {
	t   *testing.T
	src string
	n   int
}

func (v *spanVisitor) Start(n *Node) error { v.check(n); return nil }
func (v *spanVisitor) End(n *Node) error   { v.check(n); return nil }
func (v *spanVisitor) Leaf(*Node) error    { return nil }

func (v *spanVisitor) check(n *Node) {
	v.t.Helper()
	for _, a := range n.Attrs() {
		start, end, ok := n.AttrSpan(a.Name)
		if !ok {
			v.t.Fatalf("%s: AttrSpan(%s) not ok in visitor", n.Name(), a.Name)
		}
		raw, _ := n.AttrRaw(a.Name)
		if got := v.src[start:end]; got != string(raw) {
			v.t.Fatalf("%s: span [%d:%d] = %q, AttrRaw = %q", n.Name(), start, end, got, raw)
		}
		v.n++
	}
}
