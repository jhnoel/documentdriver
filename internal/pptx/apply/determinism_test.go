package apply

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jhnoel/documentdriver/internal/corpustest"
	"github.com/jhnoel/documentdriver/internal/ids"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

var testClock = time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

// planRun is one application of a bench plan: the output deck and the
// edit result JSON.
type planRun struct{ deck, result []byte }

func runPlan(t *testing.T, input, plan string, src ids.Source) planRun {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out.pptx")
	text, err := os.ReadFile(plan)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(input, text, Options{Out: out, IDs: src})
	if err != nil {
		t.Fatalf("%s: %v", filepath.Base(plan), err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	res.Saved = "" // the temp path differs per run
	j, err := res.JSON()
	if err != nil {
		t.Fatal(err)
	}
	return planRun{b, j}
}

var guidRE = regexp.MustCompile(`\{[0-9A-F]{8}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{12}\}`)

// parts reads every part of a deck with GUIDs replaced by {G}, and
// reports how many GUIDs it saw.
func parts(t *testing.T, deck []byte) (map[string]string, int) {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(deck), int64(len(deck)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	n := 0
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		n += len(guidRE.FindAll(b, -1))
		out[f.Name] = string(guidRE.ReplaceAll(b, []byte("{G}")))
	}
	return out, n
}

// TestDeterministicBenchPlans applies every plan under eval/plans twice
// with the same seed and clock and requires byte-identical decks and edit
// results; with another seed only GUIDs may differ.
func TestDeterministicBenchPlans(t *testing.T) {
	root := testdeck.Root()
	corpus := os.Getenv("PPTX_CORPUS")
	if corpus == "" {
		t.Skip("PPTX_CORPUS not set")
	}
	plans, err := filepath.Glob(filepath.Join(root, "eval", "plans", "*.json"))
	if err != nil || len(plans) == 0 {
		t.Fatalf("no bench plans: %v", err)
	}
	// Plan by plan in parallel; each reports whether it wrote a GUID.
	sawGUID := false
	files := corpustest.Files(plans, filepath.Join(root, "eval", "plans"))
	for _, r := range corpustest.Run(t, files, func(t *testing.T, f corpustest.File, wroteGUID *bool) {
		plan := f.Path
		id := strings.TrimSuffix(filepath.Base(plan), ".json")
		tb, err := os.ReadFile(filepath.Join(root, "eval", "tasks", id, "task.json"))
		if err != nil {
			t.Fatal(err)
		}
		var task struct {
			Deck struct{ Path string }
		}
		if err := json.Unmarshal(tb, &task); err != nil {
			t.Fatal(err)
		}
		input := filepath.Join(corpus, filepath.FromSlash(task.Deck.Path))
		if _, err := os.Stat(input); err != nil {
			t.Skipf("deck not in corpus: %v", err)
		}
		src := &countingSource{Source: ids.Fixed(1, testClock)}
		a := runPlan(t, input, plan, src)
		b := runPlan(t, input, plan, ids.Fixed(1, testClock))
		if !bytes.Equal(a.deck, b.deck) {
			t.Errorf("same seed: output decks differ")
		}
		if !bytes.Equal(a.result, b.result) {
			t.Errorf("same seed: edit results differ")
		}
		c := runPlan(t, input, plan, ids.Fixed(2, testClock))
		pa, _ := parts(t, a.deck)
		pc, _ := parts(t, c.deck)
		if src.guids > 0 {
			*wroteGUID = true
			if bytes.Equal(a.deck, c.deck) {
				t.Errorf("different seeds gave identical decks despite %d GUIDs asked for", src.guids)
			}
		}
		var names []string
		for n := range pa {
			names = append(names, n)
		}
		sort.Strings(names)
		if len(pa) != len(pc) {
			t.Fatalf("different seeds: %d parts vs %d", len(pa), len(pc))
		}
		for _, n := range names {
			if pa[n] != pc[n] {
				t.Errorf("different seeds: part %s differs beyond GUIDs", n)
			}
		}
	}) {
		sawGUID = sawGUID || r.Value
	}
	if !sawGUID && !t.Failed() && !corpustest.Sharded() {
		t.Error("no bench plan wrote a GUID; the seed is not exercised")
	}
}

// countingSource counts the GUIDs a run asked for.
type countingSource struct {
	ids.Source
	guids int
}

func (c *countingSource) GUID() string { c.guids++; return c.Source.GUID() }
