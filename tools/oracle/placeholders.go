package main

import (
	"regexp"
	"strings"
)

// The Go projection marks placeholders (docs/contract.md, section 7):
// content the HTML cannot edit carries its address, data-ref
// "<paragraph>/opaque:N", on the rendering docxdriver gives it (an
// image, a note reference, a field); a wrapper whose content it shows
// (a content control, a ruby, alternate content, an unknown element) is
// a span with the address and data-kind around that content; and
// content docxdriver drops (a chart, a shape, a symbol) is an empty
// <opaque>. withoutPlaceholders takes those additions out of Go markup,
// so the rest compares with docxdriver's byte for byte.
var (
	opaqueElement = regexp.MustCompile(`<opaque[^>]*></opaque>`)
	opaqueRefAttr = regexp.MustCompile(` data-ref="[^"]*opaque:[0-9]+"`)
	kindSpanOpen  = `<span data-kind="`
)

// withoutPlaceholders strips Go placeholder markup from markup for comparison.
func withoutPlaceholders(markup string) string {
	if !strings.Contains(markup, "opaque") && !strings.Contains(markup, kindSpanOpen) {
		return markup
	}
	s := opaqueElement.ReplaceAllString(markup, "")
	s = opaqueRefAttr.ReplaceAllString(s, "")
	for {
		i := strings.Index(s, kindSpanOpen)
		if i < 0 {
			return s
		}
		end := strings.IndexByte(s[i:], '>')
		if end < 0 {
			return s
		}
		open := i + end + 1
		depth, j := 1, open
		for depth > 0 {
			o := strings.Index(s[j:], "<span")
			c := strings.Index(s[j:], "</span>")
			if c < 0 {
				return s
			}
			if o >= 0 && o < c {
				depth++
				j += o + len("<span")
				continue
			}
			depth--
			if depth == 0 {
				s = s[:i] + s[open:j+c] + s[j+c+len("</span>"):]
				break
			}
			j += c + len("</span>")
		}
	}
}
