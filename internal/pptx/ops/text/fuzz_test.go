package text

import (
	"errors"
	"strings"
	"testing"
)

// FuzzParse checks that the HTML text grammar parser never panics or
// hangs, that every refusal wraps ErrGrammar, and that what it accepts
// normalises to a fixed point.
func FuzzParse(f *testing.F) {
	for _, s := range []string{
		"plain text",
		"<p>a<br/>b</p><p style=\"text-align:center\">c</p>",
		"<ul><li data-level=\"2\" data-char=\"-\">x</li></ul><ol><li data-type=\"arabicPeriod\">y</li></ol>",
		"<b><i><u data-u=\"dbl\">x</u></i></b><s>y</s>",
		"<span style=\"font-size:12pt;color:var(--accent1);font-family:'Arial'\">z</span>",
		"<span data-color=\"accent1 lumMod=75000\" data-fill=\"#FF0000\">c</span>",
		"<a href=\"https://example.com\">l</a> &amp; &#x41; &lt;",
		"<p data-bullet=\"none\" data-algn=\"dist\">q</p>",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, h string) {
		b, err := Parse(h)
		if err != nil {
			if !errors.Is(err, ErrGrammar) {
				t.Fatalf("Parse(%q): error %v does not wrap ErrGrammar", h, err)
			}
			return
		}
		got := Render(b)
		again, err := Normalise(got)
		if err != nil {
			t.Fatalf("Parse(%q) accepted, but its rendering %q is refused: %v", h, got, err)
		}
		if again != got {
			t.Fatalf("Normalise not idempotent on %q:\n got %q\nthen %q", h, got, again)
		}
	})
}

// TestParseDepth checks the nesting cap: maxHTMLDepth nested elements
// parse, one more is a grammar error, never a stack overflow.
func TestParseDepth(t *testing.T) {
	nest := func(n int) string {
		return strings.Repeat("<span>", n) + "x" + strings.Repeat("</span>", n)
	}
	if _, err := Parse(nest(maxHTMLDepth)); err != nil {
		t.Fatalf("depth %d: %v", maxHTMLDepth, err)
	}
	for _, h := range []string{nest(maxHTMLDepth + 1), strings.Repeat("<b>", 1<<20)} {
		if _, err := Parse(h); !errors.Is(err, ErrGrammar) || !strings.Contains(err.Error(), "nesting exceeds") {
			t.Fatalf("depth error = %v", err)
		}
	}
}
