package math

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/corpustest"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/repo"
)

// fragments returns the equations of a part: every m:oMath and
// m:oMathPara not inside another, in document order.
func fragments(doc *tree.Document) []*tree.Node {
	var out []*tree.Node
	doc.Node().Walk(func(n *tree.Node) bool {
		if isElem(n, "oMath") || isElem(n, "oMathPara") {
			out = append(out, n)
			return false
		}
		return true
	})
	return out
}

// lossTally adds up losses over many equations.
type lossTally map[string]int

func (lt lossTally) add(ls []Loss) {
	for _, l := range ls {
		lt[l.Kind+" "+l.What] += l.Count
	}
}

func (lt lossTally) String() string {
	keys := make([]string, 0, len(lt))
	for k := range lt {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "\n  %s: %d", k, lt[k])
	}
	return b.String()
}

// checkRoundTrip checks the laws on one equation: OMML to LaTeX to OMML
// keeps the model, the LaTeX is canonical, and the OMML written holds
// nothing the grammar drops. It reports whether the equation had losses.
func checkRoundTrip(t *testing.T, label string, n *tree.Node, lt lossTally) bool {
	t.Helper()
	var rd reader
	want := rd.readEquation(n)
	latex, display, losses := OMMLToLaTeX(n)
	lt.add(losses)
	parsed, err := parseEquation(latex, display)
	if err != nil {
		t.Errorf("%s: the LaTeX written does not parse: %v\n%s", label, err, latex)
		return len(losses) > 0
	}
	if keyOf(parsed) != keyOf(want) {
		t.Errorf("%s: LaTeX round trip changes the equation\nlatex %s\n got %s\nwant %s", label, latex, keyOf(parsed), keyOf(want))
	}
	if again := writeEquation(parsed); again != latex {
		t.Errorf("%s: LaTeX not canonical:\n%s\n%s", label, latex, again)
	}
	omml, err := LaTeXToOMML(latex, display)
	if err != nil {
		t.Errorf("%s: %v", label, err)
		return len(losses) > 0
	}
	doc, err := tree.Parse([]byte(omml))
	if err != nil {
		t.Errorf("%s: OMML written is not well formed: %v\n%s", label, err, omml)
		return len(losses) > 0
	}
	var rd2 reader
	back := rd2.readEquation(doc.Root())
	if keyOf(back) != keyOf(want) {
		t.Errorf("%s: OMML round trip changes the equation\nlatex %s\nomml %s\n got %s\nwant %s", label, latex, omml, keyOf(back), keyOf(want))
	}
	if l := rd2.report(); len(l) > 0 {
		t.Errorf("%s: OMML written has losses %v", label, l)
	}
	return len(losses) > 0
}

func TestRoundTripGoldens(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "omml", "*.xml"))
	if err != nil {
		t.Fatal(err)
	}
	lt := lossTally{}
	total, lossy := 0, 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		doc, err := tree.Parse(b)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		for i, n := range fragments(doc) {
			total++
			if checkRoundTrip(t, fmt.Sprintf("%s #%d", filepath.Base(f), i), n, lt) {
				lossy++
			}
		}
	}
	t.Logf("goldens: %d equations, %d with losses:%s", total, lossy, lt)
}

// docxSource is the docxdriver checkout holding test-docs and
// crates/docxdriver-core/tests/fixtures: $DOCXDRIVER_REPO, or the
// checkout beside the repository. docxCorpus is the corpus/lo directory:
// $DOCX_CORPUS (as for tools/oracle and the integrity tests), or corpus/lo
// in that checkout.
func docxSource(t *testing.T) string {
	t.Helper()
	if dir := os.Getenv("DOCXDRIVER_REPO"); dir != "" {
		return dir
	}
	root, err := repo.Root()
	if err != nil {
		t.Skip(err)
	}
	if main, ok := repo.MainCheckout(root); ok {
		root = main
	}
	return filepath.Join(root, "..", "docxdriver")
}

func docxCorpus(src string) string {
	if dir := os.Getenv("DOCX_CORPUS"); dir != "" {
		return dir
	}
	return filepath.Join(src, "corpus", "lo")
}

// TestRoundTripCorpus checks the laws on every equation of the docx
// corpus (skipped when it is absent) and reports what the grammar drops.
func TestRoundTripCorpus(t *testing.T) {
	src := docxSource(t)
	if _, err := os.Stat(src); err != nil {
		t.Skip("no docx corpus at ", src)
	}
	corpus := docxCorpus(src)
	var paths []string
	for _, dir := range []string{filepath.Join(src, "test-docs"), filepath.Join(src, "crates", "docxdriver-core", "tests", "fixtures"), corpus} {
		err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".docx") {
				return err
			}
			paths = append(paths, p)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// Document by document in parallel, each into tallies of its own,
	// summed in walk order.
	type tallies struct {
		lt                  lossTally
		total, lossy, parts int
	}
	files := corpustest.Files(paths, src, corpus)
	lt := lossTally{}
	total, lossy, docs := 0, 0, 0
	for _, r := range corpustest.Run(t, files, func(t *testing.T, f corpustest.File, c *tallies) {
		c.lt = lossTally{}
		p := f.Path
		pkg, err := opc.Open(p)
		if err != nil {
			return // encrypted and broken packages are not equations
		}
		defer pkg.Close()
		for _, name := range pkg.Names() {
			if !strings.HasPrefix(name, "/word/") || !strings.HasSuffix(name, ".xml") {
				continue
			}
			part, err := pkg.Part(name)
			if err != nil {
				t.Fatal(err)
			}
			data, err := part.Original()
			if err != nil {
				continue
			}
			doc, err := tree.Parse(data)
			if err != nil {
				continue
			}
			frags := fragments(doc)
			if len(frags) > 0 {
				c.parts++
			}
			for i, n := range frags {
				c.total++
				if checkRoundTrip(t, fmt.Sprintf("%s %s #%d", p, name, i), n, c.lt) {
					c.lossy++
				}
			}
		}
	}) {
		for k, v := range r.Value.lt {
			lt[k] += v
		}
		total += r.Value.total
		lossy += r.Value.lossy
		docs += r.Value.parts
	}
	t.Logf("corpus: %d equations in %d parts, %d with losses:%s", total, docs, lossy, lt)
}

// TestCanonicalLaTeX checks that parsing and writing is stable: the
// canonical form of any accepted LaTeX parses to the same equation and
// writes to itself.
func TestCanonicalLaTeX(t *testing.T) {
	// latex-inputs.json holds the LaTeX of docxdriver's tests and of the
	// earlier latex2mathml port's golden cases.
	var golden []string
	b, err := os.ReadFile(filepath.Join("testdata", "latex-inputs.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &golden); err != nil {
		t.Fatal(err)
	}
	inputs := append(append([]string(nil), canonicalCases...), golden...)
	accepted := 0
	for _, in := range inputs {
		for _, display := range []bool{false, true} {
			eq, err := parseEquation(in, display)
			if err != nil {
				continue
			}
			accepted++
			checkCanonical(t, in, display, eq)
		}
	}
	t.Logf("%d inputs, %d parses accepted", len(inputs), accepted)
}

func checkCanonical(t *testing.T, in string, display bool, eq equation) {
	t.Helper()
	c := writeEquation(eq)
	again, err := parseEquation(c, display)
	if err != nil {
		t.Errorf("%q: canonical %q does not parse: %v", in, c, err)
		return
	}
	if keyOf(again) != keyOf(eq) {
		t.Errorf("%q: canonical %q parses differently\n got %s\nwant %s", in, c, keyOf(again), keyOf(eq))
	}
	if c2 := writeEquation(again); c2 != c {
		t.Errorf("%q: canonical form not stable: %q then %q", in, c, c2)
	}
	omml := writeOMML(eq)
	doc, err := tree.Parse([]byte(omml))
	if err != nil {
		t.Errorf("%q: OMML not well formed: %v\n%s", in, err, omml)
		return
	}
	var rd reader
	if back := rd.readEquation(doc.Root()); keyOf(back) != keyOf(eq) {
		t.Errorf("%q: LaTeX to OMML to model changes it\n got %s\nwant %s", in, keyOf(back), keyOf(eq))
	}
}
