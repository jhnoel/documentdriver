package read

import (
	"fmt"
	"strings"

	"github.com/jhnoel/documentdriver/internal/casefold"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
	"github.com/jhnoel/documentdriver/internal/xlsx/sheet"
	"github.com/jhnoel/documentdriver/internal/xlsx/sml"
)

// FindRequest is find's arguments and flags.
type FindRequest struct {
	// Query is the text searched for; "" matches every cell that has
	// text in the view (as docx's find matches every paragraph).
	Query string
	// IgnoreCase is --ignore-case: both sides through casefold.Simple.
	IgnoreCase bool
	// Sheet is --sheet ("" for every worksheet in tab order, hidden ones
	// included).
	Sheet string
	// View is model.ViewValues (default) or model.ViewFormulas.
	View model.View
	// Limit is the most matches; 0 is no limit. cmd/xlsx passes
	// DefaultLimit when --limit is absent.
	Limit int
}

// Find is `xlsx find <file> <query>`.
//
// Values view: a cell matches when model.Value.MatchText of its value
// (after converter.value: dates, booleans and numbers as read prints
// them) contains the query. Shared strings are searched first (survey
// R12): on the first t="s" cell of the search, one SST.Each pass
// records the matching item indexes in a bitset (1 bit per item); a
// t="s" cell then matches by its index's bit, and only matching items
// are decoded again (SST.Text) for the match's value. A search that
// meets no t="s" cell never reads the sst.
//
// Formulas view: a cell matches when its display formula ("=" +
// Display, a shared child written out: converter.formulaText) contains
// the query; cells without a formula never match, and spill members
// (which have none) never match. The sst is never read.
//
// Matches are in tab order, then row-major order. Each worksheet is
// scanned with sheet.Scan (the whole sheet as the band) and forgotten
// after. Without --sheet, a sheet that cannot be read is skipped with a
// NotePart note at its address; non-worksheets are skipped silently
// (the summary notes them). With --sheet, the errors are Cells'
// (not_found, unreadable_target; a non-worksheet: no matches and a
// NoteSheetKind note).
//
// Limit: when a match beyond the first Limit is found the search stops
// (at any cell, not a row boundary: a match is one cell) without it;
// Truncated.After is the last match returned and a NoteTruncated note
// says so. Exactly Limit matches is not a cut.
//
// Errors: openBook's; --sheet's; nothing else.
func Find(input []byte, req FindRequest, opt Options) (*FindResult, error) {
	b, err := openBook(input, opt)
	if err != nil {
		return nil, err
	}
	defer b.Close()

	res := &FindResult{
		Base:        b.Base,
		ValuesStale: b.FullCalcOnLoad,
		Matches:     []Match{},
	}
	f := &finder{book: b, req: req, res: res, folded: req.Query, maxBytes: opt.maxOutput()}
	if req.IgnoreCase {
		f.folded = casefold.Simple(req.Query)
	}
	if req.Sheet != "" {
		t, err := b.resolve("", req.Sheet)
		if e := b.Err(); e != nil {
			return nil, invalidDocument("the workbook exceeds a safety limit", e)
		}
		if err != nil {
			return nil, err
		}
		if err := f.scanSheet(t.sheet); err != nil {
			return nil, err
		}
	} else {
		for i := range b.Sheets {
			if err := f.scanSheet(&b.Sheets[i]); err != nil {
				return nil, err
			}
			if f.cut {
				break
			}
		}
	}
	res.Notes = b.Notes.List()
	if e := b.Err(); e != nil {
		return nil, invalidDocument("the workbook exceeds a safety limit", e)
	}
	return res, nil
}

// finder is one Find's state: the book, the request, the result being
// filled, and the shared strings' match bitset. The bitset is built once
// per Find, on the first t="s" cell of any sheet, and lives across
// sheets (the sst's item indexes are the same for every sheet).
type finder struct {
	book *Book
	req  FindRequest
	res  *FindResult
	// folded is the query, through casefold.Simple when IgnoreCase.
	folded string
	// conv is the current sheet's converter (ix set per row).
	conv *converter
	// bs is the set of matching shared-string indexes; built says build
	// was tried (an unreadable sst leaves bs empty).
	bs    bitset
	built bool
	// cut says the limit stopped the search: no further sheet is scanned.
	cut bool
	// bytes is the size of the matches so far; maxBytes the output
	// budget (Options.maxOutput).
	bytes, maxBytes int
}

// scanSheet searches one worksheet. With --sheet an unreadable sheet is
// the command's error; without, it is skipped with a NotePart note. A
// non-worksheet is skipped silently without --sheet, and with --sheet is
// an empty result with a NoteSheetKind note.
func (f *finder) scanSheet(sh *SheetInfo) error {
	one := f.req.Sheet != ""
	if sh.Kind != Worksheet {
		switch {
		case one:
			f.book.Notes.Add(model.NoteSheetKind, model.SheetID(sh.Name), "a "+sheetKindWord(sh.Kind)+" has no cells to read")
		case sh.Kind == MissingSheet:
			f.book.Notes.Add(model.NotePart, model.SheetID(sh.Name), "the sheet cannot be read")
		}
		return nil
	}
	text, err := f.book.SheetText(sh)
	if e := f.book.Err(); e != nil {
		return invalidDocument("the workbook exceeds a safety limit", e)
	}
	if err == nil {
		f.conv = newConverter(f.book, sh.Name)
		_, err = sheet.Scan(text, sheet.ScanOptions{From: 1, To: formula.MaxRows, Row: f.row})
		if e := f.book.Err(); e != nil {
			return invalidDocument("the workbook exceeds a safety limit", e)
		}
	}
	f.book.Forget(sh)
	if err != nil {
		if one {
			return unreadable("--sheet", sh.Name, err)
		}
		f.book.Notes.Add(model.NotePart, model.SheetID(sh.Name), "the sheet cannot be read")
	}
	return nil
}

// row is sheet.Scan's callback: convert and test each cell of the row in
// column order, adding the matches. It returns sheet.ErrStopRows when the
// limit cuts.
func (f *finder) row(sr *sheet.Row) error {
	f.conv.ix = sr.Index
	for i := range sr.Cells {
		rc := &sr.Cells[i]
		if f.req.View == model.ViewFormulas {
			formula := f.conv.formulaText(rc)
			if formula == "" || !f.match(formula) {
				continue
			}
			if f.add(model.Cell(f.conv.sheet, rc.Row, rc.Col), model.Entry{Formula: formula}.AppendJSON(nil, model.ViewFormulas)) {
				return sheet.ErrStopRows
			}
			continue
		}
		v, ok := f.cellValue(rc)
		if !ok || !f.match(v.MatchText()) {
			continue
		}
		if f.add(model.Cell(f.conv.sheet, rc.Row, rc.Col), v.AppendJSON(nil)) {
			return sheet.ErrStopRows
		}
	}
	return nil
}

// cellValue is a cell's value in the values view. A t="s" cell is decided
// by the bitset first: converter.value (which decodes the item) runs only
// for a set bit, so a non-matching item is never decoded. A search that
// meets no t="s" cell never reads the sst (survey R12).
func (f *finder) cellValue(rc *sheet.RawCell) (model.Value, bool) {
	if rc.T == "s" {
		f.build()
		idx, ok := sml.ParseIndex(rc.V)
		if !rc.HasV || !ok || !f.bs.has(idx) {
			return model.Value{}, false
		}
	}
	return f.conv.value(rc)
}

// build indexes the shared strings' matching items in one SST.Each pass,
// once per Find (casefold.Simple on both sides when IgnoreCase).
func (f *finder) build() {
	if f.built {
		return
	}
	f.built = true
	sst := f.book.SST()
	if sst == nil {
		return
	}
	bs := newBitset(sst.Len())
	if err := sst.Each(func(i int, text string) error {
		if f.match(text) {
			bs.set(i)
		}
		return nil
	}); err != nil {
		// Each reports only the callback's error, and the callback always
		// returns nil. OpenSST scanned the same text, so the tokenizer
		// cannot fail here; leave the bitset empty (no item matches).
		return
	}
	f.bs = bs
}

// match reports whether text contains the query, folding text first when
// the search ignores case.
func (f *finder) match(text string) bool {
	if f.req.IgnoreCase {
		text = casefold.Simple(text)
	}
	return strings.Contains(text, f.folded)
}

// add records a match at cell with pre-encoded value val, and reports
// whether the limit cuts the search (the caller stops the scan).
func (f *finder) add(cell string, val []byte) bool {
	size := len(cell) + len(val) + 24
	byLimit := f.req.Limit > 0 && len(f.res.Matches) >= f.req.Limit
	bySize := f.bytes+size > f.maxBytes
	if (byLimit || bySize) && len(f.res.Matches) > 0 {
		after := f.res.Matches[len(f.res.Matches)-1].Cell
		f.res.Truncated = &FindCut{After: after}
		why := "at --limit"
		if !byLimit {
			why = fmt.Sprintf("at the %d MiB output limit", f.maxBytes>>20)
		}
		f.book.Notes.Add(model.NoteTruncated, after, fmt.Sprintf("search stopped after %d matches %s", len(f.res.Matches), why))
		f.cut = true
		return true
	}
	// A first match larger than the budget is still returned: one cell
	// is bounded by model.MaxText and MaxFormula.
	f.bytes += size
	f.res.Matches = append(f.res.Matches, Match{Cell: cell, Value: RawJSON(val)})
	return false
}

// bitset is the set of matching shared-string indexes.
type bitset []uint64

// newBitset returns a bitset sized for n indexes, all clear.
func newBitset(n int) bitset { return make(bitset, (n+63)/64) }

// set marks index i in the bitset.
func (b bitset) set(i int) { b[i/64] |= 1 << (uint(i) % 64) }

// has reports whether index i is marked in the bitset.
func (b bitset) has(i int) bool { return i >= 0 && i/64 < len(b) && b[i/64]&(1<<(uint(i)%64)) != 0 }
