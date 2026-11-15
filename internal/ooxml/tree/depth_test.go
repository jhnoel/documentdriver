package tree

import (
	"errors"
	"strings"
	"testing"
)

// nested returns depth elements nested inside each other.
func nested(depth int) []byte {
	return []byte(strings.Repeat("<e>", depth) + strings.Repeat("</e>", depth))
}

// TestMaxDepth checks the tokenizer's nesting cap: MaxDepth elements parse
// (and walk, serialise and re-check), one more is a SyntaxError, never a
// panic, in documents and fragments alike, self-closing or not.
func TestMaxDepth(t *testing.T) {
	doc, err := Parse(nested(MaxDepth))
	if err != nil {
		t.Fatalf("depth %d: %v", MaxDepth, err)
	}
	n := 0
	doc.Node().Walk(func(*Node) bool { n++; return true })
	if n != MaxDepth+1 {
		t.Fatalf("walked %d nodes, want %d", n, MaxDepth+1)
	}
	if err := Check(doc.Root().Bytes()); err != nil {
		t.Fatal(err)
	}
	deep := strings.Repeat("<e>", MaxDepth)
	for name, in := range map[string]string{
		"open":       string(nested(MaxDepth + 1)),
		"self-close": deep + "<e/>" + strings.Repeat("</e>", MaxDepth),
		"unclosed":   strings.Repeat("<e>", 100*MaxDepth),
	} {
		_, err := Parse([]byte(in))
		var se *SyntaxError
		if !errors.As(err, &se) || !strings.Contains(err.Error(), "nested more than 512 deep") {
			t.Errorf("%s: err = %v, want a depth SyntaxError", name, err)
		}
		if _, err := ParseFragment([]byte(in), nil); err == nil {
			t.Errorf("%s: fragment parsed", name)
		}
	}
}

// TestWalkOrderAndSkip checks the iterative Walk keeps document order and
// skips the descendants of a node fn rejects.
func TestWalkOrderAndSkip(t *testing.T) {
	doc, err := Parse([]byte(`<a><b><c/><d/></b><e><f/></e><g/></a>`))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	doc.Root().Walk(func(n *Node) bool {
		got = append(got, n.Name())
		return n.Name() != "e"
	})
	if s := strings.Join(got, ","); s != "a,b,c,d,e,g" {
		t.Fatalf("walk order %s", s)
	}
}
