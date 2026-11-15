package xmlscan_test

import (
	"errors"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
)

// tokens lexes all of src.
func tokens(t *testing.T, src string) []xmlscan.Token {
	t.Helper()
	var out []xmlscan.Token
	for i := 0; i < len(src); {
		tok, err := xmlscan.Next(src, i)
		if err != nil {
			t.Fatalf("Next(%q, %d): %v", src, i, err)
		}
		if tok.Start != i || tok.End <= i {
			t.Fatalf("Next(%q, %d) = %+v: no progress", src, i, tok)
		}
		out = append(out, tok)
		i = tok.End
	}
	return out
}

func TestNextKinds(t *testing.T) {
	src := `<?xml version="1.0"?>` + "\n" +
		`<!DOCTYPE r [<!ENTITY a "x>y"><!-- ] > -->]>` +
		`<x:r a="1" b='>/' >text&amp;<![CDATA[<c>]]><!-- <d/> --><?pi x?><e/><f
 g = "h"	/></x:r >`
	type want struct {
		kind xmlscan.Kind
		text string
		name string
	}
	wants := []want{
		{xmlscan.PI, `<?xml version="1.0"?>`, ""},
		{xmlscan.Text, "\n", ""},
		{xmlscan.Doctype, `<!DOCTYPE r [<!ENTITY a "x>y"><!-- ] > -->]>`, ""},
		{xmlscan.StartTag, `<x:r a="1" b='>/' >`, "x:r"},
		{xmlscan.Text, "text&amp;", ""},
		{xmlscan.CData, "<![CDATA[<c>]]>", ""},
		{xmlscan.Comment, "<!-- <d/> -->", ""},
		{xmlscan.PI, "<?pi x?>", ""},
		{xmlscan.EmptyTag, "<e/>", "e"},
		{xmlscan.EmptyTag, "<f\n g = \"h\"\t/>", "f"},
		{xmlscan.EndTag, "</x:r >", "x:r"},
	}
	got := tokens(t, src)
	if len(got) != len(wants) {
		t.Fatalf("got %d tokens, want %d: %+v", len(got), len(wants), got)
	}
	for i, w := range wants {
		g := got[i]
		if g.Kind != w.kind || src[g.Start:g.End] != w.text || g.Name(src) != w.name {
			t.Errorf("token %d = %v %q %q, want %v %q %q", i, g.Kind, src[g.Start:g.End], g.Name(src), w.kind, w.text, w.name)
		}
	}
	if l := got[3].Local(src); l != "r" {
		t.Errorf("Local = %q, want r", l)
	}
	if l := got[1].Local(src); l != "" {
		t.Errorf("Local of text = %q, want empty", l)
	}
}

func TestNextSyntax(t *testing.T) {
	for _, src := range []string{
		"<", "<a", "<a b>", "<a b=>", "<a b=1>", `<a b="1>`, `<a b="1"c="2">`,
		`<a b="<">`, "</>", "</a", "</a b>", "<!-- x", "<![CDATA[x", "<?x", "<!ELEMENT x>",
		"<!DOCTYPE x", "<a/ >", "<>", "< a>", `<a "b"="c">`, "<a =\"c\">",
	} {
		if tok, err := xmlscan.Next(src, 0); !errors.Is(err, xmlscan.ErrSyntax) {
			t.Errorf("Next(%q) = %+v, %v; want ErrSyntax", src, tok, err)
		}
	}
	for _, i := range []int{-1, 3, 100} {
		if _, err := xmlscan.Next("<a>", i); !errors.Is(err, xmlscan.ErrSyntax) {
			t.Errorf("Next at %d: %v, want ErrSyntax", i, err)
		}
	}
}

func TestElementEnd(t *testing.T) {
	src := `<a x="/a>"><a><b/></a><!-- </a> --><![CDATA[</a>]]>t</a><z/>`
	tok, err := xmlscan.Next(src, 0)
	if err != nil {
		t.Fatal(err)
	}
	ce, end, err := xmlscan.ElementEnd(src, tok)
	if err != nil {
		t.Fatal(err)
	}
	if src[ce:end] != "</a>" || src[end:] != "<z/>" {
		t.Errorf("ElementEnd = %d, %d (%q, rest %q)", ce, end, src[ce:end], src[end:])
	}
	z, _ := xmlscan.Next(src, end)
	if ce, e, err := xmlscan.ElementEnd(src, z); err != nil || ce != z.End || e != z.End {
		t.Errorf("empty tag: %d, %d, %v", ce, e, err)
	}
	for _, bad := range []string{"<a><b></a>", "<a>", "<a><b>"} {
		tk, _ := xmlscan.Next(bad, 0)
		if _, _, err := xmlscan.ElementEnd(bad, tk); !errors.Is(err, xmlscan.ErrSyntax) {
			t.Errorf("ElementEnd(%q): %v, want ErrSyntax", bad, err)
		}
	}
	text, _ := xmlscan.Next("x<a/>", 0)
	if _, _, err := xmlscan.ElementEnd("x<a/>", text); !errors.Is(err, xmlscan.ErrSyntax) {
		t.Errorf("ElementEnd of text: %v", err)
	}
}

func TestKindString(t *testing.T) {
	for k := xmlscan.Text; k <= xmlscan.Doctype; k++ {
		if k.String() == "Kind(?)" {
			t.Errorf("kind %d has no name", k)
		}
	}
	if xmlscan.Kind(99).String() != "Kind(?)" {
		t.Error("unknown kind")
	}
}
