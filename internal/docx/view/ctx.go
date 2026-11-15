package view

import (
	"encoding/hex"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// partCtx is what rendering one part needs (docxdriver's
// html/ctx.rs RenderCtx): link targets and picture sources from the part's
// relationships, table handles, list membership and labels as the view
// counts them, footnote and endnote bodies, and paragraph addresses.
type partCtx struct {
	r          *renderer
	links      map[*tree.Node]string
	images     map[string]string
	imageParts map[string]string
	tables     map[*tree.Node]string
	lists      map[*tree.Node]model.ListInfo
	markers    map[*tree.Node]string
	notes      *noteBodies
	paraIDs    map[*tree.Node]model.ParaID
	// storyRefs are the equation and placeholder addresses of a part
	// whose paragraphs have none (a header, a footer, notes): numbered
	// across the part, "equation:N" and "opaque:N", built on first use.
	storyRefs map[*tree.Node]string
	scope     *tree.Node
	// sink collects what agent markup written for the part holds
	// (agent mode only).
	sink *sink
	// runBuf is commonRunCSS's scratch list of run formatting, reused
	// from paragraph to paragraph.
	runBuf []fmtState
}

// newPartCtx builds the context of part, whose parsed XML is x. Only the
// main document part has paragraph addresses; the caller sets them.
func (r *renderer) newPartCtx(x *model.XML, part string) *partCtx {
	scope := x.Body()
	if scope == nil {
		scope = x.Root()
	}
	c := &partCtx{
		r:       r,
		links:   linkTargets(r.pkg, part, scope),
		tables:  tablePositions(x),
		lists:   map[*tree.Node]model.ListInfo{},
		markers: map[*tree.Node]string{},
		notes:   &noteBodies{},
		scope:   scope,
		sink:    newSink(),
	}
	c.images, c.imageParts = imageSources(r.pkg, part)
	if scope != nil {
		mv := r.view.model()
		c.lists = r.numbering.ListInfo(scope, mv)
		c.markers = r.numbering.ListMarkers(scope, mv)
	}
	return c
}

// noteDOMID is the page-scoped element id of a note body.
func (c *partCtx) noteDOMID(kind, id string) string {
	return "docx-" + c.r.noteScope + "-" + kind + "-" + id
}

// noteScope returns the first 24 hex characters of source for scoping note ids.
func noteScope(source [32]byte) string { return hex.EncodeToString(source[:])[:24] }

// linkTargets maps every w:hyperlink under scope to its href: the target
// of its relationship (r:id), "#anchor" for an internal link, else "".
func linkTargets(pkg *model.Package, part string, scope *tree.Node) map[*tree.Node]string {
	rels := map[string]string{}
	for _, r := range pkg.Rels(part) {
		if r.HasID && r.HasTarget {
			rels[r.ID] = r.Target
		}
	}
	links := map[*tree.Node]string{}
	if scope == nil {
		return links
	}
	scope.Walk(func(n *tree.Node) bool {
		if n == scope || !model.IsW(n, "hyperlink") {
			return true
		}
		href := ""
		if id, ok := model.Attr(n, "id"); ok {
			href = rels[id]
		} else if a, ok := model.Attr(n, "anchor"); ok {
			href = "#" + a
		}
		links[n] = href
		return true
	})
	return links
}

// tablePositions maps every body table, its direct rows and their direct
// cells to their handles: "table:T", "table:T/row:R",
// "table:T/row:R/cell:C" (tables in document order, nested ones included).
func tablePositions(x *model.XML) map[*tree.Node]string {
	out := map[*tree.Node]string{}
	for ti, t := range Tables(x) {
		at := "table:" + itoa(ti+1)
		out[t] = at
		for ri, row := range childrenW(t, "tr") {
			ra := at + "/row:" + itoa(ri+1)
			out[row] = ra
			for ci, cell := range childrenW(row, "tc") {
				out[cell] = ra + "/cell:" + itoa(ci+1)
			}
		}
	}
	return out
}

// Tables returns every w:tbl in the body, in document order, nested
// tables included.
func Tables(x *model.XML) []*tree.Node {
	b := x.Body()
	if b == nil {
		return nil
	}
	return model.Descendants(b, func(n *tree.Node) bool { return model.IsW(n, "tbl") })
}

// childrenW returns n's element children that are IsW local.
func childrenW(n *tree.Node, local string) []*tree.Node {
	var out []*tree.Node
	for c := range n.ChildrenSeq() {
		if model.IsW(c, local) {
			out = append(out, c)
		}
	}
	return out
}

// storyRef returns the story-wide address of an equation element or a
// placeholder's key in a part without paragraph addresses.
func (c *partCtx) storyRef(n *tree.Node) (string, bool) {
	if c.storyRefs == nil {
		c.storyRefs = map[*tree.Node]string{}
		eq, ph := 0, 0
		if c.scope != nil {
			for _, p := range model.Descendants(c.scope, func(n *tree.Node) bool { return model.IsW(n, "p") }) {
				for _, e := range model.CurrentEquations(p) {
					eq++
					c.storyRefs[e] = "equation:" + itoa(eq)
				}
				list, _ := model.Placeholders(p)
				for _, x := range list {
					ph++
					c.storyRefs[x.Key] = "opaque:" + itoa(ph)
				}
			}
		}
	}
	ref, ok := c.storyRefs[n]
	return ref, ok
}
