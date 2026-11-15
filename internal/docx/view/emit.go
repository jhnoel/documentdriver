package view

import (
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ascii"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// wrapper is a revision wrapper as rendered: its id and author.
type wrapper struct {
	set        bool
	id, author string
}

// wrapperOf returns the revision wrapper for n, unset when n is nil.
func wrapperOf(n *tree.Node) wrapper {
	if n == nil {
		return wrapper{}
	}
	return wrapper{set: true, id: model.AttrOr(n, "id"), author: model.AttrOr(n, "author")}
}

// pending is a run's tracked property change (w:rPrChange) identity.
type pending struct {
	set              bool
	id, author       string
	hasID, hasAuthor bool
}

// emitter streams paragraph content with minimal tag transitions
// (docxdriver's render.rs Emitter): revision wrappers nest outside
// formatting tags, formatting nests span, b, i, u, s, sup or sub and keeps
// the longest common prefix open; milestones go in place.
type emitter struct {
	out         *strings.Builder
	c           *partCtx
	view        View
	inherited   inheritFmt
	ins, del    wrapper
	fmt         fmtState
	pending     pending
	neutralOpen bool
	fieldInstr  map[*tree.Node]string
	fieldDepth  int
	fieldActive string
	// para is the paragraph being emitted and eqIndex its current
	// equations' positions, computed on first use.
	para    *tree.Node
	eqIndex map[*tree.Node]int
	// phIndex are the current paragraph's placeholders by key, 1-based,
	// computed on first use.
	phIndex map[*tree.Node]int
}

// fieldBegin opens a field span for a field beginning at begin, nesting deeper begins.
func (e *emitter) fieldBegin(begin *tree.Node) {
	e.fieldDepth++
	if e.fieldDepth == 1 {
		instr := e.fieldInstr[begin]
		e.fieldActive = instr
		e.finish()
		e.out.WriteString(`<span data-field="` + escapeAttr(instr) + `"` + e.refAttr(begin) + e.c.editable() + `>`)
	}
}

// fieldEnd closes the outermost field span when its field ends.
func (e *emitter) fieldEnd() {
	if e.fieldDepth == 0 {
		return
	}
	e.fieldDepth--
	if e.fieldDepth == 0 {
		e.finish()
		e.out.WriteString("</span>")
	}
}

// fmtStack returns the formatting tag stack for f, outermost first.
func fmtStack(f fmtState) []string {
	var s []string
	if hasInlineCSS(f) {
		s = append(s, "span")
	}
	if f.bold {
		s = append(s, "b")
	}
	if f.italic {
		s = append(s, "i")
	}
	if f.underline {
		s = append(s, "u")
	}
	if f.strike {
		s = append(s, "s")
	}
	if f.vert != baseline {
		s = append(s, f.vert.tag())
	}
	return s
}

// closeFmtTo closes formatting tags until only keep remain open.
func (e *emitter) closeFmtTo(keep int) {
	s := fmtStack(e.fmt)
	for i := len(s) - 1; i >= keep; i-- {
		e.out.WriteString("</" + s[i] + ">")
	}
}

// closeNeutral closes the neutral format-change span when open.
func (e *emitter) closeNeutral() {
	if e.neutralOpen {
		e.out.WriteString("</span>")
		e.neutralOpen = false
	}
}

// writeRevision writes pending p's revision id and author attributes.
func (e *emitter) writeRevision(p pending) {
	if p.hasID {
		e.out.WriteString(` data-revision="` + escapeAttr(p.id) + `"`)
	}
	if p.hasAuthor {
		e.out.WriteString(` data-author="` + escapeAttr(p.author) + `"`)
	}
}

// openNeutral opens a neutral format-change span carrying p's revision attributes.
func (e *emitter) openNeutral(p pending) {
	e.out.WriteString(`<span data-format-change="true"`)
	e.writeRevision(p)
	e.out.WriteByte('>')
	e.neutralOpen = true
}

// transition moves the emitter to ins, del and formatting f for the next content.
func (e *emitter) transition(ins, del wrapper, f fmtState, p pending) {
	f = withoutInheritedCSS(f, e.inherited)
	if e.view != Markup {
		e.transitionFmt(f, pending{})
		return
	}
	if e.ins != ins || e.del != del {
		e.closeFmtTo(0)
		e.closeNeutral()
		e.fmt = fmtState{}
		e.pending = pending{}
		if e.del.set {
			e.out.WriteString("</del>")
		}
		if e.ins != ins {
			if e.ins.set {
				e.out.WriteString("</ins>")
			}
			if ins.set {
				e.out.WriteString(`<ins data-revision="` + escapeAttr(ins.id) + `" data-author="` + escapeAttr(ins.author) + `">`)
			}
			e.ins = ins
		}
		if del.set {
			e.out.WriteString(`<del data-revision="` + escapeAttr(del.id) + `" data-author="` + escapeAttr(del.author) + `">`)
		}
		e.del = del
	}
	e.transitionFmt(f, p)
}

// openFmtTag writes the opening tag for formatting tag with its attributes.
func (e *emitter) openFmtTag(tag string, p pending, outermost bool, f fmtState) {
	e.out.WriteString("<" + tag)
	if tag == "span" {
		if f.highlight != "" {
			e.out.WriteString(` data-highlight="` + f.highlight + `"`)
		}
		e.out.WriteString(` style="` + escapeAttr(inlineCSS(f)) + `"`)
	}
	if outermost && p.set {
		e.out.WriteString(` data-format-change="true"`)
		e.writeRevision(p)
	}
	e.out.WriteByte('>')
}

// transitionFmt adjusts open formatting tags to match f and pending p.
func (e *emitter) transitionFmt(f fmtState, p pending) {
	pendingEq := e.pending == p
	if e.fmt == f && pendingEq {
		return
	}
	target := fmtStack(f)
	if p.set && len(target) == 0 {
		if e.neutralOpen && pendingEq && e.fmt == f {
			return
		}
		e.closeFmtTo(0)
		e.closeNeutral()
		e.openNeutral(p)
		e.fmt, e.pending = f, p
		return
	}
	e.closeNeutral()
	if !pendingEq {
		e.closeFmtTo(0)
		for i, tag := range target {
			e.openFmtTag(tag, p, i == 0, f)
		}
		e.fmt, e.pending = f, p
		return
	}
	current := fmtStack(e.fmt)
	keep := 0
	for keep < len(current) && keep < len(target) && current[keep] == target[keep] {
		keep++
	}
	if e.fmt.highlight != f.highlight || e.fmt.resets != f.resets || e.fmt.color != f.color ||
		e.fmt.hasColor != f.hasColor || e.fmt.size != f.size || e.fmt.hasSize != f.hasSize ||
		e.fmt.font != f.font || e.fmt.hasFont != f.hasFont {
		keep = 0
	}
	e.closeFmtTo(keep)
	for i := keep; i < len(target); i++ {
		e.openFmtTag(target[i], p, i == 0 && keep == 0, f)
	}
	e.fmt = f
}

// text writes s as escaped text under the given wrappers and formatting.
func (e *emitter) text(s string, ins, del wrapper, f fmtState, p pending) {
	e.transition(ins, del, f, p)
	e.out.WriteString(escapeText(s))
}

// tab writes a tab character as a marked span under the given state.
func (e *emitter) tab(ins, del wrapper, f fmtState, p pending) {
	e.transition(ins, del, f, p)
	e.out.WriteString("<span data-tab=\"true\">\t</span>")
}

// keep moves to the wrappers without changing formatting.
func (e *emitter) keep(ins, del wrapper) {
	e.transition(ins, del, e.fmt, e.pending)
}

// br writes a line, page or column break under the given wrappers.
func (e *emitter) br(kind string, ins, del wrapper) {
	e.keep(ins, del)
	switch kind {
	case "page", "column":
		e.out.WriteString(`<br data-break="` + kind + `">`)
	default:
		e.out.WriteString("<br>")
	}
}

// image writes an img element for info, or an unavailable placeholder when its source is missing.
func (e *emitter) image(info imageInfo, n *tree.Node, ins, del wrapper) {
	e.keep(ins, del)
	c := e.c
	ref := e.refAttr(n)
	src, ok := c.images[info.rid]
	if !ok {
		e.out.WriteString(`<span data-image-unavailable="true" data-image-rid="` + escapeAttr(info.rid) + `"` + ref)
		if part, ok := c.imageParts[info.rid]; ok {
			e.out.WriteString(` data-image-part="` + escapeAttr(part) + `"`)
		}
		if info.hasWidth {
			e.out.WriteString(` data-image-width="` + strconv.FormatUint(uint64(info.width), 10) + `"`)
		}
		if info.hasHeight {
			e.out.WriteString(` data-image-height="` + strconv.FormatUint(uint64(info.height), 10) + `"`)
		}
		alt := "[Image]"
		if info.hasAlt {
			e.out.WriteString(` data-image-alt="` + escapeAttr(info.alt) + `"`)
			alt = info.alt
		}
		e.out.WriteString(">" + escapeText(alt) + "</span>")
		return
	}
	e.c.sink.sources[escapeAttr(src)] = true
	e.out.WriteString(`<img src="` + escapeAttr(src) + `"` + ref)
	if part, ok := c.imageParts[info.rid]; ok {
		e.out.WriteString(` data-image-part="` + escapeAttr(part) + `"`)
	}
	if info.hasWidth {
		e.out.WriteString(` width="` + strconv.FormatUint(uint64(info.width), 10) + `"`)
	}
	if info.hasHeight {
		e.out.WriteString(` height="` + strconv.FormatUint(uint64(info.height), 10) + `"`)
	}
	if info.hasAlt {
		e.out.WriteString(` alt="` + escapeAttr(info.alt) + `"`)
	}
	e.out.WriteByte('>')
}

// milestone writes a hidden span marking a comment range start or end.
func (e *emitter) milestone(start bool, id string, ins, del wrapper) {
	e.keep(ins, del)
	attr := "data-comment-end"
	if start {
		attr = "data-comment-start"
	}
	e.out.WriteString(`<span ` + attr + `="` + escapeAttr(id) + `" hidden></span>`)
}

// noteRef writes a superscript footnote or endnote reference with its label.
func (e *emitter) noteRef(n *tree.Node, endnote bool, id string, ins, del wrapper) {
	e.keep(ins, del)
	kind := "footnote"
	if endnote {
		kind = "endnote"
	}
	label := id
	if n, ok := e.c.notes.get(endnote, id); ok {
		label = n.label
	}
	e.c.sink.refs[noteKey{kind, id}] = true
	href := ` href="#` + escapeAttr(e.c.noteDOMID(kind, id)) + `"`
	if e.c.r.agent {
		href = "" // agent markup links a note by data-note-id alone
		e.c.sink.omitted = true
	}
	e.out.WriteString(`<sup data-note="` + kind + `" data-note-id="` + escapeAttr(id) + `"` + e.refAttr(n) + `><a` +
		href + `>` + escapeText(label) + `</a></sup>`)
}

// equation writes an equation as LaTeX in an <equation> element: display
// "block" for m:oMathPara, else "inline", data-ref, its address
// (<paragraph>/equation:N, N its 1-based position among the paragraph's
// current equations, as equation operations take it), when it is one of
// them, and data-loss, what the
// LaTeX leaves out (ContentLosses, separated by "; "), when there is any.
func (e *emitter) equation(eq *tree.Node, latex string, lost []string, ins, del wrapper) {
	e.keep(ins, del)
	display := "inline"
	if model.IsLocal(eq, "oMathPara") {
		display = "block"
	}
	e.out.WriteString(`<equation display="` + display + `"`)
	if ref, ok := e.equationRef(eq); ok {
		e.out.WriteString(` data-ref="` + escapeAttr(ref) + `"`)
	}
	if len(lost) > 0 {
		e.out.WriteString(` data-loss="` + escapeAttr(strings.Join(lost, "; ")) + `"`)
	}
	e.out.WriteString(">" + escapeText(latex) + "</equation>")
}

// equationRef is eq's address: <paragraph>/equation:N, N its 1-based
// position among the paragraph's current equations, or equation:N
// counted across the part where paragraphs have no address (a header's).
func (e *emitter) equationRef(eq *tree.Node) (string, bool) {
	id, ok := e.c.paraIDs[e.para]
	if !ok {
		return e.c.storyRef(eq)
	}
	n, ok := e.equationIndex(eq)
	if !ok {
		return "", false
	}
	return id.ID + "/equation:" + itoa(n), true
}

// placeholderRef is the address of the placeholder whose key is n
// (model.Placeholders): <paragraph>/opaque:N, or opaque:N counted across
// the part where paragraphs have no address.
func (e *emitter) placeholderRef(n *tree.Node) (string, bool) {
	if e.para == nil {
		return "", false
	}
	id, ok := e.c.paraIDs[e.para]
	if !ok {
		return e.c.storyRef(n)
	}
	if e.phIndex == nil {
		e.phIndex = map[*tree.Node]int{}
		list, _ := model.Placeholders(e.para)
		for i, x := range list {
			e.phIndex[x.Key] = i + 1
		}
	}
	i, ok := e.phIndex[n]
	if !ok {
		return "", false
	}
	return id.ID + "/opaque:" + itoa(i), true
}

// refAttr is the data-ref attribute of the placeholder whose key is n,
// or "" when it has no address.
func (e *emitter) refAttr(n *tree.Node) string {
	if ref, ok := e.placeholderRef(n); ok {
		return ` data-ref="` + escapeAttr(ref) + `"`
	}
	return ""
}

// opaque writes an empty placeholder for content the HTML cannot show
// (docs/contract.md, section 7): its address and kind.
func (e *emitter) opaque(n *tree.Node, kind string, ins, del wrapper) {
	e.keep(ins, del)
	e.out.WriteString(`<opaque` + e.refAttr(n) + ` data-kind="` + kind + `"></opaque>`)
}

// wrapped writes placeholder n, a wrapper whose content the HTML shows
// but cannot edit (a content control, a ruby, alternate content, an
// unknown element), as a span carrying its address and kind around its
// content.
func (e *emitter) wrapped(n, content *tree.Node, kind string, ins, del *tree.Node) {
	e.finish()
	e.out.WriteString(`<span` + e.refAttr(n) + ` data-kind="` + kind + `">`)
	if content != nil {
		e.content(content, ins, del)
	}
	e.finish()
	e.out.WriteString("</span>")
}

// equationIndex is eq's 1-based position among the current equations of
// the paragraph being emitted.
func (e *emitter) equationIndex(eq *tree.Node) (int, bool) {
	if e.para == nil {
		return 0, false
	}
	if e.eqIndex == nil {
		e.eqIndex = map[*tree.Node]int{}
		for i, n := range model.CurrentEquations(e.para) {
			e.eqIndex[n] = i + 1
		}
	}
	n, ok := e.eqIndex[eq]
	return n, ok
}

// finish closes all open formatting, neutral and revision wrappers.
func (e *emitter) finish() {
	e.closeFmtTo(0)
	e.closeNeutral()
	e.fmt = fmtState{}
	e.pending = pending{}
	if e.view == Markup {
		if e.del.set {
			e.out.WriteString("</del>")
			e.del = wrapper{}
		}
		if e.ins.set {
			e.out.WriteString("</ins>")
			e.ins = wrapper{}
		}
	}
}

// hidden reports whether content under the given wrappers is hidden in
// the view: deleted content from the final, inserted from the original.
func (e *emitter) hidden(ins, del *tree.Node) bool {
	switch e.view {
	case Final:
		return del != nil
	case Original:
		return ins != nil
	}
	return false
}

// content emits a paragraph's (or wrapper's) children. It recurses only
// over a tree the tokenizer built.
func (e *emitter) content(n, ins, del *tree.Node) {
	c := e.c
	for ch := range n.ChildrenSeq() {
		switch {
		case !model.IsElement(ch), model.IsW(ch, "pPr"), model.IsW(ch, "p"):
		case model.IsLocal(ch, "AlternateContent"):
			e.wrapped(ch, model.AlternateContentBranch(ch), "alternate-content", ins, del)
		case model.IsW(ch, "sdt"):
			e.wrapped(ch, model.ChildW(ch, "sdtContent"), "content-control", ins, del)
		case model.IsW(ch, "ruby"):
			e.wrapped(ch, ch, "ruby", ins, del)
		case model.IsRevIns(ch):
			e.content(ch, ch, del)
		case model.IsRevDel(ch):
			e.content(ch, ins, ch)
		case model.IsW(ch, "hyperlink"):
			href := c.links[ch]
			e.finish()
			if safeURL(href) {
				e.out.WriteString(`<a href="` + escapeAttr(href) + `">`)
			} else {
				e.out.WriteString(`<a href="#" data-href="` + escapeAttr(href) + `">`)
			}
			e.content(ch, ins, del)
			e.finish()
			e.out.WriteString("</a>")
		case model.IsW(ch, "fldSimple"):
			e.fieldDepth++
			if e.fieldDepth == 1 {
				instr := rustTrim(model.AttrOr(ch, "instr"))
				e.finish()
				e.out.WriteString(`<span data-field="` + escapeAttr(instr) + `"` + e.refAttr(ch) + e.c.editable() + `>`)
			}
			e.content(ch, ins, del)
			if e.fieldDepth == 1 {
				e.finish()
				e.out.WriteString("</span>")
			}
			e.fieldDepth--
		case model.IsW(ch, "commentRangeStart"), model.IsW(ch, "commentRangeEnd"):
			if e.view == Markup {
				e.milestone(model.IsW(ch, "commentRangeStart"), model.AttrOr(ch, "id"), wrapperOf(ins), wrapperOf(del))
			}
		case model.IsW(ch, "r"):
			e.run(ch, ins, del)
		case model.IsLocal(ch, "oMath"), model.IsLocal(ch, "oMathPara"):
			latex, lost := c.r.latex(ch)
			if !e.hidden(ins, del) {
				e.equation(ch, latex, lost, wrapperOf(ins), wrapperOf(del))
			}
		case model.Classify(ch) == model.KindProps:
			// Properties describe their parent (w:smartTagPr holds
			// w:attr elements, never content).
		case model.Classify(ch) == model.KindUnknown:
			e.wrapped(ch, ch, "unknown", ins, del)
		default:
			e.content(ch, ins, del)
		}
	}
}

// runPending is the run's w:rPr/w:rPrChange identity, if it has one.
func runPending(run *tree.Node) pending {
	rpr := model.ChildW(run, "rPr")
	if rpr == nil {
		return pending{}
	}
	ch := model.ChildW(rpr, "rPrChange")
	if ch == nil {
		return pending{}
	}
	p := pending{set: true}
	if v, ok := model.Attr(ch, "id"); ok && v != "" {
		p.id, p.hasID = v, true
	}
	if v, ok := model.Attr(ch, "author"); ok && v != "" {
		p.author, p.hasAuthor = v, true
	}
	return p
}

// run emits run's children with its formatting and pending revision identity.
func (e *emitter) run(run, ins, del *tree.Node) {
	f := e.c.r.styles.runFormat(run, e.view == Original)
	var p pending
	if e.view == Markup {
		p = runPending(run)
	}
	e.runChildren(run, ins, del, wrapperOf(ins), wrapperOf(del), f, p)
}

// runChildren emits a run's children, skipping content hidden in the view.
func (e *emitter) runChildren(n, ins, del *tree.Node, iw, dw wrapper, f fmtState, p pending) {
	for ch := range n.ChildrenSeq() {
		switch {
		case !model.IsElement(ch):
		case isImageContainer(ch):
			if !e.hidden(ins, del) {
				if info, ok := pictureInfo(ch); ok {
					e.image(info, ch, iw, dw)
				} else {
					kind := "drawing"
					if model.IsW(ch, "object") {
						kind = "object"
					}
					e.opaque(ch, kind, iw, dw)
				}
			}
		case model.IsW(ch, "t"):
			if !e.hidden(ins, del) {
				e.text(model.TextContent(ch), iw, dw, f, p)
			}
		case model.IsW(ch, "delText"):
			if e.view == Markup || e.view == Original && ins == nil {
				e.text(model.TextContent(ch), iw, dw, f, p)
			}
		case model.IsW(ch, "br"):
			if !e.hidden(ins, del) {
				e.br(model.AttrOr(ch, "type"), iw, dw)
			}
		case model.IsW(ch, "tab"):
			if !e.hidden(ins, del) {
				e.tab(iw, dw, f, p)
			}
		case model.IsW(ch, "ptab"):
			// An absolute position tab is one tab character in the
			// selectable text; render it exactly as w:tab does.
			if !e.hidden(ins, del) {
				e.tab(iw, dw, f, p)
			}
		case model.IsW(ch, "noBreakHyphen"):
			// The selectable text counts a non-breaking hyphen as U+2011;
			// write that character in the markup so markup and text agree.
			if !e.hidden(ins, del) {
				e.text("\u2011", iw, dw, f, p)
			}
		case model.IsW(ch, "footnoteReference"), model.IsW(ch, "endnoteReference"):
			if !e.hidden(ins, del) {
				e.noteRef(ch, model.IsW(ch, "endnoteReference"), model.AttrOr(ch, "id"), iw, dw)
			}
		case model.Classify(ch) == model.KindObject && !model.IsLocal(ch, "oMath") && !model.IsLocal(ch, "oMathPara"):
			if !e.hidden(ins, del) {
				e.opaque(ch, model.ObjectKind(ch), iw, dw)
			}
		case model.Classify(ch) == model.KindUnknown:
			if !e.hidden(ins, del) {
				e.opaque(ch, "unknown", iw, dw)
			}
		case model.IsW(ch, "fldChar"):
			if !e.hidden(ins, del) {
				switch model.AttrOr(ch, "fldCharType") {
				case "begin":
					e.fieldBegin(ch)
				case "end":
					e.fieldEnd()
				}
			}
		}
	}
}

// safeURL reports whether an href may be rendered as a link: no control
// characters, and no scheme or one of https, http, mailto and tel.
func safeURL(v string) bool {
	v = ascii.Lower(rustTrim(v))
	if strings.ContainsFunc(v, isControl) {
		return false
	}
	if !strings.Contains(v, ":") {
		return true
	}
	for _, s := range []string{"https:", "http:", "mailto:", "tel:"} {
		if strings.HasPrefix(v, s) {
			return true
		}
	}
	return false
}
