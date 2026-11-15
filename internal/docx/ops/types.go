package ops

import (
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// Points is a non-negative length in twentieths of a point (twips). The
// wire form is points, a multiple of 0.05; it is written back as an
// integer when whole and as a float otherwise.
type Points int64

// SignedPoints is a paragraph indentation in twentieths of a point,
// between -100000 and 100000 points. It is always written as a float.
type SignedPoints int64

// pointsFrom converts points to twips as docxdriver's Points::from_points
// does, saturating as Rust's float-to-int cast does.
func pointsFrom(f float64) (int64, string) {
	// float64() keeps the compiler from fusing the multiply and subtract
	// into one rounding (an FMA), which Rust does not do.
	scaled := float64(f * 20)
	n := math.Round(scaled)
	if math.Abs(scaled-n) > 1e-8 {
		return 0, "point values must be multiples of 0.05"
	}
	switch {
	case n >= math.MaxInt64:
		return math.MaxInt64, ""
	case n <= math.MinInt64:
		return math.MinInt64, ""
	}
	return int64(n), ""
}

// Float returns the length in points.
func (p Points) Float() float64 { return float64(p) / 20 }

// encodeJSON returns the Points value as an integer or float.
func (p Points) encodeJSON() *value {
	if p%20 == 0 {
		n := int64(p) / 20
		if n < 0 {
			return &value{kind: kI64, i: n}
		}
		return &value{kind: kU64, u: uint64(n)}
	}
	return &value{kind: kF64, f: p.Float()}
}

// Float returns the length in points.
func (p SignedPoints) Float() float64 { return float64(p) / 20 }

// encodeJSON returns signed points as a float value.
func (p SignedPoints) encodeJSON() *value { return &value{kind: kF64, f: p.Float()} }

// ParagraphAddress is a paragraph id (eight uppercase hexadecimal digits,
// 1 to 7FFFFFFF, with "~N" for the Nth paragraph sharing a duplicated
// stored id, as read shows it) or an alias ("$name") an earlier operation
// defined.
type ParagraphAddress string

// IsAlias reports whether the address is an alias.
func (a ParagraphAddress) IsAlias() bool { return strings.HasPrefix(string(a), "$") }

// validParaID reports whether s is a nonzero 8-hex-digit id below 2^31.
func validParaID(s string) bool {
	if len(s) != 8 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isHexDigit(s[i]) {
			return false
		}
	}
	n, err := strconv.ParseUint(s, 16, 32)
	return err == nil && n > 0 && n < 0x8000_0000
}

// InsertAnchor is where insert_paragraph inserts: a paragraph address or
// "table:N" (N >= 1, written back without sign or leading zeros).
type InsertAnchor string

// ParagraphContent is paragraph.insert's html: one paragraph's inline
// content, or a list of them (List set).
type ParagraphContent struct {
	Items []string
	List  bool
}

// encodeJSON returns paragraph content as one string or a list.
func (c ParagraphContent) encodeJSON() *value {
	if !c.List {
		s := ""
		if len(c.Items) > 0 {
			s = c.Items[0]
		}
		return &value{kind: kString, s: s}
	}
	out := &value{kind: kArray, a: []*value{}}
	for _, s := range c.Items {
		out.a = append(out.a, &value{kind: kString, s: s})
	}
	return out
}

// SourceHash is the plan's base: "sha256:" and 64 lowercase hex digits.
type SourceHash string

// ChangeMode is how a plan's edits are recorded.
type ChangeMode string

// The change modes.
const (
	ModeTrack  ChangeMode = "track"
	ModeDirect ChangeMode = "direct"
)

// InsertPosition is before or after an anchor.
type InsertPosition string

// CommentStatus is a comment's open or resolved state.
type CommentStatus string

// ChromeKind is a header or footer slot: default, first or even.
type ChromeKind string

// unitEnums are the plan's unit enums (serialised as their variant
// names) by Rust type name, each in docxdriver's variant order.
var unitEnums = map[string][]string{
	"ChangeMode":     {"track", "direct"},
	"InsertPosition": {"before", "after"},
	"CommentStatus":  {"open", "resolved"},
	"ChromeKind":     {"default", "first", "even"},
	// HighlightColor is the ST_HighlightColor palette.
	"HighlightColor": {
		"black", "blue", "cyan", "green", "magenta", "red", "yellow", "white",
		"darkBlue", "darkCyan", "darkGreen", "darkMagenta", "darkRed",
		"darkYellow", "darkGray", "lightGray", "none",
	},
}

// UnitEnum returns the values of the plan's unit enum named by its Rust
// type name ("ChangeMode", "HighlightColor"), or nil.
func UnitEnum(name string) []string { return slices.Clone(unitEnums[name]) }

// HighlightColor is an ST_HighlightColor palette entry; none is an
// explicit off override.
type HighlightColor string

// TableWidth is a table or cell width: auto, points, or a percentage.
type TableWidth struct {
	Unit    string // auto, points or percent
	Points  Points
	Percent float64
}

// encodeJSON returns the table width with its unit and value.
func (w TableWidth) encodeJSON() *value {
	out := &value{kind: kObject, m: []member{{"unit", &value{kind: kString, s: w.Unit}}}}
	switch w.Unit {
	case "points":
		out.m = append(out.m, member{"value", w.Points.encodeJSON()})
	case "percent":
		out.m = append(out.m, member{"value", &value{kind: kF64, f: w.Percent}})
	}
	return out
}

// LineSpacing is a paragraph's line spacing: a multiple of single
// spacing, or an exact or minimum height in points.
type LineSpacing struct {
	Mode     string // multiple, exact or at_least
	Multiple float64
	Points   Points
}

// encodeJSON returns the line spacing with its mode and value.
func (l LineSpacing) encodeJSON() *value {
	val := l.Points.encodeJSON()
	if l.Mode == "multiple" {
		val = &value{kind: kF64, f: l.Multiple}
	}
	return &value{kind: kObject, m: []member{
		{"mode", &value{kind: kString, s: l.Mode}},
		{"value", val},
	}}
}

// TableBorder is one border of a table or cell.
type TableBorder struct {
	Style string  `json:"style" enum:"nil,none,single,double,dotted,dashed,dotDash,dotDotDash,triple,wave,doubleWave" schema:"w:ST_Border"`
	Color *string `json:"color" default:"null"`
	Width *Points `json:"width" default:"null"`
}

// TableBorders are the borders of a table or cell.
type TableBorders struct {
	Top              *TableBorder `json:"top" default:"null"`
	Left             *TableBorder `json:"left" default:"null"`
	Bottom           *TableBorder `json:"bottom" default:"null"`
	Right            *TableBorder `json:"right" default:"null"`
	InsideHorizontal *TableBorder `json:"inside_horizontal" default:"null"`
	InsideVertical   *TableBorder `json:"inside_vertical" default:"null"`
}

// CellPadding is the space between a cell's border and its content.
type CellPadding struct {
	Top    *Points `json:"top" default:"null"`
	Right  *Points `json:"right" default:"null"`
	Bottom *Points `json:"bottom" default:"null"`
	Left   *Points `json:"left" default:"null"`
}

// TableFormat is format_table's properties.
type TableFormat struct {
	Width        *TableWidth   `json:"width" default:"null"`
	ColumnWidths *[]Points     `json:"column_widths" default:"null"`
	Layout       *string       `json:"layout" default:"null" enum:"autofit,fixed" schema:"w:ST_TblLayoutType"`
	Alignment    *string       `json:"alignment" default:"null" enum:"left,center,right" schema:"w:ST_JcTable"`
	Borders      *TableBorders `json:"borders" default:"null"`
	CellSpacing  *Points       `json:"cell_spacing" default:"null"`
	Padding      *CellPadding  `json:"padding" default:"null"`
	Style        *string       `json:"style" default:"null"`
	Clear        []string      `json:"clear" default:"[]" enum:"width,column_widths,layout,alignment,borders,cell_spacing,padding,style"`
}

// CellFormat is format_cell's properties.
type CellFormat struct {
	Width             *TableWidth   `json:"width" default:"null"`
	Borders           *TableBorders `json:"borders" default:"null"`
	Fill              *string       `json:"fill" default:"null"`
	Padding           *CellPadding  `json:"padding" default:"null"`
	VerticalAlignment *string       `json:"vertical_alignment" default:"null" enum:"top,center,bottom" schema:"w:ST_VerticalJc"`
	Clear             []string      `json:"clear" default:"[]" enum:"width,borders,fill,padding,vertical_alignment"`
}

// RowFormat is format_row's properties.
type RowFormat struct {
	Height          *Points  `json:"height" default:"null"`
	HeightRule      *string  `json:"height_rule" default:"null" enum:"auto,at_least=atLeast,exact" schema:"w:ST_HeightRule"`
	RepeatingHeader *bool    `json:"repeating_header" default:"null"`
	AllowSplit      *bool    `json:"allow_split" default:"null"`
	Clear           []string `json:"clear" default:"[]" enum:"height,height_rule,repeating_header,allow_split"`
}

// ListLevel is one level of a new multilevel list; its array position is
// the level (0..8).
type ListLevel struct {
	Format          string        `json:"format" enum:"bullet,decimal,lowerLetter,upperLetter,lowerRoman,upperRoman" schema:"w:ST_NumberFormat"`
	Label           string        `json:"label"`
	Start           *uint32       `json:"start" default:"null"`
	RestartAfter    *uint8        `json:"restart_after" default:"null" doc:"Restart after this zero-based ancestor level; omitted uses Word's default."`
	IndentLeft      *SignedPoints `json:"indent_left" default:"null"`
	IndentRight     *SignedPoints `json:"indent_right" default:"null"`
	FirstLineIndent *SignedPoints `json:"first_line_indent" default:"null"`
	MarkerBold      *bool         `json:"marker_bold" default:"null"`
	MarkerItalic    *bool         `json:"marker_italic" default:"null"`
	MarkerFont      *string       `json:"marker_font" default:"null"`
	MarkerColor     *string       `json:"marker_color" default:"null"`
}

// TextEdit is one replacement inside a revise_text scope.
type TextEdit struct {
	Select     string  `json:"select"`
	With       string  `json:"with"`
	Occurrence *uint32 "json:\"occurrence\" default:\"null\" doc:\"1-based match within the scope; omit when `select` is unique there.\""
	All        bool    `json:"all,omitzero" default:"false" doc:"Replace every match within the scope."`
}

// reflectOf returns the struct p points to.
func reflectOf(p any) reflect.Value { return reflect.ValueOf(p).Elem() }
