// Package integrity checks an xlsx workbook for the faults that make Excel
// repair or refuse it, and compares two checks so an edit is refused only
// for what it introduced (docs/design.md, xlsx; the rule is the docx
// gate's, and the contract's: a fault the input already had never blocks).
//
// Check streams: a worksheet and the shared strings are read with
// tree.Scan, never as a tree, so what Check keeps while it reads a sheet
// is in proportion to the rows' state, the sheet's merges and shared
// formula groups, and the header cells of its tables, not to the cells.
// The parts that are small (workbook, styles, tables, relationships) are
// parsed as trees. A part that does not parse, a missing part or a
// malformed sheet is a finding, never an error for the whole workbook.
//
// Findings are internal kinds. They carry no positions in their identity
// (Finding), so an edit that moves a fault does not introduce it; the
// instances (Instance) add a human-readable location. They do not map to
// the contract's hazards or error wording: that is for the signed
// contract.
//
// Not checked yet: the formulas of defined names and cells (the formula
// track's lexer will replace the pending note), and the date formats.
package integrity
