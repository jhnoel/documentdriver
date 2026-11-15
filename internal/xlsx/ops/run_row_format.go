package ops

import (
	"strconv"

	"github.com/jhnoel/documentdriver/internal/xlsx/edit"
)

// RowFormat is row.format.
type RowFormat struct {
	At     RowsAddress `json:"at" doc:"The rows: Budget!5:7."`
	Height *float64    `json:"height" doc:"Row height in points, 0 to 409."`
	Hidden *bool       `json:"hidden" doc:"Hide (true) or show (false) the rows."`
}

// opRowFormat is row.format's entry in the operation table.
var opRowFormat = OpDef{
	Name: "row.format", New: func() Spec { return &RowFormat{} },
	Summary: "Sets the height of rows (points) or hides or shows them.",
	Rules:   rowFormatRules,
}

// Run (chunk G6) edits each row's start tag (Sheet.SetRowAttrs): height
// is ht with customHeight="1"; hidden is hidden="1" (false removes it).
// A row the sheet does not hold is created empty. Changed: none (rows
// are not units); the result's parts show the sheet.
func (o *RowFormat) Run(env *Env) error {
	s, err := env.Sheet(o.At.Sheet, "at")
	if err != nil {
		return err
	}
	var attrs []edit.AttrEdit
	if o.Height != nil {
		attrs = append(attrs, edit.AttrEdit{Name: "ht", Value: strconv.FormatFloat(*o.Height, 'f', -1, 64)}, edit.AttrEdit{Name: "customHeight", Value: "1"})
	}
	if o.Hidden != nil {
		attrs = append(attrs, edit.AttrEdit{Name: "hidden", Value: "1", Remove: !*o.Hidden})
	}
	for row := o.At.From; row <= o.At.To; row++ {
		if err := s.SetRowAttrs(row, attrs...); err != nil {
			return FromEdit(err, "at")
		}
	}
	return nil
}
