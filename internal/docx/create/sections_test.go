package create

import (
	"strings"
	"testing"
)

// TestCreateSections: sections with first-page, default and even headers
// and footers (page fields, links, a table and a list in a story); the
// document reads back with them, and read's markup sent back to create
// reads the same.
func TestCreateSections(t *testing.T) {
	in := `<section first-page/><header first><p>Title page</p></header><footer><p>Page <page-number/> of <page-count/></p></footer>` +
		`<p>one</p><section/><header even><p>Even <a href="https://e.com/">link</a></p></header>` +
		`<header><table><tr><td>left</td><td>right</td></tr></table></header><footer even><ul><li>note</li></ul></footer><p>two</p>` +
		`<section/><section/><p>four</p>`
	m := roundTrip(t, in,
		`<section data-section="1" data-first-page="true"`, `<header data-kind="first">`, `Title page`,
		`<footer>`, `data-field="PAGE"`, `data-field="NUMPAGES"`, `<p id="`,
		`<section data-section="2"`, `<header data-kind="even">`, `href="https://e.com/"`,
		`<header>`, `<td>`, `left`, `<footer data-kind="even">`, `<ul data-list=`, `note`,
		`<section data-section="3"`, `<section data-section="4"`, `four</p>`)
	if n := strings.Count(m, "<section "); n != 4 {
		t.Errorf("%d sections, want 4:\n%s", n, m)
	}
}

// TestCreateStoriesOnly: a document of headers and footers alone has one
// empty body paragraph.
func TestCreateStoriesOnly(t *testing.T) {
	roundTrip(t, `<header><p>h</p></header><footer><p>f</p></footer>`, `<header>`, `<footer>`)
}
