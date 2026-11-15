package integrity

import (
	"bufio"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/corpustest"
	"github.com/jhnoel/documentdriver/internal/repo"
)

// The corpus and parity tests read docxdriver's checkout and corpus, which
// live beside this repository, not in it:
//
//	DOCXDRIVER_REPO  docxdriver checkout (default ../docxdriver from the
//	                 main checkout of this repository)
//	DOCX_CORPUS      .docx corpus (default corpus/lo in DOCXDRIVER_REPO)
//	INTEGRITY_ORACLE_TSV  docxdriver's findings, as its examples/integrity.rs
//	                 prints them, for TestOracleParity
//
// docs/docx-integrity.md says how to produce the TSV.

// docxdriverRepo returns the docxdriver checkout, or "" when there is none.
func docxdriverRepo(t *testing.T) string {
	t.Helper()
	if dir := os.Getenv("DOCXDRIVER_REPO"); dir != "" {
		return dir
	}
	root, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}
	if main, ok := repo.MainCheckout(root); ok {
		root = main
	}
	dir := filepath.Join(filepath.Dir(root), "docxdriver")
	if _, err := os.Stat(dir); err != nil {
		return ""
	}
	return dir
}

func docxCorpus(t *testing.T, docxdriver string) string {
	t.Helper()
	if dir := os.Getenv("DOCX_CORPUS"); dir != "" {
		return dir
	}
	if docxdriver == "" {
		return ""
	}
	return filepath.Join(docxdriver, "corpus", "lo")
}

// docxFiles returns the .docx files under dir, skipping build output and
// agent worktrees.
func docxFiles(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "target", ".git", ".claude", "node_modules", ".worktrees":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(strings.ToLower(path), ".docx") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// parityFiles returns docxdriver's test documents, its fixtures and the
// corpus, keyed by the path docxdriver's example prints when run from its
// checkout.
func parityFiles(t *testing.T) map[string]string {
	t.Helper()
	dd := docxdriverRepo(t)
	files := map[string]string{}
	add := func(path string) {
		key := path
		if dd != "" {
			if rel, err := filepath.Rel(dd, path); err == nil && !strings.HasPrefix(rel, "..") {
				key = filepath.ToSlash(rel)
			}
		}
		files[key] = path
	}
	if dd != "" {
		for _, sub := range []string{"test-docs", "crates"} {
			if _, err := os.Stat(filepath.Join(dd, sub)); err == nil {
				for _, p := range docxFiles(t, filepath.Join(dd, sub)) {
					add(p)
				}
			}
		}
	}
	if c := docxCorpus(t, dd); c != "" {
		if _, err := os.Stat(c); err == nil {
			for _, p := range docxFiles(t, c) {
				add(p)
			}
		}
	}
	return files
}

// corpusFiles is parityFiles as corpus files named by their keys, in key
// order.
func corpusFiles(files map[string]string) []corpustest.File {
	var out []corpustest.File
	for i, key := range slices.Sorted(maps.Keys(files)) {
		out = append(out, corpustest.File{Path: files[key], Name: key, Index: i})
	}
	return out
}

// TestCorpus checks every document of the corpus and docxdriver's fixtures
// without panicking, and every sixteenth twice, for determinism. The
// documents run in parallel; which are checked twice goes by their place
// in the full list, whatever the shard.
func TestCorpus(t *testing.T) {
	files := parityFiles(t)
	if len(files) == 0 {
		t.Skip("no docxdriver checkout or DOCX_CORPUS")
	}
	ran := corpustest.Each(t, corpusFiles(files), func(t *testing.T, f corpustest.File) {
		data, err := os.ReadFile(f.Path)
		if err != nil {
			t.Fatal(err)
		}
		a := Check(data)
		if f.Index%16 != 0 {
			return
		}
		if b := Check(data); !slices.Equal(a.Kinds(), b.Kinds()) {
			t.Errorf("%s: findings differ between runs", f.Name)
		}
	})
	t.Logf("%d documents", len(ran))
}

// TestOracleParity compares the findings with docxdriver's on every
// document parityFiles finds.
func TestOracleParity(t *testing.T) {
	tsv := os.Getenv("INTEGRITY_ORACLE_TSV")
	if tsv == "" {
		t.Skip("INTEGRITY_ORACLE_TSV not set")
	}
	want := readOracle(t, tsv)
	files := parityFiles(t)
	// Document by document in parallel; each reports whether it matched
	// and the intended differences it explains, summed in key order.
	type parity struct {
		same     bool
		intended []string
	}
	same, intended := 0, map[string]int{}
	for _, r := range corpustest.Run(t, corpusFiles(files), func(t *testing.T, f corpustest.File, p *parity) {
		data, err := os.ReadFile(f.Path)
		if err != nil {
			t.Fatal(err)
		}
		got := map[Finding]int{}
		fs := Check(data)
		for _, k := range fs.Kinds() {
			got[k] = fs.Count(k)
		}
		rustOnly, goOnly := diffFindings(want[f.Name], got)
		if len(rustOnly)+len(goOnly) == 0 {
			p.same = true
			return
		}
		names, ok := explained(rustOnly, goOnly)
		if !ok {
			t.Errorf("%s: unexplained difference: docxdriver only %q, Go only %q", f.Name, rustOnly, goOnly)
			return
		}
		p.intended = names
		t.Logf("%s: intended (%s): docxdriver only %q, Go only %q", f.Name, strings.Join(names, "; "), rustOnly, goOnly)
	}) {
		delete(want, r.Name)
		if r.Value.same {
			same++
		}
		for _, n := range r.Value.intended {
			intended[n]++
		}
	}
	if corpustest.Sharded() {
		want = nil // the oracle lists every document; a shard checks some
	}
	for key := range want {
		t.Errorf("%s: in the oracle's findings but not found here", key)
	}
	t.Logf("%d documents identical; intended differences: %v", same, intended)
}

// readOracle reads path\tpart\tmessage\tcount lines.
func readOracle(t *testing.T, path string) map[string]map[Finding]int {
	t.Helper()
	f, err := os.Open(path) //nolint:gosec // G304: a test input named by the caller
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	out := map[string]map[Finding]int{}
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		fields := strings.Split(sc.Text(), "\t")
		if len(fields) != 4 {
			t.Fatalf("%s: malformed line %q", path, sc.Text())
		}
		n, err := strconv.Atoi(fields[3])
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if out[fields[0]] == nil {
			out[fields[0]] = map[Finding]int{}
		}
		out[fields[0]][Finding{fields[1], fields[2]}] = n
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}
