// Package edit is the xlsx edit layer: a plan's editing view of a
// workbook (docs/design.md, xlsx; P2 write core). It is the only xlsx
// package that changes parts: worksheets through splice patches
// (opc.Part.SetPatch, declared with plan.WriteSet.DeclareSpliced), the
// small parts (workbook, styles, tables, content types, relationships)
// through the edit core's handles, and whole parts through AddPart and
// DeletePart (internal/archtest: patcherPkgs, partWriterPkgs).
//
// # Layout
//
//   - workspace.go: Workspace, the transaction's view (plan.Workspace):
//     the workbook's sheets, lazily opened Sheets, the recalculation
//     state, Finish and Verify.
//   - smallparts.go: the workbook, its relationships and the tables,
//     parsed as trees (the one file of this package in archtest's
//     xlsxTreeFiles). Worksheets and shared strings never come here.
//   - lex.go, locate.go: the row and cell locator. A worksheet is
//     indexed once by sheet.Scan (8 B a row: its number and the offset of
//     its start tag); lex.go is a minimal tag lexer over a part already
//     known to be well formed, and locate.go finds, from those offsets
//     alone, a row's span, its cells' spans, sheetData's span and the
//     worksheet's other top-level elements, reading only the bytes of
//     the row or element asked for (and the head and tail of the sheet).
//   - sheet.go: Sheet, the per-sheet overlay. Cell, row and top-level
//     element edits are kept in maps until Finish, when one pass turns
//     them into one splice patch over the part's original string. A
//     structural op (row or column insert or delete) takes the sheet's
//     whole text (Text), rewrites it in one pass and gives it back
//     (SetText); the sheet then edits the new text, and Finish expresses
//     the result as one replacement of the worksheet root's content over
//     the original, so the part is still a spliced part and never a tree.
//   - cell.go: rewriting one <c> element (its <f>, its <v> and t, its s)
//     while keeping every other attribute and child.
//   - cellxml.go: rendering a new <c> from a value (inline strings,
//     numbers, booleans, errors, formulas without a cached value).
//   - styles.go: interning cell formats in styles.xml.
//   - recalc.go: the recalculation policy (fullCalcOnLoad, calcChain).
//   - units.go: plan.Units for xlsx (no units: small parts are declared
//     whole, worksheets spliced).
//
// # Streaming and memory
//
// A worksheet's sheetData and the shared strings are never parsed into
// a tree. Opening a Sheet holds its original string (N), the Index (8 B
// a row plus masters and anchors) and the layout (O(top-level
// elements)). Edits cost their own bytes: a cell edit keeps its new <c>
// and its RawCell. Finish holds the original and the patch, never a copy
// of the part: apply of one cell.set stays within 1.3 N + T (T the
// shared strings, which an edit never touches: decision 11). Text and
// SetText (structural ops only) hold the original, the current text and
// the new one at once (3 N at most while they run).
//
// # Errors
//
// Errors the operations map to contract codes are ErrNotFound (a sheet,
// table or name the workbook does not have: not_found) and *Unreadable
// (the op's own target cannot be edited safely: unreadable_target, with
// the detail). Anything else is a bug or an I/O failure.
package edit
