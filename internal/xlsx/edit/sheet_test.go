package edit

import (
	"errors"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// The overlay, the locator and the splice writer (implemented in the
// skeleton; these tests must pass).

func openBook(t *testing.T, b xlsxtest.Book) *Workspace {
	t.Helper()
	path, err := b.Write(t.TempDir(), "in.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := opc.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pkg.Close() }) //nolint:errcheck // a test's package
	w, err := New(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func oneSheet(rows string) xlsxtest.Book {
	return xlsxtest.Book{Sheets: []xlsxtest.Sheet{{Name: "S", Rows: rows, Dimension: "A1:C3"}}}
}

// flushed flushes s and returns the patched part and its patch's
// original, checking the patch is well formed.
func flushed(t *testing.T, s *Sheet) string {
	t.Helper()
	if err := s.flush(); err != nil {
		t.Fatal(err)
	}
	p := s.part.Patch()
	if p == nil {
		return s.orig
	}
	if err := p.Check(); err != nil {
		t.Fatalf("patch does not check: %v\n%s", err, p.String())
	}
	return p.String()
}

func sheetData(t *testing.T, text string) string {
	t.Helper()
	i := strings.Index(text, "<sheetData")
	j := strings.Index(text, "</sheetData>")
	if i < 0 {
		t.Fatalf("no sheetData in %s", text)
	}
	if j < 0 {
		k := strings.Index(text[i:], "/>")
		return text[i : i+k+2]
	}
	return text[i : j+len("</sheetData>")]
}

const threeRows = `<row r="1"><c r="A1"><v>1</v></c><c r="C1"><v>3</v></c></row>` +
	`<!-- between --><row r="3" ht="20" customHeight="1"><c r="B3" s="0"><v>5</v></c></row>`

func TestOverlayCells(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(s *Sheet) error
		want string
	}{
		{"replace", func(s *Sheet) error { return s.SetCell(1, 1, `<c r="A1"><v>9</v></c>`) },
			`<sheetData><row r="1"><c r="A1"><v>9</v></c><c r="C1"><v>3</v></c></row><!-- between --><row r="3" ht="20" customHeight="1"><c r="B3" s="0"><v>5</v></c></row></sheetData>`},
		{"insert middle", func(s *Sheet) error { return s.SetCell(1, 2, `<c r="B1"><v>2</v></c>`) },
			`<sheetData><row r="1"><c r="A1"><v>1</v></c><c r="B1"><v>2</v></c><c r="C1"><v>3</v></c></row><!-- between --><row r="3" ht="20" customHeight="1"><c r="B3" s="0"><v>5</v></c></row></sheetData>`},
		{"insert end", func(s *Sheet) error { return s.SetCell(1, 5, `<c r="E1"><v>2</v></c>`) },
			`<sheetData><row r="1"><c r="A1"><v>1</v></c><c r="C1"><v>3</v></c><c r="E1"><v>2</v></c></row><!-- between --><row r="3" ht="20" customHeight="1"><c r="B3" s="0"><v>5</v></c></row></sheetData>`},
		{"new row between", func(s *Sheet) error { return s.SetCell(2, 1, `<c r="A2"><v>2</v></c>`) },
			`<sheetData><row r="1"><c r="A1"><v>1</v></c><c r="C1"><v>3</v></c></row><!-- between --><row r="2"><c r="A2"><v>2</v></c></row><row r="3" ht="20" customHeight="1"><c r="B3" s="0"><v>5</v></c></row></sheetData>`},
		{"new row after", func(s *Sheet) error { return s.SetCell(9, 1, `<c r="A9"><v>2</v></c>`) },
			`<sheetData><row r="1"><c r="A1"><v>1</v></c><c r="C1"><v>3</v></c></row><!-- between --><row r="3" ht="20" customHeight="1"><c r="B3" s="0"><v>5</v></c></row><row r="9"><c r="A9"><v>2</v></c></row></sheetData>`},
		{"delete", func(s *Sheet) error { return s.SetCell(1, 3, "") },
			`<sheetData><row r="1"><c r="A1"><v>1</v></c></row><!-- between --><row r="3" ht="20" customHeight="1"><c r="B3" s="0"><v>5</v></c></row></sheetData>`},
		{"row attrs", func(s *Sheet) error {
			if err := s.SetRowAttrs(3, AttrEdit{Name: "ht", Value: "30"}, AttrEdit{Name: "hidden", Value: "1"}); err != nil {
				return err
			}
			return s.SetCell(3, 1, `<c r="A3"><v>4</v></c>`)
		}, `<sheetData><row r="1"><c r="A1"><v>1</v></c><c r="C1"><v>3</v></c></row><!-- between --><row r="3" ht="30" customHeight="1" hidden="1"><c r="A3"><v>4</v></c><c r="B3" s="0"><v>5</v></c></row></sheetData>`},
		{"two new rows at one point", func(s *Sheet) error {
			if err := s.SetCell(2, 2, `<c r="B2"><v>2</v></c>`); err != nil {
				return err
			}
			return s.SetRowAttrs(2, AttrEdit{Name: "ht", Value: "5"})
		}, `<sheetData><row r="1"><c r="A1"><v>1</v></c><c r="C1"><v>3</v></c></row><!-- between --><row r="2" ht="5"><c r="B2"><v>2</v></c></row><row r="3" ht="20" customHeight="1"><c r="B3" s="0"><v>5</v></c></row></sheetData>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := openBook(t, oneSheet(threeRows))
			s, err := w.Sheet("s")
			if err != nil {
				t.Fatal(err)
			}
			if err := tc.edit(s); err != nil {
				t.Fatal(err)
			}
			got := flushed(t, s)
			if sd := sheetData(t, got); sd != tc.want {
				t.Errorf("got  %s\nwant %s", sd, tc.want)
			}
			// Outside sheetData and dimension, every byte is the original's.
			if strings.Index(got, "<sheetData") != strings.Index(s.orig, "<sheetData") {
				t.Errorf("head changed")
			}
		})
	}
}

func TestOverlayEmptySheetData(t *testing.T) {
	for _, empty := range []bool{true, false} {
		b := xlsxtest.Book{Sheets: []xlsxtest.Sheet{{Name: "S", EmptySheetData: empty}}}
		w := openBook(t, b)
		s, err := w.Sheet("S")
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SetCell(2, 2, `<c r="B2"><v>1</v></c>`); err != nil {
			t.Fatal(err)
		}
		if err := s.SetCell(1, 1, `<c r="A1"><v>1</v></c>`); err != nil {
			t.Fatal(err)
		}
		want := `<sheetData><row r="1"><c r="A1"><v>1</v></c></row><row r="2"><c r="B2"><v>1</v></c></row></sheetData>`
		if got := sheetData(t, flushed(t, s)); got != want {
			t.Errorf("empty %v: got %s", empty, got)
		}
	}
}

func TestOverlayPrefixed(t *testing.T) {
	b := xlsxtest.Book{Sheets: []xlsxtest.Sheet{{Name: "S", Prefix: "x", Dimension: "A1",
		Rows: `<x:row r="1"><x:c r="A1"><x:v>1</x:v></x:c></x:row><x:row r="4"/>`}}}
	w := openBook(t, b)
	s, err := w.Sheet("S")
	if err != nil {
		t.Fatal(err)
	}
	if s.Prefix() != "x:" {
		t.Fatalf("prefix %q", s.Prefix())
	}
	if err := s.SetCell(4, 2, `<x:c r="B4"><x:v>2</x:v></x:c>`); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCell(1, 1, `<c r="A1"><v>1</v></c>`); err == nil {
		t.Error("an unprefixed cell in a prefixed sheet was accepted")
	}
	got := flushed(t, s)
	if !strings.Contains(got, `<x:row r="4"><x:c r="B4"><x:v>2</x:v></x:c></x:row>`) {
		t.Errorf("self-closing row not rebuilt: %s", sheetData(t, got))
	}
	if !strings.Contains(got, `<x:dimension ref="A1:B4"/>`) {
		t.Errorf("dimension not grown: %s", got)
	}
}

func TestOverlayReads(t *testing.T) {
	w := openBook(t, oneSheet(threeRows))
	s, err := w.Sheet("S")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Cell(1, 3)
	if err != nil || !c.Exists || c.Raw.V != "3" || c.XML != `<c r="C1"><v>3</v></c>` {
		t.Fatalf("Cell(1,3) = %+v %v", c, err)
	}
	if err := s.SetCell(1, 3, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCell(2, 2, `<c r="B2" t="inlineStr"><is><t>x</t></is></c>`); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.Cell(1, 3); c.Exists {
		t.Error("deleted cell still exists")
	}
	if c, _ := s.Cell(2, 2); !c.Exists || c.Raw.IS != "x" {
		t.Errorf("pending cell reads %+v", c)
	}
	var order []string
	if err := s.Each(1, 1, 3, 3, func(c Cell) error {
		order = append(order, c.Raw.V+c.Raw.IS)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(order, ","); got != "1,x,5" {
		t.Errorf("Each order %s", got)
	}
	if !w.RecalcRequested() {
		t.Error("value edits did not mark recalculation")
	}
}

func TestOverlayElements(t *testing.T) {
	b := oneSheet(threeRows)
	b.Sheets[0].Tail = `<mergeCells count="1"><mergeCell ref="A1:B1"/></mergeCells>`
	w := openBook(t, b)
	s, err := w.Sheet("S")
	if err != nil {
		t.Fatal(err)
	}
	if x, ok, _ := s.Element("mergeCells"); !ok || !strings.Contains(x, "A1:B1") {
		t.Fatalf("Element mergeCells = %q %v", x, ok)
	}
	if err := s.SetElement("mergeCells", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.SetElement("cols", `<cols><col min="1" max="1" width="20" customWidth="1"/></cols>`); err != nil {
		t.Fatal(err)
	}
	if err := s.SetElement("cols", `<cols><col`); err == nil {
		t.Error("malformed element accepted")
	}
	got := flushed(t, s)
	if strings.Contains(got, "mergeCells") {
		t.Error("mergeCells not removed")
	}
	if i, j := strings.Index(got, "<cols>"), strings.Index(got, "<sheetData"); i < 0 || i > j || i < strings.Index(got, "sheetFormatPr") {
		t.Errorf("cols not placed by schema order: %s", got)
	}
}

func TestOverlayStyles(t *testing.T) {
	b := oneSheet(threeRows)
	b.Sheets[0].Head = `<cols><col min="2" max="4" style="7" width="9"/></cols>`
	w := openBook(t, b)
	s, err := w.Sheet("S")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s.DefaultStyle(1, 3); got != 7 {
		t.Errorf("column style %d", got)
	}
	if err := s.SetRowAttrs(1, AttrEdit{Name: "s", Value: "4"}, AttrEdit{Name: "customFormat", Value: "1"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.DefaultStyle(1, 3); got != 4 {
		t.Errorf("row style %d", got)
	}
	if err := s.SetCellStyle(3, 2, 2); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCellStyle(2, 1, 3); err != nil {
		t.Fatal(err)
	}
	got := sheetData(t, flushed(t, s))
	for _, want := range []string{`<c r="B3" s="2"><v>5</v></c>`, `<row r="2"><c r="A2" s="3"/></row>`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
}

func TestOverlayFormulaAndCache(t *testing.T) {
	w := openBook(t, oneSheet(`<row r="1"><c r="A1" s="1"><f t="shared" ref="A1:A2" si="0" ca="1">B1+1</f><v>3</v></c></row>`))
	s, err := w.Sheet("S")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetFormula(1, 1, &Formula{Text: "B1+2"}); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.Cell(1, 1); c.XML != `<c r="A1" s="1"><f ca="1">B1+2</f><v>3</v></c>` {
		t.Errorf("SetFormula: %s", c.XML)
	}
	if err := s.SetCachedValue(1, 1, "str", "a<b"); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.Cell(1, 1); c.XML != `<c r="A1" s="1" t="str"><f ca="1">B1+2</f><v>a&lt;b</v></c>` {
		t.Errorf("SetCachedValue: %s", c.XML)
	}
	if err := s.ClearCell(1, 1); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.Cell(1, 1); c.XML != `<c r="A1" s="1"/>` {
		t.Errorf("ClearCell: %s", c.XML)
	}
	if err := s.SetCachedValue(1, 1, "", "1"); err == nil {
		t.Error("a cached value was written into a cell without a formula")
	}
}

func TestOverlaySetText(t *testing.T) {
	w := openBook(t, oneSheet(threeRows))
	s, err := w.Sheet("S")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetCell(1, 1, `<c r="A1"><v>7</v></c>`); err != nil {
		t.Fatal(err)
	}
	text, err := s.Text()
	if err != nil {
		t.Fatal(err)
	}
	// A structural rewrite: row 3 becomes row 4.
	text = strings.Replace(strings.Replace(text, `<row r="3"`, `<row r="4"`, 1), `r="B3"`, `r="B4"`, 1)
	if err := s.SetText(text); err != nil {
		t.Fatal(err)
	}
	if err := s.SetText(strings.Replace(text, `<row r="4"`, `<row r="0"`, 1)); err == nil {
		t.Error("a rewrite with a row outside the grid was accepted")
	}
	if err := s.SetCell(4, 3, `<c r="C4"><v>8</v></c>`); err != nil {
		t.Fatal(err)
	}
	got := flushed(t, s)
	want := `<sheetData><row r="1"><c r="A1"><v>7</v></c><c r="C1"><v>3</v></c></row><!-- between --><row r="4" ht="20" customHeight="1"><c r="B4" s="0"><v>5</v></c><c r="C4"><v>8</v></c></row></sheetData>`
	if sd := sheetData(t, got); sd != want {
		t.Errorf("got  %s\nwant %s", sd, want)
	}
	p := s.part.Patch()
	if len(p.Spans()) != 1 {
		t.Errorf("a rewritten sheet is %d spans over the original, want 1", len(p.Spans()))
	}
}

func TestOpenUnreadable(t *testing.T) {
	for name, rows := range map[string]string{
		"rows out of order":  `<row r="2"/><row r="1"/>`,
		"cells out of order": `<row r="1"><c r="B1"/><c r="A1"/></row>`,
		"cell in other row":  `<row r="1"><c r="A2"/></row>`,
	} {
		w := openBook(t, oneSheet(rows))
		_, err := w.Sheet("S")
		var u *Unreadable
		if !errors.As(err, &u) {
			t.Errorf("%s: %v, want *Unreadable", name, err)
		}
	}
	w := openBook(t, oneSheet(""))
	if _, err := w.Sheet("Nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing sheet: %v", err)
	}
}

func TestSetCellRefusesBadXML(t *testing.T) {
	w := openBook(t, oneSheet(threeRows))
	s, err := w.Sheet("S")
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range []string{`<c r="B1"><v>1</v></c>`, `<c r="A1"><v>1</v>`, `<c r="A1"/><c r="A1"/>`, `<v>1</v>`} {
		if err := s.SetCell(1, 1, x); err == nil {
			t.Errorf("SetCell(A1, %s) accepted", x)
		}
	}
	if s.dirty() {
		t.Error("refused edits left the sheet dirty")
	}
}

func FuzzLexNode(f *testing.F) {
	f.Add(`<row r="1"><c r="A1" t='x>'><v>1</v></c><!-- <c> --><![CDATA[<]]><?pi?></row>`)
	f.Fuzz(func(t *testing.T, s string) {
		for i := 0; i < len(s); {
			n, err := xmlscan.Next(s, i)
			if err != nil {
				return
			}
			if n.End <= i || n.End > len(s) {
				t.Fatalf("Next(%d) = %+v", i, n)
			}
			if n.IsElement() {
				for a := range xmlscan.Attrs(s, n) {
					_ = a.Value(s)
				}
				_, _, _ = xmlscan.ElementEnd(s, n) //nolint:errcheck // must not panic
			}
			i = n.End
		}
	})
}

func TestPieces(t *testing.T) {
	b := oneSheet(threeRows)
	b.Sheets[0].Tail = `<mergeCells count="1"><mergeCell ref="A1:B1"/></mergeCells>`
	w := openBook(t, b)
	s, err := w.Sheet("S")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetCell(1, 1, `<c r="A1"><v>2</v></c>`); err != nil {
		t.Fatal(err)
	}
	ps, err := s.Pieces()
	if err != nil {
		t.Fatal(err)
	}
	var locals []string
	for i, p := range ps {
		if p.Local != "" {
			locals = append(locals, p.Local)
		}
		if p.Local == "mergeCells" {
			ps[i].XML = ""
		}
	}
	if got := strings.Join(locals, ","); got != "dimension,sheetViews,sheetFormatPr,sheetData,mergeCells,pageMargins" {
		t.Errorf("pieces %s", got)
	}
	if err := s.SetPieces(ps); err != nil {
		t.Fatal(err)
	}
	got := flushed(t, s)
	if strings.Contains(got, "mergeCells") || !strings.Contains(got, `<c r="A1"><v>2</v></c>`) {
		t.Errorf("SetPieces: %s", got)
	}
}

func TestSetFormulaText(t *testing.T) {
	w := openBook(t, oneSheet(`<row r="1"><c r="A1"><f t="shared" ref="A1:A3" si="2">B1</f><v>1</v></c><c r="B1"><v>1</v></c></row>`))
	s, err := w.Sheet("S")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetFormulaText(1, 1, "Other!B1"); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.Cell(1, 1); c.XML != `<c r="A1"><f t="shared" ref="A1:A3" si="2">Other!B1</f><v>1</v></c>` {
		t.Errorf("SetFormulaText: %s", c.XML)
	}
	if err := s.SetFormulaText(1, 2, "1"); err == nil {
		t.Error("a value cell took a formula text")
	}
}

// TestGrowAfterFlush: dimension growth is computed at each flush, so a
// later cell still widens it after an earlier Flush or Text.
func TestGrowAfterFlush(t *testing.T) {
	w := openBook(t, oneSheet(threeRows))
	s, err := w.Sheet("S")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetCell(4, 4, `<c r="D4"><v>1</v></c>`); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCell(9, 6, `<c r="F9"><v>1</v></c>`); err != nil {
		t.Fatal(err)
	}
	if got := flushed(t, s); !strings.Contains(got, `<dimension ref="A1:F9"/>`) {
		t.Errorf("dimension: %s", got)
	}
}

// TestStructuralDropsOriginal: after SetText the original is not held.
func TestStructuralDropsOriginal(t *testing.T) {
	w := openBook(t, oneSheet(threeRows))
	s, err := w.Sheet("S")
	if err != nil {
		t.Fatal(err)
	}
	text, _ := s.Text()
	if err := s.SetText(strings.Replace(text, `<v>1</v>`, `<v>2</v>`, 1)); err != nil {
		t.Fatal(err)
	}
	if s.orig != "" {
		t.Error("the original is still held after SetText")
	}
	if got := flushed(t, s); !strings.Contains(got, `<c r="A1"><v>2</v></c>`) {
		t.Errorf("flush after SetText: %s", got)
	}
}
