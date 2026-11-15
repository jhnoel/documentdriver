// Package recalc is the opt-in `apply --recalc` step (owner, formula
// values policy, 2026-11-14): LibreOffice headless used as a calculator.
// The tool never computes a value itself, and LibreOffice's own saved
// package is never shipped: it only supplies the cached values of
// formula cells, which apply splices into its own output
// (edit.Sheet.SetCachedValue), leaving every other byte as apply wrote
// it.
//
//   - Compute (soffice.go; not on wasip1, where it returns
//     ErrUnavailable) copies the workbook into a private temporary
//     directory, runs soffice headless with a private user profile
//     (-env:UserInstallation) whose registrymodifications.xcu sets
//     org.openoffice.Office.Calc/Formula/Load/OOXMLRecalcMode to 0
//     (always recalculate; without it LibreOffice keeps stale caches,
//     E9), converts the copy to xlsx, and reads the values back
//     (Values). The binary is Options.Soffice, else $DD_SOFFICE, else
//     "soffice" on PATH; none found is ErrUnavailable.
//   - Values (values.go) reads LibreOffice's output: for each worksheet,
//     matched to the input's by sheet name, the cached value and type of
//     every formula cell (sheet.Scan), as Value entries.
//
// Nothing here writes a document; the caller does, in a plan
// transaction.
package recalc
