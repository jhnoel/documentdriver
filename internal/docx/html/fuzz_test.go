package html

import (
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/math"
)

// FuzzParseFragment runs the whole HTML input path (parse, lower, dialect
// state machine), the document split and equations (through the real
// LaTeX converter, so block equations merge) on arbitrary input: it must
// never panic, and must fail only with an error.
func FuzzParseFragment(f *testing.F) {
	for _, s := range []string{
		"<p>a<b>b</b><i>c</i></p>", `<p style="color:#f00"><span style="font-size:12pt">x</span></p>`,
		"<ul><li>a<p>b</p><ol><li>c</ol></li></ul>", "<table><tr><th colspan=2>a<td>b</table>",
		`<p><a href="https://x">l</a><br type="page"><img src="data:x" width=1></p>`,
		"<p><ins>a</ins><del data-revision=\"1\">b</del><mark>c</mark></p>",
		`<p><equation display="inline">\\frac{a}{b}</equation><equation display="block">x</equation></p>`,
		`<p><equation display="block">a \\ b</equation> <equation display="block">\sum_{i}^{n} i</equation></p>`,
		`<p><equation display="inline">\frac{a}{b} &lt; 1</equation><equation display="inline">\bogus</equation></p>`,
		"<p>x<footnote><p>a</p></footnote><comment-start id=\"1\"/>y<comment-end id=\"1\"/></p>",
		`<!DOCTYPE html><html><body><header first><p>h</p></header><p>x</p><section/><p>y</p></body></html>`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		_, err1 := ParseFragment(in, math.LaTeXToOMML)
		_, err2 := ParseDocument(in, math.LaTeXToOMML)
		for _, err := range []error{err1, err2} {
			if err != nil && err.Error() == "" {
				t.Fatalf("empty error for %q", in)
			}
		}
	})
}
