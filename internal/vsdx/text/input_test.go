package text

import (
	"reflect"
	"strings"
	"testing"
)

// TestParseHTMLRoundTrip checks the chunk K5 rows: for every T2 row
// except index 11 (the field row), ParseHTML of the T2 HTML gives the
// body with Terminated true and the formats up to row renumbering, and
// Render of the result is the T2 HTML again. The T2 rows are the
// TestRender table, copied verbatim (shape defaults to page0/s5).
func TestParseHTMLRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		body    Body
		formats Formats
		shape   string
		want    string
	}{
		{
			name: "plain",
			body: Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: -1, Text: "Hello", Field: -1}}}}},
			want: `<p>Hello</p>`,
		},
		{
			name:    "bold cp0",
			body:    Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: 0, Text: "Hello", Field: -1}}}}},
			formats: Formats{Char: map[int]CharFormat{0: {Bold: "on"}}},
			want:    `<p><b>Hello</b></p>`,
		},
		{
			name:    "bold row0 before any cp",
			body:    Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: -1, Text: "x", Field: -1}}}}},
			formats: Formats{Char: map[int]CharFormat{0: {Bold: "on"}}},
			want:    `<p><b>x</b></p>`,
		},
		{
			name:    "bold italic size color",
			body:    Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: 0, Text: "Hi", Field: -1}}}}},
			formats: Formats{Char: map[int]CharFormat{0: {Bold: "on", Italic: "on", Size: "12", Color: "#FF0000"}}},
			want:    `<p><b><i><span style="font-size:12pt;color:#FF0000">Hi</span></i></b></p>`,
		},
		{
			name:    "bold off",
			body:    Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: -1, Text: "x", Field: -1}}}}},
			formats: Formats{Char: map[int]CharFormat{0: {Bold: "off"}}},
			want:    `<p><span style="font-weight:normal">x</span></p>`,
		},
		{
			name:    "para align pp0",
			body:    Body{Paras: []Para{{PP: 0, Runs: []Run{{CP: -1, Text: "x", Field: -1}}}}},
			formats: Formats{Para: map[int]ParaFormat{0: {Align: "center"}}},
			want:    `<p style="text-align:center">x</p>`,
		},
		{
			name:    "para align pp minus1",
			body:    Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: -1, Text: "x", Field: -1}}}}},
			formats: Formats{Para: map[int]ParaFormat{0: {Align: "center"}}},
			want:    `<p style="text-align:center">x</p>`,
		},
		{
			name: "bullet second paragraph",
			body: Body{Paras: []Para{
				{PP: -1, Runs: []Run{{CP: -1, Text: "A", Field: -1}}},
				{PP: 1, Runs: []Run{{CP: -1, Text: "B", Field: -1}}},
			}},
			formats: Formats{Para: map[int]ParaFormat{1: {Bullet: true}}},
			want:    `<p>A</p><ul><li>B</li></ul>`,
		},
		{
			name:    "bullet char",
			body:    Body{Paras: []Para{{PP: 1, Runs: []Run{{CP: -1, Text: "B", Field: -1}}}}},
			formats: Formats{Para: map[int]ParaFormat{1: {Bullet: true, BulletChar: "•"}}},
			want:    `<ul><li data-char="•">B</li></ul>`,
		},
		{
			name: "break",
			body: Body{Paras: []Para{{PP: -1, Runs: []Run{
				{CP: -1, Text: "a", Field: -1},
				{CP: -1, Br: true, Field: -1},
				{CP: -1, Text: "b", Field: -1},
			}}}},
			want: `<p>a<br/>b</p>`,
		},
		{
			name: "escaped text",
			body: Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: -1, Text: "a<b&c", Field: -1}}}}},
			want: `<p>a&lt;b&amp;c</p>`,
		},
		// Index 11, the field row, is excluded from this round trip;
		// fields are covered by TestParseHTMLField below.
		{
			name: "second field",
			body: Body{Paras: []Para{{PP: -1, Runs: []Run{
				{CP: -1, Text: "3", Field: 0},
				{CP: -1, Text: "4", Field: 1},
			}}}},
			want: `<p><opaque data-ref="page0/s5/field:1" data-kind="field" data-text="3"></opaque><opaque data-ref="page0/s5/field:2" data-kind="field" data-text="4"></opaque></p>`,
		},
		{
			name: "empty body",
			body: Body{},
			want: "",
		},
		{
			name:    "font and size",
			body:    Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: -1, Text: "x", Field: -1}}}}},
			formats: Formats{Char: map[int]CharFormat{0: {Font: "Arial", Size: "10.5"}}},
			want:    `<p><span style="font-size:10.5pt;font-family:'Arial'">x</span></p>`,
		},
		{
			name: "equal formats merge",
			body: Body{Paras: []Para{{PP: -1, Runs: []Run{
				{CP: 0, Text: "a", Field: -1},
				{CP: 1, Text: "b", Field: -1},
			}}}},
			formats: Formats{Char: map[int]CharFormat{0: {}, 1: {}}},
			want:    `<p>ab</p>`,
		},
		{
			name:    "para margins",
			body:    Body{Paras: []Para{{PP: 0, Runs: []Run{{CP: -1, Text: "x", Field: -1}}}}},
			formats: Formats{Para: map[int]ParaFormat{0: {MarginLeft: "18pt", Indent: "-18pt", SpaceBefore: "6pt"}}},
			want:    `<p style="margin-top:6pt;margin-left:18pt;text-indent:-18pt">x</p>`,
		},
		{
			name:    "underline strike",
			body:    Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: -1, Text: "x", Field: -1}}}}},
			formats: Formats{Char: map[int]CharFormat{0: {Underline: "on", Strike: "on"}}},
			want:    `<p><u><s>x</s></u></p>`,
		},
		{
			name:    "font quote",
			body:    Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: -1, Text: "x", Field: -1}}}}},
			formats: Formats{Char: map[int]CharFormat{0: {Font: "O'Neil"}}},
			want:    `<p><span style="font-family:'O\'Neil'">x</span></p>`,
		},
		{
			name:    "italic off",
			body:    Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: -1, Text: "x", Field: -1}}}}},
			formats: Formats{Char: map[int]CharFormat{0: {Italic: "off"}}},
			want:    `<p><span style="font-style:normal">x</span></p>`,
		},
		{
			name:    "underline off",
			body:    Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: -1, Text: "x", Field: -1}}}}},
			formats: Formats{Char: map[int]CharFormat{0: {Underline: "off"}}},
			want:    `<p><span style="text-decoration:none">x</span></p>`,
		},
		{
			name:    "strike off",
			body:    Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: -1, Text: "x", Field: -1}}}}},
			formats: Formats{Char: map[int]CharFormat{0: {Strike: "off"}}},
			want:    `<p><span style="text-decoration:none">x</span></p>`,
		},
		{
			name: "equal formatted runs merge",
			body: Body{Paras: []Para{{PP: -1, Runs: []Run{
				{CP: 0, Text: "a", Field: -1},
				{CP: 1, Text: "b", Field: -1},
			}}}},
			formats: Formats{Char: map[int]CharFormat{0: {Bold: "on"}, 1: {Bold: "on"}}},
			want:    `<p><b>ab</b></p>`,
		},
		{
			name: "consecutive bullets group",
			body: Body{Paras: []Para{
				{PP: 1, Runs: []Run{{CP: -1, Text: "A", Field: -1}}},
				{PP: 1, Runs: []Run{{CP: -1, Text: "B", Field: -1}}},
				{PP: -1, Runs: []Run{{CP: -1, Text: "C", Field: -1}}},
			}},
			formats: Formats{Para: map[int]ParaFormat{1: {Bullet: true}}},
			want:    `<ul><li>A</li><li>B</li></ul><p>C</p>`,
		},
		{
			name: "empty paragraph",
			body: Body{Paras: []Para{
				{PP: -1, Runs: []Run{{CP: -1, Text: "A", Field: -1}}},
				{PP: -1},
			}},
			want: `<p>A</p><p></p>`,
		},
		{
			name:    "para styles order",
			body:    Body{Paras: []Para{{PP: 0, Runs: []Run{{CP: -1, Text: "x", Field: -1}}}}},
			formats: Formats{Para: map[int]ParaFormat{0: {Align: "right", LineHeight: "120%", SpaceBefore: "6pt", SpaceAfter: "3pt", MarginLeft: "18pt", MarginRight: "9pt", Indent: "-18pt"}}},
			want:    `<p style="text-align:right;line-height:120%;margin-top:6pt;margin-bottom:3pt;margin-left:18pt;margin-right:9pt;text-indent:-18pt">x</p>`,
		},
		{
			name:    "char styles order",
			body:    Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: -1, Text: "x", Field: -1}}}}},
			formats: Formats{Char: map[int]CharFormat{0: {Bold: "off", Italic: "off", Underline: "off", Size: "12", Color: "#00FF00", Font: "Arial"}}},
			want:    `<p><span style="font-weight:normal;font-style:normal;text-decoration:none;font-size:12pt;color:#00FF00;font-family:'Arial'">x</span></p>`,
		},
		{
			name:    "field is bare under a format",
			body:    Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: 0, Text: "3", Field: 0}}}}},
			formats: Formats{Char: map[int]CharFormat{0: {Size: "10", Font: "Arial"}}},
			want:    `<p><opaque data-ref="page0/s5/field:1" data-kind="field" data-text="3"></opaque></p>`,
		},
		{
			name:    "font backslash",
			body:    Body{Paras: []Para{{PP: -1, Runs: []Run{{CP: -1, Text: "x", Field: -1}}}}},
			formats: Formats{Char: map[int]CharFormat{0: {Font: `a\b`}}},
			want:    `<p><span style="font-family:'a\\b'">x</span></p>`,
		},
		{
			name:  "v7 golden",
			shape: "page0/s1",
			body: Body{Paras: []Para{
				{PP: 0, Runs: []Run{
					{CP: 0, Text: "Title", Field: -1},
					{CP: 1, Text: " red", Field: -1},
				}},
				{PP: 1, Runs: []Run{
					{CP: 2, Text: "item", Field: -1},
					{CP: 2, Br: true, Field: -1},
					{CP: 2, Text: "two\tend ", Field: -1},
					{CP: 2, Text: "3", Field: 0},
				}},
			}, Terminated: true},
			formats: Formats{
				Char: map[int]CharFormat{0: {}, 1: {Bold: "on", Color: "#FF0000"}, 2: {Size: "10", Font: "Arial"}},
				Para: map[int]ParaFormat{0: {Align: "left"}, 1: {Bullet: true, BulletChar: "\u2022", MarginLeft: "18pt", Indent: "-18pt"}},
			},
			want: "<p style=\"text-align:left\">Title<b><span style=\"color:#FF0000\"> red</span></b></p><ul><li style=\"margin-left:18pt;text-indent:-18pt\" data-char=\"\u2022\"><span style=\"font-size:10pt;font-family:'Arial'\">item</span><br/><span style=\"font-size:10pt;font-family:'Arial'\">two\tend </span><opaque data-ref=\"page0/s1/field:1\" data-kind=\"field\" data-text=\"3\"></opaque></li></ul>",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			shape := tc.shape
			if shape == "" {
				shape = "page0/s5"
			}
			got, formats, err := ParseHTML(tc.want)
			if err != nil {
				t.Fatalf("ParseHTML(%q): %v", tc.want, err)
			}
			if !got.Terminated {
				t.Errorf("ParseHTML(%q): Terminated = false; want true", tc.want)
			}
			if rendered := Render(got, formats, shape); rendered != tc.want {
				t.Errorf("Render(ParseHTML(%q)) = %q; want %q", tc.want, rendered, tc.want)
			}
			if plain := Plain(got); plain != Plain(tc.body) {
				t.Errorf("Plain(ParseHTML(%q)) = %q; want %q", tc.want, plain, Plain(tc.body))
			}
			want := Body{Paras: tc.body.Paras, Terminated: true}
			if !reflect.DeepEqual(canonBody(got, formats), canonBody(want, tc.formats)) {
				t.Errorf("ParseHTML(%q) = %#v with %#v; want %#v with %#v up to row renumbering",
					tc.want, got, formats, want, tc.formats)
			}
		})
	}
}

// canonPara is one paragraph with its paragraph format and its runs
// with their character formats, for comparison up to row renumbering.
type canonPara struct {
	fmt  ParaFormat
	runs []canonRun
}

// canonRun is one run with its character format; a break or a field
// run's format is not shown by Render, so it is zero here.
type canonRun struct {
	fmt   CharFormat
	text  string
	br    bool
	field int
}

// canonBody returns b's paragraphs as canonPara values: paragraph and
// run formats resolved through f, adjacent text runs of one format
// merged, an underline or strike "off" canonicalised to both "off"
// (Render writes one text-decoration:none for either), and empty
// unformatted runs dropped (Render skips them).
func canonBody(b Body, f Formats) []canonPara {
	var out []canonPara
	for _, p := range b.Paras {
		cp := canonPara{fmt: paraFormat(f, p.PP)}
		for _, r := range p.Runs {
			cr := canonRun{text: r.Text, br: r.Br, field: r.Field}
			if !r.Br && r.Field < 0 {
				cr.fmt = canonChar(charFormat(f, r.CP))
				if r.Text == "" && cr.fmt == (CharFormat{}) {
					continue
				}
				if n := len(cp.runs); n > 0 {
					last := &cp.runs[n-1]
					if !last.br && last.field < 0 && last.fmt == cr.fmt {
						last.text += r.Text
						continue
					}
				}
			}
			cp.runs = append(cp.runs, cr)
		}
		out = append(out, cp)
	}
	return out
}

// canonChar canonicalises a character format's underline and strike
// "off" forms, which Render writes as one declaration.
func canonChar(f CharFormat) CharFormat {
	if f.Underline == "off" || f.Strike == "off" {
		f.Underline, f.Strike = "off", "off"
	}
	return f
}

// TestParseHTMLRows checks ParseHTML's exact rows for the direct
// cases: numbered from 0 in first use, with an empty row 0 when an
// unformatted paragraph or run stands beside a formatted one.
func TestParseHTMLRows(t *testing.T) {
	tests := []struct {
		name    string
		html    string
		want    Body
		formats Formats
	}{
		{
			name: "plain",
			html: `<p>Hello</p>`,
			want: Body{Paras: []Para{{PP: 0, Runs: []Run{{CP: 0, Text: "Hello", Field: -1}}}}, Terminated: true},
			formats: Formats{
				Char: map[int]CharFormat{0: {}},
				Para: map[int]ParaFormat{0: {}},
			},
		},
		{
			name: "formatted only",
			html: `<p><b>Hello</b></p>`,
			want: Body{Paras: []Para{{PP: 0, Runs: []Run{{CP: 0, Text: "Hello", Field: -1}}}}, Terminated: true},
			formats: Formats{
				Char: map[int]CharFormat{0: {Bold: "on"}},
				Para: map[int]ParaFormat{0: {}},
			},
		},
		{
			name: "mixed runs reserve empty row 0",
			html: `<p><b>A</b> b</p>`,
			want: Body{Paras: []Para{{PP: 0, Runs: []Run{
				{CP: 1, Text: "A", Field: -1},
				{CP: 0, Text: " b", Field: -1},
			}}}, Terminated: true},
			formats: Formats{
				Char: map[int]CharFormat{0: {}, 1: {Bold: "on"}},
				Para: map[int]ParaFormat{0: {}},
			},
		},
		{
			name: "mixed paragraphs reserve empty row 0",
			html: `<p>A</p><p style="text-align:center">B</p>`,
			want: Body{Paras: []Para{
				{PP: 0, Runs: []Run{{CP: 0, Text: "A", Field: -1}}},
				{PP: 1, Runs: []Run{{CP: 0, Text: "B", Field: -1}}},
			}, Terminated: true},
			formats: Formats{
				Char: map[int]CharFormat{0: {}},
				Para: map[int]ParaFormat{0: {}, 1: {Align: "center"}},
			},
		},
		{
			name: "bullet",
			html: `<p>A</p><ul><li>B</li></ul>`,
			want: Body{Paras: []Para{
				{PP: 0, Runs: []Run{{CP: 0, Text: "A", Field: -1}}},
				{PP: 1, Runs: []Run{{CP: 0, Text: "B", Field: -1}}},
			}, Terminated: true},
			formats: Formats{
				Char: map[int]CharFormat{0: {}},
				Para: map[int]ParaFormat{0: {}, 1: {Bullet: true}},
			},
		},
		{
			name: "break",
			html: `<p>a<br/>b</p>`,
			want: Body{Paras: []Para{{PP: 0, Runs: []Run{
				{CP: 0, Text: "a", Field: -1},
				{CP: 0, Br: true, Field: -1},
				{CP: 0, Text: "b", Field: -1},
			}}}, Terminated: true},
			formats: Formats{
				Char: map[int]CharFormat{0: {}},
				Para: map[int]ParaFormat{0: {}},
			},
		},
		{
			name: "off forms",
			html: `<p><span style="font-weight:normal;font-style:normal;text-decoration:none;font-size:12pt;color:#00FF00;font-family:'Arial'">x</span></p>`,
			want: Body{Paras: []Para{{PP: 0, Runs: []Run{{CP: 0, Text: "x", Field: -1}}}}, Terminated: true},
			formats: Formats{
				Char: map[int]CharFormat{0: {Bold: "off", Italic: "off", Underline: "off", Strike: "off", Size: "12", Color: "#00FF00", Font: "Arial"}},
				Para: map[int]ParaFormat{0: {}},
			},
		},
		{
			name: "nested formats",
			html: `<p><b><i><span style="font-size:12pt;color:#FF0000">Hi</span></i></b></p>`,
			want: Body{Paras: []Para{{PP: 0, Runs: []Run{{CP: 0, Text: "Hi", Field: -1}}}}, Terminated: true},
			formats: Formats{
				Char: map[int]CharFormat{0: {Bold: "on", Italic: "on", Size: "12", Color: "#FF0000"}},
				Para: map[int]ParaFormat{0: {}},
			},
		},
		{
			name: "empty paragraph",
			html: `<p></p>`,
			want: Body{Paras: []Para{{PP: 0}}, Terminated: true},
			formats: Formats{
				Para: map[int]ParaFormat{0: {}},
			},
		},
		{
			name:    "empty",
			html:    "",
			want:    Body{Terminated: true},
			formats: Formats{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, formats, err := ParseHTML(tc.html)
			if err != nil {
				t.Fatalf("ParseHTML(%q): %v", tc.html, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ParseHTML(%q) body = %#v; want %#v", tc.html, got, tc.want)
			}
			if !reflect.DeepEqual(formats, tc.formats) {
				t.Errorf("ParseHTML(%q) formats = %#v; want %#v", tc.html, formats, tc.formats)
			}
		})
	}
}

// TestParseHTMLField checks that an <opaque> placeholder is kept as a
// field run with the number in its data-ref (1-based there, 0-based
// in the body).
func TestParseHTMLField(t *testing.T) {
	got, formats, err := ParseHTML(`<p>Page <opaque data-ref="page0/s5/field:1" data-kind="field" data-text="3"></opaque><opaque data-ref="page0/s5/field:2" data-kind="field" data-text="4"></opaque></p>`)
	if err != nil {
		t.Fatalf("ParseHTML(field): %v", err)
	}
	want := Body{Paras: []Para{{PP: 0, Runs: []Run{
		{CP: 0, Text: "Page ", Field: -1},
		{CP: 0, Text: "3", Field: 0},
		{CP: 0, Text: "4", Field: 1},
	}}}, Terminated: true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseHTML(field) = %#v; want %#v", got, want)
	}
	wantFormats := Formats{
		Char: map[int]CharFormat{0: {}},
		Para: map[int]ParaFormat{0: {}},
	}
	if !reflect.DeepEqual(formats, wantFormats) {
		t.Errorf("ParseHTML(field) formats = %#v; want %#v", formats, wantFormats)
	}
	if rendered := Render(got, formats, "page0/s5"); rendered != `<p>Page <opaque data-ref="page0/s5/field:1" data-kind="field" data-text="3"></opaque><opaque data-ref="page0/s5/field:2" data-kind="field" data-text="4"></opaque></p>` {
		t.Errorf("Render(ParseHTML(field)) = %q", rendered)
	}
}

// TestParseHTMLErrors checks that HTML outside the grammar is refused
// with an error naming the element or attribute.
func TestParseHTMLErrors(t *testing.T) {
	tests := []struct {
		name string
		html string
		want string
	}{
		{"link", `<a href="x">a</a>`, "<a>"},
		{"ordered list", `<ol><li>x</li></ol>`, "<ol>"},
		{"data-level", `<p data-level="1">x</p>`, "data-level"},
		{"colour name", `<p><span style="color:red">x</span></p>`, "color"},
		{"colour name bare span", `<span style="color:red">x</span>`, "color"},
		{"unclosed span", `<p>a<span style="font-size:12pt">`, "span"},
		{"div", `<div>x</div>`, "<div>"},
		{"unknown attribute", `<p class="x">x</p>`, "class"},
		{"paragraph style property", `<p style="color:red">x</p>`, "color"},
		{"span style property", `<p><span style="letter-spacing:1pt">x</span></p>`, "letter-spacing"},
		{"font-size unit", `<p><span style="font-size:12px">x</span></p>`, "font-size"},
		{"text-align", `<p style="text-align:middle">x</p>`, "text-align"},
		{"font-weight", `<p><span style="font-weight:bold">x</span></p>`, "font-weight"},
		{"data-char on p", `<p data-char="x">y</p>`, "data-char"},
		{"bad field ref", `<p><opaque data-ref="x" data-kind="field" data-text="3"></opaque></p>`, "data-ref"},
		{"bad field kind", `<p><opaque data-ref="page0/s5/field:1" data-kind="other" data-text="3"></opaque></p>`, "data-kind"},
		{"li outside list", `<li>x</li>`, "<li>"},
		{"text beside blocks", `x<p>y</p>`, "beside block"},
		{"empty list", `<ul></ul>`, "<ul>"},
		{"br attribute", `<p>a<br data-x="1"/></p>`, "data-x"},
		{"span attribute", `<p><span title="t">x</span></p>`, "title"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := ParseHTML(tc.html)
			if err == nil {
				t.Fatalf("ParseHTML(%q): no error; want it to mention %q", tc.html, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("ParseHTML(%q): %v; want it to mention %q", tc.html, err, tc.want)
			}
		})
	}
}
