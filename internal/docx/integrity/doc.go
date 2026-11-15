// Package integrity reports what makes Word refuse or "repair" a .docx
// package, and gates edits on it.
//
// [Check] reads a package from its bytes and reports error kinds: (part,
// message) pairs with no positions, so the same fault in two versions of a
// package is one kind. It covers
//
//   - the zip container: readable, stored or deflated, unencrypted entries
//     with valid, case-insensitively unique part names;
//   - every XML part: UTF-8 or UTF-16, no DTD, XML 1.0 characters, declared
//     prefixes, well-formed;
//   - the OPC layer: content types, relationship ids, modes and targets, the
//     content type Word writes for each relationship type;
//   - references between parts: r: ids, comments, notes, numbering, style
//     definitions;
//   - markup compatibility: mc:Ignorable, mc:MustUnderstand, mc:Choice;
//   - story structure: field, bookmark, comment and move range pairing,
//     deleted-text markup, significant whitespace, table grids, paragraph,
//     drawing and revision ids;
//   - the ECMA-376 Transitional content model of every WordprocessingML part
//     (schema.go), from internal/ooxml/schema.
//
// [Gate] is the commit rule: an edit may not introduce an error kind its
// input did not already have. Pre-existing faults are not the engine's to
// fix, and they never block an edit. [GateEdit] is the same rule with each
// finding located at its elements ([Instance]) and attributed through the
// plan's working trees: only what the edit's own elements introduced is
// refused, named with the element and the operation.
//
// The package is a port of the integrity module of docxdriver 0.5.2
// (crates/docxdriver-core/src/integrity at 1ffb0d7) and reports the same
// findings, part and message, as that version; docs/docx-integrity.md lists
// where it cannot and why. Every input is untrusted: reading is bounded by
// [opc.Limits], and no input makes it panic.
//
// The parts are read with the engine's own tokenizer, so these checks are
// not independent of it; the independent check of outputs is the
// conformance gate (make conformance: lxml and the Open XML SDK).
package integrity
