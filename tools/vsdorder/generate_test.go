package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jhnoel/documentdriver/internal/repo"
)

// inputsForFiles returns the resolved XSD path and corpus file paths
// for a test, skipping (as the chunk requires) when the XSD cache or
// the corpus is not present.
func inputsForFiles(t *testing.T) (string, []string, []string) {
	t.Helper()
	xsdPath, err := resolveXSD("")
	if err != nil {
		t.Skip("no schema cache: make vsdx-schema", err)
	}
	if _, err := os.Stat(xsdPath); err != nil {
		t.Skip("no schema cache: make vsdx-schema (run eval/corpus/fetch-vsdx-schema.sh)", err)
	}
	root, err := resolveCorpus("")
	if err != nil {
		t.Skip("no Visio corpus: set VSDX_CORPUS (eval/corpus/fetch-vsdx.sh)", err)
	}
	files, err := listCorpusFiles(root)
	if err != nil || len(files) == 0 {
		t.Skip("no Visio corpus: set VSDX_CORPUS (eval/corpus/fetch-vsdx.sh)", err)
	}
	var paths, ids []string
	for _, rel := range files {
		paths = append(paths, filepath.Join(root, filepath.FromSlash(rel)))
		ids = append(ids, rel)
	}
	return xsdPath, paths, ids
}

// TestGeneratedMatchesCommitted is the drift check: regenerating in
// memory must reproduce the committed table and provenance exactly.
// It skips without the schema cache or the corpus.
func TestGeneratedMatchesCommitted(t *testing.T) {
	xsdPath, paths, ids := inputsForFiles(t)
	table, prov, err := generate(xsdPath, paths, ids)
	if err != nil {
		t.Fatal(err)
	}
	root, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		rel  string
		want []byte
	}{{tableFile, table}, {provenanceFile, prov}} {
		got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(tc.rel)))
		if err != nil {
			t.Fatalf("%s: %v", tc.rel, err)
		}
		if !bytes.Equal(got, tc.want) {
			t.Errorf("%s is stale: run go run ./tools/vsdorder", tc.rel)
		}
	}
}

// TestGeneratedRows checks the designer's cross-check rows over a
// fresh generation: exactly two disagreements, exactly 52
// refinements, the Shape/PageSheet/Section/Text orders, the page
// roots and lookup forms, the provenance hash, and the committed
// file counts (65 scanned, 53 accepted).
func TestGeneratedRows(t *testing.T) {
	xsdPath, paths, ids := inputsForFiles(t)
	table, prov, err := generate(xsdPath, paths, ids)
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		XSD  string   `json:"xsd_sha256"`
		Scan []string `json:"scanned_files"`
		Acc  []string `json:"accepted_files"`
		Dis  []struct {
			Parent  string `json:"parent"`
			Child   string `json:"child"`
			Kind    string `json:"kind"`
			Files   int    `json:"files"`
			Parents int    `json:"parent_occurrences"`
			Occurs  int    `json:"occurrences"`
		} `json:"disagreements"`
		Refs []struct {
			Parent string `json:"parent"`
			First  string `json:"first"`
			Second string `json:"second"`
			Before int    `json:"before"`
			After  int    `json:"after"`
		} `json:"refinements"`
	}
	if err := json.Unmarshal(prov, &p); err != nil {
		t.Fatal(err)
	}
	if p.XSD != "29caea2b747dcc560cca0b112de30e3cbefe751ccd0e874618ff7dfb3ba0db76" {
		t.Errorf("xsd sha256 = %q, want 29caea2b...ba0db76", p.XSD)
	}
	if len(p.Scan) != 65 || len(p.Acc) != 53 {
		t.Errorf("scanned %d, accepted %d files; want 65 scanned and 53 accepted", len(p.Scan), len(p.Acc))
	}
	if len(p.Dis) != 2 {
		t.Fatalf("disagreements = %d, want exactly 2: %+v", len(p.Dis), p.Dis)
	}
	if d := p.Dis[0]; d.Parent != "v:PageSheet" || d.Child != "Trigger" || d.Kind != "wildcard_only" || d.Occurs != 36 || d.Parents != 33 || d.Files != 32 {
		t.Errorf("wildcard disagreement = %+v, want PageSheet>Trigger wildcard_only occurs 36 parents 33 files 32", d)
	}
	if d := p.Dis[1]; d.Child != "Trigger" || d.Kind != "undeclared" || d.Occurs != 36 {
		t.Errorf("undeclared disagreement = %+v, want Trigger undeclared with 36 occurrences", d)
	}
	if len(p.Refs) != 52 {
		t.Fatalf("refinements = %d, want exactly 52", len(p.Refs))
	}
	wantRef := map[[3]string][2]int{
		{"v:Shape", "Cell", "Section"}:         {73587, 0},
		{"v:Shape", "Section", "Text"}:         {1399, 0},
		{"v:Shape", "Text", "ForeignData"}:     {1, 0},
		{"v:Section", "Cell", "Row"}:           {25418, 0},
		{"v:Page", "PageSheet", "Rel"}:         {82, 0},
		{"v:Master", "PageSheet", "Icon"}:      {231, 0},
		{"v:Master", "Icon", "Rel"}:            {231, 0},
		{"v:DocumentSheet", "Cell", "Section"}: {510, 0},
		{"v:StyleSheet", "Cell", "Section"}:    {103155, 0},
		{"v:PageSheet", "Cell", "Section"}:     {3949, 0},
		{"v:PageSheet", "Cell", "Trigger"}:     {352, 515},
	}
	for key, w := range wantRef {
		found := false
		for _, r := range p.Refs {
			if r.Parent == key[0] && r.First == key[1] && r.Second == key[2] {
				found = true
				if r.Before != w[0] || r.After != w[1] {
					t.Errorf("refinement %v = before %d after %d, want %v", key, r.Before, r.After, w)
				}
			}
		}
		if !found {
			t.Errorf("refinement %v not in provenance", key)
		}
	}
	docSettings := 0
	for _, r := range p.Refs {
		if r.Parent == "v:DocumentSettings" {
			docSettings++
		}
	}
	if docSettings != 36 {
		t.Errorf("DocumentSettings refinements = %d, want 36 pairs", docSettings)
	}
	for _, name := range []string{`"v:Shape":`, `"v:PageContents":`, `"v:Text":`} {
		if !bytes.Contains(table, []byte(name)) {
			t.Errorf("table lacks %s", name)
		}
	}
}

// TestScaling checks generation over a copied file list is linear:
// the same corpus file listed N and 4N times, time ratio (best of 5
// runs each) at most 8. N and 4N are the row's named sizes.
func TestScaling(t *testing.T) {
	xsdPath, paths, _ := inputsForFiles(t)
	file := paths[0]
	const small = 8
	const large = 4 * small
	build := func(n int) ([]string, []string) {
		var ps, ids []string
		for i := 0; i < n; i++ {
			ps = append(ps, file)
			ids = append(ids, fmt.Sprintf("rep%d.vsdx", i))
		}
		return ps, ids
	}
	measure := func(n int) time.Duration {
		ps, ids := build(n)
		best := time.Hour
		for range 5 {
			start := time.Now()
			if _, _, err := generate(xsdPath, ps, ids); err != nil {
				t.Fatalf("generate at %d: %v", n, err)
			}
			if d := time.Since(start); d < best {
				best = d
			}
		}
		return best
	}
	t0, t1 := measure(small), measure(large)
	t.Logf("generation over %d vs %d copies of one file: best-of-5 %s vs %s, ratio %.2f", small, large, t0, t1, float64(t1)/float64(max(t0, time.Microsecond)))
	if t1 > 8*t0 {
		t.Errorf("generation time ratio %.2f over 4x input, want at most 8 (over input of %d and %d file copies)", float64(t1)/float64(max(t0, time.Microsecond)), small, large)
	}
}
