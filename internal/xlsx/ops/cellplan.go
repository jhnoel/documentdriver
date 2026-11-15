package ops

import (
	"fmt"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/edit"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/numfmt"
	"github.com/jhnoel/documentdriver/internal/xlsx/sheet"
)

// The cell-write kernel (chunk K5): pure. write_value.go (designer-owned)
// reads the facts through the edit layer, calls PlanCell, and carries
// the plan out.

// CellFacts is what the writer knows of a target cell before a write.
type CellFacts struct {
	Row, Col int
	// Exists and Raw are the cell as it stands (edit.Cell).
	Exists bool
	Raw    sheet.RawCell
	// Style is the s the write starts from: the cell's own, or
	// Sheet.DefaultStyle for a new cell; NumFmt the kind of its number
	// format (numfmt.Classify; General when the workbook has no styles
	// part or the format is unknown); HasStyles whether the workbook has
	// a styles part.
	Style     int
	NumFmt    numfmt.Kind
	HasStyles bool
	// Date1904 is the workbook's date system.
	Date1904 bool
	// Anchors are the sheet's array and data-table anchors
	// (sheet.Index.Anchors); Tables its tables.
	Anchors []sheet.Anchor
	Tables  []edit.TableRef
}

// CellWrite is what to do.
type CellWrite struct {
	// Clear empties the cell's content (an Empty value): Sheet.ClearCell.
	Clear bool
	// Content is the new content, for edit.RenderCell with Style.
	Content edit.Content
	// Style is the s to write; DateFormat, when not 0, is the built-in
	// numFmt id (14, 22 or 21) Style must take first
	// (Styles.WithNumFmt): a date or time into a cell whose format is not
	// a date or time format (decision 9).
	Style      int
	DateFormat int
	// Unshare says the cell is a shared formula's master (Raw.F shared
	// with text): its group is unshared before the write.
	Unshare bool
}

// PlanCell plans writing v into the cell f describes. Refusals are
// Rejections with Field "" (the glue sets the field):
//
//   - operation_failed: the cell is inside the ref of a multi-cell array
//     formula, dynamic-array spill or data table (anchor or member),
//     detail "an array formula at Budget!A1:A3"-style text without the
//     sheet (the glue qualifies it); the cell is in a table's header row
//     (rows Ref.From.Row .. +HeaderRows-1), detail "the header of table
//     T" (decision 13).
//   - unreadable_target: a date or time when !HasStyles.
//
// Content: numbers, strings (including ""), booleans and errors as
// they are; a date or time is its serial (numfmt.ToSerial with
// Date1904) as a number; a formula is formula.Store of its text (the
// decoder validated it). DateFormat: 14 for Date, 22 for DateTime, 21
// for Time when NumFmt is not Date, DateTime or Time.
func PlanCell(f CellFacts, v CellValue) (CellWrite, error) {
	for _, a := range f.Anchors {
		r1, c1, r2, c2 := a.Ref.Bounds()
		if (r1 != r2 || c1 != c2) && a.Ref.Contains(f.Row, f.Col) {
			kind := "an array formula"
			if a.Type == sheet.DataTable {
				kind = "a data table"
			}
			detail := kind + " at " + a.Ref.String()
			return CellWrite{}, &Rejection{Code: cli.CodeOperationFailed, Msg: "cannot write inside " + detail, Detail: detail}
		}
	}
	for _, t := range f.Tables {
		r1, _, _, _ := t.Ref.Bounds()
		if t.Ref.Contains(f.Row, f.Col) && f.Row-r1 < t.HeaderRows {
			detail := "the header of table " + t.Name
			return CellWrite{}, &Rejection{Code: cli.CodeOperationFailed, Msg: "cannot write inside " + detail, Detail: detail}
		}
	}
	w := CellWrite{Style: f.Style, Unshare: f.Exists && f.Raw.F.Present && f.Raw.F.Type == sheet.Shared && f.Raw.F.Text != ""}
	switch v.Kind {
	case Empty:
		w.Clear = true
	case Number:
		w.Content = edit.Content{Kind: edit.ContentNumber, Num: v.Num}
	case String:
		w.Content = edit.Content{Kind: edit.ContentString, Text: v.Text}
	case Bool:
		w.Content = edit.Content{Kind: edit.ContentBool, Bool: v.Bool}
	case Error:
		w.Content = edit.Content{Kind: edit.ContentError, Text: v.Text}
	case Date, DateTime, Time:
		if !f.HasStyles {
			return CellWrite{}, &Rejection{Code: cli.CodeUnreadableTarget, Msg: "cannot write a date or time without styles", Detail: "the workbook has no styles part"}
		}
		serial, ok := numfmt.ToSerial(v.Text, f.Date1904)
		if !ok {
			return CellWrite{}, fmt.Errorf("invalid date or time %q", v.Text)
		}
		w.Content = edit.Content{Kind: edit.ContentNumber, Num: serial}
		if f.NumFmt != numfmt.Date && f.NumFmt != numfmt.DateTime && f.NumFmt != numfmt.Time {
			switch v.Kind {
			case Date:
				w.DateFormat = 14
			case DateTime:
				w.DateFormat = 22
			case Time:
				w.DateFormat = 21
			}
		}
	case Formula:
		stored, err := formula.Store(v.Text)
		if err != nil {
			return CellWrite{}, fmt.Errorf("store formula: %w", err)
		}
		w.Content = edit.Content{Kind: edit.ContentFormula, Formula: stored}
	default:
		return CellWrite{}, fmt.Errorf("unknown cell value kind %d", v.Kind)
	}
	return w, nil
}
