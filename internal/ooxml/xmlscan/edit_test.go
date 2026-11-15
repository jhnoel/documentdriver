package xmlscan_test

import (
	"errors"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
)

func TestApply(t *testing.T) {
	src := "0123456789"
	got, err := xmlscan.Apply(src, []xmlscan.Edit{
		{Start: 8, End: 9, Text: "x"},
		{Start: 2, End: 4, Text: ""},
		{Start: 5, End: 5, Text: "a"},
		{Start: 5, End: 5, Text: "b"},
		{Start: 5, End: 6, Text: "-"},
	})
	if err != nil || got != "014ab-67x9" {
		t.Errorf("Apply = %q, %v", got, err)
	}
	if got, err := xmlscan.Apply(src, nil); err != nil || got != src {
		t.Errorf("no edits = %q, %v", got, err)
	}
	for _, bad := range [][]xmlscan.Edit{
		{{Start: 1, End: 4}, {Start: 3, End: 5}},
		{{Start: -1, End: 2}},
		{{Start: 4, End: 3}},
		{{Start: 9, End: 11}},
	} {
		if _, err := xmlscan.Apply(src, bad); !errors.Is(err, xmlscan.ErrOverlap) {
			t.Errorf("Apply(%v): %v, want ErrOverlap", bad, err)
		}
	}
}

func TestEditTag(t *testing.T) {
	set := func(n, v string) xmlscan.AttrEdit { return xmlscan.AttrEdit{Name: n, Value: v} }
	del := func(n string) xmlscan.AttrEdit { return xmlscan.AttrEdit{Name: n, Remove: true} }
	for _, tc := range []struct {
		tag   string
		edits []xmlscan.AttrEdit
		want  string
	}{
		// Existing attributes keep spacing, '=' and quote.
		{`<col  min = '1' max="2"/>`, []xmlscan.AttrEdit{set("min", "3")}, `<col  min = '3' max="2"/>`},
		{`<col min='1'/>`, []xmlscan.AttrEdit{set("min", "it's")}, `<col min='it&apos;s'/>`},
		// Equal decoded value: no change.
		{`<a v="x&amp;y"/>`, []xmlscan.AttrEdit{set("v", "x&y")}, `<a v="x&amp;y"/>`},
		// New attributes go right after the last closing quote, in order,
		// double-quoted; trailing whitespace stays before the close.
		{`<col min="1" max="2"  />`, []xmlscan.AttrEdit{set("width", "9"), set("customWidth", "1")}, `<col min="1" max="2" width="9" customWidth="1"  />`},
		{`<col min='1'>`, []xmlscan.AttrEdit{set("w", `a"<`)}, `<col min='1' w="a&quot;&lt;">`},
		// No attributes: right after the name.
		{`<col />`, []xmlscan.AttrEdit{set("min", "1")}, `<col min="1" />`},
		{`<x:col/>`, []xmlscan.AttrEdit{set("min", "1")}, `<x:col min="1"/>`},
		// Removal takes the leading whitespace; absent removal is a no-op.
		{`<c r="A1"` + "\n  " + `s="2" t="n"/>`, []xmlscan.AttrEdit{del("s"), del("zz")}, `<c r="A1" t="n"/>`},
		// Removing the last attribute and adding one: the new one lands
		// where the removed one ended.
		{`<c r="A1" s="2"/>`, []xmlscan.AttrEdit{del("s"), set("t", "s")}, `<c r="A1" t="s"/>`},
		// The last edit of a name wins.
		{`<c r="A1"/>`, []xmlscan.AttrEdit{set("r", "B2"), del("r")}, `<c/>`},
		// Tab, line feed and carriage return become references.
		{`<a/>`, []xmlscan.AttrEdit{set("v", "a\tb\nc\r")}, `<a v="a&#9;b&#10;c&#13;"/>`},
	} {
		tok, err := xmlscan.Next(tc.tag, 0)
		if err != nil {
			t.Fatal(err)
		}
		got, err := xmlscan.EditTag(tc.tag, tok, tc.edits...)
		if err != nil || got != tc.want {
			t.Errorf("EditTag(%q, %v) = %q, %v; want %q", tc.tag, tc.edits, got, err, tc.want)
		}
	}
	src := `<p><c r="1"/></p>`
	c, _ := xmlscan.Next(src, 3)
	if got, err := xmlscan.EditTag(src, c); err != nil || got != `<c r="1"/>` {
		t.Errorf("no edits: %q, %v", got, err)
	}
	for _, e := range []xmlscan.AttrEdit{set("1x", "v"), set("a:b:c", "v"), set("v", "\x01"), set("v", "\xff"), del("")} {
		got, err := xmlscan.EditTag(src, c, e)
		if !errors.Is(err, xmlscan.ErrValue) || got != `<c r="1"/>` {
			t.Errorf("EditTag(%+v) = %q, %v; want the tag and ErrValue", e, got, err)
		}
	}
	end, _ := xmlscan.Next(src, 13)
	if _, err := xmlscan.EditTag(src, end, set("a", "b")); !errors.Is(err, xmlscan.ErrSyntax) {
		t.Errorf("end tag: %v", err)
	}
}

// TestEditTagMatchesSetAttr holds EditTag to tree.Node.SetAttr and
// RemoveAttr for edits of existing attributes and removals. A new
// attribute is left out: tree picks its quote with preferredQuote,
// EditTag always writes '"'.
func TestEditTagMatchesSetAttr(t *testing.T) {
	tags := []string{
		`<a x="1" y='2'/>`,
		`<a  x = "1"` + "\n" + `y='2'   z="&amp;"/>`,
		`<a x="1">`,
		`<a xmlns:p="u" p:x="1" y="2"/>`,
	}
	values := []string{"1", "", "new", `q"uote`, "ap'os", "<&>", "tab\there", "multi\nline"}
	for _, tag := range tags {
		src := tag
		if tag[len(tag)-2] != '/' {
			src = tag + "</a>"
		}
		tok, err := xmlscan.Next(src, 0)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for a := range xmlscan.Attrs(src, tok) {
			names = append(names, a.Name(src))
		}
		for _, name := range names {
			edits := []xmlscan.AttrEdit{{Name: name, Remove: true}}
			for _, v := range values {
				edits = append(edits, xmlscan.AttrEdit{Name: name, Value: v})
			}
			for _, e := range edits {
				doc, err := tree.ParseString(src)
				if err != nil {
					t.Fatal(err)
				}
				root := doc.Root()
				if e.Remove {
					root.RemoveAttr(e.Name)
				} else if err := root.SetAttr(e.Name, e.Value); err != nil {
					t.Fatal(err)
				}
				want := string(root.StartTag())
				got, err := xmlscan.EditTag(src, tok, e)
				if err != nil || got != want {
					t.Errorf("EditTag(%q, %+v) = %q, %v; tree writes %q", tag, e, got, err, want)
				}
			}
		}
	}
}
