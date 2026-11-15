package edit

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// oldNext is the pre-W1 next, kept as the baseline: it scans the
// parent's elements from the first one.
func oldNext(e *tree.Node) *tree.Node {
	seen := false
	for c := range e.Parent().ElementsSeq() {
		if seen {
			return c
		}
		seen = c == e
	}
	return nil
}

// sink keeps the benchmark loops observable.
var sink *tree.Node

// BenchmarkNext measures the next element after the last of a
// container's 4096 element children: the pre-W1 baseline scans from
// the first element (old), the link walk answers at once (new).
func BenchmarkNext(b *testing.B) {
	var sb strings.Builder
	sb.WriteString(`<w:document xmlns:w="urn:w"><w:body>`)
	for range 4096 {
		sb.WriteString(`<w:p/>`)
	}
	sb.WriteString(`</w:body></w:document>`)
	doc, err := tree.Parse([]byte(sb.String()))
	if err != nil {
		b.Fatal(err)
	}
	last := doc.Root().FirstChild().LastChild()
	if last == nil || last.Kind() != tree.ElementNode {
		b.Fatal("no last element")
	}
	b.Run("old", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sink = oldNext(last)
		}
	})
	b.Run("new", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sink = next(last)
		}
	})
}
