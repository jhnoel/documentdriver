package ops

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
)

// ValueKind is the form of a plan's cell value (docs/xlsx-contract-draft.md,
// §7a; `xlsx help values`).
type ValueKind uint8

// The plan value forms.
const (
	// Empty leaves the cell empty: null in range.set's values (owner
	// decision, 2026-11-14; EmptyFromNull). Never a cell.set value.
	Empty ValueKind = iota
	// Number is a JSON number (finite: JSON has no NaN).
	Number
	// String is a JSON string, or {"text":"..."} (a string that may start
	// with "="). "" is the empty string, written as an empty inline
	// string (owner decision, 2026-11-14).
	String
	// Bool is true or false.
	Bool
	// Error is {"error":"#N/A"}: one of Excel's error literals (#NULL!,
	// #DIV/0!, #VALUE!, #REF!, #NAME?, #NUM!, #N/A, #GETTING_DATA).
	Error
	// Date is {"date":"YYYY-MM-DD"}; DateTime {"date":"YYYY-MM-DDTHH:MM:SS"};
	// Time {"time":"HH:MM:SS"}.
	Date
	DateTime
	Time
	// Formula is a string starting with "=": Text is the formula as
	// Excel shows it, without the "=" (formula.Store gives its stored
	// form).
	Formula
)

// CellValue is one plan cell value.
//
//   - Number: Num. String, Error, Formula: Text. Bool: Bool.
//   - Date, DateTime, Time: Text, the ISO text as written; the writer
//     converts it to a serial in the workbook's date system with
//     numfmt.ToSerial.
//
// Decoding rules (chunk K1), all invalid_operation at the value's path
// unless said otherwise:
//   - A JSON type that is not a value form (a list, an object with a
//     key other than text, error, date or time, or with more than one
//     key) is invalid_plan.
//   - Text longer than model.MaxText UTF-16 units, a formula longer
//     than model.MaxFormula, a formula that does not pass
//     formula.Validate after formula.Store, a formula Store refuses (a
//     spill reference "A1#"), a formula calling a function
//     formula.IsDynamicArray says spills (decision 8: new spill formulas
//     are refused), an unknown error literal, and a date or time that is
//     not a real calendar date or clock time from 1900-03-01 (the 1900
//     system's first unambiguous day) to 9999-12-31, or not in exactly
//     the forms above.
//   - "=" alone is refused (an empty formula).
type CellValue struct {
	Kind ValueKind
	Num  float64
	Text string
	Bool bool
}

// DecodePlan implements cli.PlanDecoder: one value (cell.set's value,
// one element of range.set's values). null is refused here by the
// shared decoder before it reaches this method.
func (c *CellValue) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	*c = CellValue{}
	invalid := func(code, message string) { d.Fail(code, path, v, "%s", message) }
	switch v.Kind {
	case cli.Number:
		n, err := strconv.ParseFloat(v.Num, 64)
		if err != nil {
			invalid(cli.CodeInvalidPlan, "number is out of range")
			return
		}
		*c = Num(n)
	case cli.Bool:
		*c = Boolean(v.Bool)
	case cli.String:
		if strings.HasPrefix(v.Str, "=") {
			text := v.Str[1:]
			if len(utf16.Encode([]rune(text))) > model.MaxFormula || text == "" {
				invalid(cli.CodeInvalidOperation, "invalid formula length")
				return
			}
			stored, err := formula.Store(text)
			if err != nil {
				invalid(cli.CodeInvalidOperation, err.Error())
				return
			}
			if err := formula.Validate(stored); err != nil {
				invalid(cli.CodeInvalidOperation, err.Error())
				return
			}
			if _, spills := formula.Spills(stored); spills {
				invalid(cli.CodeInvalidOperation, "dynamic array formulas are not supported")
				return
			}
			*c = Fx(text)
			return
		}
		if len(utf16.Encode([]rune(v.Str))) > model.MaxText {
			invalid(cli.CodeInvalidOperation, "text exceeds the cell limit")
			return
		}
		*c = Str(v.Str)
	case cli.Object:
		if len(v.Members) != 1 || v.Members[0].Value.Kind != cli.String {
			invalid(cli.CodeInvalidPlan, "expected one string-valued cell value key")
			return
		}
		m := v.Members[0]
		s := m.Value.Str
		switch m.Key {
		case "text":
			if len(utf16.Encode([]rune(s))) > model.MaxText {
				invalid(cli.CodeInvalidOperation, "text exceeds the cell limit")
				return
			}
			*c = Str(s)
		case "error":
			switch s {
			case "#NULL!", "#DIV/0!", "#VALUE!", "#REF!", "#NAME?", "#NUM!", "#N/A", "#GETTING_DATA":
				*c = Err(s)
			default:
				invalid(cli.CodeInvalidOperation, "unknown error literal")
			}
		case "date", "time":
			layout := "2006-01-02"
			if m.Key == "time" {
				layout = "15:04:05"
			} else if len(s) != len(layout) {
				layout = "2006-01-02T15:04:05"
			}
			parsed, err := time.Parse(layout, s)
			if err != nil || parsed.Format(layout) != s || (m.Key == "date" && s[:min(len(s), 10)] < "1900-03-01") {
				invalid(cli.CodeInvalidOperation, "invalid date or time")
				return
			}
			if m.Key == "time" {
				*c = TimeOf(s)
			} else {
				*c = DateOf(s)
			}
		default:
			invalid(cli.CodeInvalidPlan, "unknown cell value key")
		}
	default:
		invalid(cli.CodeInvalidPlan, "expected a cell value")
	}
}

// Grid is range.set's values: rows of values, top row first. Every row
// has the same length; there is at least one row and one column.
type Grid [][]CellValue

// DecodePlan implements cli.PlanDecoder: a list of lists of values, in
// which null is the one switch point between the two owner-decided forms
// (decodeNull). A ragged grid (rows of different lengths) and an empty
// grid or row are invalid_operation at values (or values[i]).
func (g *Grid) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	*g = nil
	if v.Kind != cli.Array {
		d.Fail(cli.CodeInvalidPlan, path, v, "expected a list of rows")
		return
	}
	if len(v.Items) == 0 {
		d.Fail(cli.CodeInvalidOperation, path, v, "grid must have at least one row")
		return
	}
	grid := make(Grid, len(v.Items))
	width := -1
	for i, row := range v.Items {
		rowPath := cli.Index(path, i)
		if row.Kind != cli.Array {
			d.Fail(cli.CodeInvalidPlan, rowPath, row, "expected a list of values")
			continue
		}
		if len(row.Items) == 0 {
			d.Fail(cli.CodeInvalidOperation, rowPath, row, "row must not be empty")
		} else if width >= 0 && len(row.Items) != width {
			d.Fail(cli.CodeInvalidOperation, rowPath, row, "grid rows must have the same length")
		}
		if width < 0 {
			width = len(row.Items)
		}
		grid[i] = make([]CellValue, len(row.Items))
		for j, item := range row.Items {
			itemPath := cli.Index(rowPath, j)
			if item.Kind == cli.Null {
				value, ok := decodeNull(item, itemPath, d)
				if ok {
					grid[i][j] = value
				}
			} else {
				grid[i][j].DecodePlan(item, itemPath, d)
			}
		}
	}
	*g = grid
}

// EmptyFromNull is the single switch point of the owner's decision on
// empty cells in range.set (signed off 2026-11-14): null leaves the cell
// empty (ValueKind Empty) and "" writes an empty string. Were it false,
// "" would leave the cell empty and null would be refused, as the first
// draft had it.
const EmptyFromNull = true

// decodeNull is the value range.set's null decodes to: Empty when
// EmptyFromNull, else an invalid_plan fault ("null is not a value").
func decodeNull(v *cli.Value, path string, d *cli.Decoder) (CellValue, bool) {
	if EmptyFromNull {
		return CellValue{Kind: Empty}, true
	}
	d.Fail(cli.CodeInvalidPlan, path, v, "null is not a value; leave the field out")
	return CellValue{}, false
}

// Num is a Number value. Num, Str, Boolean, Err, DateOf, TimeOf, Fx and
// Null build values for tests and NewPlan (no checks: decoding checks
// plans from JSON).
func Num(f float64) CellValue { return CellValue{Kind: Number, Num: f} }

// Str is a String value.
func Str(s string) CellValue { return CellValue{Kind: String, Text: s} }

// Boolean is a Bool value.
func Boolean(b bool) CellValue { return CellValue{Kind: Bool, Bool: b} }

// Err is an Error value.
func Err(lit string) CellValue { return CellValue{Kind: Error, Text: lit} }

// DateOf is a Date ("2024-03-01") or DateTime ("2024-03-01T13:30:00")
// value.
func DateOf(s string) CellValue {
	if len(s) > len("2006-01-02") {
		return CellValue{Kind: DateTime, Text: s}
	}
	return CellValue{Kind: Date, Text: s}
}

// TimeOf is a Time value ("13:30:00").
func TimeOf(s string) CellValue { return CellValue{Kind: Time, Text: s} }

// Fx is a Formula value; the text may start with "=".
func Fx(s string) CellValue {
	if len(s) > 0 && s[0] == '=' {
		s = s[1:]
	}
	return CellValue{Kind: Formula, Text: s}
}

// Null is the Empty value (range.set's null).
func Null() CellValue { return CellValue{Kind: Empty} }
