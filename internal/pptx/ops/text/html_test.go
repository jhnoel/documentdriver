package text

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalise(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "<p></p>"},
		{"Cache", "<p>Cache</p>"},
		{"<b>API</b> Gateway", "<p><b>API</b> Gateway</p>"},
		{"<p>a</p>\n  <p>b</p>", "<p>a</p><p>b</p>"},
		{`<span style="font-size:18pt"><b>X</b> Y</span>`, `<p><b><span style="font-size:18pt">X</span></b><span style="font-size:18pt"> Y</span></p>`},
		{`<i><b>x</b></i>`, `<p><b><i>x</i></b></p>`},
		{`<u data-u="sng">x</u>`, `<p><u>x</u></p>`},
		{`<u data-u="dbl">x</u>`, `<p><u data-u="dbl">x</u></p>`},
		{`a<br>b<br/>c`, `<p>a<br/>b<br/>c</p>`},
		{`a<span></span>b`, `<p>ab</p>`},
		{`<b></b>`, `<p><b></b></p>`},
		{`<span style="color:#ff0000">r</span>`, `<p><span style="color:#FF0000" data-color="#FF0000">r</span></p>`},
		{`<span style="color:var(--accent1)">r</span>`, `<p><span style="color:var(--accent1)">r</span></p>`},
		{`<span data-color="accent1 #4472C4">r</span>`, `<p><span style="color:var(--accent1)">r</span></p>`},
		{`<span style="color:#7F7F7F" data-color="accent2 lumMod=75000 #7f7f7f">r</span>`, `<p><span style="color:#7F7F7F" data-color="accent2 lumMod=75000 #7F7F7F">r</span></p>`},
		{`<span data-color="tx1 lumMod=50000 #?">r</span>`, `<p><span data-color="tx1 lumMod=50000 #?">r</span></p>`},
		{`<span style="color:transparent">r</span>`, `<p><span style="color:transparent">r</span></p>`},
		{`<span data-fill="gradFill">r</span>`, `<p><span data-fill="gradFill">r</span></p>`},
		{`<span style="font-family:var(--mj-lt); font-size: 10.555pt;">r</span>`, `<p><span style="font-size:10.56pt;font-family:var(--mj-lt)">r</span></p>`},
		{`<span style="font-family:&quot;Bob's; Font&quot;">r</span>`, `<p><span style="font-family:'Bob\'s; Font'">r</span></p>`},
		{`<span style="font-weight:normal;font-style:normal;text-decoration:none">r</span>`, `<p><span style="font-weight:normal;font-style:normal;text-decoration:none">r</span></p>`},
		{`<p style="text-align:center" data-level="2">x</p>`, `<p style="text-align:center" data-level="2">x</p>`},
		{`<p data-level="1" data-algn="ctr">x</p>`, `<p style="text-align:center">x</p>`},
		{`<p data-algn="dist">x</p>`, `<p data-algn="dist">x</p>`},
		{`<p data-bullet="none">x</p>`, `<p data-bullet="none">x</p>`},
		{`<ul><li data-char="•">a</li></ul><ul><li data-char="-" data-level="2">b</li></ul><ol><li data-type="arabicPeriod">c</li></ol>`,
			`<ul><li data-char="•">a</li><li data-level="2" data-char="-">b</li></ul><ol><li data-type="arabicPeriod">c</li></ol>`},
		{`<p>R&amp;D &lt;x&gt; &#8226; &#x41; &apos;q&apos; a>b</p>`, `<p>R&amp;D &lt;x&gt; • A 'q' a&gt;b</p>`},
	}
	for _, c := range cases {
		got, err := Normalise(c.in)
		if err != nil {
			t.Errorf("Normalise(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("Normalise(%q)\n got %s\nwant %s", c.in, got, c.want)
		}
		again, err := Normalise(got)
		if err != nil || again != got {
			t.Errorf("Normalise not idempotent on %q: %q, %v", got, again, err)
		}
	}
}

// TestParseHTML5Syntax: the input is HTML5, so what HTML lets an author
// write differently means the same as the form read shows: upper-case
// names, end tags HTML makes optional, <br> without a slash, unquoted
// attribute values, named character references and a bare ampersand.
func TestParseHTML5Syntax(t *testing.T) {
	for _, c := range []struct{ in, same string }{
		{"<P>x</P>", "<p>x</p>"},
		{"<p>x", "<p>x</p>"},
		{"<p>a<p>b", "<p>a</p><p>b</p>"},
		{"<p>a<br>b</p>", "<p>a<br/>b</p>"},
		{"<p data-level=2>x</p>", `<p data-level="2">x</p>`},
		{"<p>R&D</p>", "<p>R&amp;D</p>"},
		{"<p>a&nbsp;b &mdash; c</p>", "<p>a\u00a0b \u2014 c</p>"},
		{`<ul><li data-char="-">a<li data-char="-">b</ul>`, `<ul><li data-char="-">a</li><li data-char="-">b</li></ul>`},
		{"<p><B>x</B></p>", "<p><b>x</b></p>"},
	} {
		got, err := Normalise(c.in)
		if err != nil {
			t.Errorf("Normalise(%q): %v", c.in, err)
			continue
		}
		want, err := Normalise(c.same)
		if err != nil {
			t.Fatalf("Normalise(%q): %v", c.same, err)
		}
		if got != want {
			t.Errorf("%q reads as %q, want %q (as %q)", c.in, got, want, c.same)
		}
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct{ in, want string }{
		{"<div>x</div>", "<div>"},
		{"<p class=\"x\">x</p>", "attribute class"},
		{"<p><b>x</p>", "<b>"},
		{"<b>x", "<b> is not closed"},
		{"<p>x</b>", "</b> has no open <b>"},
		{"a < b", "invalid-first-character-of-tag-name"},
		{"<!-- c -->x", "comment"},
		{"x<p>y</p>", "beside block"},
		{"<p>y</p><b>x</b>", "inline <b> beside block"},
		{"<li>x</li>", "<li> outside"},
		{"<ul><li>x</li></ul>", "needs data-char"},
		{"<ol><li data-char=\"•\">x</li></ol>", "attribute data-char"},
		{"<ol><li data-type=\"bogus\">x</li></ol>", "a:ST_TextAutonumberScheme"},
		{`<p data-algn="middle">x</p>`, "a:ST_TextAlignType"},
		{`<u data-u="squiggle">x</u>`, "a:ST_TextUnderlineType"},
		{`<span data-color="sys:notAColour">x</span>`, "a:ST_SystemColorVal"},
		{`<span data-color="prst:reddish">x</span>`, "a:ST_PresetColorVal"},
		{`<span data-color="hsl:21600000,0,0">x</span>`, "a:ST_PositiveFixedAngle"},
		{`<span data-color="accent1 alpha=100001">x</span>`, "a:ST_PositiveFixedPercentage"},
		{`<span data-color="accent1 inv=1">x</span>`, "takes no value"},
		{"<ul><ul><li data-char=\"•\">x</li></ul></ul>", "only <li>"},
		{"<ul></ul>", "empty <ul>"},
		{"<p><p>x</p></p>", "</p> has no open <p>"},
		{`<p style="color:red">x</p>`, "text-align"},
		{`<p style="text-align:middle">x</p>`, "text-align:middle"},
		{`<p data-level="10">x</p>`, "data-level"},
		{`<p data-bullet="char">x</p>`, "data-bullet"},
		{`<span style="font-weight:bold">x</span>`, "font-weight:bold"},
		{`<b><span style="font-weight:normal">x</span></b>`, "font-weight:normal inside <b>"},
		{`<span style="font-size:12px">x</span>`, "font-size:12px"},
		{`<span style="font-size:0.5pt">x</span>`, "out of range"},
		{`<span style="color:red">x</span>`, "color:red"},
		{`<span style="color:var(--accent9)">x</span>`, "a:ST_SchemeColorVal"},
		{`<span style="color:#FF0000" data-color="accent1 lumMod=50000 #00FF00">x</span>`, "disagree"},
		{`<span style="color:#FF0000" data-color="#00FF00">x</span>`, "disagree"},
		{`<span style="color:var(--accent1)" data-color="accent1 lumMod=1 #?">x</span>`, "data-color goes with"},
		{`<span data-color="accent1 frob=1">x</span>`, "not a colour modifier"},
		{`<span data-color="accent1 lumMod">x</span>`, "needs a value"},
		{`<span data-color="accent1" data-fill="gradFill">x</span>`, "both"},
		{`<span data-fill="radial">x</span>`, "data-fill"},
		{`<span style="font-family:Arial">x</span>`, "font-family"},
		{`<span style="font-size:12pt"><span style="font-size:14pt">x</span></span>`, "contradicts"},
		{`<u data-u="dbl"><u>x</u></u>`, "contradicts"},
		{`<u data-u="none">x</u>`, "text-decoration:none"},
		{`<span style="text-decoration:underline">x</span>`, "only none"},
		{`<span style="x:1;x:1">x</span>`, "not in the text grammar"},
		{`<br data-x="1"/>`, "takes no attributes"},
		{`</br>`, "</br>"},
		{"&#1;", "control-character-reference"},
		{"a\x01b", "XML"},
		{`<span title=a>x</span>`, "attribute title"},
		{`<b b="1">x</b>`, "attribute b"},
		{`<p style="text-align:left" style="x">x</p>`, "duplicate HTML attribute style"},
	}
	for _, c := range cases {
		_, err := Parse(c.in)
		if err == nil {
			t.Errorf("Parse(%q): no error, want %q", c.in, c.want)
			continue
		}
		if !errors.Is(err, ErrGrammar) {
			t.Errorf("Parse(%q): %v does not wrap ErrGrammar", c.in, err)
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("Parse(%q): %v, want it to mention %q", c.in, err, c.want)
		}
	}
}
