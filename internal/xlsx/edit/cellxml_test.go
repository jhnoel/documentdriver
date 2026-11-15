package edit

import (
	"math"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// K2: RenderCell, pure (designer rows; frozen).

func TestRenderCell(t *testing.T) {
	xlsxtest.Skip(t, func() {
		for _, tc := range []struct {
			prefix   string
			row, col int
			style    int
			c        Content
			want     string
			wantErr  bool
		}{
			{"", 2, 2, 0, Content{Kind: ContentNumber, Num: 1200}, `<c r="B2"><v>1200</v></c>`, false},
			{"", 1, 1, 3, Content{Kind: ContentNumber, Num: 0.1}, `<c r="A1" s="3"><v>0.1</v></c>`, false},
			{"", 1, 1, 0, Content{Kind: ContentString, Text: "a<b"}, `<c r="A1" t="inlineStr"><is><t>a&lt;b</t></is></c>`, false},
			{"", 1, 1, 0, Content{Kind: ContentString, Text: ""}, `<c r="A1" t="inlineStr"><is><t/></is></c>`, false},
			{"", 1, 1, 0, Content{Kind: ContentString, Text: " pad "}, `<c r="A1" t="inlineStr"><is><t xml:space="preserve"> pad </t></is></c>`, false},
			{"", 1, 1, 0, Content{Kind: ContentString, Text: "a\x01b"}, `<c r="A1" t="inlineStr"><is><t>a_x0001_b</t></is></c>`, false},
			{"", 1, 1, 0, Content{Kind: ContentString, Text: "_x0041_"}, `<c r="A1" t="inlineStr"><is><t>_x005F_x0041_</t></is></c>`, false},
			{"x:", 1, 1, 0, Content{Kind: ContentBool, Bool: true}, `<x:c r="A1" t="b"><x:v>1</x:v></x:c>`, false},
			{"", 1, 1, 0, Content{Kind: ContentError, Text: "#N/A"}, `<c r="A1" t="e"><v>#N/A</v></c>`, false},
			{"", 1, 1, 0, Content{Kind: ContentFormula, Formula: "B1+1"}, `<c r="A1"><f>B1+1</f></c>`, false},
			{"", 1, 1, 2, Content{Kind: ContentEmpty}, `<c r="A1" s="2"/>`, false},
			{"", 1, 1, 0, Content{Kind: ContentEmpty}, "", true},
			{"", 1, 1, 0, Content{Kind: ContentNumber, Num: math.NaN()}, "", true},
			{"", 1, 1, 0, Content{Kind: ContentError, Text: "#OOPS"}, "", true},
		} {
			got, err := RenderCell(tc.prefix, tc.row, tc.col, tc.style, tc.c)
			if (err != nil) != tc.wantErr || got != tc.want {
				t.Errorf("RenderCell(%+v) = %q, %v; want %q", tc.c, got, err, tc.want)
			}
		}
	})
}

// TestRenderCellReads: what RenderCell writes, the overlay accepts and
// sheet.Scan reads back as the same value (RawCell).
func TestRenderCellReads(t *testing.T) {
	xlsxtest.Skip(t, func() {
		w := openBook(t, oneSheet(""))
		s, err := w.Sheet("S")
		if err != nil {
			t.Fatal(err)
		}
		x, err := RenderCell(s.Prefix(), 3, 4, 0, Content{Kind: ContentString, Text: "tab\there_x0041_"})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SetCell(3, 4, x); err != nil {
			t.Fatal(err)
		}
		c, _ := s.Cell(3, 4)
		if c.Raw.T != "inlineStr" || !c.Raw.HasIS {
			t.Errorf("read back %+v", c.Raw)
		}
	})
}

// TestRenderCellEdges: every error literal, the limits, whitespace.
func TestRenderCellEdges(t *testing.T) {
	xlsxtest.Skip(t, func() {
		for _, lit := range []string{"#NULL!", "#DIV/0!", "#VALUE!", "#REF!", "#NAME?", "#NUM!", "#N/A", "#GETTING_DATA"} {
			want := `<c r="A1" t="e"><v>` + lit + `</v></c>`
			if got, err := RenderCell("", 1, 1, 0, Content{Kind: ContentError, Text: lit}); err != nil || got != want {
				t.Errorf("%s: %q %v", lit, got, err)
			}
		}
		for _, tc := range []struct {
			c  Content
			ok bool
		}{
			{Content{Kind: ContentString, Text: strings.Repeat("a", 32767)}, true},
			{Content{Kind: ContentString, Text: strings.Repeat("a", 32768)}, false},
			{Content{Kind: ContentFormula, Formula: strings.Repeat("1+", 4095) + "1"}, true},
			{Content{Kind: ContentFormula, Formula: strings.Repeat("1+", 4096) + "1"}, false},
			{Content{Kind: ContentNumber, Num: math.Inf(1)}, false},
		} {
			if _, err := RenderCell("", 1, 1, 0, tc.c); (err == nil) != tc.ok {
				t.Errorf("%v (len %d): %v", tc.c.Kind, len(tc.c.Text)+len(tc.c.Formula), err)
			}
		}
		for _, tc := range []struct{ text, want string }{
			{"a\nb", `<t xml:space="preserve">a` + "\n" + `b</t>`},
			{"\tx", `<t xml:space="preserve">` + "\t" + `x</t>`},
			{"x ", `<t xml:space="preserve">x </t>`},
			{"a\rb", `<t>a_x000D_b</t>`},
			{"_x005F_", `<t>_x005F_x005F_</t>`},
		} {
			got, err := RenderCell("", 1, 1, 0, Content{Kind: ContentString, Text: tc.text})
			if err != nil || !strings.Contains(got, tc.want) {
				t.Errorf("%q: %s %v, want %s", tc.text, got, err, tc.want)
			}
		}
	})
}
