package span

import (
	"fmt"
	"hash/fnv"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/corpustest"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/repo"
)

// corpusDir is $DOCX_CORPUS, else corpus/lo of the docxdriver checkout
// beside the main checkout ($DOCXDRIVER_REPO overrides it), or "".
func corpusDir(t *testing.T) string {
	t.Helper()
	if d := os.Getenv("DOCX_CORPUS"); d != "" {
		return d
	}
	dd := os.Getenv("DOCXDRIVER_REPO")
	if dd == "" {
		root, err := repo.Root()
		if err != nil {
			t.Fatal(err)
		}
		if main, ok := repo.MainCheckout(root); ok {
			root = main
		}
		dd = filepath.Join(filepath.Dir(root), "docxdriver")
	}
	d := filepath.Join(dd, "corpus", "lo")
	if _, err := os.Stat(d); err != nil {
		return ""
	}
	return d
}

// TestCorpus: on every corpus document, the span engine's text over the
// lossless working tree equals the read model's (what read and find
// report) for every body paragraph in every view, and Select then Isolate
// on seeded random spans of each paragraph keeps every view's text and
// the formatting projection, with units holding exactly the span and the
// write-set verifier passing.
func TestCorpus(t *testing.T) {
	dir := corpusDir(t)
	if dir == "" {
		t.Skip("no corpus: set DOCX_CORPUS")
	}
	if testing.Short() || raceEnabled {
		t.Skip("corpus test in -short mode or under the race detector (no concurrency to check)")
	}
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".docx") {
			files = append(files, p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	// File by file in parallel, each into tallies of its own, summed in
	// file order. Each file draws its random spans from a generator of its
	// own, seeded by its name, so that what a file checks does not depend
	// on the files before it (or on the shard).
	type tallies struct {
		paras, spans, refused, unopened, unknownSpans int
		refusals, unknown                             map[string]int
	}
	var paras, spans, refused, unopened, unknownSpans int
	refusals := map[string]int{}
	unknown := map[string]int{}
	results := corpustest.Run(t, corpustest.Files(files, dir), func(t *testing.T, file corpustest.File, tl *tallies) {
		tl.refusals, tl.unknown = map[string]int{}, map[string]int{}
		f := file.Path
		seed := fnv.New64a()
		seed.Write([]byte(file.Name))                 //nolint:errcheck // hash writes never fail
		r := rand.New(rand.NewPCG(3, 4^seed.Sum64())) //nolint:gosec // G404: reproducible test spans, not secrets
		b, err := os.ReadFile(f)                      //nolint:gosec // G304: corpus files the test walks
		if err != nil {
			t.Fatal(err)
		}
		rd, err := model.OpenDocument(b)
		if err != nil {
			tl.unopened++ // docxdriver rejects it too (read's both-error-same runs)
			return
		}
		w, wps := workspace(t, b)
		rps := rd.XML.Paragraphs()
		if len(rps) != len(wps) {
			t.Fatalf("%s: %d read paragraphs, %d working", f, len(rps), len(wps))
		}
		for i, rp := range rps {
			tl.paras++
			p := wps[i]
			sp := Read(p)
			for _, v := range []model.View{model.Current, model.Baseline, model.All} {
				if got, want := sp.Text(v), model.ParagraphText(rp.Node, v); got != want {
					t.Fatalf("%s p%d view %d: span text %q, read %q", f, i+1, v, got, want)
				}
			}
			countUnknown(p, tl.unknown)
			// Spans around and over each unknown element: it comes out
			// byte-identical, or the span is refused unchanged.
			for _, k := range unknownOffsets(p) {
				for _, rng := range [][2]int{{k - 1, k + 1}, {k - 1, k}, {k, k + 1}, {0, sp.Len()}} {
					a, c := max(rng[0], 0), min(rng[1], Read(p).Len())
					if a > c {
						continue
					}
					tl.spans++
					name := fmt.Sprintf("%s p%d unknown at %d [%d,%d)", f, i+1, k, a, c)
					if err := checkIsolate(t, name, w.Target(w.DocumentPart(), "paragraph", p), Span{p, a, c}); err != nil {
						tl.refused++
						tl.refusals[err.Error()]++
					}
					tl.unknownSpans++
				}
			}
			for range 2 {
				v := []model.View{model.Current, model.Baseline, model.All}[r.IntN(3)]
				n := runeLen(sp.Text(v))
				a, c := r.IntN(n+1), r.IntN(n+1)
				if a > c {
					a, c = c, a
				}
				s, err := Read(p).Select(v, a, c)
				if err != nil {
					t.Fatal(err)
				}
				tl.spans++
				name := fmt.Sprintf("%s p%d view %d [%d,%d)", f, i+1, v, a, c)
				h := w.Target(w.DocumentPart(), "paragraph", p)
				if err := checkIsolate(t, name, h, s); err != nil {
					tl.refused++
					tl.refusals[err.Error()]++
				}
			}
		}
		if err := w.Verify(); err != nil {
			t.Fatalf("%s: verify: %v", f, err)
		}
	})
	for _, res := range results {
		paras += res.Value.paras
		spans += res.Value.spans
		refused += res.Value.refused
		unopened += res.Value.unopened
		unknownSpans += res.Value.unknownSpans
		for k, v := range res.Value.refusals {
			refusals[k] += v
		}
		for k, v := range res.Value.unknown {
			unknown[k] += v
		}
	}
	t.Logf("%d files (%d not opened), %d paragraphs, %d spans (%d around unknown content), %d refused %v; unknown content elements %v",
		len(results), unopened, paras, spans, unknownSpans, refused, refusals, unknown)
}

// countUnknown counts paragraph-content elements the taxonomy does not
// name, by qualified name.
func countUnknown(p *tree.Node, out map[string]int) {
	var walk func(n *tree.Node)
	walk = func(n *tree.Node) {
		for _, c := range n.Children() {
			if !model.IsElement(c) {
				continue
			}
			switch model.Classify(c) {
			case model.KindUnknown:
				out[c.Name()]++
			case model.KindWrapper:
				if b := model.AlternateContentBranch(c); b != nil {
					walk(b)
				} else if !model.IsLocal(c, "AlternateContent") {
					walk(c)
				}
			}
		}
	}
	walk(p)
}

// unknownOffsets returns the all-view offset at which each unknown
// paragraph-content element of p sits.
func unknownOffsets(p *tree.Node) []int {
	var out []int
	off := 0
	segs := Read(p).Segs
	next := 0
	var walk func(n *tree.Node)
	walk = func(n *tree.Node) {
		for _, c := range n.Children() {
			if !model.IsElement(c) {
				continue
			}
			if next < len(segs) && segs[next].Leaf == c {
				off = segs[next].End
				next++
				continue
			}
			switch model.Classify(c) {
			case model.KindUnknown:
				out = append(out, off)
				// Leaves inside count through it.
				walk(c)
			case model.KindWrapper:
				if model.IsLocal(c, "AlternateContent") {
					if b := model.AlternateContentBranch(c); b != nil {
						walk(b)
					}
					continue
				}
				walk(c)
			}
		}
	}
	walk(p)
	return out
}
