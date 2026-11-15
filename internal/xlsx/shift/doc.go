// Package shift is the structural edit of a worksheet: row and column
// insert and delete, with every reference the closed "rewritten" list
// names adjusted and every construct the "refused" list names refused
// (docs/design.md, xlsx: Formulas; owner decision 12; ops'
// structuralAdjusts and structuralRefuses are the agent-visible lists).
//
// Apply (apply.go, G5) is the one entry point the ops call. It
// works through the edit layer: the target sheet's whole text is taken
// (Sheet.Text), split into its top-level pieces (Sheet.Pieces),
// rewritten piece by piece in one pass each (SheetData, K7; Element,
// K8) and given back (Sheet.SetPieces); every other sheet's
// formulas, the defined names and the tables are rewritten by the pure
// functions of workbook.go (K9), which Apply writes back. Nothing is
// written when anything refuses: a refusal returns before the first
// SetPieces, and the plan's transaction discards the rest.
//
// Formulas are rewritten with formula.Shift (every reference token that
// resolves to the edited sheet, every other byte verbatim) and shared
// groups with formula.DecideShared (by position: kept, split or
// unshared). Errors: *Refused (operation_failed with the construct as
// detail), *edit.Unreadable (unreadable_target), or a bug.
//
// Memory: the target sheet's original, its current text and the new
// text (3 N at most); other sheets one at a time (Workspace.Release).
package shift
