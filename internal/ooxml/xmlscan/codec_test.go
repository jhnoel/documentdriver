package xmlscan_test

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
)

func TestDecode(t *testing.T) {
	for _, tc := range []struct{ raw, text, attr string }{
		{"plain", "plain", "plain"},
		{"a&amp;b&lt;&gt;&quot;&apos;", `a&b<>"'`, `a&b<>"'`},
		{"&#65;&#x42;&#x1F600;", "AB\U0001F600", "AB\U0001F600"},
		{"a\r\nb\rc\nd\te", "a\nb\nc\nd\te", "a b c d e"},
		{"&bogus; &#0; &#xD800; &", "&bogus; &#0; &#xD800; &", "&bogus; &#0; &#xD800; &"},
		{"&#9;&#10;&#13;", "\t\n\r", "\t\n\r"},
	} {
		if got := xmlscan.DecodeText(tc.raw); got != tc.text {
			t.Errorf("DecodeText(%q) = %q, want %q", tc.raw, got, tc.text)
		}
		if got := xmlscan.DecodeAttr(tc.raw); got != tc.attr {
			t.Errorf("DecodeAttr(%q) = %q, want %q", tc.raw, got, tc.attr)
		}
	}
}

func TestEscape(t *testing.T) {
	for _, tc := range []struct{ s, text, attrDQ, attrSQ, minDQ string }{
		{"plain", "plain", "plain", "plain", "plain"},
		{`a&b<c>d"e'f`, `a&amp;b&lt;c&gt;d"e'f`, `a&amp;b&lt;c&gt;d&quot;e'f`, `a&amp;b&lt;c&gt;d"e&apos;f`, `a&amp;b&lt;c>d&quot;e'f`},
		{"t\tn\nr\r", "t\tn\nr&#13;", "t&#9;n&#10;r&#13;", "t&#9;n&#10;r&#13;", "t&#9;n&#10;r&#13;"},
		{"]]>", "]]&gt;", "]]&gt;", "]]&gt;", "]]>"},
	} {
		if got := xmlscan.EscapeText(tc.s); got != tc.text {
			t.Errorf("EscapeText(%q) = %q, want %q", tc.s, got, tc.text)
		}
		if got := xmlscan.EscapeAttr(tc.s, '"'); got != tc.attrDQ {
			t.Errorf("EscapeAttr(%q, \") = %q, want %q", tc.s, got, tc.attrDQ)
		}
		if got := xmlscan.EscapeAttr(tc.s, '\''); got != tc.attrSQ {
			t.Errorf("EscapeAttr(%q, ') = %q, want %q", tc.s, got, tc.attrSQ)
		}
		if got := xmlscan.EscapeAttrMinimal(tc.s, '"'); got != tc.minDQ {
			t.Errorf("EscapeAttrMinimal(%q) = %q, want %q", tc.s, got, tc.minDQ)
		}
	}
	if got := string(tree.EscapeText("a<b>&\r")); got != xmlscan.EscapeText("a<b>&\r") {
		t.Errorf("tree.EscapeText = %q, xmlscan %q", got, xmlscan.EscapeText("a<b>&\r"))
	}
}

// FuzzCodec holds the codec to tree, the reference until tree calls it
// (S1b): a raw attribute value or character data that tree accepts
// decodes as tree decodes it, and any XML string escapes to a raw value
// that tree decodes back to it.
func FuzzCodec(f *testing.F) {
	for _, s := range []string{"", "a&amp;b", "&#x1F600;", "a\r\nb", "\t\n\r", `"'<>&`, "]]>", "&#13;", "\x00"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if doc, err := tree.ParseString(`<a v="` + s + `"/>`); err == nil && !strings.Contains(s, `"`) {
			want, _ := doc.Root().Attr("v")
			if got := xmlscan.DecodeAttr(s); got != want {
				t.Fatalf("DecodeAttr(%q) = %q, tree %q", s, got, want)
			}
		}
		if doc, err := tree.ParseString(`<a>` + s + `</a>`); err == nil && !strings.ContainsAny(s, "<") {
			if c := doc.Root().FirstChild(); c != nil && c.NextSibling() == nil && c.Kind() == tree.TextNode {
				if got := xmlscan.DecodeText(s); got != c.Data() {
					t.Fatalf("DecodeText(%q) = %q, tree %q", s, got, c.Data())
				}
			}
		}
		if !xmlscan.ValidChars(s) {
			return
		}
		for _, q := range []byte{'"', '\''} {
			raw := xmlscan.EscapeAttr(s, q)
			doc, err := tree.ParseString(`<a v=` + string(q) + raw + string(q) + `/>`)
			if err != nil {
				t.Fatalf("EscapeAttr(%q, %c) = %q: tree refuses it: %v", s, q, raw, err)
			}
			if got, _ := doc.Root().Attr("v"); got != s {
				t.Fatalf("EscapeAttr(%q, %c) = %q decodes to %q", s, q, raw, got)
			}
			if got := xmlscan.DecodeAttr(raw); got != s {
				t.Fatalf("DecodeAttr(EscapeAttr(%q)) = %q", s, got)
			}
			if got := xmlscan.DecodeAttr(xmlscan.EscapeAttrMinimal(s, q)); got != s {
				t.Fatalf("DecodeAttr(EscapeAttrMinimal(%q)) = %q", s, got)
			}
		}
		raw := xmlscan.EscapeText(s)
		if got := xmlscan.DecodeText(raw); got != s {
			t.Fatalf("DecodeText(EscapeText(%q)) = %q", s, got)
		}
		if _, err := tree.ParseString(`<a>` + raw + `</a>`); err != nil {
			t.Fatalf("EscapeText(%q) = %q: tree refuses it: %v", s, raw, err)
		}
	})
}
