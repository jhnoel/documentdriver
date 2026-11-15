package view

import (
	"regexp"
	"strings"
)

var (
	oracleParagraph = regexp.MustCompile(`<(?:p|h[1-6]|li)\b[^<>]*?\bid=\\?"([0-9A-Fa-f]{8})\\?"`)
	oracleEquation  = regexp.MustCompile(`data-docx-equation=(\\?")(\d+)(\\?")`)
)

// FromOracleMarkup rewrites the attributes of docxdriver 0.5.2's HTML
// (its projection, or HTML written for it) into the agent contract's
// (docs/contract.md, section 7), for parity runs and tests: the binaries
// never call it, and the docx input refuses the old names.
//
//   - data-docx-handle="table:1/row:2" is id="table:1/row:2": an
//     element's address is its id;
//   - data-docx-equation="2" is data-ref="<paragraph>/equation:2", the
//     equation's address, with the id of the paragraph it is in (the
//     nearest paragraph start before it; "equation:2" where that has
//     none);
//   - every other data-docx-<name> is data-<name>.
//
// The text may be HTML or JSON holding HTML (quotes escaped): the
// rewrite works on either.
func FromOracleMarkup(text string) string {
	if !strings.Contains(text, "data-docx-") {
		return text
	}
	paragraphs := oracleParagraph.FindAllStringSubmatchIndex(text, -1)
	var b strings.Builder
	last := 0
	for _, m := range oracleEquation.FindAllStringSubmatchIndex(text, -1) {
		// The paragraph the equation is in: the last one that starts
		// before it, unless a paragraph without an id starts in between.
		ref := "equation:" + text[m[4]:m[5]]
		for i := len(paragraphs) - 1; i >= 0; i-- {
			if paragraphs[i][0] < m[0] {
				between := text[paragraphs[i][1]:m[0]]
				if !strings.Contains(between, "<p>") && !strings.Contains(between, "<p ") {
					ref = text[paragraphs[i][2]:paragraphs[i][3]] + "/" + ref
				}
				break
			}
		}
		q := text[m[2]:m[3]]
		b.WriteString(text[last:m[0]] + "data-ref=" + q + ref + q)
		last = m[1]
	}
	b.WriteString(text[last:])
	out := strings.ReplaceAll(b.String(), "data-docx-handle=", "id=")
	return strings.ReplaceAll(out, "data-docx-", "data-")
}
