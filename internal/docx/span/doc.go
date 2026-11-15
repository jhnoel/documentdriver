// Package span is the docx span engine: the one way every span operation
// (replace_text, revise_text, replace_paragraph, format_text, comment_add,
// the bookmark operations) turns a paragraph address and a character range
// into whole runs of the lossless working tree.
//
// Text and offsets come from the read model's one taxonomy
// (model.Classify, model.LeafText): text leaves count their characters,
// tabs, breaks and hyphens one fixed character, objects (equations,
// symbols, drawings, note references) one U+FFFC, and markers, field codes
// and properties nothing. A span therefore never crosses a break, tab or
// object unseen, and the offsets agree with what read and find report.
//
// The steps:
//
//   - Read a paragraph of the working tree (Read): its segments in the
//     all-view offsets (deleted text still occupies positions) and its text
//     in each tracked-change view.
//   - Select a range of one view's text (Para.Select, Para.Find): a Span in
//     all-view offsets. A non-empty selection runs from its first visible
//     character to its last, hidden content between included.
//   - Isolate the span (Isolate): split the runs, and the text leaves, that
//     straddle its ends through the edit core's structural verbs
//     (Handle.SplitText, Handle.SplitAt with the run properties copied), so
//     the span is whole elements: Isolated.Units.
//   - Blocker reports what a span op may not cross.
//
// Comment and bookmark anchors use the same Select and Isolate. The
// package authors no revisions: the revision core consumes Isolated.
//
// UTF-16 parts: the read model transcodes a UTF-16 part before parsing,
// the working tree does not (it reads UTF-8 only), so such a part fails to
// open for editing and no span is ever computed over text the read side
// saw differently. Line ends are normalised alike on both sides
// (model.TextContent).
package span
