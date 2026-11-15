package read

import (
	"fmt"
	"strings"
	"time"

	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
	"github.com/jhnoel/documentdriver/internal/xlsx/numfmt"
	"github.com/jhnoel/documentdriver/internal/xlsx/sheet"
	"github.com/jhnoel/documentdriver/internal/xlsx/sml"
)

// converter turns the RawCells of one sheet into Entries. It is built
// per sheet scan and used from the Scan callback, so the Index it holds
// is the one being built (masters and anchors seen so far).
type converter struct {
	book  *Book
	sheet string
	ix    *sheet.Index
	notes *model.Notes
	// The parts, loaded once (nil when absent or unreadable).
	styles *Styles
	meta   *Metadata
	// sst is loaded on the first t="s" cell (find skips the sst when a
	// sheet has none; survey R12).
	sstDone bool
	sst     *sheet.SST
	// long are the shared string items and master formulas (by
	// Index.Masters position) found longer than Excel allows: noted once
	// per cell, never decoded or translated again.
	longSST    map[int]bool
	longMaster map[int]bool
}

// newConverter makes a converter for the named sheet of b (b not nil):
// its notes go to b.Notes, and it loads b.Styles and b.Metadata now and
// the shared strings on first need. The caller sets ix from each
// sheet.Row's Index before converting that row's cells.
func newConverter(b *Book, sheetName string) *converter {
	return &converter{book: b, sheet: sheetName, notes: b.Notes, styles: b.Styles(), meta: b.Metadata()}
}

// entry converts one cell. ok is false when the cell has nothing to show
// in any view: no readable value and no derivable formula and no array
// membership (each problem was noted).
//
// Rules:
//
//   - Value (value): see value.
//   - Formula: a cell with <f>:
//   - Normal, Array, UnknownType: "=" + formula.Display(F.Text). An
//     empty text gives no formula (a NoteFormula note).
//   - Shared with text (a master): as Normal.
//   - Shared without text (a child): the master in effect at this cell,
//     ix.Masters()[rc.Master-1] (resolved by the scan in document order;
//     never Index.Master(si), which an si reused later in the row would
//     answer wrongly), formula.Translate(text, master cell, this cell),
//     then Display. rc.Master 0, or Translate refusing: no formula, a
//     NoteFormula note with a fixed reason ("shared formula has no master
//     before this cell", "shared formula cannot be written out for this
//     cell") so the notes aggregate.
//   - DataTable: Formula "=" + Display(F.Text) when F.Text is not
//     empty, else none; Array model.DataTable; always a NoteDataTable
//     note at the anchor naming ref, r1, r2, dt2D and dtr.
//   - Array: Array is Metadata.ArrayKind(rc.CM); UnknownArray adds a
//     NoteArray note. Dynamic anchors show as plain formula cells.
//   - Members (rc.In != 0): Spill is model.Cell of the anchor; Array is
//     the anchor's kind (as above, for its CM; DataTable for a data
//     table). A member's own value is shown as for any cell.
//   - rc.VM != 0: a NoteRichValue note; the stored value is shown.
func (c *converter) entry(rc *sheet.RawCell) (model.Entry, bool) {
	v, _ := c.value(rc)
	e := model.Entry{Value: v, Formula: c.formulaText(rc)}
	if rc.F.Present {
		switch rc.F.Type {
		case sheet.Array:
			e.Array = c.arrayKind(rc, rc.CM)
		case sheet.DataTable:
			e.Array = model.DataTable
			c.note(rc, model.NoteDataTable, fmt.Sprintf("data table: ref=%s, r1=%s, r2=%s, dt2D=%t, dtr=%t",
				dataTableRef(rc.F.Ref, true), dataTableRef(rc.F.R1, false), dataTableRef(rc.F.R2, false), rc.F.DT2D, rc.F.DTR))
		}
	}
	if rc.In > 0 && c.ix != nil && rc.In <= len(c.ix.Anchors()) {
		a := c.ix.Anchors()[rc.In-1]
		e.Spill = model.Cell(c.sheet, a.Row, a.Col)
		if a.Type == sheet.DataTable {
			e.Array = model.DataTable
		} else {
			e.Array = c.arrayKind(rc, a.CM)
		}
	}
	if rc.VM != 0 {
		c.note(rc, model.NoteRichValue, "value metadata is not modelled; the stored value is shown")
	}
	return e, e.Shown(model.ViewCells)
}

// value converts a cell's stored value. ok false means no value (the
// cell stores none, or it cannot be read and a NoteValue note says why;
// the reason names the problem, never the text).
//
//	T          stored                    value
//	"s"        <v> index                 String: SST().Text(index); a bad index, a
//	                                     missing or unreadable sst: none, NoteValue
//	"str"      <v> text                  String: UnescapeX(V); a formula cell's text result
//	"inlineStr" <is>                     String: UnescapeX(IS) (also when <v> is present)
//	"b"        "1"/"0" ("true"/"false")  Bool; anything else: none, NoteValue
//	"e"        <v> text                  Error: V as stored
//	"n", ""    <v> number                number (see below); not a number
//	                                     (model.ParseNumber): none, NoteValue
//	"d"        ISO 8601 text             date (see below)
//	other      –                         none, NoteValue ("unknown cell type")
//
// A cell with no <v> (and no <is>) has no value: a formula without a
// cached result, or a style-only cell.
//
// Numbers: Styles.Kind(rc.S) classifies the cell's format.
//
//   - Date, DateTime, Time: numfmt.Serial(v, Book.Date1904, kind); ok
//     gives Date (Key "date") or Time (Key "time"). Not ok: the number,
//     with a NoteSerial note ("serial below 61 in the 1900 date system",
//     "serial outside the dates a cell can show"; decision 9).
//   - Duration: the number, with a NoteDuration note naming the format
//     code (cut to 64 bytes; decision 9).
//   - General, Number, Text, or no style: the number.
//
// Strict t="d" (ISO 8601 "2024-03-01", "2024-03-01T13:30:00",
// "2024-03-01T13:30:00.000", optionally "Z"; a time-only "13:30:00" or
// "T13:30:00"): the date part and the time part as parsed (a date must
// exist on the calendar, a time be below 24:00:00), then: a time-only
// text gives {"time"}; otherwise the cell's format kind picks the form:
// Time gives {"time"} (the time part); Date gives {"date":"YYYY-MM-DD"};
// DateTime, or any other kind with a non-midnight time, gives
// {"date":"...T..."}; otherwise {"date":"YYYY-MM-DD"}. Fractional seconds are rounded as numfmt.Serial
// rounds (half away from zero). Text that does not parse: the String
// as stored, with a NoteValue note.
func (c *converter) value(rc *sheet.RawCell) (model.Value, bool) {
	if !rc.HasV && !rc.HasIS {
		return model.Value{}, false
	}
	reason := ""
	switch rc.T {
	case "inlineStr":
		if rc.HasIS {
			return c.text(rc, sml.UnescapeX(rc.IS))
		}
		reason = "inline string has no text element"
	case "s":
		idx, ok := sml.ParseIndex(rc.V)
		if !rc.HasV || !ok {
			reason = "shared string index cannot be read"
			break
		}
		if !c.sstDone {
			c.sstDone = true
			c.sst = c.book.SST()
		}
		if c.sst == nil {
			reason = "shared strings are missing or unreadable"
			break
		}
		if c.longSST[idx] {
			reason = tooLongText
			break
		}
		text, err := c.sst.Text(idx)
		if err != nil {
			reason = "shared string index is not in the shared strings"
			break
		}
		if model.TextLen(text, model.MaxText) > model.MaxText {
			if c.longSST == nil {
				c.longSST = map[int]bool{}
			}
			c.longSST[idx] = true
			reason = tooLongText
			break
		}
		return model.Value{Kind: model.String, Text: text}, true
	case "str":
		if rc.HasV {
			return c.text(rc, sml.UnescapeX(rc.V))
		}
	case "b":
		if rc.HasV {
			switch rc.V {
			case "1", "true":
				return model.Value{Kind: model.Bool, Bool: true}, true
			case "0", "false":
				return model.Value{Kind: model.Bool}, true
			}
			reason = "boolean value cannot be read"
		}
	case "e":
		if rc.HasV {
			return model.Value{Kind: model.Error, Text: rc.V}, true
		}
	case "n", "":
		if rc.HasV {
			if v, ok := model.ParseNumber(rc.V); ok {
				return c.number(rc, v), true
			}
			reason = "numeric value cannot be read"
		}
	case "d":
		if rc.HasV {
			k, _ := c.styleKind(rc)
			text := sml.UnescapeX(rc.V)
			if v, ok := isoDate(text, k); ok {
				return v, true
			}
			if v, ok := c.text(rc, text); !ok {
				return v, false
			}
			c.note(rc, model.NoteValue, "ISO date cannot be read; the stored text is shown")
			return model.Value{Kind: model.String, Text: text}, true
		}
	default:
		reason = "unknown cell type"
	}
	if reason != "" {
		c.note(rc, model.NoteValue, reason)
	}
	return model.Value{}, false
}

// number is the value of a numeric cell with style xf (the Numbers
// rules of value).
func (c *converter) number(rc *sheet.RawCell, v float64) model.Value {
	out := model.Value{Kind: model.Number, Num: v}
	k, code := c.styleKind(rc)
	switch k {
	case numfmt.Date, numfmt.DateTime, numfmt.Time:
		if date, ok := numfmt.Serial(v, c.book.Date1904, k); ok {
			kind := model.Date
			if date.Key == "time" {
				kind = model.Time
			}
			switch numfmt.Hidden(v, k) {
			case "time":
				c.note(rc, model.NoteValue, "stored value also has a time of day; the format shows the date only")
			case "days":
				c.note(rc, model.NoteValue, "stored value also has whole days; the format shows the time only")
			}
			return model.Value{Kind: kind, Text: date.Text}
		}
		reason := "serial outside the dates a cell can show"
		if !c.book.Date1904 && k != numfmt.Time && v < 61 {
			reason = "serial below 61 in the 1900 date system"
		}
		c.note(rc, model.NoteSerial, reason)
	case numfmt.Duration:
		if len(code) > 64 {
			code = code[:64]
		}
		c.note(rc, model.NoteDuration, "duration is shown as a number; format "+code)
	}
	return out
}

// isoDate converts a Strict t="d" text for a cell of kind k (the
// t="d" rules of value).
func isoDate(text string, k numfmt.Kind) (model.Value, bool) {
	text = strings.TrimSuffix(text, "Z")
	timeOnly := len(text) >= 3 && (text[0] == 'T' || text[2] == ':')
	layout := "2006-01-02"
	baseLen := 10
	if timeOnly {
		text = "2000-01-01T" + strings.TrimPrefix(text, "T")
	}
	if len(text) > 10 {
		layout = "2006-01-02T15:04:05"
		baseLen = 19
	}
	if len(text) < baseLen {
		return model.Value{}, false
	}
	if len(text) > baseLen {
		if text[baseLen] != '.' || len(text) == baseLen+1 || baseLen != 19 {
			return model.Value{}, false
		}
		for i := baseLen + 1; i < len(text); i++ {
			if text[i] < '0' || text[i] > '9' {
				return model.Value{}, false
			}
		}
	}
	t, err := time.Parse(layout, text)
	if err != nil {
		return model.Value{}, false
	}
	nonMidnight := t.Hour() != 0 || t.Minute() != 0 || t.Second() != 0 || t.Nanosecond() != 0
	t = t.Round(time.Second)
	if timeOnly || k == numfmt.Time {
		return model.Value{Kind: model.Time, Text: t.Format("15:04:05")}, true
	}
	if t.Year() > 9999 {
		return model.Value{}, false
	}
	out := t.Format("2006-01-02")
	if k != numfmt.Date && (k == numfmt.DateTime || nonMidnight) {
		out += "T" + t.Format("15:04:05")
	}
	return model.Value{Kind: model.Date, Text: out}, true
}

// formulaText is the display formula of a cell with <f> ("" when it
// cannot be derived; the Formula rules of entry).
func (c *converter) formulaText(rc *sheet.RawCell) string {
	if !rc.F.Present {
		return ""
	}
	text := rc.F.Text
	if text == "" && rc.F.Type == sheet.Shared {
		if c.ix == nil || rc.Master < 1 || rc.Master > len(c.ix.Masters()) {
			c.note(rc, model.NoteFormula, "shared formula has no master before this cell")
			return ""
		}
		if c.longMaster[rc.Master] {
			c.note(rc, model.NoteFormula, tooLongFormula)
			return ""
		}
		m := c.ix.Masters()[rc.Master-1]
		var err error
		text, err = formula.Translate(m.Text, formula.Cell{Row: m.Row, Col: m.Col}, formula.Cell{Row: rc.Row, Col: rc.Col})
		if err != nil {
			c.note(rc, model.NoteFormula, "shared formula cannot be written out for this cell")
			return ""
		}
	}
	if text == "" {
		if rc.F.Type != sheet.DataTable {
			c.note(rc, model.NoteFormula, "formula has no text")
		}
		return ""
	}
	shown := "=" + formula.Display(text)
	if model.TextLen(shown, model.MaxFormula+1) > model.MaxFormula+1 {
		// (+1: the "=" is not counted.)
		if rc.Master > 0 {
			if c.longMaster == nil {
				c.longMaster = map[int]bool{}
			}
			c.longMaster[rc.Master] = true
		}
		c.note(rc, model.NoteFormula, tooLongFormula)
		return ""
	}
	return shown
}

// The fixed reasons of over-long content (model.MaxText, MaxFormula).
const (
	tooLongText    = "text is longer than Excel's 32,767 characters; left out"
	tooLongFormula = "formula is longer than Excel's 8,192 characters; left out"
)

// text is a string value, or none with a note when it is longer than
// Excel allows.
func (c *converter) text(rc *sheet.RawCell, s string) (model.Value, bool) {
	if model.TextLen(s, model.MaxText) > model.MaxText {
		c.note(rc, model.NoteValue, tooLongText)
		return model.Value{}, false
	}
	return model.Value{Kind: model.String, Text: s}, true
}

// note records a fixed per-cell reason, aggregated within the sheet.
func (c *converter) note(rc *sheet.RawCell, kind model.NoteKind, reason string) {
	c.notes.AddCell(kind, c.sheet, rc.Row, rc.Col, reason)
}

// styleKind resolves the number format, noting an invalid style index.
func (c *converter) styleKind(rc *sheet.RawCell) (numfmt.Kind, string) {
	if !rc.SOK {
		c.note(rc, model.NoteValue, "style index cannot be read; the value is shown without a number format")
		return numfmt.General, ""
	}
	k, code, ok := c.styles.Kind(rc.S)
	if !ok && c.styles != nil {
		c.note(rc, model.NoteValue, fmt.Sprintf("style index %d is not in cellXfs", rc.S))
	}
	return k, code
}

// arrayKind resolves an array's metadata, noting an unresolved chain.
func (c *converter) arrayKind(rc *sheet.RawCell, cm int) model.ArrayKind {
	k := c.meta.ArrayKind(cm)
	if k == model.UnknownArray {
		c.note(rc, model.NoteArray, "array kind cannot be derived from the cell metadata")
	}
	return k
}

// dataTableRef keeps notes bounded to parsed addresses, never arbitrary text.
func dataTableRef(text string, area bool) string {
	if text == "" {
		return "absent"
	}
	if area {
		if r, ok := formula.ParseArea(text); ok {
			return r.String()
		}
	} else if cell, ok := formula.ParseCell(text); ok {
		return cell.String()
	}
	return "unreadable"
}
