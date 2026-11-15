package tree

import "testing"

const (
	strictMain = "http://purl.oclc.org/ooxml/spreadsheetml/main"
	relURI     = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
)

// nsVisitor drives an NSStack the way a Scan visitor does: Push in
// Start, Pop in End, and records what the stack resolves.
type nsVisitor struct {
	st   NSStack
	t    *testing.T
	seen map[string]string
}

func (v *nsVisitor) Start(n *Node) error {
	v.st.Push(n)
	v.seen[n.Name()] = v.st.Space(n)
	return nil
}
func (v *nsVisitor) End(_ *Node) error { v.st.Pop(); return nil }
func (v *nsVisitor) Leaf(*Node) error  { return nil }

// TestNSStack checks prefix shadowing, the default namespace
// (including xmlns=""), the Strict namespaces and attribute lookup by
// namespace.
func TestNSStack(t *testing.T) {
	const src = `<a xmlns:p="u1" xmlns="d1" xmlns:r="` + relURI + `" xmlns:s="` + strictMain + `">` +
		`<b xmlns:p="u2"><c p:x="a" y="b" r:id="42" s:z="1"/></b>` +
		`<d xmlns=""><e/></d></a>`
	v := &nsVisitor{t: t, seen: map[string]string{}}
	if err := ScanString(src, v); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"a": "d1", "b": "d1", "c": "d1", "d": "", "e": ""}
	for name, uri := range want {
		if got := v.seen[name]; got != uri {
			t.Fatalf("Space(%s) = %q, want %q", name, got, uri)
		}
	}

	// Lookup in scope, shadowing, the undeclared default and xml.
	var st NSStack
	if _, ok := st.Lookup("p"); ok {
		t.Fatal("Lookup(p) ok on an empty stack")
	}
	st.Pop() // a no-op, never a panic
	if err := ScanString(src, &nsScan{t: t, st: &st}); err != nil {
		t.Fatal(err)
	}

	// AttrNS by namespace URI.
	av := &attrVisitor{t: t}
	if err := ScanString(src, av); err != nil {
		t.Fatal(err)
	}
	if !av.checked {
		t.Fatal("AttrNS never checked")
	}
}

// nsScan checks Lookup results while scanning.
type nsScan struct {
	t  *testing.T
	st *NSStack
}

func (v *nsScan) Start(n *Node) error {
	v.st.Push(n)
	switch n.Name() {
	case "a":
		if got, ok := v.st.Lookup("p"); !ok || got != "u1" {
			v.t.Fatalf("at a: Lookup(p) = %q, %v", got, ok)
		}
		if got, ok := v.st.Lookup(""); !ok || got != "d1" {
			v.t.Fatalf("at a: Lookup(\"\") = %q, %v", got, ok)
		}
	case "b":
		if got, ok := v.st.Lookup("p"); !ok || got != "u2" {
			v.t.Fatalf("at b: Lookup(p) = %q, %v", got, ok)
		}
	case "e":
		if _, ok := v.st.Lookup(""); ok {
			v.t.Fatal("at e: Lookup(\"\") ok, want undeclared")
		}
	}
	if got, ok := v.st.Lookup("xml"); !ok || got != XMLNamespace {
		v.t.Fatalf("Lookup(xml) = %q, %v", got, ok)
	}
	return nil
}
func (v *nsScan) End(_ *Node) error { v.st.Pop(); return nil }
func (v *nsScan) Leaf(*Node) error  { return nil }

// attrVisitor checks AttrNS at the element with namespaced attributes.
type attrVisitor struct {
	t       *testing.T
	st      NSStack
	checked bool
}

func (v *attrVisitor) Start(n *Node) error {
	v.st.Push(n)
	if n.Name() != "c" {
		return nil
	}
	v.checked = true
	if got, ok := v.st.AttrNS(n, relURI, "id"); !ok || got != "42" {
		v.t.Fatalf("AttrNS(rel, id) = %q, %v", got, ok)
	}
	if got, ok := v.st.AttrNS(n, strictMain, "z"); !ok || got != "1" {
		v.t.Fatalf("AttrNS(strict, z) = %q, %v", got, ok)
	}
	if got, ok := v.st.AttrNS(n, "", "y"); !ok || got != "b" {
		v.t.Fatalf("AttrNS(\"\", y) = %q, %v", got, ok)
	}
	if _, ok := v.st.AttrNS(n, relURI, "x"); ok {
		v.t.Fatal("AttrNS(rel, x) ok")
	}
	if _, ok := v.st.AttrNS(n, "", "id"); ok {
		v.t.Fatal("AttrNS(\"\", id) ok")
	}
	return nil
}
func (v *attrVisitor) End(_ *Node) error { v.st.Pop(); return nil }
func (v *attrVisitor) Leaf(*Node) error  { return nil }

// TestNSStackNoAlloc checks that steady-state Lookup and Push/Pop do
// not allocate.
func TestNSStackNoAlloc(t *testing.T) {
	const src = `<a xmlns:p="u"><b/></a>`
	var st NSStack
	v := &nsVisitor{seen: map[string]string{}}
	if err := ScanString(src, v); err != nil {
		t.Fatal(err)
	}
	if n := testing.AllocsPerRun(10, func() { _, _ = st.Lookup("p") }); n != 0 {
		t.Fatalf("Lookup allocates %v per run", n)
	}
	doc, err := ParseString(src)
	if err != nil {
		t.Fatal(err)
	}
	a := doc.Root()
	if n := testing.AllocsPerRun(10, func() { st.Push(a); st.Pop() }); n != 0 {
		t.Fatalf("Push/Pop allocate %v per run", n)
	}
}
