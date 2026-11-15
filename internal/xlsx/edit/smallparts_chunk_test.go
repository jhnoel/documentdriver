package edit

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
)

// TestTableNameFallbackChunk: a table with no displayName is found by
// its name (sml.ParseTable, adopted from read).
func TestTableNameFallbackChunk(t *testing.T) {
	b := oneSheet("")
	b.Sheets[0].Tables = []string{`<table id="1" name="OnlyName" ref="A1:B2"><tableColumns count="2"><tableColumn id="1" name="H1"/><tableColumn id="2" name="H2"/></tableColumns></table>`}
	w := openBook(t, b)
	tables, err := w.Tables("S")
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 1 || tables[0].Name != "OnlyName" {
		t.Errorf("Tables = %+v, want one table named OnlyName", tables)
	}
}

// TestTableRowCountsChunk: a headerRowCount or totalsRowCount that is
// not ParseIndex (a negative here) keeps the defaults 1 and 0.
func TestTableRowCountsChunk(t *testing.T) {
	b := oneSheet("")
	b.Sheets[0].Tables = []string{`<table id="1" name="T" displayName="T" ref="A1:B2" headerRowCount="-1" totalsRowCount="-5"><tableColumns count="1"><tableColumn id="1" name="H"/></tableColumns></table>`}
	w := openBook(t, b)
	tables, err := w.Tables("S")
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 1 || tables[0].HeaderRows != 1 || tables[0].TotalsRows != 0 {
		t.Errorf("Tables = %+v, want header 1 totals 0", tables)
	}
}

// TestDefinedNameInvalidScopeChunk: a localSheetId that is not a sheet
// position is scope -2: it is in no scope, so a workbook lookup does
// not find it.
func TestDefinedNameInvalidScopeChunk(t *testing.T) {
	b := oneSheet("")
	b.DefinedNames = `<definedName name="Bad" localSheetId="x">1</definedName><definedName name="Neg" localSheetId="-1">2</definedName><definedName name="Far" localSheetId="99">3</definedName>`
	w := openBook(t, b)
	names, err := w.DefinedNames()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 3 {
		t.Fatalf("DefinedNames = %+v", names)
	}
	for _, n := range names {
		if n.LocalID != -2 || n.Sheet != "" {
			t.Errorf("name %+v, want LocalID -2 with no sheet", n)
		}
	}
	if err := w.SetDefinedName("Bad", "", "9"); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetDefinedName(Bad, workbook) = %v, want ErrNotFound", err)
	}
	if err := w.SetDefinedName("Bad", "S", "9"); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetDefinedName(Bad, S) = %v, want ErrNotFound", err)
	}
}

// withoutPart writes data, a zip package, to a file without the part
// named drop, and returns the file's path.
func withoutPart(t *testing.T, data []byte, drop string) string {
	t.Helper()
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range r.File {
		if f.Name == drop {
			continue
		}
		w, err := zw.Create(f.Name)
		if err != nil {
			t.Fatal(err)
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(w, io.LimitReader(rc, 1<<30)); err != nil {
			t.Fatal(err)
		}
		if err := rc.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "in.xlsx")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestMissingSheetPartChunk: a sheet whose part the package lacks
// resolves with Part "" (sml.ResolveSheet), and opening it is still
// *Unreadable (unreadable_target) on the sheet's id.
func TestMissingSheetPartChunk(t *testing.T) {
	b := oneSheet("")
	data, err := b.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	path := withoutPart(t, data, "xl/worksheets/sheet1.xml")
	pkg, err := opc.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pkg.Close() }) //nolint:errcheck // a test's package
	w, err := New(pkg)
	if err != nil {
		t.Fatal(err)
	}
	sheets := w.Sheets()
	if len(sheets) != 1 || sheets[0].Part != "" {
		t.Fatalf("Sheets = %+v, want one sheet with no part", sheets)
	}
	_, err = w.Sheet("S")
	var u *Unreadable
	if !errors.As(err, &u) {
		t.Fatalf("Sheet(S) = %v, want *Unreadable", err)
	}
	if u.Target != model.SheetID("S") || u.Detail != "its part is missing" {
		t.Errorf("Unreadable = %+v, want sheet:S its part is missing", u)
	}
}
