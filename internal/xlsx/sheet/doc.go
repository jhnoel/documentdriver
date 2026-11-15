// Package sheet is the streaming, read-only access to a worksheet's
// cells and to the shared strings: the parts the engine never parses into
// a tree (AGENTS.md, xlsx streaming rule; internal/archtest).
//
//   - Scan reads a worksheet in one tree.ScanString pass. It builds an
//     Index (8 bytes per row, plus O(merges + shared formula groups +
//     array formulas + hidden column runs)) and, for a band of rows the
//     caller asks for, hands over each row's cells as RawCells: the
//     stored attributes and texts, before any interpretation. Array and
//     data-table membership is resolved during the pass (each RawCell
//     names the anchor whose ref holds it), so a band that starts below
//     an anchor still marks its members.
//   - SST indexes sharedStrings.xml with one offset per item (4 bytes)
//     and decodes an item on demand; Each streams every item once (find).
//   - sml.UnescapeX decodes the _xHHHH_ escapes of SpreadsheetML strings.
//
// Nothing here maps to the agent's forms (internal/xlsx/model) or knows
// styles, dates or the workbook: that is internal/xlsx/read. The Index is
// also the read-only base the P2 edit overlay builds on; this package
// never mutates or writes a part.
//
// Memory: a Scan holds the part's text (the caller's string, never
// copied), the Index, the current row's RawCells (at most 16,384, reused
// between rows) and the active anchor set. Strings in a RawCell are
// substrings of the source when the XML holds no entity references, and
// small copies otherwise; they stay valid after Scan returns.
//
// Both main namespaces are read: Transitional
// (http://schemas.openxmlformats.org/spreadsheetml/2006/main) and Strict
// (http://purl.oclc.org/ooxml/spreadsheetml/main). Elements in any other
// namespace are skipped with their subtree.
package sheet
