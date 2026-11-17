package emit

import (
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/text"
)

// TestTextXMLParseHTML checks the chunk K5b rows: ParseHTML of each
// HTML, emitted with TextXML, gives the expected sections and Text
// element, and the Text element re-parses to the same plain text.
func TestTextXMLParseHTML(t *testing.T) {
	tests := []struct {
		name         string
		html         string
		wantSections string
		wantText     string
	}{
		{
			name:         "plain",
			html:         "<p>Start</p>",
			wantSections: "",
			wantText:     "<Text>Start\n</Text>",
		},
		{
			name:         "bold",
			html:         "<p><b>End</b></p>",
			wantSections: "<Section N='Character'><Row IX='0'><Cell N='Style' V='1'/></Row></Section>",
			wantText:     "<Text><cp IX='0'/>End\n</Text>",
		},
		{
			name:         "paragraph align",
			html:         "<p>A</p><p style=\"text-align:center\">B</p>",
			wantSections: "<Section N='Paragraph'><Row IX='0'/><Row IX='1'><Cell N='HorzAlign' V='1'/></Row></Section>",
			wantText:     "<Text><pp IX='0'/>A\n<pp IX='1'/>B\n</Text>",
		},
		{
			name:         "break",
			html:         "<p>a<br/>b</p>",
			wantSections: "",
			wantText:     "<Text>a\u2028b\n</Text>",
		},
		{
			name:         "size 12pt",
			html:         "<p><span style=\"font-size:12pt\">x</span></p>",
			wantSections: "<Section N='Character'><Row IX='0'><Cell N='Size' V='0.16666666666666666'/></Row></Section>",
			wantText:     "<Text><cp IX='0'/>x\n</Text>",
		},
		{
			name:         "size 10pt",
			html:         "<p><span style=\"font-size:10pt\">x</span></p>",
			wantSections: "<Section N='Character'><Row IX='0'><Cell N='Size' V='0.1388888888888889'/></Row></Section>",
			wantText:     "<Text><cp IX='0'/>x\n</Text>",
		},
		{
			name:         "size 10.5pt",
			html:         "<p><span style=\"font-size:10.5pt\">x</span></p>",
			wantSections: "<Section N='Character'><Row IX='0'><Cell N='Size' V='0.14583333333333334'/></Row></Section>",
			wantText:     "<Text><cp IX='0'/>x\n</Text>",
		},
		{
			name:         "escaped",
			html:         "<p>a&lt;b</p>",
			wantSections: "",
			wantText:     "<Text>a&lt;b\n</Text>",
		},
		{
			name:         "mixed runs",
			html:         "<p><b>A</b> b</p>",
			wantSections: "<Section N='Character'><Row IX='0'/><Row IX='1'><Cell N='Style' V='1'/></Row></Section>",
			wantText:     "<Text><cp IX='1'/>A<cp IX='0'/> b\n</Text>",
		},
		{
			name:         "full character",
			html:         "<p><i><u><s><span style=\"font-size:10pt;color:#00FF00;font-family:'Arial'\">x</span></s></u></i></p>",
			wantSections: "<Section N='Character'><Row IX='0'><Cell N='Style' V='6'/><Cell N='Size' V='0.1388888888888889'/><Cell N='Color' V='#00FF00'/><Cell N='Font' V='Arial'/><Cell N='Strikethru' V='1'/></Row></Section>",
			wantText:     "<Text><cp IX='0'/>x\n</Text>",
		},
		{
			name:         "bold off",
			html:         "<p><span style=\"font-weight:normal\">x</span></p>",
			wantSections: "<Section N='Character'><Row IX='0'><Cell N='Style' V='0'/></Row></Section>",
			wantText:     "<Text><cp IX='0'/>x\n</Text>",
		},
		{
			name:         "bullet",
			html:         "<p>A</p><ul><li>B</li></ul>",
			wantSections: "<Section N='Paragraph'><Row IX='0'/><Row IX='1'><Cell N='Bullet' V='1'/></Row></Section>",
			wantText:     "<Text><pp IX='0'/>A\n<pp IX='1'/>B\n</Text>",
		},
		{
			name:         "bullet char",
			html:         "<p>A</p><ul><li data-char=\"\u2022\">B</li></ul>",
			wantSections: "<Section N='Paragraph'><Row IX='0'/><Row IX='1'><Cell N='Bullet' V='1'/><Cell N='BulletStr' V='\u2022'/></Row></Section>",
			wantText:     "<Text><pp IX='0'/>A\n<pp IX='1'/>B\n</Text>",
		},
		{
			name:         "full paragraph",
			html:         "<p style=\"text-align:right;margin-left:18pt;margin-right:9pt;text-indent:-18pt;margin-top:6pt;margin-bottom:3pt;line-height:120%\">x</p>",
			wantSections: "<Section N='Paragraph'><Row IX='0'><Cell N='HorzAlign' V='2'/><Cell N='IndLeft' V='0.25'/><Cell N='IndRight' V='0.125'/><Cell N='IndFirst' V='-0.25'/><Cell N='SpBefore' V='0.08333333333333333'/><Cell N='SpAfter' V='0.041666666666666664'/><Cell N='SpLine' V='-1.2'/></Row></Section>",
			wantText:     "<Text><pp IX='0'/>x\n</Text>",
		},
		{
			name:         "line height points",
			html:         "<p style=\"line-height:14pt\">x</p>",
			wantSections: "<Section N='Paragraph'><Row IX='0'><Cell N='SpLine' V='0.19444444444444445'/></Row></Section>",
			wantText:     "<Text><pp IX='0'/>x\n</Text>",
		},
		{
			name:         "field",
			html:         "<p>Page <opaque data-ref=\"page0/s5/field:1\" data-kind=\"field\" data-text=\"3\"></opaque></p>",
			wantSections: "",
			wantText:     "<Text>Page <fld IX='0'>3</fld>\n</Text>",
		},
		{
			name:         "empty",
			html:         "",
			wantSections: "",
			wantText:     "<Text></Text>",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, formats, err := text.ParseHTML(tc.html)
			if err != nil {
				t.Fatalf("ParseHTML(%q): %v", tc.html, err)
			}
			sections, textEl, err := TextXML(body, formats)
			if err != nil {
				t.Fatalf("TextXML: %v", err)
			}
			if string(sections) != tc.wantSections {
				t.Errorf("sections = %s\nwant %s", sections, tc.wantSections)
			}
			if string(textEl) != tc.wantText {
				t.Errorf("text = %q\nwant %q", textEl, tc.wantText)
			}
			doc, err := tree.ParseString(string(textEl))
			if err != nil {
				t.Fatalf("ParseString(%q): %v", textEl, err)
			}
			back, _ := text.Parse(doc.Root())
			if got, want := text.Plain(back), text.Plain(body); got != want {
				t.Errorf("round trip plain = %q, want %q", got, want)
			}
		})
	}
}

// TestTextXMLDirect checks that a body from Parse (no markers, not
// terminated) emits no markers and no trailing line feed: row -1 is
// row 0 by default, as Visio reads it.
func TestTextXMLDirect(t *testing.T) {
	body := text.Body{Paras: []text.Para{{PP: -1, Runs: []text.Run{{CP: -1, Text: "x", Field: -1}}}}}
	formats := text.Formats{Char: map[int]text.CharFormat{0: {Bold: "on"}}}
	sections, textEl, err := TextXML(body, formats)
	if err != nil {
		t.Fatalf("TextXML: %v", err)
	}
	if want := "<Section N='Character'><Row IX='0'><Cell N='Style' V='1'/></Row></Section>"; string(sections) != want {
		t.Errorf("sections = %s\nwant %s", sections, want)
	}
	if want := "<Text>x</Text>"; string(textEl) != want {
		t.Errorf("text = %q\nwant %q", textEl, want)
	}
}

// TestTextXMLErrors checks that a bad format or marker is refused.
func TestTextXMLErrors(t *testing.T) {
	tests := []struct {
		name string
		body text.Body
		f    text.Formats
	}{
		{"size", text.Body{}, text.Formats{Char: map[int]text.CharFormat{0: {Size: "wide"}}}},
		{"colour", text.Body{}, text.Formats{Char: map[int]text.CharFormat{0: {Color: "red"}}}},
		{"bold", text.Body{}, text.Formats{Char: map[int]text.CharFormat{0: {Bold: "yes"}}}},
		{"align", text.Body{}, text.Formats{Para: map[int]text.ParaFormat{0: {Align: "middle"}}}},
		{"line height", text.Body{}, text.Formats{Para: map[int]text.ParaFormat{0: {LineHeight: "wide"}}}},
		{"margin", text.Body{}, text.Formats{Para: map[int]text.ParaFormat{0: {MarginLeft: "wide"}}}},
		{"paragraph pp", text.Body{Paras: []text.Para{{PP: -2}}}, text.Formats{}},
		{"run cp", text.Body{Paras: []text.Para{{PP: -1, Runs: []text.Run{{CP: -2, Field: -1}}}}}, text.Formats{}},
		{"negative row", text.Body{}, text.Formats{Char: map[int]text.CharFormat{-1: {Bold: "on"}}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := TextXML(tc.body, tc.f); err == nil {
				t.Errorf("TextXML: no error, want one")
			}
		})
	}
}
