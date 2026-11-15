package integrity

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/corpustest"
	"github.com/jhnoel/documentdriver/internal/repo"
)

// xlsxCorpus returns the xlsx corpus directory: XLSX_CORPUS, else the
// xlsx directory of the pptx corpus (as make corpus finds it), or "".
func xlsxCorpus(t *testing.T) string {
	t.Helper()
	if d := os.Getenv("XLSX_CORPUS"); d != "" {
		return d
	}
	dir, err := repo.Corpus()
	if err != nil {
		return ""
	}
	dir = filepath.Join(dir, "xlsx")
	if _, err := os.Stat(dir); err != nil {
		return ""
	}
	return dir
}

type corpusCounts struct {
	kinds    map[Kind]int
	examples map[Kind]string
}

// TestCorpus runs Check over every workbook of the xlsx corpus: it must
// not panic or error, and it logs the findings by kind (files, total,
// one example), which design.md records. XLSX_CORPUS_REPORT=1 prints the
// per-kind table.
func TestCorpus(t *testing.T) {
	dir := xlsxCorpus(t)
	if dir == "" {
		t.Skip("no xlsx corpus (set XLSX_CORPUS)")
	}
	paths := walkAll(dir)
	files := corpustest.Files(paths, dir)
	res := corpustest.Run(t, files, func(t *testing.T, f corpustest.File, r *corpusCounts) {
		data, err := os.ReadFile(f.Path)
		if err != nil {
			t.Fatal(err)
		}
		fs := Check(data)
		r.kinds = map[Kind]int{}
		r.examples = map[Kind]string{}
		for _, k := range fs.Kinds() {
			r.kinds[k.Kind] += fs.Count(k)
		}
		for _, in := range fs.Instances() {
			if _, ok := r.examples[in.Kind]; !ok {
				r.examples[in.Kind] = in.Detail
			}
		}
	})
	if corpustest.Sharded() {
		return
	}
	total := map[Kind]int{}
	filesWith := map[Kind]int{}
	example := map[Kind]string{}
	ran := 0
	for _, r := range res {
		if !r.Ran {
			continue
		}
		ran++
		for k, n := range r.Value.kinds {
			total[k] += n
			filesWith[k]++
			if _, ok := example[k]; !ok {
				example[k] = r.Name + ": " + r.Value.examples[k]
			}
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "xlsx corpus: %d workbooks, %d with findings\n", ran, countFiles(res))
	for _, k := range slices.Sorted(maps.Keys(total)) {
		ex := example[k]
		if len(ex) > 160 {
			ex = ex[:160] + "..."
		}
		fmt.Fprintf(&b, "%-34s files %5d  total %8d  e.g. %s\n", k, filesWith[k], total[k], ex)
	}
	t.Log("\n" + b.String())
}

func countFiles(res []corpustest.Result[corpusCounts]) int {
	n := 0
	for _, r := range res {
		if len(r.Value.kinds) > 0 {
			n++
		}
	}
	return n
}

func walkAll(dir string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".xlsx", ".xlsm", ".xltx", ".xltm":
			out = append(out, p)
		}
		return nil
	})
	slices.Sort(out)
	return out
}
