package ops

import (
	"errors"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
)

// The address fields of xlsx plans (docs/xlsx-contract-draft.md, §6).
// Each decodes from a JSON string with model.ParseRef's rules (always
// sheet-qualified, Excel quoting, no "$", no R1C1) and keeps the text as
// written in Raw. A wrong JSON type is invalid_plan; a string that is
// not the address the field takes is invalid_operation at the field.
// Ranges are normalised (top-left to bottom-right). Positional
// addresses are resolved against the document when the op runs.

// CellAddress is one cell: "Budget!B2".
type CellAddress struct {
	Raw, Sheet string
	Row, Col   int
}

// RangeAddress is a cell, an area, whole rows or whole columns:
// "Budget!B2", "Budget!A1:D10", "Budget!5:7", "Budget!C:E". R1..C2 are
// its bounds (whole rows cover every column, whole columns every row).
type RangeAddress struct {
	Raw, Sheet     string
	Kind           formula.RangeKind
	Single         bool
	R1, C1, R2, C2 int
}

// RowsAddress is a run of whole rows: "Budget!5:7" ("Budget!5:5" for
// one).
type RowsAddress struct {
	Raw, Sheet string
	From, To   int
}

// ColsAddress is a run of whole columns: "Budget!C:E".
type ColsAddress struct {
	Raw, Sheet string
	From, To   int
}

// TextScope is where text.replace looks: a range (as RangeAddress) or a
// whole sheet ("sheet:Budget", Whole).
type TextScope struct {
	RangeAddress
	Whole bool
}

// SheetAddress is a sheet: "sheet:Budget", or "$name", an earlier
// sheet.add's `as` (Alias set until the runner resolves it).
type SheetAddress struct {
	Raw, Name, Alias string
}

// NameAddress is a defined name: "name:Tax_Rate" (workbook scope) or
// "name:Budget!Local" (Sheet the scope).
type NameAddress struct {
	Raw, Name, Sheet string
}

// NameScope is a defined name's scope: "workbook", or "sheet:Budget".
type NameScope struct {
	Raw   string
	Sheet string // "" for workbook scope
}

// Position is before or after, for insert ops.
type Position string

// The positions.
const (
	Before Position = "before"
	After  Position = "after"
)

// addressString returns v's string, or records invalid_plan.
func addressString(d *cli.Decoder, v *cli.Value, path string) (string, bool) {
	if v.Kind != cli.String {
		d.Fail(cli.CodeInvalidPlan, path, v, "expected an address string, found %s", v.Kind)
		return "", false
	}
	return v.Str, true
}

// badAddress records invalid_operation for an address.
func badAddress(d *cli.Decoder, v *cli.Value, path string, err error, want string) {
	msg := "expected " + want
	var ae *model.AddressError
	if errors.As(err, &ae) {
		msg = ae.Error()
	} else if err != nil {
		msg = err.Error()
	}
	e := cli.Errorf(cli.CodeInvalidOperation, path, "%s", msg)
	e.Hint = "write " + want
	d.Add(e, v)
}

// parseRange parses a sheet-qualified range of the kinds allowed.
func parseRange(s string) (model.Ref, error) {
	r, err := model.ParseRef(s)
	if err != nil {
		return r, err
	}
	if r.Kind != model.RefRange {
		return r, &model.AddressError{Input: s, Reason: "not a range"}
	}
	return r, nil
}

// DecodePlan implements cli.PlanDecoder.
func (a *CellAddress) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	s, ok := addressString(d, v, path)
	if !ok {
		return
	}
	r, err := parseRange(s)
	if err != nil || r.Range.Kind != formula.Cells || !r.Range.Single {
		badAddress(d, v, path, err, "one sheet-qualified cell, such as Budget!B2")
		return
	}
	*a = CellAddress{Raw: s, Sheet: r.Sheet, Row: r.Range.From.Row, Col: r.Range.From.Col}
}

// DecodePlan implements cli.PlanDecoder.
func (a *RangeAddress) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	s, ok := addressString(d, v, path)
	if !ok {
		return
	}
	r, err := parseRange(s)
	if err != nil {
		badAddress(d, v, path, err, "a sheet-qualified range, such as Budget!A1:D10")
		return
	}
	*a = rangeOf(s, r)
}

// rangeOf builds a RangeAddress from raw text s and parsed reference r.
func rangeOf(s string, r model.Ref) RangeAddress {
	r1, c1, r2, c2 := r.Range.Bounds()
	return RangeAddress{Raw: s, Sheet: r.Sheet, Kind: r.Range.Kind, Single: r.Range.Single, R1: r1, C1: c1, R2: r2, C2: c2}
}

// DecodePlan implements cli.PlanDecoder.
func (a *RowsAddress) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	s, ok := addressString(d, v, path)
	if !ok {
		return
	}
	r, err := parseRange(s)
	if err != nil || r.Range.Kind != formula.Rows {
		badAddress(d, v, path, err, "sheet-qualified whole rows, such as Budget!5:7 (Budget!5:5 for one)")
		return
	}
	r1, _, r2, _ := r.Range.Bounds()
	*a = RowsAddress{Raw: s, Sheet: r.Sheet, From: r1, To: r2}
}

// DecodePlan implements cli.PlanDecoder.
func (a *ColsAddress) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	s, ok := addressString(d, v, path)
	if !ok {
		return
	}
	r, err := parseRange(s)
	if err != nil || r.Range.Kind != formula.Cols {
		badAddress(d, v, path, err, "sheet-qualified whole columns, such as Budget!C:E (Budget!C:C for one)")
		return
	}
	_, c1, _, c2 := r.Range.Bounds()
	*a = ColsAddress{Raw: s, Sheet: r.Sheet, From: c1, To: c2}
}

// DecodePlan implements cli.PlanDecoder.
func (a *TextScope) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	s, ok := addressString(d, v, path)
	if !ok {
		return
	}
	r, err := model.ParseRef(s)
	switch {
	case err == nil && r.Kind == model.RefSheet:
		*a = TextScope{RangeAddress: RangeAddress{Raw: s, Sheet: r.Sheet, Kind: formula.Cells, R1: 1, C1: 1, R2: formula.MaxRows, C2: formula.MaxCols}, Whole: true}
	case err == nil && r.Kind == model.RefRange:
		*a = TextScope{RangeAddress: rangeOf(s, r)}
	default:
		badAddress(d, v, path, err, "a sheet-qualified range or a sheet, such as Budget!A1:D10 or sheet:Budget")
	}
}

// DecodePlan implements cli.PlanDecoder.
func (a *SheetAddress) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	s, ok := addressString(d, v, path)
	if !ok {
		return
	}
	if name, ok := strings.CutPrefix(s, "$"); ok {
		if !cli.ValidAlias(name) {
			badAddress(d, v, path, nil, "an alias $name with name matching [A-Za-z_][A-Za-z0-9_]*")
			return
		}
		*a = SheetAddress{Raw: s, Alias: name}
		return
	}
	r, err := model.ParseRef(s)
	if err != nil || r.Kind != model.RefSheet {
		badAddress(d, v, path, err, "a sheet, such as sheet:Budget, or $name")
		return
	}
	*a = SheetAddress{Raw: s, Name: r.Sheet}
}

// DecodePlan implements cli.PlanDecoder.
func (a *NameAddress) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	s, ok := addressString(d, v, path)
	if !ok {
		return
	}
	r, err := model.ParseRef(s)
	if err != nil || r.Kind != model.RefName {
		badAddress(d, v, path, err, "a defined name, such as name:Tax_Rate or name:Budget!Local")
		return
	}
	*a = NameAddress{Raw: s, Name: r.Name, Sheet: r.Sheet}
}

// DecodePlan implements cli.PlanDecoder.
func (a *NameScope) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	s, ok := addressString(d, v, path)
	if !ok {
		return
	}
	if s == "workbook" {
		*a = NameScope{Raw: s}
		return
	}
	r, err := model.ParseRef(s)
	if err != nil || r.Kind != model.RefSheet {
		badAddress(d, v, path, err, `"workbook" or a sheet, such as sheet:Budget`)
		return
	}
	*a = NameScope{Raw: s, Sheet: r.Sheet}
}

// DecodePlan implements cli.PlanDecoder.
func (p *Position) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	cli.DecodeEnum(d, v, path, []string{"before", "after"}, p)
}
