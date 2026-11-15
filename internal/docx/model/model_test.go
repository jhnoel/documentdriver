package model

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

func parseBody(t *testing.T, body string) *XML {
	t.Helper()
	x, err := ParseXML([]byte(`<w:document xmlns:w="` + NSW + `" xmlns:mc="` + NSMC + `"><w:body>` + body + `</w:body></w:document>`))
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func texts(ps []Paragraph, v View) []string {
	var out []string
	for _, p := range ps {
		out = append(out, ParagraphText(p.Node, v))
	}
	return out
}

// Ported from document.rs: story_paragraphs_exclude_nested_textbox_paragraphs_but_include_tables.
func TestStoryParagraphsExcludeNestedTextboxParagraphsButIncludeTables(t *testing.T) {
	x := parseBody(t, `<w:p><w:r><w:t>outer</w:t><w:drawing><w:txbxContent>`+
		`<w:p><w:r><w:t>textbox</w:t></w:r></w:p>`+
		`</w:txbxContent></w:drawing></w:r></w:p>`+
		`<w:tbl><w:tr><w:tc><w:p><w:r><w:t>table</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`+
		`<mc:AlternateContent>`+
		`<mc:Choice Requires="wps"><w:p><w:r><w:t>choice paragraph</w:t></w:r></w:p></mc:Choice>`+
		`<mc:Fallback><w:p><w:r><w:t>fallback paragraph</w:t></w:r></w:p></mc:Fallback>`+
		`</mc:AlternateContent>`+
		`<w:p><w:r><w:t>last</w:t></w:r></w:p>`)
	got := texts(x.Paragraphs(), Current)
	want := []string{"outer\uFFFC", "table", "fallback paragraph", "last"} // the drawing is one object
	if !slices.Equal(got, want) {
		t.Fatalf("paragraphs = %q, want %q", got, want)
	}
	for i, p := range x.Paragraphs() {
		if p.Index != i+1 {
			t.Errorf("index %d = %d", i, p.Index)
		}
	}
}

// Ported from document.rs: paragraph_text_selects_fallback_without_crossing_nested_story.
func TestParagraphTextSelectsFallbackWithoutCrossingNestedStory(t *testing.T) {
	x := parseBody(t, `<w:p><w:r><w:t>before-</w:t>`+
		`<mc:AlternateContent>`+
		`<mc:Choice Requires="wps"><w:r><w:t>choice</w:t></w:r></mc:Choice>`+
		`<mc:Fallback><w:r><w:t>fallback</w:t><w:drawing><w:txbxContent>`+
		`<w:p><w:r><w:t>16′</w:t></w:r></w:p>`+
		`</w:txbxContent></w:drawing></w:r></mc:Fallback>`+
		`</mc:AlternateContent><w:t>-after</w:t></w:r></w:p>`)
	if got := ParagraphText(x.Paragraphs()[0].Node, Current); got != "before-fallback\uFFFC-after" { // the drawing is one object
		t.Fatalf("text = %q", got)
	}
}

// Ported from document.rs: alternate_content_without_fallback_has_no_effective_text.
func TestAlternateContentWithoutFallbackHasNoEffectiveText(t *testing.T) {
	x := parseBody(t, `<w:p><w:r><w:t>before</w:t><mc:AlternateContent>`+
		`<mc:Choice Requires="wps"><w:t>unsupported</w:t></mc:Choice>`+
		`</mc:AlternateContent><w:t>after</w:t></w:r></w:p>`)
	if got := ParagraphText(x.Paragraphs()[0].Node, Current); got != "beforeafter" {
		t.Fatalf("text = %q", got)
	}
}

func TestParagraphTextViews(t *testing.T) {
	x := parseBody(t, `<w:p><w:r><w:t>keep </w:t></w:r>`+
		`<w:ins w:id="1"><w:r><w:t>new </w:t></w:r></w:ins>`+
		`<w:del w:id="2"><w:r><w:delText>old </w:delText></w:r></w:del>`+
		`<w:moveTo w:id="3"><w:r><w:t>to </w:t></w:r></w:moveTo>`+
		`<w:moveFrom w:id="4"><w:r><w:t>from</w:t></w:r></w:moveFrom></w:p>`)
	p := x.Paragraphs()[0].Node
	for v, want := range map[View]string{
		Current:  "keep new to ",
		Baseline: "keep old from",
		All:      "keep new old to from",
	} {
		if got := ParagraphText(p, v); got != want {
			t.Errorf("view %d: %q, want %q", v, got, want)
		}
	}
}

// Ported from segments.rs: segments_match_visible_fallback_text_and_ignore_nested_paragraphs.
func TestSegmentsMatchVisibleFallbackTextAndIgnoreNestedParagraphs(t *testing.T) {
	x := parseBody(t, `<w:p>`+
		`<w:r><w:t>abc</w:t></w:r>`+
		`<w:r><mc:AlternateContent>`+
		`<mc:Choice Requires="wps"><w:r><w:t>wrong</w:t></w:r></mc:Choice>`+
		`<mc:Fallback><w:t>def</w:t><w:drawing><w:txbxContent>`+
		`<w:p><w:r><w:t>16′16′16′16′</w:t></w:r></w:p>`+
		`</w:txbxContent></w:drawing></mc:Fallback>`+
		`</mc:AlternateContent></w:r>`+
		`<w:r><w:t>ghi</w:t></w:r>`+
		`</w:p>`)
	p := x.Paragraphs()[0].Node
	segs := BuildSegments(p)
	if got := AllText(segs); got != "abcdef\uFFFCghi" { // the drawing is one object
		t.Fatalf("all text = %q", got)
	}
	if got := ParagraphText(p, All); got != "abcdef\uFFFCghi" {
		t.Fatalf("paragraph text = %q", got)
	}
	type span struct {
		text       string
		start, end int
	}
	var got []span
	for _, s := range segs {
		got = append(got, span{s.Text, s.Start, s.End})
	}
	want := []span{{"abc", 0, 3}, {"def", 3, 6}, {"\uFFFC", 6, 7}, {"ghi", 7, 10}}
	if !slices.Equal(got, want) {
		t.Fatalf("segments = %v, want %v", got, want)
	}
	if !IsW(segs[1].Run, "r") || segs[1].Run.Parent() != p {
		t.Fatalf("segment 1 run is not the paragraph's run")
	}
}

// Ported from segments.rs: segments_ignore_unsupported_choice_without_fallback.
func TestSegmentsIgnoreUnsupportedChoiceWithoutFallback(t *testing.T) {
	x := parseBody(t, `<w:p><w:r><w:t>before</w:t><mc:AlternateContent>`+
		`<mc:Choice Requires="wps"><w:t>unsupported</w:t></mc:Choice>`+
		`</mc:AlternateContent><w:t>after</w:t></w:r></w:p>`)
	segs := BuildSegments(x.Paragraphs()[0].Node)
	if got := AllText(segs); got != "beforeafter" {
		t.Fatalf("all text = %q", got)
	}
	if segs[0].Start != 0 || segs[0].End != 6 || segs[1].Start != 6 || segs[1].End != 11 {
		t.Fatalf("spans = %d-%d, %d-%d", segs[0].Start, segs[0].End, segs[1].Start, segs[1].End)
	}
}

// docxdriver panics on a text leaf outside a run; the port keeps going.
func TestSegmentOutsideRunHasNoRun(t *testing.T) {
	x := parseBody(t, `<w:p><w:t>bare</w:t><w:r><w:t>run</w:t></w:r></w:p>`)
	segs := BuildSegments(x.Paragraphs()[0].Node)
	if len(segs) != 2 || segs[0].Run != nil || segs[1].Run == nil {
		t.Fatalf("segments = %+v", segs)
	}
}

func TestCharSlice(t *testing.T) {
	for _, c := range []struct {
		s          string
		start, end int
		want       string
	}{
		{"😀ab", 0, 1, "😀"}, {"😀ab", 1, 3, "ab"}, {"abc", 2, 9, "c"}, {"abc", 5, 9, ""}, {"abc", 2, 1, ""},
	} {
		if got := CharSlice(c.s, c.start, c.end); got != c.want {
			t.Errorf("CharSlice(%q, %d, %d) = %q, want %q", c.s, c.start, c.end, got, c.want)
		}
	}
}

func TestParaIDs(t *testing.T) {
	for v, want := range map[string]bool{
		"00000001": true, "7FFFFFFF": true, "7fffffff": true, "00000000": false,
		"80000000": false, "1234567": false, "1234567G": false, "+1234567": false,
	} {
		if got := IsValidParaID(v); got != want {
			t.Errorf("IsValidParaID(%q) = %v", v, got)
		}
	}
	if got := FormatParaID(0xabc); got != "00000ABC" {
		t.Errorf("FormatParaID = %q", got)
	}
	x := parseBody(t, `<w:p xmlns:w14="`+NSW14+`" w14:paraId="0000000a"/>`+
		`<w:p xmlns:w14="`+NSW14+`" w14:paraId="0000000A"/>`+
		`<w:p/><w:p><w:r><w:drawing><w:txbxContent><w:p/></w:txbxContent></w:drawing></w:r></w:p>`)
	idx := x.ResolveParagraphIDs(DocumentPart, [32]byte{})
	ps := x.AllParagraphs()
	if got := idx.IDs[ps[0]]; got.ID != "0000000A" || got.Ord != 1 {
		t.Errorf("first = %+v", got)
	}
	// The duplicate keeps its stored digits, addressed "~2"; the three
	// paragraphs without a stored id are repaired.
	if got := idx.IDs[ps[1]].ID; got != "0000000A~2" || idx.Repaired[ps[1]] {
		t.Errorf("duplicate = %s", got)
	}
	if idx.Repairs != 3 || idx.Repaired[ps[0]] || !idx.Repaired[ps[2]] {
		t.Errorf("repairs = %d, repaired %v", idx.Repairs, idx.Repaired)
	}
	if got := idx.IDs[ps[4]]; got.Ord != 0 || !IsValidParaID(got.ID) {
		t.Errorf("nested = %+v", got)
	}
	// A stored id outside the mintable range is the address as it stands;
	// one that is not eight hex digits gets an unwritten provisional id.
	y := parseBody(t, `<w:p xmlns:w14="`+NSW14+`" w14:paraId="F0000000"/><w:p xmlns:w14="`+NSW14+`" w14:paraId="ZZ"/>`)
	yi := y.ResolveParagraphIDs(DocumentPart, [32]byte{})
	yp := y.AllParagraphs()
	if got := yi.IDs[yp[0]].ID; got != "F0000000" || yi.Repaired[yp[0]] || yi.Unstable[yp[0]] {
		t.Errorf("out of range = %s", got)
	}
	if !yi.Unstable[yp[1]] || yi.Repaired[yp[1]] || !IsValidParaID(yi.IDs[yp[1]].ID) || yi.Repairs != 1 {
		t.Errorf("malformed = %+v %v %v", yi.IDs[yp[1]], yi.Unstable, yi.Repaired)
	}
	seen := map[string]bool{}
	for _, p := range ps {
		id := idx.IDs[p].ID
		if seen[id] {
			t.Errorf("duplicate id %s", id)
		}
		seen[id] = true
	}
	// Deterministic: the same input gives the same ids.
	again := x.ResolveParagraphIDs(DocumentPart, [32]byte{})
	for _, p := range ps {
		if idx.IDs[p] != again.IDs[p] {
			t.Errorf("ids differ between runs")
		}
	}
}

func TestParseIntegers(t *testing.T) {
	if v, ok := ParseU32("+7"); !ok || v != 7 {
		t.Errorf("ParseU32(+7) = %d, %v", v, ok)
	}
	for _, s := range []string{"", "-1", " 1", "1 ", "4294967296", "0x1", "+"} {
		if _, ok := ParseU32(s); ok {
			t.Errorf("ParseU32(%q) accepted", s)
		}
	}
	if v, ok := ParseI64("-12"); !ok || v != -12 {
		t.Errorf("ParseI64(-12) = %d, %v", v, ok)
	}
	if _, ok := ParseI64("--1"); ok {
		t.Error("ParseI64(--1) accepted")
	}
}

func TestFormatNumber(t *testing.T) {
	for _, c := range []struct {
		v    int64
		f    string
		want string
	}{
		{1, "lowerLetter", "a"}, {27, "lowerLetter", "aa"}, {28, "upperLetter", "AB"},
		{0, "lowerLetter", "0"}, {1994, "upperRoman", "MCMXCIV"}, {-3, "lowerRoman", "-3"},
		{7, "decimalZero", "07"}, {-5, "decimalZero", "0-5"}, {12, "decimalZero", "12"},
		{3, "ordinal", "3"},
	} {
		if got := formatNumber(c.v, c.f); got != c.want {
			t.Errorf("formatNumber(%d, %s) = %q, want %q", c.v, c.f, got, c.want)
		}
	}
}

func TestParseErrorWording(t *testing.T) {
	_, err := ParseXML([]byte("<a>\n  <b x=\"1\" x=\"2\"/></a>"))
	var pe *ParseError
	if !errors.As(err, &pe) || pe.Msg != "document.xml parse failed: parse error at 2:17: duplicate attribute: 'x'" {
		t.Errorf("duplicate attribute: %v", err)
	}
	_, err = ParseXML([]byte("<a><b></c></a>"))
	if err == nil || err.Error() != "document.xml parse failed: parse error at 1:10: mismatched end tag: expected </b>, found </c>" {
		t.Errorf("mismatched end tag: %v", err)
	}
	if _, ok := errors.AsType[*tree.SyntaxError](err); !ok {
		t.Errorf("cause is not a tree.SyntaxError")
	}
}

func TestResolveRelTarget(t *testing.T) {
	for _, c := range [][3]string{
		{"word/document.xml", "media/a.png", "word/media/a.png"},
		{"word/document.xml", "../customXml/x.xml", "customXml/x.xml"},
		{"word/document.xml", "/word/x.xml", "word/x.xml"},
		{"document.xml", "./a//b.xml", "a/b.xml"},
		{"word/document.xml", "../../x", "x"},
	} {
		if got := ResolveRelTarget(c[0], c[1]); got != c[2] {
			t.Errorf("ResolveRelTarget(%q, %q) = %q, want %q", c[0], c[1], got, c[2])
		}
	}
	if got := RelsPartFor("word/document.xml"); got != "word/_rels/document.xml.rels" {
		t.Errorf("RelsPartFor = %q", got)
	}
	if got := RelsPartFor("doc.xml"); got != "_rels/doc.xml.rels" {
		t.Errorf("RelsPartFor = %q", got)
	}
}

func TestImageMIME(t *testing.T) {
	for part, want := range map[string]string{
		"word/media/a.PNG": "image/png", "x.jpg": "image/jpeg", "x.JPEG": "image/jpeg",
		"a.b.svg": "image/svg+xml", "x.webp": "image/webp", "x.bmp": "image/bmp", "x.gif": "image/gif",
		"x.emf": "", "png": "image/png", "word/media.png/x": "",
	} {
		got, ok := ImageMIME(part)
		if got != want || ok != (want != "") {
			t.Errorf("ImageMIME(%q) = %q, %v", part, got, ok)
		}
	}
	if got := ImageExt("a/B.PnG"); got != "png" {
		t.Errorf("ImageExt = %q", got)
	}
	if !strings.HasPrefix(ImageExt("x.İ"), "İ") {
		t.Errorf("ImageExt lowered a non-ASCII letter")
	}
}

func TestParseUTF16(t *testing.T) {
	src := `<?xml version="1.0" encoding="UTF-16" standalone="yes"?>` + "\r\n" + `<w:document xmlns:w="` + NSW + `"><w:body><w:p><w:r><w:t>héllo 😀</w:t></w:r></w:p></w:body></w:document>`
	for _, le := range []bool{true, false} {
		b := []byte{0xFE, 0xFF}
		if le {
			b = []byte{0xFF, 0xFE}
		}
		for _, u := range utf16.Encode([]rune(src)) {
			if le {
				b = append(b, byte(u), byte(u>>8))
			} else {
				b = append(b, byte(u>>8), byte(u))
			}
		}
		x, err := ParseXML(b)
		if err != nil {
			t.Fatalf("little endian %v: %v", le, err)
		}
		if got := ParagraphText(x.Paragraphs()[0].Node, Current); got != "héllo 😀" {
			t.Errorf("text = %q", got)
		}
	}
}

// TestLineEndsAtTheAccessor: the tree keeps a part's bytes as written
// (CRLF and lone CR included, so an untouched part serialises unchanged)
// and the text accessor normalises them, in text and CDATA alike; a
// syntax error is positioned in the normalised text, as docxdriver's
// parser counts.
func TestLineEndsAtTheAccessor(t *testing.T) {
	src := "<w:document xmlns:w=\"" + NSW + "\">\r\n<w:body>\r<w:p><w:r><w:t xml:space=\"preserve\">a\r\nb\rc</w:t>" +
		"<w:t><![CDATA[d\r\ne]]></w:t></w:r></w:p></w:body></w:document>"
	x, err := ParseXML([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(x.Doc.Bytes()); got != src {
		t.Errorf("tree bytes changed:\n%q\n%q", got, src)
	}
	if got := ParagraphText(x.Paragraphs()[0].Node, Current); got != "a\nb\ncd\ne" {
		t.Errorf("text = %q", got)
	}
	_, err = ParseXML([]byte("<a>\r\n\r<b x=\"1\" x=\"2\"/></a>"))
	if err == nil || err.Error() != "document.xml parse failed: parse error at 3:15: duplicate attribute: 'x'" {
		t.Errorf("position: %v", err)
	}
}
