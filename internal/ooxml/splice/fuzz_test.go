package splice

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// boundaries returns, for each element at or below the document element
// that has content (not self-closing), the positions between the nodes
// of its content: where it starts, where each child ends. It reads a
// parsed tree, independently of Check's scan.
func boundaries(doc *tree.Document) [][]int {
	var out [][]int
	doc.Root().Walk(func(n *tree.Node) bool {
		if n.Kind() != tree.ElementNode || n.EndTag() == nil {
			return true
		}
		s, _, _ := n.SpanIn(doc)
		b := []int{s + len(n.StartTag())}
		for c := range n.ChildrenSeq() {
			_, e, _ := c.SpanIn(doc)
			b = append(b, e)
		}
		out = append(out, b)
		return true
	})
	return out
}

var fuzzNames = []string{"a", "row", "c", "x14ac:e", "q:e"}

// FuzzPatch applies replacements chosen by sel to a well-formed orig:
// some over spans of whole sibling nodes with writer-built content, some
// over arbitrary offsets or with raw content. Check must never pass a
// patch whose result is not well formed (tree.Check), and must pass one
// made only of node spans and writer content with names in scope whose
// result is well formed (a deletion can join "]]" and ">").
func FuzzPatch(f *testing.F) {
	f.Add([]byte(sheet), []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, "<x/>")
	f.Add([]byte(sheet), []byte{3, 0, 0, 0, 0, 0, 0, 0, 200, 1, 9, 7}, "a&amp;b")
	f.Add([]byte(`<a xmlns:q="u"><b>t</b><!--c--><?p x?><![CDATA[d]]></a>`), []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}, "]]>")
	f.Add([]byte(`<a><b/></a>`), []byte{0, 0, 0, 0, 0, 0, 255, 255, 255}, "<b>")
	f.Add([]byte(`<a>]]<b/>></a>`), []byte{2, 1, 2, 0}, "")
	f.Fuzz(func(t *testing.T, orig, sel []byte, raw string) {
		if tree.Check(orig) != nil {
			return
		}
		src := string(orig)
		doc, err := tree.ParseString(src)
		if err != nil {
			t.Fatalf("Check passed but Parse failed: %v", err)
		}
		_, rootDeclaresX14ac := doc.Root().Attr("xmlns:x14ac")
		bounds := boundaries(doc)
		p, q := New(src), NewWellFormed(src)
		clean := true // only node spans and in-scope writer content so far
		for len(sel) >= 4 {
			mode, a, b, k := sel[0], int(sel[1]), int(sel[2]), int(sel[3])
			sel = sel[4:]
			var start, end int
			if mode&1 == 0 && len(bounds) > 0 {
				bs := bounds[k%len(bounds)]
				i, j := a%len(bs), b%len(bs)
				if i > j {
					i, j = j, i
				}
				start, end = bs[i], bs[j]
			} else {
				start, end = (a*len(src))/256, (b*len(src))/256
				if start > end {
					start, end = end, start
				}
				clean = false
			}
			var repl string
			if mode&2 == 0 {
				var w Writer
				name := fuzzNames[k%len(fuzzNames)]
				w.Start(name)
				w.Attr("r", raw)
				w.Text(raw)
				w.End()
				var werr error
				if repl, werr = w.String(); werr != nil {
					repl = ""
				}
				if name == "q:e" || (name == "x14ac:e" && !rootDeclaresX14ac) {
					clean = false
				}
			} else {
				repl = raw
				clean = false
			}
			if err := p.Replace(start, end, repl); err != nil {
				continue
			}
			if err := q.Replace(start, end, repl); err != nil {
				t.Fatalf("Replace differs between New and NewWellFormed: %v", err)
			}
		}
		err = p.Check()
		// Over a proved original the early stop changes the cost only.
		if qerr := q.Check(); (qerr == nil) != (err == nil) {
			t.Fatalf("NewWellFormed's Check %v, New's %v", qerr, err)
		}
		out := p.String()
		if err == nil {
			if werr := tree.Check([]byte(out)); werr != nil {
				t.Fatalf("Check passed but the result is not well formed: %v\n%q", werr, out)
			}
		} else if clean && tree.Check([]byte(out)) == nil {
			t.Fatalf("node spans with writer content refused: %v\n%q", err, out)
		}
		if p.Len() != len(out) {
			t.Fatalf("Len %d, result %d bytes", p.Len(), len(out))
		}
		var b strings.Builder
		if _, err := p.WriteTo(&b); err != nil || b.String() != out {
			t.Fatalf("WriteTo differs from String: %v", err)
		}
	})
}
