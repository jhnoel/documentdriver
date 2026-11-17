// Package vsdxgrammar checks the cross-grammar law X1
// (docs/vsdx-design.md, section 8): every html vsdx reads on the
// fixtures and the corpus, with <opaque> placeholders removed, is
// accepted by pptx text.Parse and Normalise of it is itself. The
// package is test-only: it may import both formats, as tools/ may.
package vsdxgrammar

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/corpustest"
	pptxtext "github.com/jhnoel/documentdriver/internal/pptx/ops/text"
	"github.com/jhnoel/documentdriver/internal/vsdx/read"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// opaqueRe matches one <opaque ...></opaque> placeholder: data-text
// is escaped (&gt; for '>'), so [^>]* never stops inside the open
// tag and the element holds no nested markup.
var opaqueRe = regexp.MustCompile(`<opaque[^>]*></opaque>`)

// stripOpaque removes every <opaque> placeholder from h.
func stripOpaque(h string) string {
	return opaqueRe.ReplaceAllString(h, "")
}

// pageView is the part of a read.Page result the law reads.
type pageView struct {
	Objects []struct {
		ID   string `json:"id"`
		HTML string `json:"html"`
	} `json:"objects"`
}

// checkHTML checks one html against the law: stripped of placeholders
// it parses and Normalise of it is itself. where names the file and
// object for the failure report. It reports failure by t.Errorf and
// returns whether the html passed.
func checkHTML(t *testing.T, where, html string) bool {
	t.Helper()
	h := stripOpaque(html)
	if _, err := pptxtext.Parse(h); err != nil {
		t.Errorf("%s: stripped html does not parse: %v\n html %q", where, err, h)
		return false
	}
	norm, err := pptxtext.Normalise(h)
	if err != nil {
		t.Errorf("%s: Normalise: %v\n html %q", where, err, h)
		return false
	}
	if norm != h {
		t.Errorf("%s: not a fixed point of Normalise\n html %q\n norm %q", where, h, norm)
		return false
	}
	return true
}

// checkPage reads page addr of input and checks every html it holds,
// returning the checked count and the failure count. A read or decode
// failure is returned as err with the caller to classify it.
func checkPage(t *testing.T, input []byte, addr, label string) (checked, failures int, err error) {
	t.Helper()
	b, err := read.Page(input, "B", addr, read.Options{})
	if err != nil {
		return 0, 0, err
	}
	var v pageView
	if err := json.Unmarshal(b, &v); err != nil {
		return 0, 0, err
	}
	for _, o := range v.Objects {
		if o.HTML == "" {
			continue
		}
		checked++
		if !checkHTML(t, label+" "+o.ID, o.HTML) {
			failures++
		}
	}
	return checked, failures, nil
}

// pageAddrs returns every address Document names: its pages in
// order, then its masters.
func pageAddrs(sum *read.Summary) []string {
	addrs := make([]string, 0, len(sum.Pages)+len(sum.Masters))
	for _, p := range sum.Pages {
		addrs = append(addrs, p.ID)
	}
	for _, m := range sum.Masters {
		addrs = append(addrs, m.ID)
	}
	return addrs
}

// TestGrammarFixtures checks the X1 fixture rows: every html of V1
// (v1-minimal), V2 (v2-master), V5 (v5-glue) and V7 (v7-text), with
// placeholders removed, parses and is a fixed point of Normalise.
// The want counts keep the law non-vacuous: they are the numbers of
// html the fixtures hold (V5 holds none).
func TestGrammarFixtures(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		want    int
	}{
		{"v1-minimal", 1},
		{"v2-master", 6},
		{"v5-glue", 0},
		{"v7-text", 1},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			input, err := os.ReadFile(vsdxtest.Fixture(t, tc.fixture))
			if err != nil {
				t.Fatalf("read fixture %s: %v", tc.fixture, err)
			}
			sum, err := read.Document(input, "B")
			if err != nil {
				t.Fatalf("Document(%s): %v", tc.fixture, err)
			}
			checked := 0
			for _, addr := range pageAddrs(sum) {
				n, _, err := checkPage(t, input, addr, tc.fixture)
				if err != nil {
					t.Fatalf("%s %s: %v", tc.fixture, addr, err)
				}
				checked += n
			}
			if checked != tc.want {
				t.Errorf("%s: checked %d html, want %d", tc.fixture, checked, tc.want)
			}
		})
	}
}

// grammarCounts are one corpus file's tally.
type grammarCounts struct {
	// checked counts the html checked.
	checked int
	// failures counts the html that failed the law.
	failures int
	// invalid counts files with no Document (invalid_document, no html).
	invalid int
	// unreadable counts pages that read as unreadable_target (no html).
	unreadable int
}

// corpusDir resolves VSDX_CORPUS: the worktree root (corpus under
// corpus/vsdx/repos), the corpus directory itself, or a repos/
// directory directly. It returns "" when the variable is unset.
func corpusDir() string {
	d := os.Getenv("VSDX_CORPUS")
	if d == "" {
		return ""
	}
	for _, cand := range []string{d, filepath.Join(d, "corpus", "vsdx"), filepath.Join(d, "vsdx")} {
		if _, err := os.Stat(filepath.Join(cand, "repos")); err == nil {
			return cand
		}
	}
	return ""
}

// corpusDrawings lists the drawings under dir in traversal order, sorted.
func corpusDrawings(dir string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".vsdx", ".vsdm", ".vstx", ".vstm", ".vssx", ".vssm":
			out = append(out, p)
		}
		return nil
	})
	slices.Sort(out)
	return out
}

// TestGrammarCorpus checks the X1 corpus row: over VSDX_CORPUS, every
// page's html, with placeholders removed, parses and is a fixed point
// of Normalise, failures listed by file and object. It skips without
// VSDX_CORPUS; with it set, an unresolvable or empty corpus fails.
// Files with no Document (invalid_document) and unreadable_target
// pages hold no html and are counted, not failed.
func TestGrammarCorpus(t *testing.T) {
	if os.Getenv("VSDX_CORPUS") == "" {
		t.Skip("no vsdx corpus (set VSDX_CORPUS)")
	}
	dir := corpusDir()
	if dir == "" {
		t.Fatalf("VSDX_CORPUS=%s: no corpus with repos/ under it", os.Getenv("VSDX_CORPUS"))
	}
	paths := corpusDrawings(dir)
	if len(paths) == 0 {
		t.Fatalf("no vsdx corpus drawings under %s", dir)
	}
	files := corpustest.Files(paths, dir)
	if len(corpustest.Shard(t, files)) == 0 {
		t.Skip("no corpus drawings in this CORPUS_SHARD")
	}
	res := corpustest.Run(t, files, func(t *testing.T, f corpustest.File, r *grammarCounts) {
		input, err := os.ReadFile(f.Path)
		if err != nil {
			t.Fatal(err)
		}
		sum, err := read.Document(input, "B")
		if err != nil {
			r.invalid++
			return
		}
		for _, addr := range pageAddrs(sum) {
			checked, failures, err := checkPage(t, input, addr, f.Name)
			if err != nil {
				var ce *cli.Error
				if errors.As(err, &ce) && ce.Code == cli.CodeUnreadableTarget {
					r.unreadable++
					continue
				}
				t.Errorf("%s %s: %v", f.Name, addr, err)
				continue
			}
			r.checked += checked
			r.failures += failures
		}
	})
	if corpustest.Sharded() {
		return
	}
	checked, failures, invalid, unreadable := 0, 0, 0, 0
	for _, r := range res {
		if !r.Ran {
			continue
		}
		checked += r.Value.checked
		failures += r.Value.failures
		invalid += r.Value.invalid
		unreadable += r.Value.unreadable
	}
	t.Logf("vsdx grammar corpus: %d files, %d html checked, %d failures, %d invalid_document, %d unreadable_target",
		len(res), checked, failures, invalid, unreadable)
}
