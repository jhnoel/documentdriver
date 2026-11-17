package read

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/corpustest"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/repo"
)

// fuzzInvalidDocuments are the 8 POI clusterfuzz files of the corpus
// that are not valid zips and must each give invalid_document, by name.
var fuzzInvalidDocuments = []string{
	"clusterfuzz-testcase-minimized-POIVisioFuzzer-4513310052515840.vsdx",
	"clusterfuzz-testcase-minimized-POIVisioFuzzer-5026516754628608.vsdx",
	"clusterfuzz-testcase-minimized-POIVisioFuzzer-5313947071217664.vsdx",
	"clusterfuzz-testcase-minimized-POIVisioFuzzer-5492358185353216.vsdx",
	"clusterfuzz-testcase-minimized-POIVisioFuzzer-5842659694215168.vsdx",
	"clusterfuzz-testcase-minimized-POIVisioFuzzer-5981064948219904.vsdx",
	"clusterfuzz-testcase-minimized-POIVisioFuzzer-6312416206454784.vsdx",
	"clusterfuzz-testcase-minimized-POIVisioFuzzer-6358126418591744.vsdx",
}

// classifiedInvalidDocuments are the corpus files besides the fuzz files
// that give invalid_document, each with its inspected reason. A file is
// reviewed here, as xlsx's classified table is. Any other file giving
// invalid_document fails the test until it is added with its reason.
var classifiedInvalidDocuments = map[string]string{
	"recursion-cycle.vsdx":  "minimal libvisio file with no [Content_Types].xml: opc cannot resolve content types",
	"tab-short-prefix.vsdx": "minimal libvisio file with no [Content_Types].xml: opc cannot resolve content types",
}

// vsdxCorpus returns the vsdx corpus directory: VSDX_CORPUS (directly, below
// corpus/vsdx when VSDX_CORPUS is a worktree root, or the vsdx directory of the
// repository corpus when unset), or "" when there is none.
func vsdxCorpus() string {
	if d := os.Getenv("VSDX_CORPUS"); d != "" {
		for _, cand := range []string{d, filepath.Join(d, "corpus", "vsdx"), filepath.Join(d, "vsdx")} {
			if _, err := os.Stat(filepath.Join(cand, "repos")); err == nil {
				return cand
			}
		}
		if _, err := os.Stat(d); err == nil {
			return d
		}
		return ""
	}
	dir, err := repo.Corpus()
	if err != nil {
		return ""
	}
	dir = filepath.Join(dir, "vsdx")
	if _, err := os.Stat(dir); err != nil {
		return ""
	}
	return dir
}

// corpusDrawings lists the drawings under dir in traversal order, sorted:
// the Visio extensions the design names for the corpus sweep.
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

// corpusTimeout is the per-file limit of the sweep.
func corpusTimeout() time.Duration {
	gate := time.Minute
	if laws.RaceEnabled {
		gate = 10 * time.Minute
	}
	return gate
}

// corpusCounts are one file's sweep tally.
type corpusCounts struct {
	// outcome is "ok" or "invalid_document".
	outcome string
	// err is the Document error of an invalid_document file.
	err string
	// notes counts every note of the summary and every page read.
	notes int
	// pages and masters count the summary entries.
	pages int
	// masters counts the summary masters.
	masters int
	// objects counts the objects of every page read.
	objects int
	// unreadable counts pages and masters that read as unreadable_target.
	unreadable int
	// limit reports a safety limit trip (opc.ErrLimit).
	limit bool
	// timedOut reports a sweep that did not finish in corpusTimeout.
	timedOut bool
}

// sweepResult is one file's tally with its own problems.
type sweepResult struct {
	counts   corpusCounts
	problems []string
}

// sweepDrawings reads the summary and every page and master of the
// drawing in input: a Document error is invalid_document, a page or
// master that cannot be read is unreadable_target (counted, not a
// failure), and any other page error, a limit trip or a panic is a
// problem.
func sweepDrawings(input []byte) sweepResult {
	var out sweepResult
	out.counts.outcome = "ok"
	defer func() {
		if e := recover(); e != nil {
			out.problems = append(out.problems, fmt.Sprintf("panic: %v", e))
		}
	}()
	sum, err := Document(input, "B")
	if err != nil {
		out.counts.outcome = "invalid_document"
		out.counts.err = err.Error()
		if errors.Is(err, opc.ErrLimit) {
			out.counts.limit = true
			out.problems = append(out.problems, fmt.Sprintf("limit trip: %v", err))
		}
		return out
	}
	out.counts.notes += len(sum.Notes)
	out.counts.pages = len(sum.Pages)
	out.counts.masters = len(sum.Masters)
	for _, p := range sum.Pages {
		notes, objects, unread, probs := sweepPage(input, p.ID)
		out.counts.notes += notes
		out.counts.objects += objects
		if unread {
			out.counts.unreadable++
		}
		out.problems = append(out.problems, probs...)
	}
	for _, m := range sum.Masters {
		notes, objects, unread, probs := sweepPage(input, m.ID)
		out.counts.notes += notes
		out.counts.objects += objects
		if unread {
			out.counts.unreadable++
		}
		out.problems = append(out.problems, probs...)
	}
	return out
}

// sweepPage reads the page or master id of input, tallying its object
// and note counts. An unreadable_target page is counted as unreadable
// (not a problem); any other error, a limit trip or unparseable output
// is a problem.
func sweepPage(input []byte, id string) (notes, objects int, unread bool, problems []string) {
	b, err := Page(input, "B", id, Options{})
	if err != nil {
		var ce *cli.Error
		if errors.As(err, &ce) && ce.Code == cli.CodeUnreadableTarget {
			return 0, 0, true, nil
		}
		if errors.Is(err, opc.ErrLimit) {
			problems = append(problems, fmt.Sprintf("%s: limit trip: %v", id, err))
			return 0, 0, false, problems
		}
		problems = append(problems, fmt.Sprintf("%s: %v", id, err))
		return 0, 0, false, problems
	}
	var v struct {
		Objects []json.RawMessage `json:"objects"`
		Notes   []Note            `json:"notes"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		problems = append(problems, fmt.Sprintf("%s: page JSON: %v", id, err))
		return 0, 0, false, problems
	}
	return len(v.Notes), len(v.Objects), false, nil
}

// TestReadCorpus checks the L1c corpus row: over the 65 VSDX_CORPUS
// drawings the summary and every page and master of every file read
// with no panic, hang or limit trip; the 8 POI clusterfuzz files each
// give invalid_document by name, any other invalid_document fails
// unless it is listed in classifiedInvalidDocuments; outcome counts
// are logged. It skips without VSDX_CORPUS.
func TestReadCorpus(t *testing.T) {
	dir := vsdxCorpus()
	if dir == "" {
		t.Skip("no vsdx corpus (set VSDX_CORPUS)")
	}
	paths := corpusDrawings(dir)
	if len(paths) == 0 {
		t.Skip("no vsdx corpus drawings under VSDX_CORPUS")
	}
	files := corpustest.Files(paths, dir)
	if len(corpustest.Shard(t, files)) == 0 {
		t.Skip("no corpus drawings in this CORPUS_SHARD")
	}
	res := corpustest.Run(t, files, func(t *testing.T, f corpustest.File, r *corpusCounts) {
		input, err := os.ReadFile(f.Path)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), corpusTimeout())
		defer cancel()
		done := make(chan sweepResult, 1)
		go func() { done <- sweepDrawings(input) }()
		select {
		case s := <-done:
			*r = s.counts
			for _, msg := range s.problems {
				t.Error(msg)
			}
		case <-ctx.Done():
			r.timedOut = true
			r.outcome = "timed-out"
			t.Errorf("timed out after %v", corpusTimeout())
		}
	})
	if corpustest.Sharded() {
		return
	}
	fuzz := make(map[string]bool, len(fuzzInvalidDocuments))
	for _, name := range fuzzInvalidDocuments {
		fuzz[name] = true
	}
	var ok, invalid, notes, pages, masters, objects, unread, limits, timeouts int
	var invalidNames []string
	outcomeByBase := map[string]string{}
	for _, r := range res {
		if !r.Ran {
			continue
		}
		outcomeByBase[filepath.Base(r.Name)] = r.Value.outcome
		switch r.Value.outcome {
		case "invalid_document":
			invalid++
			invalidNames = append(invalidNames, r.Name)
		case "ok":
			ok++
		}
		notes += r.Value.notes
		pages += r.Value.pages
		masters += r.Value.masters
		objects += r.Value.objects
		unread += r.Value.unreadable
		if r.Value.limit {
			limits++
		}
		if r.Value.timedOut {
			timeouts++
		}
	}
	t.Logf("vsdx read corpus: %d files, %d ok, %d invalid_document %v, %d pages, %d masters, %d objects, %d notes, %d unreadable_target, %d limit trips, %d timeouts",
		len(res), ok, invalid, invalidNames, pages, masters, objects, notes, unread, limits, timeouts)
	if len(files) != 65 {
		t.Errorf("corpus has %d drawings, want 65", len(files))
	}
	for _, name := range fuzzInvalidDocuments {
		got, found := outcomeByBase[name]
		if !found {
			t.Errorf("fuzz file %s not in corpus", name)
			continue
		}
		if got != "invalid_document" {
			t.Errorf("fuzz file %s: outcome %q, want invalid_document", name, got)
		}
	}
	for _, r := range res {
		if !r.Ran || r.Value.outcome != "invalid_document" {
			continue
		}
		base := filepath.Base(r.Name)
		if fuzz[base] {
			continue
		}
		if _, listed := classifiedInvalidDocuments[base]; listed {
			continue
		}
		t.Errorf("unclassified invalid_document: %s: %s; inspect it and list it in classifiedInvalidDocuments", r.Name, r.Value.err)
	}
}
