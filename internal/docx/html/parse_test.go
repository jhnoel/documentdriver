package html

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// convStub records the equations it converts.
type convStub struct{ got []string }

func (s *convStub) conv(latex string, block bool) (string, error) {
	s.got = append(s.got, fmt.Sprintf("%s/%v", latex, block))
	if latex == "bad" {
		return "", errors.New("unknown command")
	}
	return "<m:oMath/>", nil
}

func equations(items []Item) []Atom {
	var out []Atom
	for _, it := range items {
		if it.Kind == ItemAtom && it.Atom.Kind == AtomEquation {
			out = append(out, it.Atom)
		}
	}
	return out
}

func mustFragment(t *testing.T, input string, eq LatexToOMML) []Item {
	t.Helper()
	items, err := ParseFragment(input, eq)
	if err != nil {
		t.Fatalf("ParseFragment(%q): %v", input, err)
	}
	return items
}

// Equations are LaTeX only (the docx surface change recorded as the
// latex-equations intended difference): parse.rs's equation tests, which
// covered MathML children, latex attributes and display inferred from the
// paragraph, give way to these.

func TestEquations(t *testing.T) {
	s := &convStub{}
	eqs := equations(mustFragment(t, `<p>Area <equation display="inline">\pi r^2</equation> and <ins><equation display="block">a &lt; b &amp; c</equation></ins></p>`, s.conv))
	if len(eqs) != 2 || eqs[0].Display || !eqs[1].Display || eqs[0].Text != `\pi r^2` || eqs[1].Text != "a < b & c" ||
		eqs[0].OMML != "<m:oMath/>" || !eqs[1].Ins.OK {
		t.Fatalf("equations %+v", eqs)
	}
	if strings.Join(s.got, "|") != `\pi r^2/false|a < b & c/true` {
		t.Fatalf("converted %q", s.got)
	}
}

func TestEquationErrors(t *testing.T) {
	s := &convStub{}
	for _, tc := range []struct{ in, want string }{
		{`<p><equation>x</equation></p>`, `<equation> requires display="inline" or display="block"`},
		{`<p><equation display="wide">x</equation></p>`, `<equation> requires display="inline" or display="block"`},
		{`<p><equation display="inline" latex="x"></equation></p>`, "<equation> takes its LaTeX as text content, not a latex attribute"},
		{`<p><equation display="inline">a<b>c</b></equation></p>`, "<equation> holds LaTeX text only; found <b> (write < as &lt;)"},
		{`<p><equation display="inline"> </equation></p>`, "<equation> is empty"},
		{`<p><equation display="inline">bad</equation></p>`, "invalid LaTeX equation: unknown command"},
		{`<equation display="inline">x</equation>`, "<equation> outside a paragraph"},
		{`<p><math><mi>x</mi></math></p>`, `MathML is not supported: write an equation as <equation display="inline">LaTeX</equation> (or display="block")`},
	} {
		_, err := ParseFragment(tc.in, s.conv)
		if err == nil || err.Error() != tc.want {
			t.Errorf("ParseFragment(%q) = %v, want %q", tc.in, err, tc.want)
		}
	}
	if _, err := ParseFragment(`<p><equation display="inline">x</equation></p>`, nil); !errors.Is(err, errNoEquations) {
		t.Errorf("without a converter: %v", err)
	}
}

func TestListContinuation(t *testing.T) {
	items := mustFragment(t, `<ol start="3"><li>a<p>b</p><p>c</p><ul><li>d</li></ul></li><li>e</li></ol>`, nil)
	type para struct {
		text      string
		id, level uint32
		format    string
		cont      bool
	}
	var got []para
	var cur *para
	for _, it := range items {
		switch it.Kind {
		case ItemParaOpen:
			l := it.Para.List.V
			got = append(got, para{id: l.ID, level: l.Level, format: l.Format, cont: l.Continuation})
			cur = &got[len(got)-1]
		case ItemAtom:
			cur.text += it.Atom.Text
		}
	}
	want := []para{
		{"a", 1, 0, "decimal", false},
		{"b", 1, 0, "decimal", true},
		{"c", 1, 0, "decimal", true},
		{"d", 2, 1, "bullet", false},
		{"e", 1, 0, "decimal", false},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("paragraph %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestListContinuationIgnoresText(t *testing.T) {
	// docxdriver inserts list-continuation into text that spells
	// ` list-id="1"` (intended difference list-continuation-text).
	items := mustFragment(t, `<ul><li>a list-id="1" b<p>c</p></li></ul>`, nil)
	var text strings.Builder
	for _, it := range items {
		if it.Kind == ItemAtom {
			text.WriteString(it.Atom.Text)
		}
	}
	if text.String() != `a list-id="1" bc` {
		t.Fatalf("text %q", text.String())
	}
}

func TestFragmentErrors(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"<p>a</p>text", `text outside a paragraph element: "\ntext" — content lives inside <p>/<h1..6>`},
		{"<p><a>x</a></p>", "<a> requires href"},
		{`<p><a href="javascript:x">x</a></p>`, "unsupported hyperlink URL scheme"},
		{"<p><field>1</field></p>", `<field> requires instr="…"`},
		{"<p><ins><ins>a</ins></ins></p>", "<ins> nested inside <ins>"},
		{`<p><mark data-highlight="nope">x</mark></p>`, `unknown highlight palette value "nope"`},
		{`<p style="bogus:1">x</p>`, "unsupported paragraph CSS property bogus"},
		{`<p onclick="x">a</p>`, "unsupported HTML attribute onclick"},
		{`<table style="x"><tr><td>a</td></tr></table>`, "unsupported table formatting attribute style on <table>; set table formatting with format_table, format_row or format_cell"},
		{"<section><p>x</p></section>", "sections require document HTML"},
		{"<ul><li>a<div>b</div></li></ul>", "<div> is block content; use a paragraph operation"},
		{"<p>&amp</p>", `invalid HTML: ParseDiagnostic { severity: Error, message: "missing-semicolon-after-character-reference", location: SourceLocation { line: 1, column: 8, byte_offset: 7 } }`},
		{"<p>a<b>b<p>c</b>d", "unsupported HTML structure: <p> closes the open <p> while <b> is still open"},
		{`<p class="a" class="b">x</p>`, "duplicate HTML attribute class; write it once"},
		{"<script>x</script>", "unsupported HTML element <script>"},
		{"<p><b><b>x</b></b></p><p>\x00</p>", `invalid HTML: ParseDiagnostic { severity: Error, message: "unexpected-null-character", location: SourceLocation { line: 1, column: 27, byte_offset: 26 } }`},
	} {
		_, err := ParseFragment(tc.in, nil)
		if err == nil || err.Error() != tc.want {
			t.Errorf("ParseFragment(%q) = %v, want %q", tc.in, err, tc.want)
		}
	}
}

func TestTables(t *testing.T) {
	items := mustFragment(t, `<table><tr><th colspan=2>a</th></tr><tr><td><p>b</p><p>c</p></td><td data-format="x&quot;">d</td></tr></table>`, nil)
	var structs []string
	for _, it := range items {
		if it.Kind == ItemStruct {
			structs = append(structs, it.Struct)
		}
	}
	want := `table|tr|td colspan="2"|/td|/tr|tr|td|/td|td data-format="x&quot;"|/td|/tr|/table`
	if got := strings.Join(structs, "|"); got != want {
		t.Fatalf("structure %s, want %s", got, want)
	}
}

func TestParseDocument(t *testing.T) {
	secs, err := ParseDocument(`<!DOCTYPE html><html><body><header><p>h</p></header><p>x</p><section first-page/><footer even><p>f</p></footer><p>y</p></body></html>`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(secs) != 2 || secs[0].TitlePage || !secs[1].TitlePage {
		t.Fatalf("sections %+v", secs)
	}
	if s := secs[0].Slots; len(s) != 1 || s[0].Scope != "hdr" || s[0].Kind != "default" || s[0].Err != nil {
		t.Errorf("first section slots %+v", s)
	}
	if s := secs[1].Slots; len(s) != 1 || s[0].Scope != "ftr" || s[0].Kind != "even" {
		t.Errorf("second section slots %+v", s)
	}
	// A header after body content stays in the body, where the dialect
	// rejects it.
	secs, err = ParseDocument(`<!DOCTYPE html><html><body><p>x</p><header><p>h</p></header></body></html>`, nil)
	if err != nil || len(secs) != 1 || secs[0].BodyErr == nil ||
		!strings.HasPrefix(secs[0].BodyErr.Error(), "<header> is a document-surface element") {
		t.Fatalf("sections %+v, %v", secs, err)
	}
	// Only chrome: the body becomes one empty paragraph.
	secs, err = ParseDocument(`<!DOCTYPE html><html><body><header><p>h</p></header></body></html>`, nil)
	if err != nil || len(secs) != 1 || len(secs[0].Body) != 2 {
		t.Fatalf("sections %+v, %v", secs, err)
	}
}

func TestSafeURL(t *testing.T) {
	for in, want := range map[string]bool{
		"https://x": true, " HTTP://x": true, "mailto:a@b": true, "tel:1": true, "#frag": true,
		"rel/path": true, "javascript:x": false, "data:x": false, "http://x\x01": false,
	} {
		if SafeURL(in) != want {
			t.Errorf("SafeURL(%q) = %v", in, !want)
		}
	}
}
