package tree

import (
	"errors"
	"strings"
	"testing"
)

const (
	nsA = "http://schemas.openxmlformats.org/drawingml/2006/main"
	nsP = "http://schemas.openxmlformats.org/presentationml/2006/main"
	nsR = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
)

const slide = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\r\n" +
	`<p:sld xmlns:a="` + nsA + `" xmlns:r="` + nsR + `" xmlns:p="` + nsP + `">` +
	`<p:cSld><p:spTree>` +
	`<p:sp><p:nvSpPr><p:cNvPr id="2" name='Box &amp; co'/></p:nvSpPr>` +
	`<p:spPr><a:xfrm rot="0"><a:off x="100"  y='200'/><a:ext cx="10" cy="20"></a:ext></a:xfrm></p:spPr></p:sp>` +
	`<p:sp><p:nvSpPr><p:cNvPr id="3" name="Other"/></p:nvSpPr><p:spPr/></p:sp>` +
	`</p:spTree></p:cSld></p:sld>`

func mustParse(t *testing.T, s string) *Document {
	t.Helper()
	d, err := Parse([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func mustSelect1(t *testing.T, d *Document, sel string) *Node {
	t.Helper()
	ns, err := d.Select(sel)
	if err != nil {
		t.Fatal(err)
	}
	if len(ns) != 1 {
		t.Fatalf("Select(%q) matched %d nodes", sel, len(ns))
	}
	return ns[0]
}

// edit applies fn to a fresh parse of in and checks the serialisation
// against want, and that it is well formed.
func edit(t *testing.T, in string, fn func(d *Document) error, want string) {
	t.Helper()
	d := mustParse(t, in)
	if err := fn(d); err != nil {
		t.Fatal(err)
	}
	got := string(d.Bytes())
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if err := Check([]byte(got)); err != nil {
		t.Fatalf("output not well formed: %v", err)
	}
}

func TestSetAttrChangesOnlyItsBytes(t *testing.T) {
	cases := []struct {
		name, sel, attr, val string
		from, to             string
	}{
		{"double quoted", "//a:ext", "cx", "999", `cx="10"`, `cx="999"`},
		{"single quoted keeps quote", "//a:off", "y", "it's", `y='200'`, `y='it&apos;s'`},
		{"spacing kept", "//a:off", "x", "5", `x="100"  y`, `x="5"  y`},
		{"escaping", "//p:cNvPr[@id='3']", "name", `a<b&"c"`, `name="Other"`, `name="a&lt;b&amp;&quot;c&quot;"`},
		{"new attribute", "//a:xfrm", "flipH", "1", `<a:xfrm rot="0">`, `<a:xfrm rot="0" flipH="1">`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			want := strings.Replace(slide, c.from, c.to, 1)
			edit(t, slide, func(d *Document) error {
				return mustSelect1(t, d, c.sel).SetAttr(c.attr, c.val)
			}, want)
		})
	}
}

func TestSetAttrSameValueIsNoOp(t *testing.T) {
	d := mustParse(t, slide)
	n := mustSelect1(t, d, "//p:cNvPr[@id='2']")
	if err := n.SetAttr("name", "Box & co"); err != nil {
		t.Fatal(err)
	}
	if d.Modified() {
		t.Fatal("setting an unchanged value marked the document modified")
	}
}

func TestRemoveAttr(t *testing.T) {
	edit(t, slide, func(d *Document) error {
		if !mustSelect1(t, d, "//a:off").RemoveAttr("x") {
			t.Fatal("x not found")
		}
		return nil
	}, strings.Replace(slide, `<a:off x="100"  y='200'/>`, `<a:off  y='200'/>`, 1))
}

func TestSetAttrUndeclaredPrefix(t *testing.T) {
	d := mustParse(t, slide)
	err := mustSelect1(t, d, "//a:off").SetAttr("zz:q", "1")
	if !errors.Is(err, ErrUndeclaredPrefix) {
		t.Fatalf("err = %v", err)
	}
}

func TestSetAttrNS(t *testing.T) {
	// r is declared on the root: reuse it.
	edit(t, slide, func(d *Document) error {
		return mustSelect1(t, d, "//p:cNvPr[@id='3']").SetAttrNS(nsR, "rel", "id", "rId4")
	}, strings.Replace(slide, `name="Other"/>`, `name="Other" r:id="rId4"/>`, 1))
	// An unknown namespace is declared on the element itself.
	edit(t, slide, func(d *Document) error {
		return mustSelect1(t, d, "//p:cNvPr[@id='3']").SetAttrNS("urn:x", "x", "y", "1")
	}, strings.Replace(slide, `name="Other"/>`, `name="Other" xmlns:x="urn:x" x:y="1"/>`, 1))
}

func TestInsertNewElementReusesPrefix(t *testing.T) {
	edit(t, slide, func(d *Document) error {
		// The caller suggests prefix "dm", but "a" is in scope for the URI.
		el, err := NewElement(nsA, "dm", "solidFill")
		if err != nil {
			return err
		}
		spPr := mustSelect1(t, d, "//p:sp[2]/p:spPr")
		return spPr.AppendChild(el)
	}, strings.Replace(slide, `<p:spPr/></p:sp></p:spTree>`, `<p:spPr><a:solidFill/></p:spPr></p:sp></p:spTree>`, 1))
}

func TestInsertNewElementDeclaresMissingNamespaceLocally(t *testing.T) {
	edit(t, slide, func(d *Document) error {
		el, err := NewElement("urn:ext", "e", "thing")
		if err != nil {
			return err
		}
		return mustSelect1(t, d, "//p:sp[2]/p:spPr").AppendChild(el)
	}, strings.Replace(slide, `<p:spPr/></p:sp></p:spTree>`, `<p:spPr><e:thing xmlns:e="urn:ext"/></p:spPr></p:sp></p:spTree>`, 1))
}

func TestInsertFragment(t *testing.T) {
	cases := []struct {
		name string
		frag string
		ns   map[string]string
		want string
	}{
		{"prefix in scope", `<a:ln w="12700"><a:noFill/></a:ln>`, nil,
			`<p:spPr><a:ln w="12700"><a:noFill/></a:ln></p:spPr>`},
		{"hinted prefix renamed to in-scope prefix", `<d:ln d:x="1"><d:noFill/></d:ln>`, map[string]string{"d": nsA},
			`<p:spPr><a:ln a:x="1"><a:noFill/></a:ln></p:spPr>`},
		{"hinted prefix not in scope declared on new element", `<q:a><q:b/></q:a>`, map[string]string{"q": "urn:q"},
			`<p:spPr><q:a xmlns:q="urn:q"><q:b/></q:a></p:spPr>`},
		{"fragment declares its own namespace", `<z:a xmlns:z="urn:z"/>`, nil,
			`<p:spPr><z:a xmlns:z="urn:z"/></p:spPr>`},
		{"text and comment", `x<!--c--><a:b/>`, nil,
			`<p:spPr>x<!--c--><a:b/></p:spPr>`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			edit(t, slide, func(d *Document) error {
				_, err := mustSelect1(t, d, "//p:sp[2]/p:spPr").InsertFragment(0, []byte(c.frag), c.ns)
				return err
			}, strings.Replace(slide, `<p:spPr/></p:sp></p:spTree>`, c.want+`</p:sp></p:spTree>`, 1))
		})
	}
}

func TestInsertFragmentErrors(t *testing.T) {
	cases := []struct{ name, frag string }{
		{"malformed", `<a:ln><a:noFill></a:ln>`},
		{"undeclared prefix", `<zz:a/>`},
		{"undeclared attribute prefix", `<a:b zz:c="1"/>`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := mustParse(t, slide)
			if _, err := mustSelect1(t, d, "//p:sp[2]/p:spPr").InsertFragment(0, []byte(c.frag), nil); err == nil {
				t.Fatal("InsertFragment succeeded")
			}
			if d.Modified() {
				t.Fatal("failed insert modified the document")
			}
		})
	}
}

func TestDefaultNamespaceFragment(t *testing.T) {
	const ct = `<?xml version="1.0"?><Types xmlns="urn:ct"><Default Extension="xml" ContentType="application/xml"/></Types>`
	edit(t, ct, func(d *Document) error {
		_, err := d.Root().InsertFragment(1, []byte(`<Override PartName="/x.xml" ContentType="t"/>`), nil)
		return err
	}, strings.Replace(ct, `</Types>`, `<Override PartName="/x.xml" ContentType="t"/></Types>`, 1))
	// An element meant to be in no namespace under a default namespace gets
	// xmlns="".
	edit(t, ct, func(d *Document) error {
		_, err := d.Root().InsertFragment(1, []byte(`<x/>`), map[string]string{"": ""})
		return err
	}, strings.Replace(ct, `</Types>`, `<x xmlns=""/></Types>`, 1))
	// Unprefixed fragment for a namespace bound to a prefix in scope.
	edit(t, `<p:r xmlns:p="urn:p"/>`, func(d *Document) error {
		_, err := d.Root().InsertFragment(0, []byte(`<c><d/></c>`), map[string]string{"": "urn:p"})
		return err
	}, `<p:r xmlns:p="urn:p"><p:c><p:d/></p:c></p:r>`)
}

func TestRemoveAndReplace(t *testing.T) {
	edit(t, slide, func(d *Document) error {
		return mustSelect1(t, d, "//p:sp[2]").Remove()
	}, strings.Replace(slide, `<p:sp><p:nvSpPr><p:cNvPr id="3" name="Other"/></p:nvSpPr><p:spPr/></p:sp>`, ``, 1))
	edit(t, slide, func(d *Document) error {
		ext := mustSelect1(t, d, "//a:ext")
		_, err := ext.Parent().ReplaceWithFragment(ext, []byte(`<a:ext cx="1" cy="2"/>`), nil)
		return err
	}, strings.Replace(slide, `<a:ext cx="10" cy="20"></a:ext>`, `<a:ext cx="1" cy="2"/>`, 1))
}

func TestEmptyFormChanges(t *testing.T) {
	// Removing the only child keeps the pair form; adding a child to a
	// self-closing element turns it into a pair.
	edit(t, `<r><a><b/></a><c x="1" /></r>`, func(d *Document) error {
		r := d.Root()
		a, c := r.Elements()[0], r.Elements()[1]
		if err := a.Elements()[0].Remove(); err != nil {
			return err
		}
		_, err := c.InsertFragment(0, []byte(`<d/>`), nil)
		return err
	}, `<r><a></a><c x="1" ><d/></c></r>`)
}

func TestSetText(t *testing.T) {
	edit(t, `<r><t>old</t></r>`, func(d *Document) error {
		return d.Root().Elements()[0].SetText("a < b & c")
	}, `<r><t>a &lt; b &amp; c</t></r>`)
}

func TestFind(t *testing.T) {
	d := mustParse(t, slide)
	if n := len(d.Root().FindAll(nsP, "sp")); n != 2 {
		t.Fatalf("FindAll sp = %d", n)
	}
	if n := d.Root().Find(nsA, "off"); n == nil || n.Name() != "a:off" {
		t.Fatal("Find a:off failed")
	}
	if n := len(d.Root().FindQName("p:cNvPr")); n != 2 {
		t.Fatalf("FindQName = %d", n)
	}
	// Namespace matching ignores the prefix actually used.
	d2 := mustParse(t, `<x:sld xmlns:x="`+nsP+`"><x:sp/></x:sld>`)
	if n := len(d2.Root().FindAll(nsP, "sp")); n != 1 {
		t.Fatalf("FindAll with other prefix = %d", n)
	}
}

func TestSelect(t *testing.T) {
	d := mustParse(t, slide)
	cases := []struct {
		sel  string
		want int
	}{
		{"/p:sld/p:cSld/p:spTree/p:sp", 2},
		{"/p:sld/p:cSld/p:spTree/p:sp[2]", 1},
		{"//p:cNvPr[@id='3']", 1},
		{"//p:cNvPr[@name]", 2},
		{"p:sp/p:spPr", 2},
		{"//a:off[1]", 1},
		{"//*[@id]", 2},
		{"//p:sp[3]", 0},
	}
	for _, c := range cases {
		ns, err := d.Select(c.sel)
		if err != nil {
			t.Fatalf("%s: %v", c.sel, err)
		}
		if len(ns) != c.want {
			t.Errorf("%s: %d matches, want %d", c.sel, len(ns), c.want)
		}
	}
	for _, bad := range []string{"", "//p:sp[0]", "//p:sp[", "/1x"} {
		if _, err := d.Select(bad); err == nil {
			t.Errorf("Select(%q) succeeded", bad)
		}
	}
}

func TestLookup(t *testing.T) {
	d := mustParse(t, `<r xmlns="urn:d" xmlns:a="urn:a"><b:x xmlns:b="urn:a" xmlns:a="urn:other"><y xmlns=""/></b:x></r>`)
	x := d.Root().Elements()[0]
	y := x.Elements()[0]
	if p, ok := x.LookupNamespace("urn:a"); !ok || p != "b" {
		t.Errorf("LookupNamespace(urn:a) = %q %v (a is shadowed)", p, ok)
	}
	if u, ok := y.LookupPrefix(""); ok {
		t.Errorf("default namespace undeclared, got %q", u)
	}
	if x.Space() != "urn:a" || d.Root().Space() != "urn:d" || y.Space() != "" {
		t.Error("Space resolution wrong")
	}
}

func TestData(t *testing.T) {
	d := mustParse(t, "<r>a&amp;b\r\nc<x/><![CDATA[<d>]]></r>")
	kids := d.Root().Children()
	if got := kids[0].Data(); got != "a&b\nc" {
		t.Errorf("text Data = %q, want %q", got, "a&b\nc")
	}
	if got := kids[1].Data(); got != "" {
		t.Errorf("element Data = %q, want empty", got)
	}
	if got := kids[2].Data(); got != "<d>" {
		t.Errorf("CDATA Data = %q, want %q", got, "<d>")
	}
}

func TestRename(t *testing.T) {
	d, err := Parse([]byte(`<w:r xmlns:w="u"><w:t xml:space="preserve"> a </w:t><w:t/></w:r>`))
	if err != nil {
		t.Fatal(err)
	}
	ts := d.Root().Elements()
	for _, e := range ts {
		if err := e.Rename("delText"); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := string(d.Bytes()), `<w:r xmlns:w="u"><w:delText xml:space="preserve"> a </w:delText><w:delText/></w:r>`; got != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
	if ts[0].Space() != "u" || ts[0].Local() != "delText" {
		t.Fatalf("renamed element is %s in %q", ts[0].Name(), ts[0].Space())
	}
	if err := ts[0].Rename("a:b"); err == nil {
		t.Fatal("renamed to a qualified local name")
	}
	if err := d.Root().Children()[0].Rename("x"); err != nil {
		t.Fatal(err) // an element
	}
}

func TestStartTagWithout(t *testing.T) {
	src := `<w:p xmlns:w="u" xmlns:x="v" a='1'  x:id="bad" b="2" ><w:r/></w:p>`
	d, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	p := d.Root()
	none := func(string) bool { return false }
	if got := string(p.StartTagWithout(none)); got != `<w:p xmlns:w="u" xmlns:x="v" a='1'  x:id="bad" b="2" >` {
		t.Fatalf("nothing skipped: %s", got)
	}
	skipID := func(q string) bool { return q == "x:id" }
	before := string(p.StartTagWithout(skipID))
	if err := p.SetAttr("x:id", "good"); err != nil {
		t.Fatal(err)
	}
	if after := string(p.StartTagWithout(skipID)); after != before || before != `<w:p xmlns:w="u" xmlns:x="v" a='1' b="2" >` {
		t.Fatalf("before %s after %s", before, after)
	}
	if err := p.SetAttr("c", "3"); err != nil {
		t.Fatal(err)
	}
	if after := string(p.StartTagWithout(skipID)); after == before {
		t.Fatal("an attribute not skipped changed but the tags compare equal")
	}
}

func TestNewTextEmpty(t *testing.T) {
	n := NewText("")
	if got := n.Data(); got != "" {
		t.Fatalf("Data = %q", got)
	}
	if got := n.Raw(); got == nil || len(got) != 0 {
		t.Fatalf("Raw = %#v, want empty", got)
	}
}
