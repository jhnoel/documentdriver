package ops

// ColumnFormat is column.format.
type ColumnFormat struct {
	At     ColsAddress `json:"at" doc:"The columns: Budget!C:E."`
	Width  *float64    `json:"width" doc:"Column width in characters, 0 to 255."`
	Hidden *bool       `json:"hidden" doc:"Hide (true) or show (false) the columns."`
}

// opColumnFormat is column.format's entry in the operation table.
var opColumnFormat = OpDef{
	Name: "column.format", New: func() Spec { return &ColumnFormat{} },
	Summary: "Sets the width of columns (Excel character widths, as read shows them) or hides or shows them.",
	Rules:   columnFormatRules,
}

// Run (chunk G7) rewrites the sheet's cols element (Sheet.Element,
// SetElement): the col runs covering the columns are split at the
// range's ends so that the columns have runs of their own, which get
// width with customWidth="1" or hidden; other runs and their attributes
// are kept as they are; a missing cols element is created. Runs stay
// sorted and disjoint.
func (o *ColumnFormat) Run(env *Env) error {
	s, err := env.Sheet(o.At.Sheet, "at")
	if err != nil {
		return err
	}
	cols, _, err := s.Element("cols")
	if err != nil {
		return err
	}
	cols, err = SetColumns(cols, s.Prefix(), o.At.From, o.At.To, o.Width, o.Hidden, nil)
	if err != nil {
		return err
	}
	return s.SetElement("cols", cols)
}
