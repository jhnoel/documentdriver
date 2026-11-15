package text

import (
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
)

// spliceParagraphs returns src (the bytes body was parsed from) with the
// range ReplaceParagraphs replaces swapped for frag: from body's first
// a:p to its last, or, with none, the point after a:lstStyle, else after
// a:bodyPr.
func spliceParagraphs(src []byte, body *tree.Node, frag []byte) []byte {
	s, e := -1, -1
	for _, c := range body.Elements() {
		if derive.Is(c, derive.DML, "p") {
			cs, ce, _ := c.Span()
			if s < 0 {
				s = cs
			}
			e = ce
		}
	}
	if s < 0 {
		anchor := derive.Child(body, derive.DML, "lstStyle")
		if anchor == nil {
			anchor = derive.Child(body, derive.DML, "bodyPr")
		}
		_, e, _ = anchor.Span()
		s = e
	}
	return splice(src, s, e, string(frag))
}
