package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/repo"
)

// oldNextSibling is the pre-W1 NextSibling, kept as the baseline: it
// builds the parent's Children slice and scans it.
func oldNextSibling(n *tree.Node) *tree.Node {
	p := n.Parent()
	if p == nil {
		return nil
	}
	kids := p.Children()
	for i, c := range kids {
		if c == n {
			if i+1 < len(kids) {
				return kids[i+1]
			}
			return nil
		}
	}
	return nil
}

// oldPrevSibling is the pre-W1 PrevSibling, kept as the baseline: it
// builds the parent's Children slice and scans it.
func oldPrevSibling(n *tree.Node) *tree.Node {
	p := n.Parent()
	if p == nil {
		return nil
	}
	kids := p.Children()
	for i, c := range kids {
		if c == n {
			if i > 0 {
				return kids[i-1]
			}
			return nil
		}
	}
	return nil
}

// oldNextElement is the pre-W1 next-element walk, kept as the baseline:
// it steps through oldNextSibling skipping non-elements.
func oldNextElement(n *tree.Node) *tree.Node {
	for s := oldNextSibling(n); s != nil; s = oldNextSibling(s) {
		if IsElement(s) {
			return s
		}
	}
	return nil
}

// oldPrevElement is the pre-W1 previous-element walk, kept as the
// baseline: it steps through oldPrevSibling skipping non-elements.
func oldPrevElement(n *tree.Node) *tree.Node {
	for s := oldPrevSibling(n); s != nil; s = oldPrevSibling(s) {
		if IsElement(s) {
			return s
		}
	}
	return nil
}

// checkSiblings checks one node: the tree's sibling methods and the
// model element walks agree with the pre-W1 baselines.
func checkSiblings(t *testing.T, n *tree.Node) {
	t.Helper()
	if got, want := n.NextSibling(), oldNextSibling(n); got != want {
		t.Errorf("%s: NextSibling = %p, baseline %p", n.Name(), got, want)
	}
	if got, want := n.PrevSibling(), oldPrevSibling(n); got != want {
		t.Errorf("%s: PrevSibling = %p, baseline %p", n.Name(), got, want)
	}
	if got, want := NextElement(n), oldNextElement(n); got != want {
		t.Errorf("%s: NextElement = %p, baseline %p", n.Name(), got, want)
	}
	if got, want := PrevElement(n), oldPrevElement(n); got != want {
		t.Errorf("%s: PrevElement = %p, baseline %p", n.Name(), got, want)
	}
}

// checkTree checks every node of the tree rooted at doc against the
// baselines.
func checkTree(t *testing.T, doc *tree.Node) {
	t.Helper()
	doc.Walk(func(n *tree.Node) bool {
		checkSiblings(t, n)
		return true
	})
}

// TestSiblingsMatchBaseline checks, for every node of every docx
// fixture's document, header and footer trees, that the tree methods
// and NextElement/PrevElement equal the pre-W1 baselines.
func TestSiblingsMatchBaseline(t *testing.T) {
	root, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(root, "testdata", "docx", "*.docx"))
	if err != nil || len(files) == 0 {
		t.Fatalf("fixture glob: %v (%d files)", err, len(files))
	}
	for _, f := range files {
		b, err := os.ReadFile(f) //nolint:gosec // G304: the fixture the glob named
		if err != nil {
			t.Fatal(err)
		}
		d, err := OpenDocument(b)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		checkTree(t, d.XML.Doc.Node())
		for _, p := range d.Pkg.Parts() {
			if !strings.HasPrefix(p.Name, "word/header") && !strings.HasPrefix(p.Name, "word/footer") {
				continue
			}
			x, err := d.Pkg.XML(p.Name)
			if err != nil {
				t.Fatalf("%s %s: %v", f, p.Name, err)
			}
			checkTree(t, x.Doc.Node())
		}
	}
}

// TestSiblingsSynthetic checks the baselines on a tree whose
// paragraph mixes text, comment, PI and CDATA siblings, plus a lone
// child, a detached node and a node removed mid-walk.
func TestSiblingsSynthetic(t *testing.T) {
	x, err := ParseXML([]byte(`<?xml version="1.0"?><!--prolog-->` +
		`<w:document xmlns:w="` + NSW + `"><w:body>` +
		`<w:p>text<!--c--><?pi d?><![CDATA[raw]]><w:r/>tail<w:r/></w:p>` +
		`<w:p><w:only/></w:p>` +
		`</w:body></w:document><!--trail-->`))
	if err != nil {
		t.Fatal(err)
	}
	checkTree(t, x.Doc.Node())

	// A lone child has no siblings; a detached node has no parent.
	detached, err := tree.ParseFragment([]byte(`<w:r/>`), map[string]string{"w": NSW})
	if err != nil || len(detached) != 1 {
		t.Fatalf("fragment: %v (%d nodes)", err, len(detached))
	}
	checkSiblings(t, detached[0])

	// Nodes ParseFragment unlinked from the holder they were parsed
	// under (unlink clears parent, prev and next, as a removal mid-walk
	// does) answer nil from both sides, and the walk over the former
	// sibling chain still agrees. The unlink itself is the tree
	// parser's, the only removal a docx/model test may perform: a
	// direct RemoveChild here would call a tree mutator outside the
	// edit core (TestArchitecture).
	frag, err := tree.ParseFragment([]byte(`<w:r/>text<!--c--><w:r/>`), map[string]string{"w": NSW})
	if err != nil || len(frag) != 4 {
		t.Fatalf("fragment: %v (%d nodes)", err, len(frag))
	}
	for _, c := range frag {
		checkSiblings(t, c)
	}
}

// sink keeps the benchmark loops observable.
var sink int

// BenchmarkSiblingWalk measures walking one w:p of 4096 children (w:r
// and whitespace text alternating) from first to last: the pre-W1
// baseline builds a Children slice per step (old), the tree's sibling
// links do not (new).
func BenchmarkSiblingWalk(b *testing.B) {
	var sb strings.Builder
	sb.WriteString(`<w:document xmlns:w="` + NSW + `"><w:body><w:p>`)
	for range 2048 {
		sb.WriteString(`<w:r/> `)
	}
	sb.WriteString(`</w:p></w:body></w:document>`)
	x, err := ParseXML([]byte(sb.String()))
	if err != nil {
		b.Fatal(err)
	}
	first := x.Body().FirstElement().FirstChild()
	if first == nil {
		b.Fatal("no first child")
	}
	b.Run("old", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			steps := 0
			for n := first; n != nil; n = oldNextSibling(n) {
				steps++
			}
			sink = steps
		}
	})
	b.Run("new", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			steps := 0
			for n := first; n != nil; n = n.NextSibling() {
				steps++
			}
			sink = steps
		}
	})
}
