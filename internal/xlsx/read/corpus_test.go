package read

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
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
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
)

// xlsxCorpus returns the xlsx corpus directory: XLSX_CORPUS, else the
// xlsx directory of the repository corpus, or "" (as integrity's corpus
// test finds it).
func xlsxCorpus() string {
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

func corpusFiles(dir string) []string {
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

// corpusTimeout returns the per-file limit of the sweep (brief: 60 s),
// ten times that under the race detector, which slows the sweep about as
// much.
func corpusTimeout() time.Duration {
	if laws.RaceEnabled {
		return 10 * time.Minute
	}
	return time.Minute
}

type corpusCounts struct {
	notes    map[model.NoteKind]int
	errs     map[string]int    // contract code of a failing read, by command
	example  map[string]string // the first message of each errs key
	limit    bool              // a safety limit tripped (opc.ErrLimit)
	cells    int
	timedOut bool
}

// TestCorpus runs the summary, every worksheet's default cell read and a
// find over each corpus workbook: no panic, no hang (corpusTimeout per
// file), every error a contract error. It logs notes by kind, errors by
// code, limit trips and files that hit the timeout, for the report.
func TestCorpus(t *testing.T) {
	skipIfUnimplemented(t, probeAll)
	dir := xlsxCorpus()
	if dir == "" {
		t.Skip("no xlsx corpus (set XLSX_CORPUS)")
	}
	files := corpustest.Files(corpusFiles(dir), dir)
	res := corpustest.Run(t, files, func(t *testing.T, f corpustest.File, r *corpusCounts) {
		input, err := os.ReadFile(f.Path)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), corpusTimeout())
		defer cancel()
		// The sweep runs in its own goroutine with its own counts, so a
		// file that hangs is reported and left behind without racing
		// with the result.
		done := make(chan sweepResult, 1)
		go func() { done <- sweepFile(input) }()
		select {
		case s := <-done:
			*r = s.counts
			for _, msg := range s.problems {
				t.Error(msg)
			}
		case <-ctx.Done():
			r.timedOut = true
			t.Errorf("timed out after %v", corpusTimeout())
		}
	})
	if corpustest.Sharded() {
		return
	}
	notes, errs := map[model.NoteKind]int{}, map[string]int{}
	// Up to three example files per error key, for the report.
	examples := map[string][]string{}
	ran, limits, cells := 0, 0, 0
	var slow []string
	for _, r := range res {
		if !r.Ran {
			continue
		}
		ran++
		cells += r.Value.cells
		if r.Value.limit {
			limits++
		}
		if r.Value.timedOut {
			slow = append(slow, r.Name)
		}
		for k, n := range r.Value.notes {
			notes[k] += n
		}
		for k, n := range r.Value.errs {
			errs[k] += n
			if len(examples[k]) < 3 {
				ex := r.Name + ": " + r.Value.example[k]
				if len(ex) > 200 {
					ex = ex[:200] + "..."
				}
				examples[k] = append(examples[k], ex)
			}
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "xlsx read corpus: %d workbooks, %d cells read, %d limit trips, %d timeouts %v\n", ran, cells, limits, len(slow), slow)
	for _, k := range slices.Sorted(maps.Keys(notes)) {
		fmt.Fprintf(&b, "note %-12s %8d\n", k, notes[k])
	}
	for _, k := range slices.Sorted(maps.Keys(errs)) {
		fmt.Fprintf(&b, "error %-30s %8d\n", k, errs[k])
		for _, ex := range examples[k] {
			fmt.Fprintf(&b, "    e.g. %s\n", ex)
		}
	}
	t.Log("\n" + b.String())
}

type sweepResult struct {
	counts   corpusCounts
	problems []string
}

func sweepFile(input []byte) sweepResult {
	var out sweepResult
	r := &out.counts
	*r = corpusCounts{notes: map[model.NoteKind]int{}, errs: map[string]int{}, example: map[string]string{}}
	fail := func(cmd string, err error) {
		var ce *cli.Error
		if !errors.As(err, &ce) {
			out.problems = append(out.problems, fmt.Sprintf("%s: %T %v is not a contract error", cmd, err, err))
			return
		}
		key := cmd + " " + ce.Code
		r.errs[key]++
		if _, ok := r.example[key]; !ok {
			r.example[key] = ce.Message + ": " + ce.Detail
		}
		if errors.Is(err, opc.ErrLimit) {
			r.limit = true
		}
	}
	wb, err := Workbook(input, Options{})
	if err != nil {
		fail("summary", err)
		return out
	}
	for _, n := range wb.Notes {
		r.notes[n.Kind]++
	}
	for _, sh := range wb.Sheets {
		res, err := Cells(input, CellsRequest{Sheet: sh.ID, Limit: DefaultLimit}, Options{})
		if err != nil {
			fail("cells", err)
			continue
		}
		r.cells += res.Cells.Len()
		for _, n := range res.Notes {
			r.notes[n.Kind]++
		}
	}
	if _, err := Find(input, FindRequest{Query: "total", IgnoreCase: true, View: model.ViewValues, Limit: DefaultLimit}, Options{}); err != nil {
		fail("find", err)
	}
	return out
}

// TestOracleDump writes, for the cross-check against an independent
// reader (brief: the scratchpad scorer's compare.py read_cells, run with
// `uv run`), one JSON line per non-empty cell of every corpus worksheet:
// {"file","sheet","cell","f","v"} with f the display formula ("" for
// none) and v the cells-view value (a JSON value, null for none). It
// runs only when XLSX_ORACLE_OUT names the output file. The comparison
// itself is ad hoc and not committed; its difference classes go in the
// chunk 8 report (known: shared-formula children, spill members, display
// vs stored formulas, dates vs serials, and zips with a duplicate member
// name: opc keeps the first entry with a NotePackage note, as for docx
// and pptx, while another reader may keep the last).
func TestOracleDump(t *testing.T) {
	skipIfUnimplemented(t, probeAll)
	out := os.Getenv("XLSX_ORACLE_OUT")
	dir := xlsxCorpus()
	if out == "" || dir == "" {
		t.Skip("set XLSX_ORACLE_OUT and XLSX_CORPUS")
	}
	fh, err := os.Create(out) //nolint:gosec // G304: the test's own output, named by the caller
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := fh.Close(); err != nil {
			t.Error(err)
		}
	}()
	w := bufio.NewWriter(fh)
	for _, path := range corpusFiles(dir) {
		input, err := os.ReadFile(path) //nolint:gosec // G304: a corpus file
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(dir, path)
		wb, err := Workbook(input, Options{})
		if err != nil {
			continue
		}
		for _, sh := range wb.Sheets {
			res, err := Cells(input, CellsRequest{Sheet: sh.ID, Limit: 0}, Options{})
			if err != nil {
				continue
			}
			b, err := res.Cells.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			var cells map[string]json.RawMessage
			if err := json.Unmarshal(b, &cells); err != nil {
				t.Fatal(err)
			}
			for _, key := range slices.Sorted(maps.Keys(cells)) {
				line, err := json.Marshal(oracleLine(rel, sh.Name, key, cells[key]))
				if err != nil {
					t.Fatal(err)
				}
				if _, err := w.Write(append(line, '\n')); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
}

// oracleLine splits a cells-view entry into formula and value.
func oracleLine(file, sheet, cell string, raw json.RawMessage) map[string]any {
	line := map[string]any{"file": file, "sheet": sheet, "cell": cell, "f": "", "v": raw}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) == nil {
		if f, ok := obj["f"]; ok {
			var s string
			_ = json.Unmarshal(f, &s)
			line["f"] = s
			line["v"] = obj["v"]
		} else if v, ok := obj["v"]; ok {
			line["v"] = v
		}
	}
	return line
}
