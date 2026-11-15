package math

import (
	"errors"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// omml strips the namespace declaration from LaTeXToOMML output.
func omml(t *testing.T, latex string, display bool) string {
	t.Helper()
	out, err := LaTeXToOMML(latex, display)
	if err != nil {
		t.Fatalf("%q: %v", latex, err)
	}
	return strings.Replace(out, ` xmlns:m="`+OMMLNS+`"`, "", 1)
}

func TestLaTeXToOMML(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{`x^{2}`, `<m:oMath><m:sSup><m:e><m:r><m:t>x</m:t></m:r></m:e><m:sup><m:r><m:t>2</m:t></m:r></m:sup></m:sSup></m:oMath>`},
		{`x^{a}_{b}`, `<m:oMath><m:sSubSup><m:e><m:r><m:t>x</m:t></m:r></m:e><m:sub><m:r><m:t>b</m:t></m:r></m:sub><m:sup><m:r><m:t>a</m:t></m:r></m:sup></m:sSubSup></m:oMath>`},
		{`\frac{a}{b}`, `<m:oMath><m:f><m:num><m:r><m:t>a</m:t></m:r></m:num><m:den><m:r><m:t>b</m:t></m:r></m:den></m:f></m:oMath>`},
		{`\sum_{i=1}^{n} x_{i}+y`, `<m:oMath><m:nary><m:naryPr><m:chr m:val="` + "\u2211" + `"/></m:naryPr><m:sub><m:r><m:t>i=1</m:t></m:r></m:sub><m:sup><m:r><m:t>n</m:t></m:r></m:sup><m:e><m:sSub><m:e><m:r><m:t>x</m:t></m:r></m:e><m:sub><m:r><m:t>i</m:t></m:r></m:sub></m:sSub></m:e></m:nary><m:r><m:t>+y</m:t></m:r></m:oMath>`},
		{`\sum^{n}{x}`, `<m:oMath><m:nary><m:naryPr><m:chr m:val="` + "\u2211" + `"/><m:subHide m:val="1"/></m:naryPr><m:sub></m:sub><m:sup><m:r><m:t>n</m:t></m:r></m:sup><m:e><m:r><m:t>x</m:t></m:r></m:e></m:nary></m:oMath>`},
		{`\sin{x}`, `<m:oMath><m:func><m:fName><m:r><m:rPr><m:sty m:val="p"/></m:rPr><m:t>sin</m:t></m:r></m:fName><m:e><m:r><m:t>x</m:t></m:r></m:e></m:func></m:oMath>`},
		{`\lim_{n}{a}`, `<m:oMath><m:func><m:fName><m:limLow><m:e><m:r><m:rPr><m:sty m:val="p"/></m:rPr><m:t>lim</m:t></m:r></m:e><m:lim><m:r><m:t>n</m:t></m:r></m:lim></m:limLow></m:fName><m:e><m:r><m:t>a</m:t></m:r></m:e></m:func></m:oMath>`},
		{`\mathbb{R}\mathbf{v}\text{if }x`, `<m:oMath><m:r><m:rPr><m:scr m:val="double-struck"/></m:rPr><m:t>R</m:t></m:r><m:r><m:rPr><m:sty m:val="b"/></m:rPr><m:t>v</m:t></m:r><m:r><m:rPr><m:nor/></m:rPr><m:t xml:space="preserve">if </m:t></m:r><m:r><m:t>x</m:t></m:r></m:oMath>`},
		{`a\,b\quad c\!d`, "<m:oMath><m:r><m:t>a\u2009b\u2003cd</m:t></m:r></m:oMath>"},
		{`x<y\&z`, `<m:oMath><m:r><m:t>x&lt;y&amp;z</m:t></m:r></m:oMath>`},
		{`\begin{aligned}a &= b \\ c &= d\end{aligned}`, `<m:oMath><m:eqArr><m:e><m:r><m:t>a&amp;=b</m:t></m:r></m:e><m:e><m:r><m:t>c&amp;=d</m:t></m:r></m:e></m:eqArr></m:oMath>`},
		{`\begin{pmatrix}1 & 0\end{pmatrix}`, `<m:oMath><m:d><m:e><m:m><m:mr><m:e><m:r><m:t>1</m:t></m:r></m:e><m:e><m:r><m:t>0</m:t></m:r></m:e></m:mr></m:m></m:e></m:d></m:oMath>`},
		{`\begin{split}a &= b \\ &= c\end{split}`, `<m:oMath><m:r><m:t>a</m:t></m:r><m:r><m:rPr><m:aln/></m:rPr><m:t>=b</m:t></m:r><m:r><m:rPr><m:brk/><m:aln/></m:rPr><m:t>=c</m:t></m:r></m:oMath>`},
		{`\underbrace{a}_{n}`, `<m:oMath><m:limLow><m:e><m:groupChr><m:groupChrPr><m:chr m:val="` + "\u23df" + `"/><m:pos m:val="bot"/><m:vertJc m:val="top"/></m:groupChrPr><m:e><m:r><m:t>a</m:t></m:r></m:e></m:groupChr></m:e><m:lim><m:r><m:t>n</m:t></m:r></m:lim></m:limLow></m:oMath>`},
		{`\phantom{x}`, `<m:oMath><m:phant><m:phantPr><m:show m:val="0"/></m:phantPr><m:e><m:r><m:t>x</m:t></m:r></m:e></m:phant></m:oMath>`},
	} {
		if got := omml(t, c.in, false); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.in, got, c.want)
		}
	}
	if got := omml(t, `a=b \\ c=d`, true); got != `<m:oMathPara><m:oMath><m:r><m:t>a=b</m:t></m:r></m:oMath><m:oMath><m:r><m:t>c=d</m:t></m:r></m:oMath></m:oMathPara>` {
		t.Errorf("display lines: %s", got)
	}
}

func TestOMMLToLaTeX(t *testing.T) {
	m := `xmlns:m="` + OMMLNS + `" xmlns:w="` + wordNS + `"`
	for _, c := range []struct{ in, want string }{
		{`<m:oMathPara ` + m + `><m:oMath><m:r><m:t>a=b</m:t></m:r></m:oMath><m:oMath><m:r><m:t>c=d</m:t></m:r></m:oMath></m:oMathPara>`, `a=b \\ c=d`},
		{`<m:oMath ` + m + `><m:r><m:t>a=b</m:t><w:br/><m:t>c=d</m:t></m:r></m:oMath>`, `\begin{split}a=b \\ c=d\end{split}`},
		{`<m:oMath ` + m + `><m:r><m:t>a</m:t></m:r><m:r><m:rPr><m:brk m:alnAt="1"/></m:rPr><m:t>=b</m:t></m:r></m:oMath>`, `\begin{split}a \\\docxalnat{1} =b\end{split}`},
		{`<m:oMath ` + m + `><m:eqArr><m:e><m:r><m:t>a&amp;=b</m:t></m:r></m:e><m:e><m:r><m:t>c&amp;=d</m:t></m:r></m:e></m:eqArr></m:oMath>`, `\begin{aligned}a&=b \\ c&=d\end{aligned}`},
		{`<m:oMath ` + m + `><m:r><m:t xml:space="preserve">a	b` + "\n" + `c&#13;d  e</m:t></m:r></m:oMath>`, `a\unicode{x9}b\unicode{xA}c\unicode{xD}d\ \ e`},
		{`<m:oMath ` + m + `><m:groupChr><m:groupChrPr><m:pos m:val="top"/></m:groupChrPr><m:e><m:r><m:t>x</m:t></m:r></m:e></m:groupChr></m:oMath>`, `\docxovergroup{` + "\u23df" + `}{x}`},
		{`<m:oMath ` + m + `><m:bar><m:e><m:r><m:t>x</m:t></m:r></m:e></m:bar></m:oMath>`, `\underline{x}`},
		{`<m:oMath ` + m + `><m:d><m:dPr><m:begChr m:val=""/></m:dPr><m:e><m:r><m:t>x</m:t></m:r></m:e><m:e><m:r><m:t>y</m:t></m:r></m:e></m:d></m:oMath>`, `\left.x\middle|y\right)`},
	} {
		doc, err := tree.Parse([]byte(c.in))
		if err != nil {
			t.Fatal(err)
		}
		got, _, losses := OMMLToLaTeX(doc.Root())
		if got != c.want || len(losses) > 0 {
			t.Errorf("%s:\n got %s %v\nwant %s", c.in, got, losses, c.want)
		}
	}
}

func TestLaTeXErrors(t *testing.T) {
	for _, c := range []struct {
		in      string
		display bool
		want    string
	}{
		{`a \\ b`, false, `only display math`},
		{`\frac{a}{b \\ c}`, true, `\\ is only allowed`},
		{`a & b`, false, `& is only allowed`},
		{`\foo`, false, `unknown command \foo; supported commands: `},
		{`x^`, false, `missing argument`},
		{`x^{a}^{b}`, false, `double superscript`},
		{`x_{a}_{b}`, false, `double subscript`},
		{`}`, false, `unexpected '}'`},
		{`\frac{`, false, `missing }`},
		{`\right)`, false, `\right without its \left`},
		{`\left(x`, false, `missing \right`},
		{`\begin{array}x\end{array}`, false, `unknown environment array`},
		{`\begin{matrix}x\end{pmatrix}`, false, `ended by`},
		{"a\x00b", false, `U+0000`},
		{`\text{\alpha}`, false, `not allowed in \text`},
		{`\frac{\begin{split}a\end{split}}{b}`, false, `split is only allowed`},
		{strings.Repeat("{", maxLaTeXDepth+1), false, `nested more than`},
	} {
		_, err := LaTeXToOMML(c.in, c.display)
		if err == nil || !errors.Is(err, ErrInvalidLaTeX) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: got %v, want an error with %q", c.in, err, c.want)
		}
	}
}

func TestSplitDelimiters(t *testing.T) {
	for _, c := range []struct {
		in      string
		latex   string
		display bool
	}{
		{`$x$`, "x", false}, {`$$x$$`, "x", true}, {`\(x\)`, "x", false}, {`\[x\]`, "x", true}, {` $a\$b$ `, `a\$b`, false},
	} {
		l, d, err := SplitDelimiters(c.in)
		if err != nil || l != c.latex || d != c.display {
			t.Errorf("%q: got %q %v %v", c.in, l, d, err)
		}
		if back := Delimit(l, d); d && back != "$$"+l+"$$" || !d && back != "$"+l+"$" {
			t.Errorf("Delimit(%q): %q", l, back)
		}
	}
	for _, in := range []string{"x", "$x", "$a$b$"} {
		if _, _, err := SplitDelimiters(in); err == nil {
			t.Errorf("%q: accepted", in)
		}
	}
}
