// Package formula lexes, validates and rewrites SpreadsheetML formulas in
// the form a workbook stores them (no leading "=", function prefixes such
// as "_xlfn." written out, ANCHORARRAY and _xlfn.SINGLE instead of the
// display forms "#" and "@"), and parses A1 references.
//
// It is pure: it imports only the standard library and knows nothing of
// packages, parts or the engine (docs/design.md, xlsx, Formulas).
//
//   - Lex splits a formula into tokens whose texts concatenate back to the
//     input byte for byte, whatever the input.
//   - Validate is a depth-limited syntax check that returns a reason, never
//     a contract error code or a path.
//   - Shift and Rename rewrite only the reference tokens that resolve to
//     the affected sheet and copy every other byte verbatim.
//   - Translate gives a shared-formula child its own text, and
//     DecideShared says what a row or column shift does to a shared group.
//   - ParseCell, ParseRange, ParseSqref and LooksLikeCellRef are the A1
//     parsing every xlsx package uses.
//   - LookupFunction reports the stored prefix and the dynamic-array
//     evidence the corpus holds for a function.
//
// Excel behaviours this package assumes, for excel-smoke to check, are
// listed on Shift and DecideShared.
package formula
