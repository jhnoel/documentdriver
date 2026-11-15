package ops

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ascii"
	"github.com/jhnoel/documentdriver/internal/cli"
)

// parseRustUsize parses as Rust's usize::from_str on a 64-bit target.
func parseRustUsize(s string) (uint64, bool) {
	s = strings.TrimPrefix(s, "+")
	if !ascii.Digits(s) {
		return 0, false
	}
	n, err := strconv.ParseUint(s, 10, 64)
	return n, err == nil
}

// tableAddressAs is docxdriver's tables::address check, over the given
// components: each a prefix and a number from 1 ("table:", "row:",
// "cell:", "column:"); the first may be an alias ("$name"). It returns
// the message for a malformed address.
func tableAddressAs(at string, kinds []string) (string, bool) {
	parts := strings.Split(at, "/")
	if len(parts) != len(kinds) {
		form := ""
		for i, k := range kinds {
			if i > 0 {
				form += "/"
			}
			form += k + "N"
		}
		return fmt.Sprintf("expected %s address, got %s", form, strconv.Quote(at)), true
	}
	for i, p := range parts {
		if i == 0 && len(p) > 1 && p[0] == '$' && asciiWord(p[1:]) {
			continue
		}
		rest, ok := strings.CutPrefix(p, kinds[i])
		if n, num := parseRustUsize(rest); !ok || !num || n == 0 {
			return "invalid table address component " + strconv.Quote(p), true
		}
	}
	return "", false
}

// asciiWord reports whether s is ASCII letters, digits and underscores.
func asciiWord(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

// addressRule returns the table address rule for the given depth.
func addressRule[T any](depth int, at func(*T) string, example string) Rule {
	return addressRuleAs([]string{"table:", "row:", "cell:"}[:depth], at, example)
}

// addressRuleAs returns the table address rule for the given kinds.
func addressRuleAs[T any](kinds []string, at func(*T) string, example string) Rule {
	return ruleX(`expected <form> address, got "…" | invalid table address component "…"`,
		func(t *T) (string, string, bool) {
			msg, bad := tableAddressAs(at(t), kinds)
			return msg, "at", bad
		}, example)
}

const dimensionMsg = "table dimensions must be nonnegative and fit signed 32-bit twips"

// badDimension is tables::dimension_valid's complement.
func badDimension(p *Points) bool { return p != nil && (*p < 0 || *p > maxI32) }

// tableColor is the table validators' color check.
func tableColor(c string) bool {
	return len(c) == 7 && c[0] == '#' && hexDigits(c[1:], 6) && c == strings.ToUpper(c)
}

// borderStyles are the border styles format_table and format_cell write.
var borderStyles = []string{"nil", "none", "single", "double", "dotted", "dashed", "dotDash", "dotDotDash", "triple", "wave", "doubleWave"}

type side struct {
	name   string
	border func(*TableBorders) *TableBorder
	pad    func(*CellPadding) *Points
}

// borderSides are the borders in the order docxdriver checks them.
var borderSides = []side{
	{name: "top", border: func(b *TableBorders) *TableBorder { return b.Top }},
	{name: "left", border: func(b *TableBorders) *TableBorder { return b.Left }},
	{name: "bottom", border: func(b *TableBorders) *TableBorder { return b.Bottom }},
	{name: "right", border: func(b *TableBorders) *TableBorder { return b.Right }},
	{name: "inside_horizontal", border: func(b *TableBorders) *TableBorder { return b.InsideHorizontal }},
	{name: "inside_vertical", border: func(b *TableBorders) *TableBorder { return b.InsideVertical }},
}

// paddingSides are the padding sides in the order docxdriver checks them.
var paddingSides = []side{
	{name: "top", pad: func(p *CellPadding) *Points { return p.Top }},
	{name: "right", pad: func(p *CellPadding) *Points { return p.Right }},
	{name: "bottom", pad: func(p *CellPadding) *Points { return p.Bottom }},
	{name: "left", pad: func(p *CellPadding) *Points { return p.Left }},
}

// paddingRules are padding_valid's checks, one rule per side.
func paddingRules[T any](pad func(*T) *CellPadding, op, at string) []Rule {
	var out []Rule
	for _, s := range paddingSides {
		out = append(out, rule(dimensionMsg, "format.padding."+s.name,
			func(t *T) bool { p := pad(t); return p != nil && badDimension(s.pad(p)) },
			fmt.Sprintf(`{"op":%q,"at":%q,"format":{"padding":{%q:3e9}}}`, op, at, s.name)))
	}
	return out
}

// widthRules are width_valid's checks.
func widthRules[T any](width func(*T) *TableWidth, op, at string) []Rule {
	return []Rule{
		rule(dimensionMsg, "format.width.value",
			func(t *T) bool {
				w := width(t)
				return w != nil && w.Unit == "points" && badDimension(&w.Points)
			}, fmt.Sprintf(`{"op":%q,"at":%q,"format":{"width":{"unit":"points","value":2e9}}}`, op, at)),
		rule("table percentage width must be > 0 and <= 100", "format.width.value",
			func(t *T) bool {
				w := width(t)
				return w != nil && w.Unit == "percent" && !(w.Percent > 0 && w.Percent <= 100)
			}, fmt.Sprintf(`{"op":%q,"at":%q,"format":{"width":{"unit":"percent","value":150}}}`, op, at)),
	}
}

// borderRules are borders_valid's checks: per side, style, color and
// width.
func borderRules[T any](borders func(*T) *TableBorders, op, at string) []Rule {
	var out []Rule
	for _, s := range borderSides {
		get := func(t *T) *TableBorder {
			if b := borders(t); b != nil {
				return s.border(b)
			}
			return nil
		}
		base := "format.borders." + s.name
		out = append(out,
			rule("unsupported table border style", base+".style",
				func(t *T) bool { b := get(t); return b != nil && !slices.Contains(borderStyles, b.Style) },
				fmt.Sprintf(`{"op":%q,"at":%q,"format":{"borders":{%q:{"style":"groove"}}}}`, op, at, s.name)),
			rule("border color must be #RRGGBB", base+".color",
				func(t *T) bool { b := get(t); return b != nil && b.Color != nil && !tableColor(*b.Color) },
				fmt.Sprintf(`{"op":%q,"at":%q,"format":{"borders":{%q:{"style":"single","color":"red"}}}}`, op, at, s.name)),
			rule("border width must be 0.25..12 points in quarter-point increments", base+".width",
				func(t *T) bool {
					b := get(t)
					if b == nil || b.Width == nil {
						return false
					}
					w := *b.Width
					return w.Float() > 12 || w.Float() < 0.25 || w%5 != 0
				}, fmt.Sprintf(`{"op":%q,"at":%q,"format":{"borders":{%q:{"style":"single","width":0.3}}}}`, op, at, s.name)),
		)
	}
	return out
}

// clearRule is clear_valid: the first clear entry not in names.
func clearRule[T any](clearOf func(*T) []string, names []string, example string) Rule {
	return ruleX(`unknown clear property "…"`,
		func(t *T) (string, string, bool) {
			for i, c := range clearOf(t) {
				if !slices.Contains(names, c) {
					return "unknown clear property " + strconv.Quote(c), fmt.Sprintf("format.clear[%d]", i), true
				}
			}
			return "", "", false
		}, example)
}

var formatTableRules = slices.Concat(
	[]Rule{addressRule(1, func(f *FormatTable) string { return f.At },
		`{"op":"table.format","at":"table:1/row:1","format":{"alignment":"center"}}`)},
	paddingRules(func(f *FormatTable) *CellPadding { return f.Format.Padding }, "table.format", "table:1"),
	[]Rule{
		rule(dimensionMsg, "format.cell_spacing",
			func(f *FormatTable) bool { return badDimension(f.Format.CellSpacing) },
			`{"op":"table.format","at":"table:1","format":{"cell_spacing":2e9}}`),
		ruleX(dimensionMsg,
			func(f *FormatTable) (string, string, bool) {
				if f.Format.ColumnWidths != nil {
					for i, w := range *f.Format.ColumnWidths {
						if badDimension(&w) {
							return dimensionMsg, fmt.Sprintf("format.column_widths[%d]", i), true
						}
					}
				}
				return "", "", false
			}, `{"op":"table.format","at":"table:1","format":{"column_widths":[72,2e9]}}`),
		ruleX("column_widths cannot be cleared: the grid must retain its dimensions",
			func(f *FormatTable) (string, string, bool) {
				i := slices.Index(f.Format.Clear, "column_widths")
				return "column_widths cannot be cleared: the grid must retain its dimensions", fmt.Sprintf("format.clear[%d]", i), i >= 0
			}, `{"op":"table.format","at":"table:1","format":{"clear":["column_widths"]}}`),
	},
	widthRules(func(f *FormatTable) *TableWidth { return f.Format.Width }, "table.format", "table:1"),
	borderRules(func(f *FormatTable) *TableBorders { return f.Format.Borders }, "table.format", "table:1"),
	[]Rule{
		clearRule(func(f *FormatTable) []string { return f.Format.Clear }, []string{"width", "column_widths", "layout", "alignment", "borders", "cell_spacing", "padding", "style"},
			`{"op":"table.format","at":"table:1","format":{"clear":["shading"]}}`),
		rule("layout must be autofit or fixed", "format.layout",
			func(f *FormatTable) bool {
				return f.Format.Layout != nil && !slices.Contains([]string{"autofit", "fixed"}, *f.Format.Layout)
			}, `{"op":"table.format","at":"table:1","format":{"layout":"auto"}}`),
		rule("table alignment must be left, center or right", "format.alignment",
			func(f *FormatTable) bool {
				return f.Format.Alignment != nil && !slices.Contains([]string{"left", "center", "right"}, *f.Format.Alignment)
			}, `{"op":"table.format","at":"table:1","format":{"alignment":"justify"}}`),
		rule("column_widths must contain positive point widths", "format.column_widths",
			func(f *FormatTable) bool {
				if f.Format.ColumnWidths == nil {
					return false
				}
				ws := *f.Format.ColumnWidths
				return len(ws) == 0 || slices.ContainsFunc(ws, func(w Points) bool { return w <= 0 })
			}, `{"op":"table.format","at":"table:1","format":{"column_widths":[72,0]}}`),
		rule("table.format requires a property", "format",
			func(f *FormatTable) bool {
				t := f.Format
				return t.Width == nil && t.ColumnWidths == nil && t.Layout == nil && t.Alignment == nil &&
					t.Borders == nil && t.CellSpacing == nil && t.Padding == nil && t.Style == nil && len(t.Clear) == 0
			}, `{"op":"table.format","at":"table:1","format":{}}`),
	},
)

var formatCellRules = slices.Concat(
	[]Rule{addressRule(3, func(f *FormatCell) string { return f.At },
		`{"op":"cell.format","at":"table:1/row:1/cell:0","format":{"fill":"#FFFFFF"}}`)},
	paddingRules(func(f *FormatCell) *CellPadding { return f.Format.Padding }, "cell.format", "table:1/row:1/cell:1"),
	widthRules(func(f *FormatCell) *TableWidth { return f.Format.Width }, "cell.format", "table:1/row:1/cell:1"),
	borderRules(func(f *FormatCell) *TableBorders { return f.Format.Borders }, "cell.format", "table:1/row:1/cell:1"),
	[]Rule{
		clearRule(func(f *FormatCell) []string { return f.Format.Clear }, []string{"width", "borders", "fill", "padding", "vertical_alignment"},
			`{"op":"cell.format","at":"table:1/row:1/cell:1","format":{"clear":["shading"]}}`),
		rule("cell fill must be #RRGGBB", "format.fill",
			func(f *FormatCell) bool { return f.Format.Fill != nil && !tableColor(*f.Format.Fill) },
			`{"op":"cell.format","at":"table:1/row:1/cell:1","format":{"fill":"#fff"}}`),
		rule("vertical_alignment must be top, center or bottom", "format.vertical_alignment",
			func(f *FormatCell) bool {
				v := f.Format.VerticalAlignment
				return v != nil && !slices.Contains([]string{"top", "center", "bottom"}, *v)
			}, `{"op":"cell.format","at":"table:1/row:1/cell:1","format":{"vertical_alignment":"middle"}}`),
		rule("cell.format requires a property", "format",
			func(f *FormatCell) bool {
				c := f.Format
				return c.Width == nil && c.Borders == nil && c.Fill == nil && c.Padding == nil &&
					c.VerticalAlignment == nil && len(c.Clear) == 0
			}, `{"op":"cell.format","at":"table:1/row:1/cell:1","format":{}}`),
	},
)

var formatRowRules = []Rule{
	addressRule(2, func(f *FormatRow) string { return f.At },
		`{"op":"row.format","at":"table:1/cell:1","format":{"allow_split":true}}`),
	rule(dimensionMsg, "format.height",
		func(f *FormatRow) bool { return badDimension(f.Format.Height) },
		`{"op":"row.format","at":"table:1/row:1","format":{"height":2e9}}`),
	clearRule(func(f *FormatRow) []string { return f.Format.Clear }, []string{"height", "height_rule", "repeating_header", "allow_split"},
		`{"op":"row.format","at":"table:1/row:1","format":{"clear":["width"]}}`),
	rule("height_rule must be auto, at_least or exact", "format.height_rule",
		func(f *FormatRow) bool {
			h := f.Format.HeightRule
			return h != nil && !slices.Contains([]string{"auto", "at_least", "exact"}, *h)
		}, `{"op":"row.format","at":"table:1/row:1","format":{"height_rule":"atLeast"}}`),
	rule("row.format requires a property", "format",
		func(f *FormatRow) bool {
			r := f.Format
			return r.Height == nil && r.HeightRule == nil && r.RepeatingHeader == nil && r.AllowSplit == nil && len(r.Clear) == 0
		}, `{"op":"row.format","at":"table:1/row:1","format":{}}`),
}

// The row, column and cell edits (docxdriver's edit_table actions): the
// address names the row, column or cell; counts are 1..100.

var columnAddress = []string{"table:", "column:"}

const countMsg = "count must be 1..100"

// badCount reports whether count n lies outside 1 to 100.
func badCount(n uint32) bool { return n == 0 || n > 100 }

const groupAliasMsg = "invalid inserted table group alias"

var deleteTableRules = []Rule{
	addressRule(1, func(d *DeleteTable) string { return d.At },
		`{"op":"table.delete","at":"1"}`),
}

var insertRowsRules = []Rule{
	addressRule(2, func(r *InsertRows) string { return r.At },
		`{"op":"row.insert","at":"table:1/row:0","position":"after","count":1}`),
	rule(groupAliasMsg, "as",
		func(r *InsertRows) bool { return r.Alias != nil && !cli.ValidAlias(*r.Alias) },
		`{"op":"row.insert","at":"table:1/row:1","position":"after","count":1,"as":"new rows"}`),
	rule(countMsg, "count",
		func(r *InsertRows) bool { return badCount(r.Count) },
		`{"op":"row.insert","at":"table:1/row:1","position":"after","count":0}`),
}

var deleteRowsRules = []Rule{
	addressRule(2, func(r *DeleteRows) string { return r.At },
		`{"op":"row.delete","at":"table:1","count":1}`),
	rule(countMsg, "count",
		func(r *DeleteRows) bool { return badCount(r.Count) },
		`{"op":"row.delete","at":"table:1/row:1","count":101}`),
}

var insertColumnsRules = []Rule{
	addressRuleAs(columnAddress, func(c *InsertColumns) string { return c.At },
		`{"op":"column.insert","at":"table:1/row:1","position":"after","count":1}`),
	rule(groupAliasMsg, "as",
		func(c *InsertColumns) bool { return c.Alias != nil && !cli.ValidAlias(*c.Alias) },
		`{"op":"column.insert","at":"table:1/column:1","position":"after","count":1,"as":"new columns"}`),
	rule(countMsg, "count",
		func(c *InsertColumns) bool { return badCount(c.Count) },
		`{"op":"column.insert","at":"table:1/column:1","position":"after","count":101}`),
}

var deleteColumnsRules = []Rule{
	addressRuleAs(columnAddress, func(c *DeleteColumns) string { return c.At },
		`{"op":"column.delete","at":"table:1/column:0","count":1}`),
	rule(countMsg, "count",
		func(c *DeleteColumns) bool { return badCount(c.Count) },
		`{"op":"column.delete","at":"table:1/column:1","count":0}`),
}

var mergeCellsRules = []Rule{
	addressRule(3, func(m *MergeCells) string { return m.At },
		`{"op":"cell.merge","at":"table:1/row:1","row_span":2,"column_span":1}`),
	ruleX("row_span and column_span must be positive and cover more than one cell",
		func(m *MergeCells) (string, string, bool) {
			const msg = "row_span and column_span must be positive and cover more than one cell"
			switch {
			case m.RowSpan == 0:
				return msg, "row_span", true
			case m.ColumnSpan == 0, m.RowSpan <= 1 && m.ColumnSpan <= 1:
				return msg, "column_span", true
			}
			return "", "", false
		}, `{"op":"cell.merge","at":"table:1/row:1/cell:1","row_span":1,"column_span":1}`),
}

var splitCellRules = []Rule{
	addressRule(3, func(c *SplitCell) string { return c.At },
		`{"op":"cell.split","at":"table:1/row:1/cell:0"}`),
}

var insertTableRules = []Rule{
	ruleX("table.insert dimensions must be 1..100",
		func(t *InsertTable) (string, string, bool) {
			const msg = "table.insert dimensions must be 1..100"
			switch {
			case t.Rows == 0 || t.Rows > 100:
				return msg, "rows", true
			case t.Columns == 0 || t.Columns > 100:
				return msg, "columns", true
			}
			return "", "", false
		}, `{"op":"table.insert","at":"0000000A","position":"after","rows":0,"columns":2}`),
	rule("invalid alias", "as",
		func(t *InsertTable) bool { return t.Alias != nil && !cli.ValidAlias(*t.Alias) },
		`{"op":"table.insert","at":"0000000A","position":"after","rows":1,"columns":1,"as":"t-1"}`),
	ruleX("cells has N rows but the table has R: remove cells[R..] (row R+1[ to row N])",
		func(t *InsertTable) (string, string, bool) {
			if t.Cells == nil || len(*t.Cells) <= int(t.Rows) {
				return "", "", false
			}
			n := len(*t.Cells)
			to := ""
			if n-int(t.Rows) > 1 {
				to = fmt.Sprintf(" to row %d", n)
			}
			return fmt.Sprintf("cells has %d rows but the table has %d: remove cells[%d..] (row %d%s)",
				n, t.Rows, t.Rows, t.Rows+1, to), "cells", true
		}, `{"op":"table.insert","at":"0000000A","position":"after","rows":1,"columns":1,"cells":[["a"],["b"],["c"]]}`),
	ruleX("cells[I] (row I+1) has N cells but the table has C columns: remove cells[I][C..] (column C+1[ to column N])",
		func(t *InsertTable) (string, string, bool) {
			if t.Cells == nil {
				return "", "", false
			}
			for i, row := range *t.Cells {
				if len(row) <= int(t.Columns) {
					continue
				}
				to := ""
				if len(row)-int(t.Columns) > 1 {
					to = fmt.Sprintf(" to column %d", len(row))
				}
				return fmt.Sprintf("cells[%d] (row %d) has %d cells but the table has %d columns: remove cells[%d][%d..] (column %d%s)",
					i, i+1, len(row), t.Columns, i, t.Columns, t.Columns+1, to), fmt.Sprintf("cells[%d]", i), true
			}
			return "", "", false
		}, `{"op":"table.insert","at":"0000000A","position":"after","rows":2,"columns":1,"cells":[["a"],["b","c"]]}`),
}
