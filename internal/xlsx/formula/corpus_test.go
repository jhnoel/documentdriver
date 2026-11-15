package formula

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/corpustest"
)

// refusalsFile lists every corpus formula Validate refuses, each with why
// it is genuinely malformed. The sweep fails on a refusal not listed here
// and, over the whole corpus, on a listed one that no longer occurs.
const refusalsFile = "testdata/corpus-refusals.tsv"

// sweepResult is one workbook's counts and refusals.
type sweepResult struct {
	counts   map[string]int
	refused  []string // refusal keys, see refusalKey
	reasons  map[string]string
	notLexed []string
}

func refusalKey(file string, f corpusFormula) string {
	return file + "\t" + f.Part + "\t" + f.Where + "\t" + strconv.Quote(f.Text)
}

// TestCorpusFormulas lexes and validates every formula of the xlsx corpus:
// cell formulas (masters of shared and array groups included), defined
// names, conditional-formatting and data-validation formulas, x14
// extension formulas, chart series formulas and table column formulas.
// Every formula must lex back to its text, and every refusal must be
// listed in testdata/corpus-refusals.tsv with its justification.
func TestCorpusFormulas(t *testing.T) {
	_, files := corpusFiles(t)
	allowed := readRefusals(t)
	res := corpustest.Run(t, files, func(_ *testing.T, f corpustest.File, r *sweepResult) {
		cf := readCorpusFile(f.Path)
		r.counts = map[string]int{}
		r.reasons = map[string]string{}
		if cf.Unreadable != "" {
			r.counts["files unreadable"]++
			return
		}
		r.counts["files"]++
		for _, fm := range cf.Formulas {
			r.counts["found "+fm.Kind]++
			if fm.Kind == "shared-child" || fm.Kind == "data-table" || strings.HasPrefix(fm.Kind, "empty ") {
				continue
			}
			text := fm.Text
			if fm.Kind == "external-name" {
				// An externalLink definedName@refersTo is written with a
				// leading "=" (every one in the corpus is).
				text = strings.TrimPrefix(text, "=")
			}
			toks := Lex(text)
			if Join(toks) != text {
				r.notLexed = append(r.notLexed, refusalKey(f.Name, fm))
				continue
			}
			r.counts["lexed"]++
			if err := ValidateTokens(toks); err != nil {
				k := refusalKey(f.Name, fm)
				r.refused = append(r.refused, k)
				r.reasons[k] = err.Error()
				r.counts["refused"]++
				continue
			}
			r.counts["validated"]++
		}
	})
	total := map[string]int{}
	seen := map[string]bool{}
	for _, r := range res {
		for k, n := range r.Value.counts {
			total[k] += n
		}
		for _, k := range r.Value.notLexed {
			t.Errorf("does not lex back: %s", k)
		}
		for _, k := range r.Value.refused {
			seen[k] = true
			if _, ok := allowed[k]; !ok {
				t.Errorf("refused, not in %s: %s\t%s", refusalsFile, k, r.Value.reasons[k])
			}
		}
	}
	if !corpustest.Sharded() {
		for k := range allowed {
			if !seen[k] {
				t.Errorf("listed in %s but no longer refused: %s", refusalsFile, k)
			}
		}
	}
	t.Logf("xlsx formula sweep: %s", formatCounts(total))
}

func formatCounts(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sortStrings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s %d", k, m[k])
	}
	return b.String()
}

// readRefusals reads the justified refusals: file, part, where, quoted
// text, justification; "#" starts a comment line.
func readRefusals(t *testing.T) map[string]string {
	t.Helper()
	f, err := os.Open(filepath.FromSlash(refusalsFile))
	if err != nil {
		t.Fatalf("open refusals: %v", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Errorf("close refusals: %v", err)
		}
	}()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for line := 1; sc.Scan(); line++ {
		s := sc.Text()
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		cols := strings.Split(s, "\t")
		if len(cols) != 5 || strings.TrimSpace(cols[4]) == "" {
			t.Fatalf("%s:%d: want 5 tab-separated columns with a justification", refusalsFile, line)
		}
		out[strings.Join(cols[:4], "\t")] = cols[4]
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read refusals: %v", err)
	}
	return out
}

func sortStrings(s []string) { slices.Sort(s) }
