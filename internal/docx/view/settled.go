package view

import (
	"errors"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/revise"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// renderSettled renders the final or original view as accept-all or
// reject-all actually leaves the document (surface.rs render_settled): a
// private copy of the main document and of its headers and footers is
// settled and rendered. Its paragraphs carry the source's addresses and
// story positions, and the returned regions and blocks name the source's
// paragraphs. It reports false when the copy cannot be settled; the caller
// then renders the view from the unsettled document.
func renderSettled(d *model.Document, v View, opt Options, agent bool) (*Rendered, bool, error) {
	b, err := d.Pkg.DocumentXML()
	if err != nil {
		return nil, false, nil //nolint:nilerr // no copy: the caller renders unsettled, as docxdriver does
	}
	cp, err := model.ParseXML(b)
	if err != nil {
		return nil, false, nil //nolint:nilerr // as above
	}
	originals, copies := d.XML.AllParagraphs(), cp.AllParagraphs()
	if len(originals) != len(copies) {
		return nil, false, nil
	}
	source := d.ParaIDs().IDs
	orig := make(map[*tree.Node]*tree.Node, len(copies))
	for i, c := range copies {
		if _, ok := source[originals[i]]; ok {
			orig[c] = originals[i]
		}
	}
	moved := func(from, to *tree.Node) {
		if o, ok := orig[from]; ok {
			orig[to] = o
		}
	}
	accept := v == Final
	if _, err := revise.Settle(core.Private(docxedit.Format, cp.Root()), revise.Scope(cp), revise.Options{Accept: accept, Moved: moved}); err != nil {
		if errors.Is(err, revise.ErrSettle) {
			return nil, false, nil
		}
		return nil, false, err
	}
	overlay := map[string]*model.XML{}
	for _, part := range ChromePartNames(d.Pkg, cp) {
		px, ok := d.Pkg.ParsePart(part)
		if !ok {
			continue
		}
		if _, err := revise.Settle(core.Private(docxedit.Format, px.Root()), revise.Scope(px), revise.Options{Accept: accept}); err != nil {
			if errors.Is(err, revise.ErrSettle) {
				return nil, false, nil
			}
			return nil, false, err
		}
		overlay[part] = px
	}
	ids := map[*tree.Node]model.ParaID{}
	for _, c := range cp.AllParagraphs() {
		if o, ok := orig[c]; ok {
			ids[c] = source[o]
		}
	}
	r := newRenderer(d, cp, v, opt, overlay, agent)
	out := r.renderParts(ids, nil)
	if r.err != nil {
		return nil, false, r.err
	}
	remap := func(nodes []*tree.Node) []*tree.Node {
		var mapped []*tree.Node
		for _, n := range nodes {
			if o, ok := orig[n]; ok {
				mapped = append(mapped, o)
			}
		}
		return mapped
	}
	for i := range out.Regions {
		if out.Regions[i].Kind == RegionBody {
			out.Regions[i].Paragraphs = remap(out.Regions[i].Paragraphs)
		}
	}
	for i := range out.Blocks {
		out.Blocks[i].Paragraphs = remap(out.Blocks[i].Paragraphs)
	}
	return out, true, nil
}
