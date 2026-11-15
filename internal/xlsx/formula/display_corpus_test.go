package formula

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/corpustest"
)

// TestCorpusDisplay runs Display over every formula of the xlsx corpus,
// checking that it never panics and that Display(Display(s)) ==
// Display(s), and reports how many times each rewrite fired.
func TestCorpusDisplay(t *testing.T) {
	_, files := corpusFiles(t)
	type counts map[string]int
	res := corpustest.Run(t, files, func(t *testing.T, f corpustest.File, r *counts) {
		*r = counts{}
		cf := readCorpusFile(f.Path)
		for _, fm := range cf.Formulas {
			if fm.Kind == "shared-child" || fm.Kind == "data-table" || strings.HasPrefix(fm.Kind, "empty ") {
				continue
			}
			text := fm.Text
			if fm.Kind == "external-name" {
				text = strings.TrimPrefix(text, "=")
			}
			(*r)["formulas"]++
			out, c := displayWithCounts(text)
			for k, n := range c {
				(*r)[k] += n
			}
			if again := Display(out); again != out {
				t.Errorf("%s %s: Display(%q) = %q is not idempotent: %q", fm.Part, fm.Where, text, out, again)
			}
		}
	})
	total := map[string]int{}
	for _, r := range res {
		for k, n := range r.Value {
			total[k] += n
		}
	}
	t.Logf("corpus display: %s", formatCounts(total))
}
