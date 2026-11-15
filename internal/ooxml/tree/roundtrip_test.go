package tree

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
)

// roundTripCases are inputs that must survive parse and serialise byte for
// byte, both untouched and with every start tag rebuilt from its parts.
var roundTripCases = []struct {
	name string
	in   string
}{
	{"minimal", `<a/>`},
	{"decl standalone", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\r\n" + `<p:sld xmlns:p="urn:p"/>`},
	{"decl single quotes", `<?xml version='1.0' encoding='utf-8'?><r/>`},
	{"bom", "\xEF\xBB\xBF<?xml version=\"1.0\"?>\n<r/>"},
	{"bom no decl", "\xEF\xBB\xBF<r/>"},
	{"crlf everywhere", "<?xml version=\"1.0\"?>\r\n<r\r\n  a=\"1\"\r\n>\r\n  <c>x\r\ny</c>\r\n</r>\r\n"},
	{"lone cr", "<r>a\rb</r>"},
	{"prefixes", `<p:sld xmlns:a="urn:a" xmlns:p="urn:p" xmlns:r="urn:r"><p:cSld><a:off x="1" y="2"/><p:pic r:embed="rId2"/></p:cSld></p:sld>`},
	{"default ns redeclared", `<r xmlns="urn:1"><a xmlns="urn:2"><b xmlns=""><c/></b></a></r>`},
	{"prefix redeclared", `<x:r xmlns:x="urn:1"><x:a xmlns:x="urn:2"><x:b/></x:a></x:r>`},
	{"mixed quotes", `<r a='1' b="2" c='say "hi"' d="it's"/>`},
	{"entity refs", `<r a="&amp;&lt;&gt;&quot;&apos;">x &amp; y &lt; z &#x41;&#66;&#x1F600;</r>`},
	{"char ref whitespace in attr", `<r a="line&#10;break&#9;tab&#13;"/>`},
	{"literal whitespace in attr", "<r a=\"x\ny\tz\"/>"},
	{"comments and pis", "<?xml version=\"1.0\"?>\n<!-- before -->\n<?pi data?>\n<r><!--in--><?x y z?></r>\n<!-- after -->\n"},
	{"cdata", `<r><![CDATA[<not> & markup]]]></r>`},
	{"empty pair vs self-closing", `<r><a></a><b/><c /><d	/></r>`},
	{"unusual whitespace in tags", "<r\n\ta = \"1\"\n\tb\t=\t'2'\n   ><c\t\n/></r\n\t>"},
	{"end tag whitespace", `<r><a></a  ></r >`},
	{"doctype", `<!DOCTYPE r [ <!ELEMENT r ANY> <!-- ] > --> ]><r/>`},
	{"unicode names", `<ü:élan xmlns:ü="urn:u" ü:ñ="ß">日本語</ü:élan>`},
	{"trailing whitespace", "<r/>\n\n\t "},
	{"mc alternate content", `<p:sp xmlns:p="urn:p" xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006" xmlns:a14="urn:a14" mc:Ignorable="a14"><mc:AlternateContent><mc:Choice Requires="a14"><a14:x/></mc:Choice><mc:Fallback><p:y/></mc:Fallback></mc:AlternateContent></p:sp>`},
	{"long attribute", `<r v="` + strings.Repeat("abc&amp;", 200000) + `"/>`},
	{"many attributes", `<r` + manyAttrs(500) + `/>`},
	{"deep nesting", strings.Repeat("<a>", MaxDepth) + strings.Repeat("</a>", MaxDepth)},
	{"xml prefix", `<r xml:space="preserve" xml:lang="en"> x </r>`},
	{"gt in attr and text", `<r a="a>b">c>d</r>`},
}

func manyAttrs(n int) string {
	var b strings.Builder
	for i := range n {
		b.WriteString(" a")
		b.WriteString(strings.Repeat("x", i%7))
		b.WriteString(string(rune('a' + i%26)))
		b.WriteString(strconv.Itoa(i))
		b.WriteString(`="v"`)
	}
	return b.String()
}

func TestRoundTrip(t *testing.T) {
	for _, c := range roundTripCases {
		t.Run(c.name, func(t *testing.T) {
			doc, err := Parse([]byte(c.in))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got := doc.Bytes(); string(got) != c.in {
				t.Fatalf("untouched round trip differs:\n got %q\nwant %q", trunc(got), trunc([]byte(c.in)))
			}
			rebuildAll(doc)
			if got := doc.Bytes(); string(got) != c.in {
				t.Fatalf("rebuilt round trip differs:\n got %q\nwant %q", trunc(got), trunc([]byte(c.in)))
			}
		})
	}
}

func trunc(b []byte) []byte {
	if len(b) > 300 {
		return append(b[:300:300], "..."...)
	}
	return b
}

func TestMalformed(t *testing.T) {
	cases := []struct{ name, in string }{
		{"empty", ``},
		{"no root", `<?xml version="1.0"?>`},
		{"two roots", `<a/><b/>`},
		{"text outside root", `x<a/>`},
		{"unclosed", `<a><b></a>`},
		{"mismatched", `<a></b>`},
		{"stray end", `<a/></a>`},
		{"dup attr", `<a x="1" x="2"/>`},
		{"dup expanded attr", `<a xmlns:p="u" xmlns:q="u" p:x="1" q:x="2"/>`},
		{"unquoted", `<a x=1/>`},
		{"no space between attrs", `<a x="1"y="2"/>`},
		{"lt in attr", `<a x="<"/>`},
		{"bad entity", `<a>&nbsp;</a>`},
		{"bare amp", `<a>a & b</a>`},
		{"bad char ref", `<a>&#0;</a>`},
		{"bad hex ref", `<a>&#xZZ;</a>`},
		{"undeclared prefix", `<p:a/>`},
		{"undeclared attr prefix", `<a r:id="x"/>`},
		{"decl not first", ` <?xml version="1.0"?><a/>`},
		{"decl bad encoding", `<?xml version="1.0" encoding="UTF-16"?><a/>`},
		{"cdata end in text", `<a>]]></a>`},
		{"double dash comment", `<a><!-- a -- b --></a>`},
		{"control char", "<a>\x01</a>"},
		{"invalid utf8", "<a>\xff</a>"},
		{"bad name", `<1a/>`},
		{"two colons", `<a:b:c xmlns:a="u"/>`},
		{"xmlns prefix bound", `<a xmlns:xmlns="u"/>`},
		{"empty prefix binding", `<a xmlns:p=""/>`},
		{"cdata outside root", `<![CDATA[x]]><a/>`},
		{"unterminated tag", `<a x="1"`},
		{"unterminated comment", `<a><!-- x</a>`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Parse([]byte(c.in)); err == nil {
				t.Fatalf("Parse(%q) succeeded, want error", c.in)
			}
		})
	}
}

func TestSpans(t *testing.T) {
	in := `<r><a x="1">t</a><!--c--></r>`
	doc, err := Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	a := doc.Root().Elements()[0]
	s, e, ok := a.Span()
	if !ok || in[s:e] != `<a x="1">t</a>` {
		t.Fatalf("span = %q", in[s:e])
	}
	c := doc.Root().Children()[1]
	if c.Kind() != CommentNode || string(c.Raw()) != "<!--c-->" {
		t.Fatalf("comment = %v %q", c.Kind(), c.Raw())
	}
}

func TestDecode(t *testing.T) {
	doc, err := Parse([]byte("<r a=\"x&#10;y\nz\t&amp;\" b='&apos;'>p\r\nq &lt;&#x41;<![CDATA[&amp;]]></r>"))
	if err != nil {
		t.Fatal(err)
	}
	r := doc.Root()
	if v, _ := r.Attr("a"); v != "x\ny z &" {
		t.Errorf("a = %q", v)
	}
	if v, _ := r.Attr("b"); v != "'" {
		t.Errorf("b = %q", v)
	}
	if v := r.Text(); v != "p\nq <A&amp;" {
		t.Errorf("text = %q", v)
	}
}

func TestCheck(t *testing.T) {
	if err := Check([]byte(`<a/>`)); err != nil {
		t.Fatal(err)
	}
	err := Check([]byte("<a>\n<b></a>"))
	se, ok := err.(*SyntaxError)
	if !ok || se.Line != 2 {
		t.Fatalf("err = %v", err)
	}
	if !bytes.Contains([]byte(err.Error()), []byte("line 2")) {
		t.Fatalf("error text %q lacks line", err)
	}
}
