package xmlscan_test

import (
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
)

func TestAttrs(t *testing.T) {
	src := `<c r="A1"  s = '2'` + "\n\t" + `t="a&amp;b&#10;c" xmlns:x="u"/>`
	tok, err := xmlscan.Next(src, 0)
	if err != nil {
		t.Fatal(err)
	}
	type want struct{ whole, name, raw, value string }
	wants := []want{
		{` r="A1"`, "r", "A1", "A1"},
		{`  s = '2'`, "s", "2", "2"},
		{"\n\t" + `t="a&amp;b&#10;c"`, "t", "a&amp;b&#10;c", "a&b\nc"},
		{` xmlns:x="u"`, "xmlns:x", "u", "u"},
	}
	i := 0
	for a := range xmlscan.Attrs(src, tok) {
		if i >= len(wants) {
			t.Fatalf("extra attribute %+v", a)
		}
		w := wants[i]
		if src[a.Start:a.End()] != w.whole || a.Name(src) != w.name || a.Raw(src) != w.raw || a.Value(src) != w.value {
			t.Errorf("attr %d: %q %q %q %q, want %+v", i, src[a.Start:a.End()], a.Name(src), a.Raw(src), a.Value(src), w)
		}
		i++
	}
	if i != len(wants) {
		t.Errorf("got %d attributes, want %d", i, len(wants))
	}
	a, ok := xmlscan.FindAttr(src, tok, "s")
	if !ok || a.Quote(src) != '\'' || a.Raw(src) != "2" {
		t.Errorf("FindAttr s = %+v, %v", a, ok)
	}
	if _, ok := xmlscan.FindAttr(src, tok, "x"); ok {
		t.Error("FindAttr found a missing attribute")
	}
	// Early break, and tokens that are not tags.
	for range xmlscan.Attrs(src, tok) {
		break
	}
	end, _ := xmlscan.Next("</c>", 0)
	for range xmlscan.Attrs("</c>", end) {
		t.Error("an end tag has no attributes")
	}
	if _, ok := xmlscan.FindAttr("</c>", end, "r"); ok {
		t.Error("FindAttr on an end tag")
	}
	// A token from another source never panics.
	for range xmlscan.Attrs("<c", tok) {
		t.Error("attributes past the source")
	}
}
