package create

import (
	"fmt"
	"strings"
	"testing"
)

// largeHTML is document HTML of n paragraphs with formatting and a link
// each, and a heading every fifty: about 135 bytes per paragraph.
func largeHTML(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		if i%50 == 1 {
			fmt.Fprintf(&b, "<h2>Section %d</h2>\n", i)
		}
		fmt.Fprintf(&b, `<p>Paragraph %d has <b>bold</b>, <i>italic</i> and <a href="https://example.com/%d">a link</a> in ordinary text that runs on.</p>`+"\n", i, i)
	}
	return b.String()
}

// BenchmarkCreate builds documents of growing size: time and allocation
// per paragraph should stay flat (linear in the input).
func BenchmarkCreate(b *testing.B) {
	for _, n := range []int{500, 2000, 8000} {
		in := largeHTML(n)
		b.Run(fmt.Sprintf("p%d", n), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(in)))
			for b.Loop() {
				if _, err := Build(in); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
