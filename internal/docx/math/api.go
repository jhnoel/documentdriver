package math

import (
	"errors"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// OMMLToLaTeX writes an m:oMath or m:oMathPara element in the grammar's
// canonical LaTeX (without $ delimiters). display is true for an
// m:oMathPara, whose m:oMath elements are the lines, separated by \\.
// losses lists what the element holds that the grammar cannot carry; it
// is empty when the conversion is lossless.
func OMMLToLaTeX(n *tree.Node) (latex string, display bool, losses []Loss) {
	var rd reader
	eq := rd.readEquation(n)
	return writeEquation(eq), eq.display, rd.report()
}

// LaTeXToOMML compiles LaTeX in the grammar (without $ delimiters) to
// Office Math: an m:oMathPara with one m:oMath per line when display is
// set, otherwise an m:oMath, declaring xmlns:m. Malformed LaTeX and
// unknown commands are errors wrapping ErrInvalidLaTeX.
func LaTeXToOMML(latex string, display bool) (string, error) {
	eq, err := parseEquation(latex, display)
	if err != nil {
		return "", err
	}
	return writeOMML(eq), nil
}

// CanonicalLaTeX parses LaTeX in the grammar and writes it back in the
// canonical form OMMLToLaTeX writes.
func CanonicalLaTeX(latex string, display bool) (string, error) {
	eq, err := parseEquation(latex, display)
	if err != nil {
		return "", err
	}
	return writeEquation(eq), nil
}

// errDelimiters is returned for text that is not one delimited equation.
var errDelimiters = errors.New("an equation is $...$ or \\(...\\) inline, or $$...$$ or \\[...\\] display")

// SplitDelimiters removes the math delimiters around an equation:
// $...$ and \(...\) are inline, $$...$$ and \[...\] display.
func SplitDelimiters(s string) (latex string, display bool, err error) {
	t := strings.TrimSpace(s)
	for _, d := range []struct {
		open, close string
		display     bool
	}{{"$$", "$$", true}, {`\[`, `\]`, true}, {`\(`, `\)`, false}, {"$", "$", false}} {
		if len(t) >= len(d.open)+len(d.close) && strings.HasPrefix(t, d.open) && strings.HasSuffix(t, d.close) {
			body := t[len(d.open) : len(t)-len(d.close)]
			if d.open == "$" && strings.Contains(strings.ReplaceAll(body, `\$`, ""), "$") {
				break
			}
			return body, d.display, nil
		}
	}
	return "", false, errDelimiters
}

// Delimit wraps LaTeX in $...$ (inline) or $$...$$ (display).
func Delimit(latex string, display bool) string {
	if display {
		return "$$" + latex + "$$"
	}
	return "$" + latex + "$"
}
