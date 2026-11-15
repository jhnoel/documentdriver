package integrity

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// parseCases are inputs and docxdriver 0.5.2's verdict on each: "ok" or
// the error category of its parser (integrity::tree::parse), recorded by
// running that function on them. Inputs escape bytes as \xHH, \n, \r
// and \t.
var parseCases = []struct{ in, want string }{
	{`<a/>`, "ok"},
	{`<!DOCTYPE a><a/>`, "contains a DTD"},
	{`<a>&#1;</a>`, "character U+0001 is not allowed in XML"},
	{`<a>\x01</a>`, "character U+0001 is not allowed in XML"},
	{`<p:a/>`, "undeclared namespace prefix p"},
	{`<a b='1' b='2'/>`, "not well-formed: position "},
	{`<a xmlns:x='u' xmlns:y='u' x:b='1' y:b='2'/>`, "not well-formed: duplicate attribute"},
	{`<a><b></a>`, "not well-formed: ill-formed document"},
	{`<a/><b/>`, "not well-formed: more than one root element"},
	{`<a>&nbsp;</a>`, "not well-formed: undefined entity"},
	{`<?xml version='1.0' encoding='windows-1252'?><a/>`, "declares encoding windows-1252, not UTF-8 or UTF-16"},
	{`<a></a></b>`, "not well-formed: ill-formed document"},
	{`<a>`, "not well-formed: unclosed element"},
	{`<a><!-- x -- y --></a>`, "not well-formed: ill-formed document"},
	{`<a><!-- x </a>`, "not well-formed: syntax error"},
	{`<a><?pi x</a>`, "not well-formed: syntax error"},
	{`<a><![CDATA[x</a>`, "not well-formed: syntax error"},
	{`<a b='1</a>`, "not well-formed: syntax error"},
	{`<a b="1</a>`, "not well-formed: syntax error"},
	{`<a b=1/>`, "not well-formed: position "},
	{`<a b/>`, "not well-formed: position "},
	{`<a b='1'c='2'/>`, "ok"},
	{`<a b = '1'/>`, "ok"},
	{`<a>x & y</a>`, "not well-formed: ill-formed document"},
	{`<a>x &amp y</a>`, "not well-formed: ill-formed document"},
	{`<a>&#xZZ;</a>`, "not well-formed: invalid character reference"},
	{`<a>&#x110000;</a>`, "not well-formed: invalid character reference"},
	{`<a>&#xD800;</a>`, "not well-formed: invalid character reference"},
	{`<a>&#0;</a>`, "not well-formed: invalid character reference"},
	{`<a>&#65;</a>`, "ok"},
	{`<a>&</a>`, "not well-formed: ill-formed document"},
	{`<a>]]></a>`, "not well-formed: ']]>' in text"},
	{`<a b='&#1;'/>`, "character U+0001 is not allowed in XML"},
	{`<a b='&nbsp;'/>`, "not well-formed: invalid reference in attribute value"},
	{`<a b='<'/>`, "ok"},
	{`<a xmlns:p=''/>`, "namespace prefix bound to the empty name"},
	{`<a xmlns:xmlns='u'/>`, "reserved namespace prefix redeclared"},
	{`<a xmlns:xml='u'/>`, "reserved namespace prefix redeclared"},
	{`<a xmlns:xml='http://www.w3.org/XML/1998/namespace'/>`, "ok"},
	{`<a xmlns:p='u' xmlns:p='v'/>`, "not well-formed: position "},
	{`<a p:b='1'/>`, "undeclared namespace prefix p"},
	{`<a xmlns:x='http://www.w3.org/2000/xmlns/' x:b='1'/>`, "reserved namespace used for an attribute"},
	{`<1a/>`, "not well-formed: invalid element name"},
	{`<a 1b='x'/>`, "not well-formed: invalid attribute name"},
	{`<a:b:c xmlns:a='u'/>`, "not well-formed: invalid element name"},
	{`<a>text</a>trailing`, "not well-formed: text outside the root element"},
	{`  <a/>  `, "ok"},
	{`\xef\xbb\xbf<a/>`, "ok"},
	{`<a>\xef\xbb\xbf</a>`, "ok"},
	{`\xff\xfe<\x00a\x00/\x00>\x00`, "ok"},
	{`<\x00a\x00/\x00>\x00`, "UTF-16 without a byte order mark"},
	{`\xc3\x28`, "invalid UTF-8"},
	{`<a>\xc3\x28</a>`, "invalid UTF-8"},
	{`<?xml version='1.0' encoding='UTF-16'?><a/>`, "declares encoding utf-16 but is utf-8"},
	{`<?xml version='1.0' encoding='utf8'?><a/>`, "ok"},
	{`<?xml version='2.0'?><a/>`, "ok"},
	{`<?xml encoding='UTF-8'?><a/>`, "ok"},
	{`<a/><?xml version='1.0'?>`, "ok"},
	{` <?xml version='1.0'?><a/>`, "ok"},
	{`<!-- c --><a/><!-- d -->`, "ok"},
	{`<?pi?><a/>`, "ok"},
	{`<a></b>`, "not well-formed: ill-formed document"},
	{`</a>`, "not well-formed: ill-formed document"},
	{`<a><b/></a></a>`, "not well-formed: ill-formed document"},
	{`<a b='1' c='2' b='3'/>`, "not well-formed: position "},
	{`<a xmlns='u' xmlns='v'/>`, "not well-formed: position "},
	{`<a></a >`, "ok"},
	{`<a ></a>`, "ok"},
	{`<a/ >`, "not well-formed: invalid element name"},
	{`< a/>`, "not well-formed: position "},
	{`<a>&#x41;&#65;&lt;&gt;&amp;&apos;&quot;</a>`, "ok"},
	{`<a>&#x;</a>`, "not well-formed: invalid character reference"},
	{`<a>&#;</a>`, "not well-formed: invalid character reference"},
	{`<a>&;</a>`, "not well-formed: undefined entity"},
	{`<a>&x y;</a>`, "not well-formed: undefined entity"},
	{`<!DOCTYPE a [<!ENTITY e 'x'>]><a>&e;</a>`, "contains a DTD"},
	{`<a><!DOCTYPE b></a>`, "contains a DTD"},
	{`<a><![CDATA[x]]></a>`, "ok"},
	{`<a><![CDATA[]]]]></a>`, "ok"},
	{`<a b="'"/>`, "ok"},
	{`<a b='\t\n\r'/>`, "ok"},
	{`<a>\r\n</a>`, "ok"},
	{`<a>a\rb</a>`, "ok"},
	{`x`, "not well-formed: text outside the root element"},
	{`<?xml version='1.0'?>`, "not well-formed: no root element"},
	{`<a>&#+65;</a>`, "not well-formed: invalid character reference"},
	{`<a b='&#+65;'/>`, "not well-formed: invalid reference in attribute value"},
	{`<a>&#X41;</a>`, "not well-formed: invalid character reference"},
	{`<a b='x & y'/>`, "not well-formed: invalid reference in attribute value"},
	{`<a b='&#0;'/>`, "not well-formed: invalid reference in attribute value"},
	{`<?xml version='1.0'?><?xml version='1.0'?><a/>`, "ok"},
	{`<a><?xml version='1.0'?></a>`, "ok"},
	{`<?1pi x?><a/>`, "ok"},
	{`<?pi?><a/>`, "ok"},
	{`<a xmlns:p='http://www.w3.org/XML/1998/namespace'/>`, "ok"},
	{`<a xmlns:p='http://www.w3.org/2000/xmlns/'/>`, "ok"},
	{`<a><![CDATA[x]]></a><![CDATA[y]]>`, "not well-formed: text outside the root element"},
	{`<a>x</a><![CDATA[ ]]>`, "ok"},
	{`<!DOCTYPE a><a>&nbsp;</a>`, "contains a DTD"},
	{`<a></a><!DOCTYPE a>`, "contains a DTD"},
	{`<a b='1' xmlns:b='u' b:c='2'/>`, "ok"},
	{`<a xmlns='' />`, "ok"},
	{`<p:a xmlns:p='u'><b xmlns:p=''/></p:a>`, "namespace prefix bound to the empty name"},
	{`<a>&lt</a>`, "not well-formed: ill-formed document"},
	{`<a b="1"c="2" d='3'e='4'/>`, "ok"},
	{`<a b='<<'/>`, "ok"},
	{`<a b='&lt;<'/>`, "ok"},
	{`<a>\xef\xbf\xbe</a>`, "character U+FFFE is not allowed in XML"},
	{`<a>\xed\xa0\x80</a>`, "invalid UTF-8"},
	{`\xfe\xff\x00<\x00a\x00/\x00>`, "ok"},
	{`\xff\xfe<\x00a\x00>\x00\x00\xd8<\x00/\x00a\x00>\x00`, "invalid UTF-16"},
	{`\xff\xfe<\x00a\x00/\x00>`, "invalid UTF-16"},
	{`<a>\xef\xbb\xbf\xef\xbb\xbf</a>`, "ok"},
	{`\xef\xbb\xbf\xef\xbb\xbf<a/>`, "ok"},
	{`<a>&#x10FFFF;</a>`, "ok"},
	{`<a>&#xFFFE;</a>`, "character U+FFFE is not allowed in XML"},
	{`<a></a>\x00`, "character U+0000 is not allowed in XML"},
	{`<a:b xmlns:a='u'></a:c>`, "not well-formed: ill-formed document"},
	{`<a/ >x`, "not well-formed: invalid element name"},
}

// unescapeCase decodes the escapes of a parseCases input.
func unescapeCase(t *testing.T, s string) []byte {
	t.Helper()
	var out []byte
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				out, i = append(out, '\n'), i+1
				continue
			case 'r':
				out, i = append(out, '\r'), i+1
				continue
			case 't':
				out, i = append(out, '\t'), i+1
				continue
			case 'x':
				b, err := strconv.ParseUint(s[i+2:i+4], 16, 8)
				if err != nil {
					t.Fatal(err)
				}
				out, i = append(out, byte(b)), i+3
				continue
			}
		}
		out = append(out, s[i])
	}
	return out
}

// parseFixes are the parseCases where docxdriver's verdict is wrong, with
// the verdict reported instead. The first group are inputs XML 1.0 (or
// Namespaces in XML) forbids and quick-xml accepted; the second are faults
// it named by the "position N:" prefix of its message.
var parseFixes = map[string]string{
	`<a b='1'c='2'/>`:                                      "not well-formed: missing white space between attributes",
	`<a b="1"c="2" d='3'e='4'/>`:                           "not well-formed: missing white space between attributes",
	`<a b='<'/>`:                                           "not well-formed: < in an attribute value",
	`<a b='<<'/>`:                                          "not well-formed: < in an attribute value",
	`<a b='&lt;<'/>`:                                       "not well-formed: < in an attribute value",
	`<?xml version='2.0'?><a/>`:                            "not well-formed: malformed XML declaration",
	`<?xml encoding='UTF-8'?><a/>`:                         "not well-formed: malformed XML declaration",
	`<a/><?xml version='1.0'?>`:                            "not well-formed: XML declaration not at the start",
	` <?xml version='1.0'?><a/>`:                           "not well-formed: XML declaration not at the start",
	`<?xml version='1.0'?><?xml version='1.0'?><a/>`:       "not well-formed: XML declaration not at the start",
	`<a><?xml version='1.0'?></a>`:                         "not well-formed: XML declaration not at the start",
	`<?1pi x?><a/>`:                                        "not well-formed: invalid processing instruction target",
	`<a xmlns:p='http://www.w3.org/XML/1998/namespace'/>`:  "reserved namespace bound to a prefix",
	`<a xmlns:p='http://www.w3.org/2000/xmlns/'/>`:         "reserved namespace bound to a prefix",
	`<a xmlns:x='http://www.w3.org/2000/xmlns/' x:b='1'/>`: "reserved namespace bound to a prefix",
	`<a>x</a><![CDATA[ ]]>`:                                "not well-formed: CDATA section outside the root element",
	`<a><![CDATA[x]]></a><![CDATA[y]]>`:                    "not well-formed: CDATA section outside the root element",

	`<a b='1' b='2'/>`:             "not well-formed: duplicate attribute",
	`<a b='1' c='2' b='3'/>`:       "not well-formed: duplicate attribute",
	`<a xmlns:p='u' xmlns:p='v'/>`: "not well-formed: duplicate attribute",
	`<a xmlns='u' xmlns='v'/>`:     "not well-formed: duplicate attribute",
	`<a b=1/>`:                     "not well-formed: malformed attribute",
	`<a b/>`:                       "not well-formed: malformed attribute",
	`< a/>`:                        "not well-formed: invalid element name",
}

func TestParseMatchesDocxdriver(t *testing.T) {
	inputs := map[string]bool{}
	for _, c := range parseCases {
		inputs[c.in] = true
		want, fixed := parseFixes[c.in]
		if !fixed {
			want = c.want
		} else if want == c.want {
			t.Errorf("%s: listed as a fix but docxdriver agrees", c.in)
		}
		_, got := parseXML(unescapeCase(t, c.in))
		if got == "" {
			got = "ok"
		}
		if got != want {
			t.Errorf("%s: got %q, want %q (docxdriver %q)", c.in, got, want, c.want)
		}
	}
	for in := range parseFixes {
		if !inputs[in] {
			t.Errorf("fix for %q, which is not a case", in)
		}
	}
}

// Ported from the tests of docxdriver 0.5.2's integrity/tree.rs.

func TestRejectsWhatWordRejects(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"<a/>", ""},
		{"<!DOCTYPE a><a/>", "DTD"},
		{"<a>&#1;</a>", "U+0001"},
		{"<a>\x01</a>", "U+0001"},
		{"<p:a/>", "undeclared"},
		{"<a b='1' b='2'/>", "not well-formed"},
		{"<a xmlns:x='u' xmlns:y='u' x:b='1' y:b='2'/>", "not well-formed"},
		{"<a><b></a>", "not well-formed"},
		{"<a/><b/>", "not well-formed"},
		{"<a>&nbsp;</a>", "not well-formed"},
		{"<?xml version='1.0' encoding='windows-1252'?><a/>", "windows-1252"},
	} {
		_, got := parseXML([]byte(c.in))
		if c.want == "" && got != "" || !strings.Contains(got, c.want) {
			t.Errorf("%q: %q, want it to contain %q", c.in, got, c.want)
		}
	}
	utf16 := []byte{0xFF, 0xFE}
	for _, u := range utf16Units("<?xml version='1.0' encoding='UTF-8'?><a/>") {
		utf16 = append(utf16, byte(u), byte(u>>8))
	}
	if _, got := parseXML(utf16); !strings.Contains(got, "is utf-16") {
		t.Errorf("UTF-16 declared UTF-8: %q", got)
	}
}

func utf16Units(s string) []uint16 { return utf16.Encode([]rune(s)) }

func mustParse(t *testing.T, s string) *document {
	t.Helper()
	d, msg := parseXML([]byte(s))
	if msg != "" {
		t.Fatalf("%s: %s", s, msg)
	}
	return d
}

// TestDecodedContent checks what the resolved view holds: references
// decoded, line ends normalised in text and attribute values, CDATA merged
// into text.
func TestDecodedContent(t *testing.T) {
	d := mustParse(t, "<a b='x\r\ny&#10;z'>1&lt;\r\n2<![CDATA[<3\r]]>&#13;</a>")
	if got := d.root.attrOr("", "b"); got != "x y\nz" {
		t.Errorf("attribute %q", got)
	}
	if got := d.root.text(); got != "1<\n2<3\n\r" {
		t.Errorf("text %q", got)
	}
	if !d.root.hasText() {
		t.Error("the root has no text of its own")
	}
}

// dumpView writes a view in document order: names, attributes,
// declarations, text runs and element offsets.
func dumpView(d *document) string {
	var b strings.Builder
	for e := range d.elements() {
		fmt.Fprintf(&b, "%d %q %q %q parent=%d first=%d next=%d end=%d", e.index, e.ns, e.local, e.prefix, e.parent, e.first, e.next, e.end)
		for _, a := range e.attrs() {
			fmt.Fprintf(&b, " @%q:%q:%q=%q", a.ns, a.prefix, a.local, a.value)
		}
		if e.decls {
			fmt.Fprintf(&b, " decls=%q", d.decls[e.index])
		}
		fmt.Fprintf(&b, " text=%q own=%v", e.text(), e.hasText())
		if d.starts != nil {
			fmt.Fprintf(&b, " start=%d", d.starts[e.index])
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// TestScanViewMatchesTree checks that the view built as a part is read
// (tree.Scan) is the one built from its parsed tree.
func TestScanViewMatchesTree(t *testing.T) {
	for _, in := range []string{
		"<a b='x\r\ny&#10;z'>1&lt;\r\n2<![CDATA[<3\r]]>&#13;</a>",
		`<?xml version="1.0"?><!--c--><w:document xmlns:w="urn:w" xmlns="urn:d"><w:body><w:p w:x="1"> a <!--k--> b <w:r><?pi x?><w:t xml:space="preserve"> t </w:t></w:r> tail</w:p><x xmlns=""/><y/></w:body></w:document><!--after-->`,
		`<r xmlns:a="urn:a"><a:x a:y="1" z="2"><b xmlns:a="urn:b" a:q="3"/></a:x></r>`,
	} {
		d, err := tree.Parse([]byte(in))
		if err != nil {
			t.Fatal(err)
		}
		want, msg := build(d)
		if msg != "" {
			t.Fatalf("%s: %s", in, msg)
		}
		got := mustParse(t, in)
		if a, b := dumpView(got), dumpView(want); a != b {
			t.Errorf("%s: scanned view\n%s\nwant\n%s", in, a, b)
		}
	}
}

// TestIsNCNameRefusesAColon checks that isNCName, over the shared
// xmlscan name tables, admits an NCName and refuses a colon at its
// start, middle, or end (a colon names a QName, never an NCName).
func TestIsNCNameRefusesAColon(t *testing.T) {
	for _, s := range []string{"a", "_a-1.0", "éx"} {
		if !isNCName(s) {
			t.Errorf("isNCName(%q) = false, want true", s)
		}
	}
	for _, s := range []string{":", ":a", "a:", "a:b", "a:b:c"} {
		if isNCName(s) {
			t.Errorf("isNCName(%q) = true, want false", s)
		}
	}
}

// parseXML is parsePart of a part's bytes.
func parseXML(b []byte) (*document, string) { return parsePart(string(b)) }

// parseXMLTree is parsePartTree of a part's bytes.
func parseXMLTree(b []byte) (*document, string) { return parsePartTree(string(b)) }
