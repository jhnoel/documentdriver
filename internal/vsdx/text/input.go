package text

import "github.com/jhnoel/documentdriver/internal/vsdx/vsd"

// ParseHTML reads the HTML of a text.set or object.add (the grammar
// Render writes, through htmlx.ParseHTML in strict mode and
// htmlx.StyleDecls) into a body and the formats its runs and paragraphs
// set, one Character row per distinct CharFormat and one Paragraph row
// per distinct ParaFormat, numbered from 0 in first use; Terminated is
// true (Visio ends text with a line feed). A field placeholder is kept
// by its data-ref. Anything outside the grammar is an error naming the
// element or attribute. Render(ParseHTML(h)) == h for every h Render
// writes. Chunk K5.
func ParseHTML(h string) (Body, Formats, error) {
	_ = h
	return Body{}, Formats{}, vsd.ErrNotImplemented
}
