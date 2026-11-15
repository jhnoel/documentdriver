package read

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/jhnoel/documentdriver/internal/corpustest"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
)

// cellsOnlyRanges are the ranges TestCellsOnlyMatchesRead reads on each
// sheet: a few rows from the top (the early stop matters), rows in the
// middle, and an area cut by a small limit (the full scan again).
var cellsOnlyRanges = []struct {
	r1, c1, r2, c2, limit int
}{
	{1, 1, 3, 30, DefaultLimit},
	{2, 1, 40, 8, DefaultLimit},
	{1, 1, 200, 50, 5},
}

// sameCellsOnly checks that a CellsOnly read of rng gives the full read's
// cells and truncation, or fails exactly when it fails.
func sameCellsOnly(t *testing.T, data []byte, rng string, limit int) {
	t.Helper()
	full, ferr := Cells(data, CellsRequest{Range: rng, Limit: limit}, Options{})
	only, oerr := Cells(data, CellsRequest{Range: rng, Limit: limit, CellsOnly: true}, Options{})
	if (ferr == nil) != (oerr == nil) {
		// CellsOnly does not prove the tail well formed, so a read the
		// full scan refuses for a fault past the range may succeed.
		if ferr != nil && oerr == nil {
			return
		}
		t.Fatalf("%s: full read %v, CellsOnly %v", rng, ferr, oerr)
	}
	if ferr != nil {
		return
	}
	a, err := json.Marshal([]any{full.Cells, full.Truncated})
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal([]any{only.Cells, only.Truncated})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Errorf("%s: CellsOnly differs from read:\nread      %s\nCellsOnly %s", rng, a, b)
	}
}

// TestCellsOnlyMatchesRead: apply's read-back (CellsOnly, which stops the
// scan after the range) gives read's cells and truncation, on a fixture
// with shared formulas, spills and arrays and on every corpus workbook.
func TestCellsOnlyMatchesRead(t *testing.T) {
	skipIfUnimplemented(t, probeAll)
	check := func(t *testing.T, data []byte) {
		wb, err := Workbook(data, Options{})
		if err != nil {
			return
		}
		for _, sh := range wb.Sheets {
			for _, r := range cellsOnlyRanges {
				sameCellsOnly(t, data, model.Area(sh.Name, r.r1, r.c1, r.r2, r.c2), r.limit)
			}
		}
	}
	t.Run("fixtures", func(t *testing.T) {
		for _, f := range []fx{valuesBook(), formulasBook(), spillBook(), addressBook(), reusedSIBook()} {
			check(t, f.zip(t))
		}
	})
	dir := xlsxCorpus()
	if dir == "" {
		return
	}
	files := corpustest.Files(corpusFiles(dir), dir)
	corpustest.Each(t, files, func(t *testing.T, f corpustest.File) {
		data, err := os.ReadFile(f.Path)
		if err != nil {
			t.Fatal(err)
		}
		check(t, data)
	})
}
