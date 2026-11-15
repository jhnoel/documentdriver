package shift

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/corpustest"
	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
	"github.com/jhnoel/documentdriver/internal/repo"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/sheet"
	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// TestImplicitPositionsAgree: the implicit row and cell positions
// sheetdata computes (sdRowNum and sdCellPos: the next row or column
// after the last) are the ones sheet.Scan computes for the same sheet.
// SheetData proves it on an identity edit (an insert past the last row
// moves nothing, so the output is the input byte for byte and Scan
// reads the same positions from both), and, more strongly, on edits
// that move every row and column: the r SheetData writes for a moved
// implicit row or cell is the position Scan gave the input plus the
// move, so a drift between the two rules shows up as a wrong r in the
// output. Corpus sheets get the identity half: a sheet SheetData
// refuses or Scan cannot read is skipped, not failed.
func TestImplicitPositionsAgree(t *testing.T) {
	identity := formula.Edit{Sheet: "Budget", Kind: formula.InsertRows, At: formula.MaxRows, Count: 1}
	moveRow := formula.Edit{Sheet: "Budget", Kind: formula.InsertRows, At: 1, Count: 1}
	moveCol := formula.Edit{Sheet: "Budget", Kind: formula.InsertCols, At: 1, Count: 1}
	for _, sd := range []string{
		`<sheetData><row><c><v>1</v></c><c><v>2</v></c><c><v>3</v></c></row><row><c><v>4</v></c></row></sheetData>`,
		`<sheetData><row r="3"><c r="B3"><v>1</v></c><c><v>2</v></c></row><row><c r="A4"><v>3</v></c><c><v>4</v></c></row></sheetData>`,
		`<sheetData><row r="5"><c><v>1</v></c></row><row><c><v>2</v></c></row><row r="9"><c r="D9"><v>3</v></c></row></sheetData>`,
		`<x:sheetData><x:row><x:c><v>1</v></x:c><x:c><v>2</v></x:c></x:row></x:sheetData>`,
		`<sheetData><row><c/><c><v>1</v></c></row><row/><row><c><v>2</v></c></row></sheetData>`,
	} {
		in, err := scanPositions(wrapSheet(sd))
		if err != nil {
			t.Fatalf("Scan %s: %v", sd, err)
		}
		out, _, err := SheetData(sd, own, identity)
		if err != nil {
			t.Fatalf("identity edit on %s: %v", sd, err)
		}
		if out != sd {
			t.Fatalf("identity edit changed %s:\n%s", sd, out)
		}
		if got, err := scanPositions(wrapSheet(out)); err != nil || !slices.Equal(got, in) {
			t.Fatalf("%s: identity positions %v, want %v (%v)", sd, got, in, err)
		}
		for _, e := range []formula.Edit{moveRow, moveCol} {
			moved, _, err := SheetData(sd, own, e)
			if err != nil {
				t.Fatalf("move %v on %s: %v", e, sd, err)
			}
			want := make([][2]int, len(in))
			for i, p := range in {
				want[i] = p
				if e.Kind == formula.InsertRows {
					want[i][0]++
				} else {
					want[i][1]++
				}
			}
			if got, err := scanPositions(wrapSheet(moved)); err != nil || !slices.Equal(got, want) {
				t.Fatalf("%s: moved positions %v, want %v (%v)\n%s", sd, got, want, err, moved)
			}
		}
	}
	testImplicitPositionsCorpus(t, identity)
}

// testImplicitPositionsCorpus runs the identity half of
// TestImplicitPositionsAgree over the sheetData of every corpus
// workbook's worksheet parts: SheetData with an identity edit is the
// identity on them, and Scan reads the same positions from the input
// and the output. A sheet SheetData refuses, Scan cannot read, or
// that is not a worksheet is skipped, not failed.
func testImplicitPositionsCorpus(t *testing.T, identity formula.Edit) {
	t.Helper()
	dir := ""
	if d := os.Getenv("XLSX_CORPUS"); d != "" {
		dir = d
	} else if cd, err := repo.Corpus(); err == nil {
		dir = filepath.Join(cd, "xlsx")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Skip("no xlsx corpus (set XLSX_CORPUS)")
	}
	var paths []string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".xlsx", ".xlsm", ".xltx", ".xltm":
			paths = append(paths, p)
		}
		return nil
	})
	slices.Sort(paths)
	corpustest.Each(t, corpustest.Files(paths, dir), func(t *testing.T, f corpustest.File) {
		data, err := os.ReadFile(f.Path)
		if err != nil {
			t.Fatal(err)
		}
		parts, err := xlsxtest.Parts(data)
		if err != nil {
			return
		}
		var names []string
		for name := range parts {
			if strings.HasPrefix(name, "xl/worksheets/") && strings.HasSuffix(name, ".xml") {
				names = append(names, name)
			}
		}
		slices.Sort(names)
		for _, name := range names {
			sd, ok := sheetDataOf(string(parts[name]))
			if !ok {
				continue
			}
			out, _, err := SheetData(sd, own, identity)
			if err != nil || out != sd {
				continue // a defect the input has: not a positions question
			}
			in, err := scanPositions(wrapSheet(sd))
			if err != nil {
				continue
			}
			got, err := scanPositions(wrapSheet(out))
			if err != nil {
				t.Fatalf("%s %s: Scan output: %v", f.Name, name, err)
			}
			if !slices.Equal(got, in) {
				t.Fatalf("%s %s: positions %v, want %v", f.Name, name, got, in)
			}
		}
	})
}

// sheetDataOf returns the sheetData element of a worksheet part as
// Sheet.Pieces gives it, or false when the part has none.
func sheetDataOf(src string) (string, bool) {
	for pos := 0; pos < len(src); {
		tok, err := xmlscan.Next(src, pos)
		if err != nil {
			return "", false
		}
		pos = tok.End
		if !tok.IsElement() || tok.Local(src) != "sheetData" {
			continue
		}
		if tok.Kind == xmlscan.EmptyTag {
			return src[tok.Start:tok.End], true
		}
		_, end, err := xmlscan.ElementEnd(src, tok)
		if err != nil {
			return "", false
		}
		return src[tok.Start:end], true
	}
	return "", false
}

// wrapSheet wraps a sheetData element in its worksheet, keeping its
// prefix, for sheet.Scan.
func wrapSheet(sd string) string {
	p := ""
	if tok, err := xmlscan.Next(sd, 0); err == nil {
		if i := strings.IndexByte(tok.Name(sd), ':'); i >= 0 {
			p = tok.Name(sd)[:i+1]
		}
	}
	const main = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
	decl := `xmlns="` + main + `"`
	if p != "" {
		decl = `xmlns:` + p[:len(p)-1] + `="` + main + `"`
	}
	return "<" + p + "worksheet " + decl + ">" + sd + "</" + p + "worksheet>"
}

// scanPositions returns the (row, column) of every cell sheet.Scan
// hands over for src, in document order.
func scanPositions(src string) ([][2]int, error) {
	var out [][2]int
	_, err := sheet.Scan(src, sheet.ScanOptions{From: 1, To: formula.MaxRows, Row: func(r *sheet.Row) error {
		for _, c := range r.Cells {
			out = append(out, [2]int{c.Row, c.Col})
		}
		return nil
	}})
	return out, err
}
