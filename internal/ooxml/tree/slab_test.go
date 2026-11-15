package tree

import "testing"

// TestNodeBound checks that the first chunk of a small source is never
// smaller than the nodes it parses into, so a fragment is one chunk.
func TestNodeBound(t *testing.T) {
	ins := []string{`<a/>`, `t<a>x</a>y`, `<w:r><w:t xml:space="preserve">a > b</w:t></w:r>`, slide,
		`<r><![CDATA[</x><y>]]><!--</z>--><?p <q?></r>`, "<r>\n  <a/>\n</r>\n"}
	for _, in := range ins {
		nodes, err := ParseFragment([]byte(in), nil)
		if err != nil {
			doc, err2 := Parse([]byte(in))
			if err2 != nil {
				t.Fatal(err, err2)
			}
			nodes = doc.Node().Children()
		}
		count := 0
		for _, n := range nodes {
			n.Walk(func(*Node) bool { count++; return true })
		}
		if b := nodeBound(in); b < count {
			t.Errorf("nodeBound(%q) = %d, below its %d nodes", in, b, count)
		}
	}
}
