// Package text implements the text operations of M3: text.set, which
// replaces a text body with an HTML fragment in the grammar the read view
// emits, and text.replace, which replaces a substring keeping the
// formatting of the run where it starts.
//
// The package works in three layers:
//
//   - Body is the text body as the views show it: paragraphs (alignment,
//     level, bullet) holding runs (bold, italic, underline, size, colour,
//     font) and line breaks. Parse reads it from HTML, strictly; ReadBody
//     projects it from an a:txBody exactly as the read model does; Render
//     writes it back as HTML. Normalise is Render after Parse, so that
//     html(read(set(h))) == Normalise(h) is a testable law.
//   - Build turns a wanted Body and the existing a:txBody into the new
//     a:p elements as XML bytes. Paragraphs and runs the HTML leaves as
//     they are keep their bytes; changed ones carry the properties HTML
//     cannot express from the paragraph or run they replace (see
//     "Text writes (M3)" in docs/design.md).
//   - Set and Replace apply the operations through Object, the small
//     part of edit.Handle they need.
//
// The package imports derive, tree and the edit layer, never the read
// model; a law test in internal/pptx/laws checks that ReadBody and the read
// model agree on every corpus text body.
package text
