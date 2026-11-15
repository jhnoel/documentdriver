package view

import (
	"slices"
	"strings"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// The note parts, at docxdriver's fixed names.
const (
	footnotesPart = "word/footnotes.xml"
	endnotesPart  = "word/endnotes.xml"
)

// sectionMeta is the w:sectPr child attributes a section element carries,
// in docxdriver's order.
var sectionMeta = []struct{ child, attr, key string }{
	{"pgSz", "w", "page-width-twips"}, {"pgSz", "h", "page-height-twips"},
	{"pgSz", "orient", "orientation"}, {"cols", "num", "columns"},
	{"cols", "space", "column-spacing-twips"}, {"cols", "sep", "column-separator"},
	{"type", "val", "break-type"}, {"pgNumType", "fmt", "page-number-format"},
	{"pgNumType", "start", "page-number-start"},
}

// renderParts renders the package-wide surface (docxdriver's surface.rs
// render_parts): per section its marker, explicit headers and footers and
// body blocks, then the footnote and endnote bodies the surface links to.
// ids are the main document's paragraph addresses. A filter, when not
// nil, limits what is emitted (renderFilter).
func (r *renderer) renderParts(ids map[*tree.Node]model.ParaID, f *renderFilter) *Rendered {
	x := r.main
	if r.story == nil {
		r.story = r.storyParaIDs(ids)
	}
	notes := r.loadNotes(f == nil)
	r.assignNoteLabels(notes)
	ctx := r.newPartCtx(x, r.pkg.DocumentPartName())
	ctx.notes = notes
	ctx.paraIDs = ids
	r.docSink = ctx.sink
	out := &Rendered{}
	if r.agent {
		out.StyleIDs, out.Sources = ctx.sink.classes, ctx.sink.sources
	}
	b := x.Body()
	if b == nil {
		return out
	}
	sections := partitionSections(b)
	sectNodes := SectPrNodes(x)
	chrome := ChromeMap(r.pkg, x)
	var html, bodyHTML strings.Builder
	tableN := 1
	mainPart := r.pkg.DocumentPartName()
	for si, blocks := range sections {
		n := si + 1
		var cs *ChromeSection
		for i := range chrome {
			if chrome[i].N == n {
				cs = &chrome[i]
				break
			}
		}
		var sect *tree.Node
		if si < len(sectNodes) {
			sect = sectNodes[si]
			if r.view == Original {
				if ch := model.ChildW(sect, "sectPrChange"); ch != nil {
					if old := model.ChildW(ch, "sectPr"); old != nil {
						sect = old
					}
				}
			}
		}
		titlePage := cs != nil && cs.TitlePage
		if sect != nil {
			titlePage = onOff(model.ChildW(sect, "titlePg"))
		}
		if html.Len() > 0 {
			html.WriteByte('\n')
		}
		start := html.Len()
		html.WriteString(`<section data-section="` + itoa(n) + `"`)
		if titlePage {
			html.WriteString(` data-first-page="true"`)
		}
		if sect != nil {
			for _, m := range sectionMeta {
				if c := model.ChildW(sect, m.child); c != nil {
					if v, ok := model.Attr(c, m.attr); ok {
						v = strings.NewReplacer("&", "&amp;", `"`, "&quot;", "<", "&lt;").Replace(v)
						html.WriteString(" data-" + m.key + `="` + v + `"`)
					}
				}
			}
		}
		html.WriteByte('>')
		out.Regions = append(out.Regions, Region{Kind: RegionSection, Start: start, End: html.Len(), Section: n})
		if cs != nil {
			for _, scope := range []string{"hdr", "ftr"} {
				for _, kind := range slotKinds {
					for _, slot := range cs.Slots {
						if slot.Explicit && slot.Scope == scope && slot.Kind == kind {
							if f == nil || f.chrome[slot.Part] {
								r.emitChromeSlot(slot, n, &html, out)
							}
							break
						}
					}
				}
			}
		}
		if len(blocks) == 0 {
			html.WriteString("</section>")
			continue
		}
		var keep blockFilter
		if f != nil {
			keep = f.blocks
			if keep == nil {
				keep = blockFilter{}
			}
		}
		rendered := ctx.renderFiltered(blocks, &tableN, keep)
		for _, blk := range rendered.blocks {
			html.WriteByte('\n')
			start := html.Len()
			text := rendered.html[blk.Start:blk.End]
			html.WriteString(text)
			out.Regions = append(out.Regions, Region{Kind: RegionBody, Start: start, End: html.Len(), Part: mainPart, Section: n, Paragraphs: blk.Paragraphs})
			if bodyHTML.Len() > 0 {
				bodyHTML.WriteByte('\n')
			}
			bs := bodyHTML.Len()
			bodyHTML.WriteString(text)
			out.Blocks = append(out.Blocks, Block{Start: bs, End: bodyHTML.Len(), Paragraphs: blk.Paragraphs})
		}
		html.WriteString("\n</section>")
	}
	for _, k := range []struct {
		kind  string
		notes map[string]*noteBody
	}{{"footnote", notes.footnotes}, {"endnote", notes.endnotes}} {
		if f != nil {
			break // a filtered render emits no notes
		}
		var ids []string
		if r.agent {
			// The full markup's links, which agent markup leaves out.
			for id := range k.notes {
				if ctx.sink.refs[noteKey{k.kind, id}] {
					ids = append(ids, id)
				}
			}
		} else {
			doc := html.String()
			for id := range k.notes {
				if strings.Contains(doc, `href="#`+ctx.noteDOMID(k.kind, id)+`"`) {
					ids = append(ids, id)
				}
			}
		}
		slices.SortFunc(ids, func(a, b string) int {
			na, nb := noteOrder(a), noteOrder(b)
			if na != nb {
				if na < nb {
					return -1
				}
				return 1
			}
			return strings.Compare(a, b)
		})
		if len(ids) == 0 {
			continue
		}
		html.WriteString("\n<aside data-notes=\"" + k.kind + "\">")
		for _, id := range ids {
			if r.agent {
				ctx.sink.add(k.notes[id].sink)
				html.WriteString(`<div data-note-body="` + k.kind + `" data-note-id="` + escapeAttr(id) + `">` + k.notes[id].html + "</div>")
				continue
			}
			html.WriteString(`<div id="` + escapeAttr(ctx.noteDOMID(k.kind, id)) + `" data-note-body="` + k.kind +
				`" data-note-id="` + escapeAttr(id) + `">` + k.notes[id].html + "</div>")
		}
		html.WriteString("</aside>")
	}
	out.HTML = html.String()
	out.Body = bodyHTML.String()
	return out
}

// renderFilter limits a render to what AffectedMarkup reports: the
// selected top-level body blocks and header and footer parts. Everything
// a skipped block or part would add to document-wide state is still
// counted (blockFilter); the note bodies are not emitted.
type renderFilter struct {
	blocks blockFilter
	chrome map[string]bool
}

// noteOrder is a note id's sort key: its u32 value, 0 when it has none.
func noteOrder(id string) uint64 {
	v, ok := model.ParseU32(id)
	if !ok {
		return 0
	}
	return uint64(v)
}

// partitionSections splits the body's effective blocks at section
// boundaries (a w:sectPr a block carries, or a body-level one).
func partitionSections(b *tree.Node) [][]*tree.Node {
	var sections [][]*tree.Node
	var cur []*tree.Node
	for _, c := range effectiveStoryChildren(b, "p", "tbl", "sectPr") {
		if model.IsW(c, "sectPr") {
			sections = append(sections, cur)
			cur = nil
			continue
		}
		cur = append(cur, c)
		if model.FindSectPr(c) != nil {
			sections = append(sections, cur)
			cur = nil
		}
	}
	if len(cur) > 0 || len(sections) == 0 {
		sections = append(sections, cur)
	}
	if len(sections) > 1 && len(sections[len(sections)-1]) == 0 {
		sections = sections[:len(sections)-1]
	}
	return sections
}

// emitChromeSlot writes slot's header or footer element and its region into out.
func (r *renderer) emitChromeSlot(slot ChromeSlot, n int, html *strings.Builder, out *Rendered) {
	tag, kind := "header", RegionHeader
	if slot.Scope != "hdr" {
		tag, kind = "footer", RegionFooter
	}
	kindAttr := ""
	if slot.Kind == "first" || slot.Kind == "even" {
		kindAttr = ` data-kind="` + slot.Kind + `"`
	}
	inner, paras, blank, s := r.renderChromeInner(slot.Part)
	if !blank && r.agent {
		r.docSink.add(s)
	}
	if html.Len() > 0 {
		html.WriteByte('\n')
	}
	start := html.Len()
	if blank {
		html.WriteString("<" + tag + kindAttr + "></" + tag + ">")
	} else {
		html.WriteString("<" + tag + kindAttr + ">\n" + inner + "\n</" + tag + ">")
	}
	out.Regions = append(out.Regions, Region{Kind: kind, Start: start, End: html.Len(), Part: slot.Part, Section: n, Paragraphs: paras})
}

// renderChromeInner renders a header or footer part, reporting whether
// it is blank and what its markup holds (sink).
func (r *renderer) renderChromeInner(part string) (string, []*tree.Node, bool, *sink) {
	x, ok := r.part(part)
	if !ok || x.Root() == nil {
		return "", nil, true, nil
	}
	ctx := r.newPartCtx(x, part)
	ctx.paraIDs = r.story
	rendered := ctx.renderBlocks(x.Root())
	var paras []*tree.Node
	for _, b := range rendered.blocks {
		paras = append(paras, b.Paragraphs...)
	}
	t := rustTrim(rendered.html)
	// Agent markup that left something out is not the full markup's
	// blank part, whatever it reads.
	if (t == "" || t == "<p></p>" || t == "<p/>") && !ctx.sink.omitted {
		return "", paras, true, ctx.sink
	}
	return rendered.html, paras, false, ctx.sink
}

// noteBody is a footnote or endnote body and the label its references
// show.
type noteBody struct {
	html, label string
	// sink is what the body's agent markup holds (agent mode).
	sink *sink
}

// noteBodies are the footnote and endnote bodies by note id.
type noteBodies struct {
	footnotes, endnotes map[string]*noteBody
}

// get returns the note body for id, choosing footnotes or endnotes by endnote.
func (n *noteBodies) get(endnote bool, id string) (*noteBody, bool) {
	m := n.footnotes
	if endnote {
		m = n.endnotes
	}
	b, ok := m[id]
	return b, ok
}

// loadNotes lists the regular notes (those without a w:type) of the note
// parts and, when bodies is set, renders their bodies in the view.
func (r *renderer) loadNotes(bodies bool) *noteBodies {
	return &noteBodies{
		footnotes: r.loadNotePart(footnotesPart, "footnote", bodies),
		endnotes:  r.loadNotePart(endnotesPart, "endnote", bodies),
	}
}

// loadNotePart returns regular notes of part by id, rendering bodies when bodies is set.
func (r *renderer) loadNotePart(part, tag string, bodies bool) map[string]*noteBody {
	out := map[string]*noteBody{}
	x, ok := r.part(part)
	if !ok || x.Root() == nil {
		return out
	}
	var ctx *partCtx
	if bodies {
		ctx = r.newPartCtx(x, part)
		ctx.paraIDs = r.story
	}
	for note := range x.Root().ChildrenSeq() {
		if !model.IsW(note, tag) {
			continue
		}
		if _, typed := model.Attr(note, "type"); typed {
			continue
		}
		id := model.AttrOr(note, "id")
		if id == "" {
			continue
		}
		b := &noteBody{label: id}
		if bodies {
			ctx.sink = newSink()
			b.html = ctx.renderBlocks(note).html
			b.sink = ctx.sink
		}
		out[id] = b
	}
	return out
}

// assignNoteLabels numbers the notes in the order the main document
// references them, each once, skipping references the view hides.
func (r *renderer) assignNoteLabels(notes *noteBodies) {
	foot, end := 0, 0
	type key struct {
		endnote bool
		id      string
	}
	seen := map[key]bool{}
	r.main.Doc.Node().Walk(func(n *tree.Node) bool {
		endnote := model.IsW(n, "endnoteReference")
		if !endnote && !model.IsW(n, "footnoteReference") {
			return true
		}
		for p := n.Parent(); p != nil; p = p.Parent() {
			if r.view == Final && model.IsRevDel(p) || r.view == Original && model.IsRevIns(p) {
				return true
			}
		}
		id := model.AttrOr(n, "id")
		if seen[key{endnote, id}] {
			return true
		}
		seen[key{endnote, id}] = true
		counter, m := &foot, notes.footnotes
		if endnote {
			counter, m = &end, notes.endnotes
		}
		*counter++
		if b, ok := m[id]; ok {
			b.label = itoa(*counter)
		}
		return true
	})
}
