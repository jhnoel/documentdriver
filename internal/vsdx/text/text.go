// Package text maps Visio text to the HTML of the contract's text
// markup, the pptx grammar's subset (docs/vsdx-design.md, decision f).
// A Text element is mixed content: characters, with cp, pp and tp
// markers that start a character, paragraph or tab run (by the IX of a
// Character, Paragraph or Tabs row) and fld elements whose content is a
// field's cached text. A line feed ends a paragraph; U+2028 is a line
// break. Parse and Plain are pure over the tree; Render is pure over
// Body and the resolved row formats.
package text

import "github.com/jhnoel/documentdriver/internal/ooxml/tree"

// Body is a text body: its paragraphs in order.
type Body struct {
	Paras []Para
}

// Para is one paragraph and the Paragraph row (pp IX) in force at its
// start, -1 when no pp marker precedes it (row 0 applies, as Visio
// does).
type Para struct {
	PP   int
	Runs []Run
}

// Run is a stretch of text under one Character row (cp IX, -1 when no
// marker precedes it), a line break (Br), or a field (Field >= 0: the
// Field row IX, Text its cached text).
type Run struct {
	CP    int
	Text  string
	Br    bool
	Field int
}

// MaxText bounds the characters one Text element may yield; a longer
// one is cut with a note (the contract's output bound, as xlsx's
// 32,767).
const MaxText = 1 << 20

// Parse reads a Text element into a body. Unknown child elements are
// skipped and counted in skipped (a note). Chunk T1.
func Parse(el *tree.Node) (b Body, skipped int) {
	_ = el
	return Body{}, 0
}

// Plain returns the body's text as find matches it and read's text
// shows it: paragraphs joined by "\n", a break as "\n", a tab as
// "\t", a field as its cached text. Chunk T1.
func Plain(b Body) string {
	_ = b
	return ""
}
