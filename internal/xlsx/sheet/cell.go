package sheet

// RawCell is one <c> of sheetData as stored, before interpretation: no
// shared string is resolved, no style is applied, no number is parsed,
// no _xHHHH_ escape is decoded and no formula is translated.
type RawCell struct {
	// Row and Col are the cell's 1-based position: from its r attribute,
	// or, when r is absent, the row's number and the column after the
	// previous cell's (column 1 for the first).
	Row, Col int
	// T is the t attribute as stored ("s", "str", "inlineStr", "b", "e",
	// "n", "d"); "" when absent, which means "n".
	T string
	// S is the s attribute (the cellXfs index); 0 when absent, Excel's
	// default. SOK is false when s is present but not a non-negative
	// integer (the reader then treats the style as unknown).
	S   int
	SOK bool
	// CM and VM are the cm and vm attributes (1-based indexes into the
	// metadata part's cellMetadata and valueMetadata); 0 when absent or
	// not a positive integer.
	CM, VM int
	// V is the character data of <v>, entity-decoded; HasV says <v> was
	// present (an empty <v/> is HasV with V "").
	V    string
	HasV bool
	// IS is the plain text of an inline string <is>: the <t> of <is> and
	// of each <r> run, joined, phonetic runs (<rPh>) left out; HasIS says
	// <is> was present.
	IS    string
	HasIS bool
	// F is the cell's <f>, if any.
	F Formula
	// Master is, for a shared formula (F.Type Shared with a valid si), the
	// 1-based index in Index.Masters of the master in effect at this cell:
	// the cell itself for a master, else the latest master of its si
	// before it in document order. 0 when there is none (an orphan child,
	// or not a shared formula). Resolved when the cell is read, so an si
	// reused within one row resolves each child to the master before it.
	Master int
	// In is the 1-based index, in Index.Anchors, of the array formula or
	// data table whose ref holds this cell when this cell is not that
	// formula's anchor; 0 when it is in none. A cell that has its own <f>
	// is never In an anchor (its own formula wins; the file is malformed
	// and the reader shows what the cell stores).
	In int
}

// Empty reports whether the cell holds no content: no value, no inline
// string and no formula (a style-only cell). The used range and --limit
// count only cells that are not Empty (survey R13).
func (c *RawCell) Empty() bool { return !c.HasV && !c.HasIS && !c.F.Present }

// FormulaType is the t attribute of <f>.
type FormulaType uint8

// The formula types (ST_CellFormulaType).
const (
	// Normal is t absent or "normal".
	Normal FormulaType = iota
	// Shared is t="shared": a master (with text and ref) or a child
	// (no text, its si names the master).
	Shared
	// Array is t="array": an array formula anchor, its ref the cells it
	// fills (a dynamic spill or a legacy CSE array; the anchor's cm tells
	// which).
	Array
	// DataTable is t="dataTable": a what-if data table anchor.
	DataTable
	// UnknownType is any other t; the reader shows the text as for a
	// normal formula.
	UnknownType
)

// Formula is a cell's <f> as stored.
type Formula struct {
	// Present says the cell has an <f> element.
	Present bool
	Type    FormulaType
	// Text is the entity-decoded character data of <f>: the stored form
	// (no "=", _xlfn. prefixes written out). "" for a shared child.
	Text string
	// Ref is the ref attribute as stored ("" when absent): the cells of a
	// shared group, an array or a data table.
	Ref string
	// SI is the si attribute of a shared formula; SIOK is false when it
	// is absent or not a non-negative integer.
	SI   int
	SIOK bool
	// DataTable attributes as stored (ECMA-376 Part 1 §18.3.1.40), for
	// the data-table note: dt2D, dtr, r1 and r2.
	DT2D, DTR bool
	R1, R2    string
}

// Row is one <row> as Scan hands it to the caller.
type Row struct {
	// Num is the 1-based row number: the r attribute, or the previous
	// row's number plus one when r is absent.
	Num int
	// Hidden is row@hidden ("1" or "true"). A zero-height row (ht="0")
	// without hidden is not hidden (brief: say so in help).
	Hidden bool
	// Cells are the row's cells inside the band's columns, in column
	// order. The slice and the RawCells are reused for the next row:
	// the caller copies what it keeps (the strings themselves stay
	// valid).
	Cells []RawCell
	// Index is the Index being built: during the callback, Master and
	// Anchors answer for everything read so far (which includes every
	// master and anchor a cell of this row can refer to).
	Index *Index
}
