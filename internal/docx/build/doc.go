// Package build lowers HTML input items (internal/docx/html) into
// WordprocessingML: the one lowering behind every operation that takes
// `html` (paragraph.insert, and next paragraph.replace, header.set,
// footer.set and table cells) and behind `create`. It ports docxdriver
// 0.5.2's html/build.rs and the run construction of commands/typed.rs
// (insert_plain_pieces) and segments.rs (insert_ins_pieces_at), with
// their bugs fixed rather than copied.
//
// Everything is emitted through the edit core's placed inserts: new
// elements are parsed detached and placed with Handle.PlaceAt (runs,
// hyperlinks, fields, equations in the order the content gives) or
// Handle.PlaceNew (paragraph properties, where the schema puts them), and
// run properties go in through Handle.EnsureIn, so they stand in the
// schema's declaration order (the order Word writes) whatever order the
// HTML gave them in. Nothing here mutates a tree directly.
//
// Content that carries an existing identity is never rebuilt: an
// equation sent back with its data-ref keeps its original bytes (the
// Resolver gives them), and only new equations are converted from LaTeX,
// with the run formatting template of Options.EquationRunProperties.
//
// The package authors no revisions. Inline returns the placed content
// with the groups of siblings one w:ins may wrap, and a tracking
// operation hands those to its Reviser.
package build
