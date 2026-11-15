// Package read is the xlsx read surface: `xlsx read` (the workbook
// summary and cell reads in the cells, values and formulas views) and
// `xlsx find`. Its agent-visible shapes are those of
// docs/xlsx-contract-draft.md: the owner decisions signed off 2026-11-14
// and the approved shapes (design approver for the owner,
// 2026-11-14). Reading never changes a document.
//
// # Layout
//
//   - book.go, styles.go, metadata.go, tables.go parse the small parts
//     (workbook, its relationships, styles, xl/metadata.xml, tables) into
//     trees; they are the files listed in internal/archtest's
//     xlsxTreeFiles. Nothing else here parses a tree.
//   - convert.go turns a sheet.RawCell into a model.Entry: shared
//     strings, value types, dates through numfmt, formulas through
//     formula.Display and formula.Translate, array kinds through the
//     metadata.
//   - workbook.go, cells.go and find.go are the three commands; resolve.go
//     maps --range and --sheet to a sheet and a rectangle; result.go has
//     the result types and their JSON; errors.go the contract errors.
//
// # Input, base and errors
//
// Every entry point takes the input file's bytes, as docx's read does:
// base is cli.Hash(input) and the package is opened from the same bytes
// with opc.OpenBytes, so the file is read once (no separate hashing
// pass). Errors are *cli.Error:
//
//   - invalid_document: not a zip, a safety limit tripped (opc; also
//     when it trips on a part read lazily, such as a worksheet or the
//     shared strings: any error wrapping opc.ErrLimit), no
//     officeDocument relationship or workbook part, a workbook part whose
//     content type is not a SpreadsheetML workbook (a binary .xlsb), or a
//     workbook part that is not well-formed XML or has no <workbook>
//     root. An encrypted workbook is a compound file, not a zip: the
//     first case.
//   - usage (cmd/xlsx checks): a bad --limit; --kind workbook with
//     --range or --sheet; --view, --limit or --group out of place.
//   - invalid_operation: a malformed address (path --range or --sheet,
//     detail the model.AddressError reason); a name or table that does
//     not resolve to one plain range; a flag or kind A1 does not
//     implement ("not yet supported", path the flag): --detail, --kind
//     names|tables|styles|comments. cmd/xlsx checks --kind first: a kind
//     other than workbook is this error whatever else is given, before
//     any usage check.
//   - not_found: a sheet, name, table or table column the workbook does
//     not have (path the flag).
//   - unreadable_target: the sheet a cell read or a --sheet find targets
//     cannot be read (its part is missing or not well-formed, or is not a
//     worksheet): path --range or --sheet. Find without --sheet skips an
//     unreadable sheet with a note instead.
//
// A damaged auxiliary part (styles, shared strings, metadata, a table) is
// never an error: the facet it carries is left out and a note says so
// (contract §10).
//
// # Memory
//
// Budget: live heap at most N + T + S + 8 B per row + 4 B per shared
// string + 2 x output, with N the input size, T the largest decompressed
// worksheet and S the shared strings' text, held while any sheet is read
// (the brief's formula omits S; TestMemoryBudget states and measures it). The
// output counts twice: the cell buffer and the encoded result.
//
// The input bytes are held throughout; one worksheet's text is held at a
// time (opc.Part.Forget after each sheet in the summary and in
// find); the shared strings are indexed once (sheet.SST) and their text
// kept; the cells are encoded into the output buffer as they are read,
// never held as a []model.Entry.
package read
