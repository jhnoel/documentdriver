package read

import (
	"encoding/json"
	"testing"
	"unicode/utf8"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
)

// readAll runs every read on input: the summary, each worksheet in the
// three views with a small limit, and finds in both views. Every result
// must encode as valid JSON, and every error must be a *cli.Error with a
// contract code. It never panics on any input (AGENTS.md: no panics on
// input).
func readAll(t *testing.T, input []byte) {
	t.Helper()
	check := func(what string, v any, err error) {
		if err != nil {
			if _, ok := err.(*cli.Error); !ok {
				t.Fatalf("%s: error %T %v is not a *cli.Error", what, err, err)
			}
			return
		}
		b, err := cli.Marshal(v)
		if err != nil || !json.Valid(b) {
			t.Fatalf("%s: does not encode: %v", what, err)
		}
	}
	wb, err := Workbook(input, Options{})
	check("summary", wb, err)
	if err != nil {
		return
	}
	for _, sh := range wb.Sheets {
		for _, view := range []model.View{model.ViewCells, model.ViewValues, model.ViewFormulas} {
			res, err := Cells(input, CellsRequest{Sheet: sh.ID, View: view, Limit: 50, Group: view == model.ViewFormulas}, Options{})
			check("cells "+sh.ID, res, err)
		}
	}
	for _, view := range []model.View{model.ViewValues, model.ViewFormulas} {
		res, err := Find(input, FindRequest{Query: "a", IgnoreCase: true, View: view, Limit: 50}, Options{})
		check("find", res, err)
	}
}

// FuzzRead runs every read on arbitrary bytes, seeded with the fixture
// workbooks.
func FuzzRead(f *testing.F) {
	for _, fx := range []fx{valuesBook(), formulasBook(), spillBook(), addressBook(), findBook(), bigBook(3, 3)} {
		f.Add(fx.zip(f))
	}
	f.Add([]byte("PK\x05\x06" + string(make([]byte, 18))))
	f.Fuzz(func(t *testing.T, data []byte) {
		skipIfUnimplemented(t, func() { probeAll(); probeGroup() })
		readAll(t, data)
	})
}

// FuzzReadParts runs every read on a workbook whose worksheet, shared
// strings, styles, metadata and workbook parts are arbitrary, so the
// fuzzer works on the streaming readers rather than the zip.
func FuzzReadParts(f *testing.F) {
	seed := func(x fx) {
		p := x.parts()
		f.Add(p["xl/worksheets/sheet1.xml"], p["xl/sharedStrings.xml"], p["xl/styles.xml"], p["xl/metadata.xml"], p["xl/workbook.xml"])
	}
	seed(valuesBook())
	seed(spillBook())
	seed(formulasBook())
	f.Fuzz(func(t *testing.T, ws, sst, styles, meta, wb string) {
		skipIfUnimplemented(t, func() { probeAll(); probeGroup() })
		x := valuesBook()
		x.metadata = dynamicMetadata
		p := x.parts()
		p["xl/worksheets/sheet1.xml"], p["xl/sharedStrings.xml"], p["xl/styles.xml"], p["xl/metadata.xml"], p["xl/workbook.xml"] = ws, sst, styles, meta, wb
		data, err := zipParts(p)
		if err != nil {
			t.Fatal(err)
		}
		readAll(t, data)
	})
}

// FuzzFind runs find with arbitrary queries over the fixture: it never
// fails, and for a valid UTF-8 query --ignore-case finds at least what
// it finds without.
func FuzzFind(f *testing.F) {
	for _, q := range []string{"", "Item", "1200", "=", "ΣΑΣ", "\x00", "_x000D_", "#N/A", "2024-03"} {
		f.Add(q)
	}
	input := findBook().zip(f)
	f.Fuzz(func(t *testing.T, q string) {
		skipIfUnimplemented(t, probeFind)
		// The superset law needs whole runes: a query that is a
		// fragment of a UTF-8 sequence can match the bytes of a rune
		// that folding changes.
		if !utf8.ValidString(q) {
			return
		}
		for _, view := range []model.View{model.ViewValues, model.ViewFormulas} {
			exact, err := Find(input, FindRequest{Query: q, View: view}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			folded, err := Find(input, FindRequest{Query: q, View: view, IgnoreCase: true}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if len(folded.Matches) < len(exact.Matches) {
				t.Fatalf("%q: %d matches ignoring case, %d exact", q, len(folded.Matches), len(exact.Matches))
			}
		}
	})
}
