package ops

import (
	"errors"
	"strconv"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/edit"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
)

// RangeFormat is range.format.
type RangeFormat struct {
	At           RangeAddress `json:"at" doc:"The range: Budget!A1:D10 (whole rows or columns format their row or column style)."`
	NumberFormat *string      `json:"number_format" doc:"An Excel format code: 0.00, #,##0, yyyy-mm-dd, @."`
	Bold         *bool        `json:"bold"`
	Italic       *bool        `json:"italic"`
	Underline    *bool        `json:"underline" doc:"Single underline."`
	Color        *string      `json:"color" doc:"Font colour, RRGGBB."`
	Fill         *string      `json:"fill" doc:"Solid fill colour, RRGGBB."`
	Align        *string      `json:"align" enum:"general,left,center,right,fill,justify,centerContinuous,distributed"`
	Valign       *string      `json:"valign" enum:"top,center,bottom,justify,distributed"`
	Wrap         *bool        `json:"wrap" doc:"Wrap text."`
}

// opRangeFormat is range.format's entry in the operation table.
var opRangeFormat = OpDef{
	Name: "range.format", New: func() Spec { return &RangeFormat{} },
	Summary: "Changes the format of every cell in a range: number format, bold, italic, underline, font colour, fill, alignment and wrapping. Content is unchanged.",
	Rules:   rangeFormatRules,
}

// Run (chunk G8): for each cell of an area (existing or not), its s
// becomes Styles.WithFormat(s, format) (Sheet.SetCellStyle; a missing
// cell takes Sheet.DefaultStyle first). Whole rows set the row's s and
// customFormat="1" and restyle the row's existing cells; whole columns
// set the col runs' style (split as column.format does) and restyle
// existing cells in them. An area over more than 100,000 cells is
// operation_failed (use whole rows or columns; approver ruling, V
// measures it). Changed: the range
// (an area only).
func (o *RangeFormat) Run(env *Env) error {
	a := o.At
	if a.Kind == formula.Cells && int64(a.R2-a.R1+1)*int64(a.C2-a.C1+1) > 100000 {
		return Refuse(cli.CodeOperationFailed, "at", "range exceeds 100,000 cells; use whole rows or columns")
	}
	s, err := env.Sheet(a.Sheet, "at")
	if err != nil {
		return err
	}
	st, err := env.WS.Styles()
	if errors.Is(err, edit.ErrNoStyles) {
		return &Rejection{Code: cli.CodeUnreadableTarget, Field: "at", Msg: "the workbook has no styles part", Detail: err.Error()}
	}
	if err != nil {
		return FromEdit(err, "at")
	}
	f := edit.CellFormat{NumberFormat: o.NumberFormat, Bold: o.Bold, Italic: o.Italic, Underline: o.Underline, Color: o.Color, Fill: o.Fill, Align: o.Align, Valign: o.Valign, Wrap: o.Wrap}
	// WithFormat interns: the same base gives the same style, so it is
	// asked once per distinct base, not once per cell.
	memo := map[int]int{}
	format := func(base int) (int, error) {
		if n, ok := memo[base]; ok {
			return n, nil
		}
		n, err := st.WithFormat(base, f)
		if err == nil {
			memo[base] = n
		}
		return n, err
	}
	cell := func(c edit.Cell) error {
		if c.Exists && !c.Raw.SOK {
			return &Rejection{Code: cli.CodeUnreadableTarget, Field: "at", Msg: "cell style cannot be read", Detail: "invalid cell style"}
		}
		base := c.Raw.S
		if !c.Exists {
			var e error
			base, e = s.DefaultStyle(c.Row, c.Col)
			if e != nil {
				return e
			}
		}
		n, e := format(base)
		if e != nil {
			return e
		}
		return s.SetCellStyle(c.Row, c.Col, n)
	}
	switch a.Kind {
	case formula.Cells:
		for r := a.R1; r <= a.R2; r++ {
			for c := a.C1; c <= a.C2; c++ {
				v, e := s.Cell(r, c)
				if e != nil {
					return FromEdit(e, "at")
				}
				if e = cell(v); e != nil {
					return FromEdit(e, "at")
				}
			}
		}
		env.ChangedRange(a.Sheet, a.R1, a.C1, a.R2, a.C2)
	case formula.Rows:
		// Restyle cells before changing the inherited row styles.
		if err = s.Each(a.R1, a.C1, a.R2, a.C2, cell); err != nil {
			return FromEdit(err, "at")
		}
		for r := a.R1; r <= a.R2; r++ {
			attrs, _, e := s.RowAttrs(r)
			if e != nil {
				return FromEdit(e, "at")
			}
			base := 0
			for _, v := range attrs {
				if v.Name == "s" {
					base, e = strconv.Atoi(v.Value)
					if e != nil || base < 0 {
						return &Rejection{Code: cli.CodeUnreadableTarget, Field: "at", Msg: "row style cannot be read", Detail: "invalid row style"}
					}
				}
			}
			n, e := format(base)
			if e != nil {
				return FromEdit(e, "at")
			}
			if e = s.SetRowAttrs(r, edit.AttrEdit{Name: "s", Value: strconv.Itoa(n)}, edit.AttrEdit{Name: "customFormat", Value: "1"}); e != nil {
				return FromEdit(e, "at")
			}
		}
	case formula.Cols:
		if err = s.Each(a.R1, a.C1, a.R2, a.C2, cell); err != nil {
			return FromEdit(err, "at")
		}
		// Find a row without a row-level override to query column defaults.
		row := 1
		for ; row <= formula.MaxRows; row++ {
			attrs, _, e := s.RowAttrs(row)
			if e != nil {
				return FromEdit(e, "at")
			}
			custom := false
			for _, v := range attrs {
				if v.Name == "customFormat" && (v.Value == "1" || v.Value == "true") {
					custom = true
				}
			}
			if !custom {
				break
			}
		}
		if row > formula.MaxRows {
			return Refuse(cli.CodeOperationFailed, "at", "cannot determine column styles beneath row overrides")
		}
		cols, _, e := s.Element("cols")
		if e != nil {
			return FromEdit(e, "at")
		}
		// Compute all defaults against the original cols, then apply runs.
		bases := make([]int, a.C2-a.C1+1)
		for c := a.C1; c <= a.C2; c++ {
			bases[c-a.C1], e = s.DefaultStyle(row, c)
			if e != nil {
				return FromEdit(e, "at")
			}
		}
		for c := a.C1; c <= a.C2; {
			base := bases[c-a.C1]
			end := c
			for end < a.C2 && bases[end+1-a.C1] == base {
				end++
			}
			n, e := format(base)
			if e != nil {
				return FromEdit(e, "at")
			}
			cols, e = SetColumns(cols, s.Prefix(), c, end, nil, nil, &n)
			if e != nil {
				return FromEdit(e, "at")
			}
			c = end + 1
		}
		if e = s.SetElement("cols", cols); e != nil {
			return FromEdit(e, "at")
		}
	}
	return nil
}
