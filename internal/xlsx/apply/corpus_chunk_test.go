package apply

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/corpustest"
	"github.com/jhnoel/documentdriver/internal/repo"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
	"github.com/jhnoel/documentdriver/internal/xlsx/read"
)

// xlsxCorpus is XLSX_CORPUS, else the xlsx directory of the repository
// corpus, or "".
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

func xlsxFiles(dir string) []string {
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

// corpusRefusals are the codes a corpus plan may be refused with: the
// workbook or the target is broken, or the op's own rule refuses (a
// shift that would cut a construct). Anything else (internal,
// invalid_output) is a bug.
var corpusRefusals = map[string]bool{
	cli.CodeInvalidDocument: true, cli.CodeUnreadableTarget: true,
	cli.CodeOperationFailed: true, cli.CodeInvalidOperation: true,
}

// TestApplyCorpus (verification chunk V): on every corpus workbook, on
// its first sheet, plans through ParsePlan and Run with the real gate: a
// cell.set past the used range, a range.format, a row insert at the top,
// and a column delete with a sheet rename. Each succeeds or is refused
// with a contract code a broken file or the op's rule explains, never
// internal or invalid_output; a success leaves the other parts byte for
// byte, and a cell.set reads back. It logs outcomes by op and code.
func TestApplyCorpus(t *testing.T) {
	dir := xlsxCorpus()
	if dir == "" {
		t.Skip("no xlsx corpus (set XLSX_CORPUS)")
	}
	files := corpustest.Files(xlsxFiles(dir), dir)
	res := corpustest.Run(t, files, func(t *testing.T, f corpustest.File, r *map[string]int) {
		*r = map[string]int{}
		data, err := os.ReadFile(f.Path)
		if err != nil {
			t.Fatal(err)
		}
		wb, err := read.Workbook(data, read.Options{})
		if err != nil || len(wb.Sheets) == 0 {
			(*r)["no summary"]++
			return
		}
		sh := wb.Sheets[0].Name
		far := model.Cell(sh, 1048000, 16000)
		plans := []struct{ name, plan string }{
			{"cell.set", `{"ops":[{"op":"cell.set","at":` + q(far) + `,"value":"dd corpus <&>"}]}`},
			{"range.format", `{"ops":[{"op":"range.format","at":` + q(model.Area(sh, 1, 1, 3, 3)) + `,"bold":true}]}`},
			{"row.insert", `{"ops":[{"op":"row.insert","at":` + q(model.Rows(sh, 1, 1)) + `,"position":"before"}]}`},
			{"column.delete+sheet.rename", `{"ops":[{"op":"column.delete","at":` + q(model.Cols(sh, 2, 2)) + `},{"op":"sheet.rename","at":` + q(model.SheetID(sh)) + `,"name":"DD renamed"}]}`},
		}
		for _, pl := range plans {
			p, errs := ops.ParsePlan([]byte(pl.plan))
			if len(errs) > 0 {
				(*r)[pl.name+" plan "+errs[0].Code]++
				continue
			}
			out := filepath.Join(t.TempDir(), "out.xlsx")
			got, err := Run(f.Path, p, Options{Out: out})
			if err != nil {
				var e *cli.Error
				if !errors.As(err, &e) || !corpusRefusals[e.Code] {
					detail := ""
					if e != nil {
						detail = e.Detail
					}
					t.Errorf("%s: %v (%s)", pl.name, err, detail)
					(*r)[pl.name+" BUG"]++
					continue
				}
				(*r)[pl.name+" "+e.Code]++
				if os.Getenv("XLSX_CORPUS_REPORT") != "" {
					(*r)[pl.name+" "+e.Code+": "+classify(e)]++
				}
				continue
			}
			(*r)[pl.name+" ok"]++
			if got.Saved == "" {
				continue // a no-op
			}
			written, err := os.ReadFile(out) //nolint:gosec // test output
			if err != nil {
				t.Fatal(err)
			}
			if zipReadable(data) {
				checkUntouched(t, data, written, got.Parts)
			}
			if pl.name == "cell.set" {
				if c := cellsOf(t, written, far, model.ViewValues); !strings.Contains(c, `"dd corpus <&>"`) {
					t.Errorf("cell.set reads back %s", c)
				}
			}
		}
	})
	if corpustest.Sharded() {
		return
	}
	total := map[string]int{}
	for _, r := range res {
		for k, n := range r.Value {
			total[k] += n
		}
	}
	for _, k := range slices.Sorted(maps.Keys(total)) {
		t.Logf("%-40s %d", k, total[k])
	}
}

// zipReadable reports whether archive/zip reads every member of data
// once: a few corpus files have bad checksums or truncated members opc
// copies through, or duplicate names (opc keeps the first), which the
// byte comparison's reader cannot judge.
func zipReadable(data []byte) bool {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return false
	}
	seen := map[string]bool{}
	for _, f := range zr.File {
		if seen[f.Name] {
			return false
		}
		seen[f.Name] = true
		rc, err := f.Open()
		if err != nil {
			return false
		}
		_, err = io.Copy(io.Discard, io.LimitReader(rc, 1<<30))
		if cerr := rc.Close(); err != nil || cerr != nil {
			return false
		}
	}
	return true
}

// classify is a refusal's detail with numbers and addresses blanked,
// for the XLSX_CORPUS_REPORT tally.
func classify(e *cli.Error) string {
	d := e.Detail
	if d == "" {
		d = e.Message
	}
	if i := strings.IndexAny(d, "0123456789'!"); i > 0 && i < 60 {
		d = d[:i]
	}
	if len(d) > 60 {
		d = d[:60]
	}
	return d
}

func q(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}
