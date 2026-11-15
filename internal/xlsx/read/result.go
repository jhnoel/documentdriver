package read

import (
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
)

// The results. Each is one JSON document on stdout, encoded with
// cli.Marshal (no HTML escaping) and printed with cli.WriteJSON; struct
// field order is key order, so base is first (contract §4) and notes
// last. Lists the contract shows are never null: an empty list is [].

// WorkbookResult is `xlsx read <file>` (and `--kind workbook`): the
// summary of design §2 without opaque (A2).
type WorkbookResult struct {
	Base string `json:"base"`
	// ValuesStale is calcPr@fullCalcOnLoad (decision 7); left out when
	// false.
	ValuesStale bool         `json:"values_stale,omitempty"`
	Sheets      []SheetEntry `json:"sheets"`
	Names       []NameEntry  `json:"names"`
	Tables      []TableEntry `json:"tables"`
	Notes       []model.Note `json:"notes"`
}

// SheetEntry is one sheet of the summary, in tab order.
type SheetEntry struct {
	// ID is the sheet's address, "sheet:Budget".
	ID   string `json:"id"`
	Name string `json:"name"`
	// State is "visible", "hidden" or "very_hidden" (stateName).
	State string `json:"state"`
	// Dimension is the stored dimension@ref, sheet-qualified
	// ("Budget!A1:D10", model.Qualified of the parsed ref); left out when
	// the sheet stores none, it does not parse (a NoteSheet note), or the
	// sheet is not a worksheet.
	Dimension string `json:"dimension,omitempty"`
	// Tables are the names of the sheet's tables (approved: names only;
	// the top-level tables list has the details).
	Tables []string `json:"tables"`
}

// NameEntry is one defined name (approved shape {name, scope,
// refers_to, hidden}).
type NameEntry struct {
	// Name is the stored name.
	Name string `json:"name"`
	// Scope is "workbook" or the scope sheet's address ("sheet:Budget");
	// left out when localSheetId is invalid (a NoteName note).
	Scope string `json:"scope,omitempty"`
	// RefersTo is "=" and formula.Display of the stored text.
	RefersTo string `json:"refers_to"`
	// Hidden is definedName@hidden ("1" or "true"); left out when false.
	Hidden bool `json:"hidden,omitempty"`
}

// TableEntry is one table (approved shape {name, sheet, ref, columns}).
type TableEntry struct {
	Name string `json:"name"`
	// Sheet is the holding sheet's address, "sheet:Budget".
	Sheet string `json:"sheet"`
	// Ref is the table's range, sheet-qualified ("Budget!A1:D20"),
	// header and totals rows included.
	Ref     string   `json:"ref"`
	Columns []string `json:"columns"`
}

// CellsResult is `xlsx read <file> --sheet S` or `--range R`.
type CellsResult struct {
	Base string `json:"base"`
	// Sheet is the sheet's address, "sheet:Budget".
	Sheet string `json:"sheet"`
	// Range is the range read, sheet-qualified and normalised: the
	// --range as resolved (a name or table resolves to its plain range),
	// or the used range for --sheet alone (approved); left out when
	// --sheet names a sheet with no used range.
	Range string `json:"range,omitempty"`
	// UsedRange is the sheet's scanned used range (non-empty cells only,
	// survey R13), sheet-qualified (approved key); left out for a sheet
	// with no non-empty cell.
	UsedRange string `json:"used_range,omitempty"`
	// View is "cells", "values" or "formulas".
	View string `json:"view"`
	// ValuesStale as in WorkbookResult.
	ValuesStale bool `json:"values_stale,omitempty"`
	// Cells are the cells read, keyed by bare A1 address, in row-major
	// order.
	Cells *CellMap `json:"cells"`
	// Groups are the formula groups of --group (formulas view only);
	// left out otherwise.
	Groups []Group `json:"groups,omitempty"`
	// Merges are the merged ranges that intersect Range, unclipped,
	// sheet-qualified, in document order.
	Merges []string `json:"merges"`
	// Hidden are the hidden row runs ("Budget!5:7") then hidden column
	// runs ("Budget!C:C") that intersect Range, clipped to it (decision 5;
	// clipping approved).
	Hidden []string `json:"hidden"`
	// Truncated is set when --limit cut the read (decision 6).
	Truncated *Truncated   `json:"truncated,omitempty"`
	Notes     []model.Note `json:"notes"`
}

// Truncated is where a cut read resumes.
type Truncated struct {
	// Next is the rest of Range from the first row not read: "Budget!A501:D1000"
	// (for a whole-rows range, "Budget!501:1000"), its last row the
	// smaller of Range's and the used range's.
	Next string `json:"next"`
}

// Group is one --group entry (approved shape): the members
// of one shared-formula group inside the range, which fill the rectangle
// At, all holding F written relative to At's top-left cell, as Excel
// fills a formula down or across. Their cells are not repeated in Cells.
// A group whose members in the range do not fill a rectangle, or that
// has one member in the range, is not grouped: its cells stay in Cells.
type Group struct {
	At string `json:"at"`
	F  string `json:"f"`
}

// CellMap is read's `cells` object, encoded as it is filled: one buffer of
// `"A1":<entry>` members, never a map or a []model.Entry, so its memory
// is the output's. The zero CellMap is an empty object.
type CellMap struct {
	buf []byte
	n   int
}

// Add appends the entry of the cell at row, col in view. The caller adds
// cells in row-major order and each cell once (sheet.Scan guarantees
// order and uniqueness), and only entries Shown in view.
func (c *CellMap) Add(row, col int, e model.Entry, view model.View) {
	if !e.Shown(view) {
		return
	}
	if c.n > 0 {
		c.buf = append(c.buf, ',')
	}
	c.buf = append(c.buf, '"')
	c.buf = model.AppendCellKey(c.buf, row, col)
	c.buf = append(c.buf, '"', ':')
	c.buf = e.AppendJSON(c.buf, view)
	c.n++
}

// Len is the number of cells added.
func (c *CellMap) Len() int { return c.n }

// MarshalJSON writes the object: "{" + members + "}".
func (c *CellMap) MarshalJSON() ([]byte, error) {
	out := make([]byte, 0, len(c.buf)+2)
	out = append(out, '{')
	out = append(out, c.buf...)
	out = append(out, '}')
	return out, nil
}

// FindResult is `xlsx find <file> <query>`.
type FindResult struct {
	Base        string       `json:"base"`
	ValuesStale bool         `json:"values_stale,omitempty"`
	Matches     []Match      `json:"matches"`
	Truncated   *FindCut     `json:"truncated,omitempty"`
	Notes       []model.Note `json:"notes"`
}

// Match is one matching cell (approved shape {"cell","value"}).
type Match struct {
	// Cell is the sheet-qualified address ("Budget!A1").
	Cell string `json:"cell"`
	// Value is the cell in the searched view: the value form for
	// --view values (model.Value.AppendJSON), the formula string
	// "=..." for --view formulas.
	Value RawJSON `json:"value"`
}

// FindCut is where a cut find stopped (approved shape: find
// has no --range to resume with): the last cell matched.
type FindCut struct {
	After string `json:"after"`
}

// RawJSON is a pre-encoded JSON value (encoding/json.RawMessage without
// importing encoding/json into the result types' users).
type RawJSON []byte

// MarshalJSON returns the bytes.
func (r RawJSON) MarshalJSON() ([]byte, error) {
	if len(r) == 0 {
		return []byte("null"), nil
	}
	return r, nil
}
