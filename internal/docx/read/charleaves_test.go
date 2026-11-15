package read

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/htmlx"
)

// TestCharLeavesInMarkup: the character leaves the selectable text counts
// must appear in read's markup, so that a selection found by find or a
// text op is text the markup shows (and markup read writes round-trips).
// A w:noBreakHyphen is U+2011 in the selectable text and so in the
// markup; a w:ptab counts as a tab, exactly the tab w:tab renders.
func TestCharLeavesInMarkup(t *testing.T) {
	const body = `<w:p w14:paraId="00000001"><w:r><w:t>before</w:t><w:noBreakHyphen/><w:t>mid</w:t><w:ptab/><w:t>after</w:t></w:r></w:p>`
	r, err := Read(docOnly(t, body), "document", "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	markup := r.Object()["markup"].(string)

	if !strings.Contains(markup, "before\u2011mid") {
		t.Errorf("markup lacks U+2011 at the hyphen's position:\n%s", markup)
	}
	if !strings.Contains(markup, "<span data-tab=\"true\">\t</span>") {
		t.Errorf("markup lacks the tab span w:tab renders:\n%s", markup)
	}

	// The plain text of the markup's paragraph equals the paragraph's
	// selectable text, so nothing the selectable text counts is lost.
	x, err := model.ParseXML([]byte(`<w:document xmlns:w="` + nsW + `" xmlns:w14="` + nsW14 + `"><w:body>` + body + `</w:body></w:document>`))
	if err != nil {
		t.Fatal(err)
	}
	want := model.ParagraphText(x.Paragraphs()[0].Node, model.Current)

	root, err := htmlx.ParseHTML(markup, htmlx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var got string
	var find func(n *htmlx.Node) bool
	find = func(n *htmlx.Node) bool {
		if n.Tag == "p" {
			if id, ok := n.Attr("id"); ok && id == "00000001" {
				got = n.TextContent()
				return true
			}
		}
		for _, k := range n.Kids {
			if find(k) {
				return true
			}
		}
		return false
	}
	if !find(root) {
		t.Fatalf("markup has no paragraph 00000001:\n%s", markup)
	}
	if got != want {
		t.Errorf("markup text %q, selectable text %q", got, want)
	}
}
