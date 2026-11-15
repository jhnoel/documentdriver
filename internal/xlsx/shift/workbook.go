package shift

import (
	"fmt"
	"strings"

	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/sheet"
)

// The references outside the edited sheet (kernel K9a: OtherSheet and DefinedName; Table is table.go, K9b;
// Guard is guard.go, K9c).
// Every function here is pure: it takes the bytes or texts it needs and
// returns the rewrite and its Stats, with no I/O and no shared state.
// Apply (chunk K9) reads the inputs through the edit layer and writes
// the outputs back.

// FormulaEdit is one formula cell of another sheet to rewrite: its new
// <f> text (stored form). Shared groups keep their type, ref and si
// (on another sheet no cell moves, so only a master's text changes).
type FormulaEdit struct {
	Row, Col int
	Text     string
}

// OtherSheet returns the formula rewrites another worksheet needs for e
// (src its whole text, sh.Own its name): a byte prefilter for e.Sheet's
// name (bare or quoted, ignoring case) returns nothing at once for a
// sheet that cannot reference it; otherwise one sheet.Scan finds every
// <f> with text and formula.Shift rewrites it. Stats.RefErrors are the
// cells (A1, no sheet) that gained #REF!. A formula that does not lex
// cleanly (formula.Validate fails) and mentions the sheet's name is
// refused (*Refused naming the cell, survey R6).
func OtherSheet(src string, sh formula.Sheets, e formula.Edit) ([]FormulaEdit, Stats, error) {
	name := strings.ToLower(e.Sheet)
	lower := strings.ToLower(src)
	quoted := strings.ReplaceAll(name, "'", "''")
	if name != "" && !strings.Contains(lower, name) && !strings.Contains(lower, quoted) && !strings.Contains(src, "&") {
		return nil, Stats{}, nil
	}
	var edits []FormulaEdit
	var stats Stats
	_, err := sheet.Scan(src, sheet.ScanOptions{From: 1, To: formula.MaxRows, Row: func(row *sheet.Row) error {
		for _, cell := range row.Cells {
			text := cell.F.Text
			if text == "" {
				continue
			}
			address := (formula.Cell{Row: cell.Row, Col: cell.Col}).String()
			if err := formula.Validate(text); err != nil {
				lowerText := strings.ToLower(text)
				if name == "" || strings.Contains(lowerText, name) || strings.Contains(lowerText, quoted) {
					return &Refused{Construct: fmt.Sprintf("an invalid formula at %s!%s", sh.Own, address)}
				}
				continue
			}
			out, refError, err := DefinedName(text, sh, e)
			if err != nil {
				return fmt.Errorf("formula at %s!%s: %w", sh.Own, address, err)
			}
			if out != text {
				edits = append(edits, FormulaEdit{Row: cell.Row, Col: cell.Col, Text: out})
				stats.Adjusted++
			}
			if refError {
				stats.RefErrors = append(stats.RefErrors, address)
			}
		}
		return nil
	}})
	if err != nil {
		return nil, Stats{}, err
	}
	return edits, stats, nil
}

// DefinedName rewrites one definedName's stored formula for e (sh.Own
// the name's localSheetId sheet, "" for workbook scope): formula.Shift,
// and whether it gained #REF!.
func DefinedName(text string, sh formula.Sheets, e formula.Edit) (out string, refError bool, err error) {
	out, err = formula.Shift(text, sh, e)
	if err != nil {
		return "", false, err
	}
	return out, strings.Count(out, "#REF!") > strings.Count(text, "#REF!"), nil
}
