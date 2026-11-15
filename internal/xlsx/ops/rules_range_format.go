package ops

// range.format's rules (designer-owned).
var rangeFormatRules = []Rule{
	rule("give at least one format field", "",
		func(o *RangeFormat) bool {
			return o.NumberFormat == nil && o.Bold == nil && o.Italic == nil && o.Underline == nil &&
				o.Color == nil && o.Fill == nil && o.Align == nil && o.Valign == nil && o.Wrap == nil
		},
		`{"op":"range.format","at":"Budget!A1"}`),
	rule("number_format must be 1 to 255 characters", "number_format",
		func(o *RangeFormat) bool {
			return o.NumberFormat != nil && (*o.NumberFormat == "" || len(*o.NumberFormat) > 255)
		},
		`{"op":"range.format","at":"Budget!A1","number_format":""}`),
	rule("color must be six hex digits, RRGGBB", "color",
		func(o *RangeFormat) bool { return o.Color != nil && !isRGB(*o.Color) },
		`{"op":"range.format","at":"Budget!A1","color":"red"}`),
	rule("fill must be six hex digits, RRGGBB", "fill",
		func(o *RangeFormat) bool { return o.Fill != nil && !isRGB(*o.Fill) },
		`{"op":"range.format","at":"Budget!A1","fill":"#FF0000"}`),
	rule("align must be general, left, center, right, fill, justify, centerContinuous or distributed", "align",
		func(o *RangeFormat) bool { return o.Align != nil && !oneOf(*o.Align, alignValues) },
		`{"op":"range.format","at":"Budget!A1","align":"middle"}`),
	rule("valign must be top, center, bottom, justify or distributed", "valign",
		func(o *RangeFormat) bool { return o.Valign != nil && !oneOf(*o.Valign, valignValues) },
		`{"op":"range.format","at":"Budget!A1","valign":"middle"}`),
}

// The alignment values: ST_HorizontalAlignment and ST_VerticalAlignment
// (checked against schema.Enum by TestAlignValues).
var (
	alignValues  = []string{"general", "left", "center", "right", "fill", "justify", "centerContinuous", "distributed"}
	valignValues = []string{"top", "center", "bottom", "justify", "distributed"}
)

// oneOf reports whether s equals any value in vs.
func oneOf(s string, vs []string) bool {
	for _, v := range vs {
		if s == v {
			return true
		}
	}
	return false
}

// isRGB reports whether s is six hex digits, an RRGGBB color.
func isRGB(s string) bool {
	if len(s) != 6 {
		return false
	}
	for i := 0; i < 6; i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'A' || c > 'F') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
