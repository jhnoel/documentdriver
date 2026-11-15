package create

import (
	"errors"
	"strings"
	"testing"
)

// TestCreateAcceptsReadMarkup: read's markup of a whole document is
// create's input unchanged: the <section> wrappers and their derived
// attributes, read's derived attributes elsewhere (ids, list markers,
// data-style naming a heading's own style, data-number), a page field
// read shows as a placeholder (a new field), and an equation's data-ref
// (a new equation from its LaTeX). The derived section attributes are
// ignored: a section keeps the new document's page.
func TestCreateAcceptsReadMarkup(t *testing.T) {
	in := `<section data-section="1" data-first-page="true" data-page-width-twips="15840" data-page-height-twips="12240" data-orientation="landscape" data-columns="2" data-column-spacing-twips="360" data-column-separator="true" data-break-type="nextPage" data-page-number-format="lowerRoman" data-page-number-start="4">
<header data-kind="first">
<p>T</p>
</header>
<footer>
<p>Page <span data-field="PAGE" data-ref="opaque:1">1</span> of <span data-field="NUMPAGES" data-ref="opaque:2">1</span></p>
</footer>
<h2 id="75CE3F39" data-style="Heading2" data-number="1." style="font-weight:unset;"><b>c</b></h2>
<p id="0A1B2C3D">x <equation data-ref="0A1B2C3D/equation:1" display="inline">x^{2}</equation></p>
</section>
<section data-section="2" data-page-width-twips="12240" data-page-height-twips="15840" data-column-spacing-twips="720">
<p id="40A06DF1">two</p>
</section>`
	m := markupOf(t, created(t, in))
	for _, want := range []string{
		`<section data-section="1" data-first-page="true" data-page-width-twips="12240" data-page-height-twips="15840" data-column-spacing-twips="720">`,
		`<header data-kind="first">`, `data-field="PAGE"`, `data-field="NUMPAGES"`,
		`data-style="Heading2"`, `x^{2}`, `<section data-section="2"`, `two</p>`,
	} {
		if !strings.Contains(m, want) {
			t.Errorf("no %s in\n%s", want, m)
		}
	}
	if again := markupOf(t, created(t, m)); sameContent(again) != sameContent(m) {
		t.Errorf("read's markup of the created document does not read the same when created again:\n%s\n%s", m, again)
	}
}

// TestCreateReadMarkupRefusals: what read's markup carries that a new
// document cannot have is refused, never dropped: a placeholder (content
// of the document read showed), section attributes on another element,
// and attributes read does not write.
func TestCreateReadMarkupRefusals(t *testing.T) {
	for _, c := range []struct{ in, path, msg string }{
		{`<p><opaque data-ref="0A1B2C3D/opaque:1" data-kind="drawing"></opaque></p>`, "html", "placeholder"},
		{`<p>a<span data-field="DATE" data-ref="0A1B2C3D/opaque:1">today</span></p>`, "html", "placeholder"},
		{`<p data-page-width-twips="1">a</p>`, "html", "unknown HTML attribute"},
		{`<section data-page-size="1"><p>a</p></section>`, "html", "unknown HTML attribute"},
	} {
		_, err := Build(c.in)
		var e *Error
		if !errors.As(err, &e) || e.Code != "invalid_operation" || e.Path != c.path || !strings.Contains(e.Msg, c.msg) {
			t.Errorf("%s: %v (%+v), want invalid_operation at %s (%s)", c.in, err, e, c.path, c.msg)
		}
	}
}
