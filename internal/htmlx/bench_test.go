package htmlx

import (
	"strings"
	"testing"
)

// BenchmarkParseHTML parses a corpus-sized docx HTML fragment, the
// tokenizer hot path the ASCII helpers sit on.
func BenchmarkParseHTML(b *testing.B) {
	const block = `<H1>Title</H1>` +
		`<P CLASS="Quote" STYLE="text-align:center;margin-top:6pt">Plain <B>bold</B>, <I>italic</I>, H<SUB>2</SUB>O, x<SUP>2</SUP> and <A HREF="https://example.com/x?a=1&amp;b=2">a link</A> with &#x41;&#65;&notin; entities.</P>` +
		`<UL><LI>one<li>two</UL>` +
		`<TABLE><TR><TD>cell</TD><TD>other</TD></TR></TABLE>`
	fragment := strings.Repeat(block, 40)
	b.ReportAllocs()
	for b.Loop() {
		root, err := ParseHTML(fragment, Options{})
		if err != nil {
			b.Fatal(err)
		}
		_ = root
	}
}
