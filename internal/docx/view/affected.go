package view

import (
	"strings"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// AffectedMarkup returns, for each requested paragraph address, the agent
// markup of the body block that renders it in the markup view: the
// "affected" markup edit reports carry (docxdriver's commands/mod.rs
// affected_markup). One render serves every address; an address no block
// lists (a text box paragraph) is found by its rendered id attribute, and
// an address not rendered at all is absent from the result. A paragraph
// of a header, footer or note (StoryParagraphs) has the markup of the
// header, footer or notes it is in.
//
// The render emits only what the addresses need: the top-level body
// blocks that hold their paragraphs (whole lists, as the markup groups
// them) and the headers and footers that hold their story paragraphs;
// the rest of the document is laid out, never rendered. A note paragraph,
// or a document rendered without an equation converter, takes the full
// render. The result is AffectedMarkupFull's.
func AffectedMarkup(d *model.Document, paraIDs []string, opt Options) (map[string]string, error) {
	found := map[string]string{}
	if len(paraIDs) == 0 {
		return found, nil
	}
	if opt.Equation == nil {
		// ErrNoEquation depends on every part the full render renders.
		return AffectedMarkupFull(d, paraIDs, opt)
	}
	ids := d.ParaIDs().IDs
	r := newRenderer(d, d.XML, Markup, opt, nil, true)
	f, ok := r.affectedFilter(ids, paraIDs)
	if !ok {
		return AffectedMarkupFull(d, paraIDs, opt)
	}
	out := r.renderParts(ids, f)
	if r.err != nil {
		return nil, r.err
	}
	return markupOf(out, paraIDs, blockIndex(out, ids), func(s string) string { return s }), nil
}

// AffectedMarkupFull is AffectedMarkup from a render of the whole
// document: the reference the filtered render is tested against.
func AffectedMarkupFull(d *model.Document, paraIDs []string, opt Options) (map[string]string, error) {
	found := map[string]string{}
	if len(paraIDs) == 0 {
		return found, nil
	}
	r, err := Render(d, Markup, opt)
	if err != nil {
		return nil, err
	}
	return markupOf(r, paraIDs, blockIndex(r, d.ParaIDs().IDs), agentForm(LoadStyles(d.Pkg))), nil
}

// blockIndex maps each body paragraph address to the first body block
// listing its paragraph.
func blockIndex(r *Rendered, ids map[*tree.Node]model.ParaID) map[string]int {
	blockOf := map[string]int{}
	for i, b := range r.Blocks {
		for _, p := range b.Paragraphs {
			if id, ok := ids[p]; ok {
				if _, seen := blockOf[id.ID]; !seen {
					blockOf[id.ID] = i
				}
			}
		}
	}
	return blockOf
}

// affectedFilter selects what rendering the paragraphs with addresses
// want needs: for a body paragraph the top-level body block holding it
// (found through its ancestors, so a text box or table cell paragraph
// selects its block), for a header or footer paragraph that part. It
// sets the renderer's story addresses. It reports false when an address
// is a note paragraph's: the notes need the whole body.
func (r *renderer) affectedFilter(ids map[*tree.Node]model.ParaID, want []string) (*renderFilter, bool) {
	f := &renderFilter{blocks: blockFilter{}, chrome: map[string]bool{}}
	wanted := make(map[string]bool, len(want))
	for _, id := range want {
		wanted[id] = true
	}
	story := r.storyParagraphs(ids)
	r.story = make(map[*tree.Node]model.ParaID, len(story))
	for _, p := range story {
		r.story[p.Node] = model.ParaID{ID: p.ID, Ord: -1}
		if !wanted[p.ID] {
			continue
		}
		if p.Part == footnotesPart || p.Part == endnotesPart {
			return nil, false
		}
		f.chrome[p.Part] = true
	}
	b := r.main.Body()
	if b == nil {
		return f, true
	}
	top := map[*tree.Node]bool{}
	for _, c := range effectiveStoryChildren(b, "p", "tbl") {
		top[c] = true
	}
	for n, id := range ids {
		if !wanted[id.ID] {
			continue
		}
		for a := n; a != nil; a = a.Parent() {
			if top[a] {
				f.blocks[a] = true
				break
			}
		}
	}
	return f, true
}

// agentForm turns full markup into agent markup (AgentHTML) with the
// document's style catalog.
func agentForm(styles *Styles) func(string) string {
	catalog := styles.Catalog()
	def, hasDef := styles.DefaultParagraphStyle()
	return func(html string) string { return AgentHTML(html, catalog, def, hasDef) }
}

// markupOf finds each address's markup in a rendering, through form:
// agentForm for a full render, the identity for one in agent mode.
func markupOf(r *Rendered, paraIDs []string, blockOf map[string]int, form func(string) string) map[string]string {
	rendered := map[int]string{}
	found := map[string]string{}
	for _, id := range paraIDs {
		i, ok := blockOf[id]
		if !ok {
			needle := `id="` + id + `"`
			for j, b := range r.Blocks {
				if strings.Contains(r.Body[b.Start:b.End], needle) {
					i, ok = j, true
					break
				}
			}
		}
		if !ok {
			if m, ok := storyMarkup(r, id, form); ok {
				found[id] = m
			}
			continue
		}
		m, done := rendered[i]
		if !done {
			b := r.Blocks[i]
			m = form(r.Body[b.Start:b.End])
			rendered[i] = m
		}
		found[id] = m
	}
	return found
}

// storyMarkup is the agent markup of the header, footer or notes whose
// rendering holds the paragraph with address id.
func storyMarkup(r *Rendered, id string, form func(string) string) (string, bool) {
	needle := `id="` + id + `"`
	at := strings.Index(r.HTML, needle)
	if at < 0 {
		return "", false
	}
	for _, g := range r.Regions {
		if g.Part != "" && g.Start <= at && at < g.End {
			return form(r.HTML[g.Start:g.End]), true
		}
	}
	start := strings.LastIndex(r.HTML[:at], "<aside data-notes=")
	end := strings.Index(r.HTML[at:], "</aside>")
	if start < 0 || end < 0 {
		return "", false
	}
	return form(r.HTML[start : at+end+len("</aside>")]), true
}
