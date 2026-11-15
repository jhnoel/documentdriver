package integrity

import (
	"slices"
	"testing"
)

// Ported from the tests of docxdriver 0.5.2's integrity/schema.rs.

func schemaFindings(t *testing.T, xml string) []string {
	t.Helper()
	d, msg := parseXML([]byte(xml))
	if msg != "" {
		t.Fatalf("parse: %s", msg)
	}
	var f Findings
	checkSchema(d, "word/document.xml", newContentModel(), &f)
	return f.Messages()
}

const (
	schemaOpen  = `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006" xmlns:w14="http://schemas.microsoft.com/office/word/2010/wordml" mc:Ignorable="w14"><w:body>`
	schemaClose = `</w:body></w:document>`
)

func schemaBody(t *testing.T, inner string) []string {
	t.Helper()
	return schemaFindings(t, schemaOpen+inner+schemaClose)
}

// TestModelCoversEveryRoot checks the tables give the content model of
// every root element a WordprocessingML part can have.
func TestModelCoversEveryRoot(t *testing.T) {
	for _, root := range []string{"comments", "document", "endnotes", "fonts", "footnotes", "ftr", "glossaryDocument",
		"hdr", "numbering", "recipients", "settings", "styles", "txbxContent", "webSettings"} {
		kind, ok := rootType(wNS, root)
		if !ok {
			t.Errorf("no root type for w:%s", root)
			continue
		}
		if _, ok := newContentModel().typeModel(kind); !ok {
			t.Errorf("no model for %s", kind)
		}
	}
}

func TestValidParagraphsPass(t *testing.T) {
	got := schemaBody(t, `<w:p w14:paraId="1A2B3C4D"><w:pPr><w:pStyle w:val="H"/><w:jc w:val="center"/></w:pPr><w:r><w:rPr><w:i/><w:b/></w:rPr><w:t xml:space="preserve"> a </w:t></w:r></w:p><w:sectPr/>`)
	if len(got) != 0 {
		t.Fatalf("%q", got)
	}
}

func TestReportsOrderCountPlacementAndValues(t *testing.T) {
	for _, c := range []struct {
		body string
		want []string
	}{
		{`<w:p><w:pPr><w:jc w:val="center"/><w:pStyle w:val="H"/></w:pPr></w:p>`, []string{"schema: w:pStyle is out of order in w:pPr"}},
		{`<w:p><w:pPr><w:jc w:val="middle"/></w:pPr></w:p>`, []string{"schema: w:jc/@w:val is not a valid ST_Jc"}},
		{`<w:p><w:pPr><w:jc/></w:pPr></w:p>`, []string{"schema: w:jc lacks required attribute w:val"}},
		{`<w:p><w:t>x</w:t></w:p>`, []string{"schema: w:t is not allowed in w:p"}},
		{`<w:p><w:r><w:t>a</w:t><w:rPr/></w:r></w:p>`, []string{"schema: w:rPr is out of order in w:r"}},
		{`<w:sectPr/><w:p/>`, []string{"schema: w:p is out of order in w:body"}},
		{`<w:p><w:pPr><w:jc w:val="left"/><w:jc w:val="left"/></w:pPr></w:p>`, []string{"schema: w:jc occurs too often in w:pPr"}},
		{`<w:tbl><w:tr><w:tc><w:p/></w:tc></w:tr></w:tbl>`, []string{"schema: w:tbl lacks required w:tblGrid", "schema: w:tbl lacks required w:tblPr"}},
		{`<w:p><w:ins w:id="1"><w:r/></w:ins></w:p>`, []string{"schema: w:ins lacks required attribute w:author"}},
		{`<w:p><w:ins w:id="1" w:author="a" w:date="yesterday"><w:r/></w:ins></w:p>`, []string{"schema: w:ins/@w:date is not a valid ST_DateTime"}},
		{`<w:p><w:ins w:id="x" w:author="a" w:date="2024-01-02T03:04:05Z"><w:r/></w:ins></w:p>`, []string{"schema: w:ins/@w:id is not a valid ST_DecimalNumber"}},
		{`<w:p><w:pPr><w:bogus/></w:pPr></w:p>`, []string{"schema: w:bogus is not allowed in w:pPr"}},
		{`<w:p w:bogus="1"/>`, []string{"schema: w:p has unexpected attribute w:bogus"}},
		{`<w:p>stray</w:p>`, []string{"schema: text is not allowed directly in w:p"}},
	} {
		if got := schemaBody(t, c.body); !slices.Equal(got, c.want) {
			t.Errorf("%s: %q, want %q", c.body, got, c.want)
		}
	}
}

func TestExtensionContentAndFallbacks(t *testing.T) {
	// w14 content is ignorable; AlternateContent is read as its Fallback.
	if got := schemaBody(t, `<w:p><w:pPr><w14:foo/></w:pPr><w:r><mc:AlternateContent><mc:Choice Requires="w14"><w14:x/></mc:Choice><mc:Fallback><w:t>a</w:t></mc:Fallback></mc:AlternateContent></w:r></w:p>`); len(got) != 0 {
		t.Errorf("%q", got)
	}
	want := []string{"schema: w:t is not allowed in w:p"}
	if got := schemaBody(t, `<w:p><mc:AlternateContent><mc:Choice Requires="w14"/><mc:Fallback><w:t>a</w:t></mc:Fallback></mc:AlternateContent></w:p>`); !slices.Equal(got, want) {
		t.Errorf("%q, want %q", got, want)
	}
}

func TestSimpleTypes(t *testing.T) {
	m := newContentModel()
	for _, c := range []struct {
		typ, v string
		ok     bool
	}{
		{"s:ST_OnOff", "true", true}, {"s:ST_OnOff", "on", true}, {"s:ST_OnOff", "yes", false},
		{"s:ST_TwipsMeasure", "1440", true}, {"s:ST_TwipsMeasure", "1in", true}, {"s:ST_TwipsMeasure", "-1", false},
		{"w:ST_LongHexNumber", "00A1B2C3", true}, {"w:ST_LongHexNumber", "A1B2", false},
		{"s:ST_HexColorRGB", "FF00AA", true}, {"s:ST_HexColorRGB", "red", false},
	} {
		if got := m.valid(c.typ, c.v, 0); got != c.ok {
			t.Errorf("valid(%s, %q) = %v", c.typ, c.v, got)
		}
	}
	for _, c := range []struct {
		v  string
		ok bool
	}{{"2026-10-19T12:00:00Z", true}, {"2026-10-19T12:00:00", true}, {"2026-13-01T00:00:00Z", false},
		{"2024-02-29T00:00:00", true}, {"2023-02-29T00:00:00", false}, {"2024-04-31T00:00:00", false}, {"2000-02-29T24:00:00", true}, {"1900-02-29T00:00:00", false}} {
		if got := isDateTime(c.v); got != c.ok {
			t.Errorf("isDateTime(%q) = %v", c.v, got)
		}
	}
}

// TestBuiltins pins the lexical rules docxdriver applied to XSD
// built-ins, where Go's parsers differ from Rust's.
func TestBuiltins(t *testing.T) {
	for _, c := range []struct {
		typ, v string
		ok     bool
	}{
		{"integer", "+5", true}, {"integer", "-5", true}, {"integer", "+-5", false}, {"integer", "", false},
		{"integer", "123456789012345678901234567890123456789012345", true},
		{"nonNegativeInteger", "123456789012345678901234567890123456789012345", false},
		{"long", "9223372036854775808", false}, {"unsignedLong", "18446744073709551615", true},
		{"decimal", "-.5", true}, {"decimal", ".", false}, {"decimal", "1.2.3", false},
		{"double", "1e400", true}, {"double", "INF", true}, {"double", "-INF", true}, {"double", "NaN", true},
		{"double", "inf", false}, {"double", "-Infinity", false}, {"double", "+INF", false}, {"double", "1_0", false},
		{"double", "0x10", false}, {"double", "1.", true}, {"double", ".e1", false},
		{"hexBinary", "0aF", false}, {"hexBinary", "0aFF", true},
		{"NCName", "a:b", false}, {"NCName", "_a.b-c", true}, {"NMTOKEN", "1:2", true},
		{"boolean", " true ", true}, {"string", " x ", true},
	} {
		if got := builtinValid(c.typ, c.v); got != c.ok {
			t.Errorf("builtinValid(%s, %q) = %v", c.typ, c.v, got)
		}
	}
}
