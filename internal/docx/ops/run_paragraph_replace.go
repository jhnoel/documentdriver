package ops

import (
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/docx/build"
	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/html"
	"github.com/jhnoel/documentdriver/internal/docx/math"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// ReplaceParagraph is paragraph.replace (docxdriver's replace_paragraph).
type ReplaceParagraph struct {
	At     ParagraphAddress `json:"at"`
	HTML   string           `json:"html"`
	Delete []string         "json:\"delete\" default:\"[]\" doc:\"Addresses of the equations and placeholders of the paragraph that `html` leaves out on purpose.\""
}

// opParagraphReplace is paragraph.replace's entry in the operation table.
var opParagraphReplace = OpDef{
	Name: "paragraph.replace", Oracle: "replace_paragraph", New: func() Spec { return &ReplaceParagraph{} },
	Moved: "`html` ← `with`",
	Rules: replaceParagraphRules, DirectGuards: textGuard,
	TrackGuards: []string{"moving an equation or placeholder to another place in the paragraph",
		"deleting a content control, ruby, alternate content or unknown element"},
}

// kept is one existing piece of the paragraph the html sends back: an
// equation or a placeholder, by address, with the nodes that are it.
type kept struct {
	ref   string
	nodes []*tree.Node
	// inRun: nodes[0] is an object inside a run, isolated into a run of
	// its own before anything else changes.
	inRun bool
}

// Run replaces the current content of a paragraph of the main document,
// keeping its properties, its paragraph mark and its id (typed.rs
// replace_paragraph_typed). The html is the paragraph's content as read
// shows it: equations and placeholders it sends back by their data-ref
// stay (byte for byte), what delete lists is deleted, and anything else
// it leaves out is refused (docs/contract.md, section 7). Bare inline
// content takes the direct run formatting every run of the paragraph
// shares, in both modes (docxdriver gives it in track mode only, W4);
// a <p> or heading sets the paragraph's style and CSS.
//
// Old content goes as the author's deletions and new content in as the
// author's insertions, each new segment beside the kept piece after it,
// so rejecting gives the input back; direct mode accepts exactly those.
// Kept pieces moved out of their order can only be made directly.
// Paragraphs holding others' tracked changes or comment ranges are
// refused, as docxdriver refuses them.
func (o *ReplaceParagraph) Run(env *Env) error {
	part, _, err := env.Main()
	if err != nil {
		return err
	}
	p, id, err := env.paragraph(o.At)
	if err != nil {
		return err
	}
	if label := replaceBlocker(p, env.Author); label != "" {
		return reject("operation_failed", "at", "the paragraph contains a %s that cannot be safely preserved — paragraph.replace requires unambiguous content; accept, resolve, or remove it first", label)
	}
	list, blocked := model.Placeholders(p)
	if blocked != "" {
		return reject("operation_failed", "at", "the paragraph contains a %s that is not one placeholder (it does not begin and end within the paragraph's content) — paragraph.replace cannot keep it whole", blocked)
	}
	// The target is checked first: read's markup of a paragraph this
	// op refuses (revision tags, comment markers) is refused for the
	// paragraph, not for the markup.
	c, err := parseReplacementHTML(o.HTML)
	if err != nil {
		return reject("invalid_operation", "html", "%s", err.Error())
	}

	// What the paragraph holds, by address, and what the html sends back.
	var have []string
	pieces := map[string]kept{}
	equations := model.CurrentEquations(p)
	for i, eq := range equations {
		ref := id + "/equation:" + strconv.Itoa(i+1)
		have = append(have, ref)
		pieces[ref] = kept{ref: ref, nodes: []*tree.Node{eq}}
	}
	phs := map[string]model.Placeholder{}
	for i, ph := range list {
		ref := id + "/opaque:" + strconv.Itoa(i+1)
		have = append(have, ref)
		phs[ref] = ph
		pieces[ref] = kept{ref: ref, nodes: ph.Nodes, inRun: ph.InRun}
	}
	var sent []string
	seen := map[string]bool{}
	for _, a := range c.Atoms {
		if !isKept(a) {
			continue
		}
		if _, ok := pieces[a.Ref]; !ok {
			return reject("not_found", "html", "data-ref %s names nothing in paragraph %s", a.Ref, id)
		}
		if seen[a.Ref] {
			return reject("invalid_operation", "html", "%s appears twice in html: existing content is moved, never copied", a.Ref)
		}
		seen[a.Ref] = true
		switch a.Kind {
		case html.AtomEquation:
			latex, _, _ := math.OMMLToLaTeX(pieces[a.Ref].nodes[0])
			if !build.SameLaTeX(latex, a.Text, a.Display) {
				return reject("invalid_operation", "html", "equation %s: its LaTeX differs from the document's; change an equation with equation.replace, or leave out data-ref to write a new one", a.Ref)
			}
		case html.AtomOpaque:
			ph := phs[a.Ref]
			if want := model.PlaceholderText(ph); a.Text != want && !build.Textless(ph.Kind) {
				return reject("invalid_operation", "html", "placeholder %s: its text differs from the document's; the content of a %s is not editable through html", a.Ref, ph.Kind)
			}
		}
		sent = append(sent, a.Ref)
	}
	if err := accountFor(have, sent, o.Delete, "html"); err != nil {
		return err
	}
	ordered := inDocumentOrder(p, sent, pieces)
	if !ordered && env.Mode == ModeTrack {
		return reject("refused_in_mode", "html", "the html moves an equation or placeholder to another place in the paragraph; a move cannot be tracked here: keep their order, or use direct mode")
	}
	for _, ref := range sent {
		if k := pieces[ref]; !ordered && k.nodes[0].Parent() != p && !k.inRun || !ordered && k.inRun && k.nodes[0].Parent().Parent() != p {
			return reject("operation_failed", "html", "%s sits inside a hyperlink or another wrapper and cannot be moved out of its place", ref)
		}
	}
	if env.Mode == ModeTrack {
		for _, d := range o.Delete {
			if ph, ok := phs[canonicalContentRef(d)]; ok && !trackDeletable(ph) {
				return reject("operation_failed", "delete", "%s is a %s, which cannot be deleted as a tracked change; deleting a content control, ruby, alternate content or unknown element is direct only", d, ph.Kind)
			}
		}
	}

	// New paragraph properties, checked before anything changes.
	var styles *model.XML
	if sx, ok := env.part(env.stylesPart()); ok {
		styles = sx
	}
	style := ""
	if c.Attrs != nil {
		style = build.StyleOf(*c.Attrs)
		if style != "" && !paragraphStyleDeclared(styles, style) {
			return reject("not_found", "html", "style %q (from <%s>) is not a declared paragraph style of the document", style, c.Attrs.Tag)
		}
		if c.Attrs.CSS.OK {
			if _, err := html.ParagraphProperties(c.Attrs.CSS.V); err != nil {
				return reject("invalid_operation", "html", "%s", err.Error())
			}
		}
	}
	atoms := c.Atoms
	if c.Attrs == nil {
		atoms = build.WithBase(atoms, build.SharedFormat(p))
	}
	eqTemplate := ""
	if len(equations) > 0 {
		eqTemplate = math.RunProperties(equations[0])
	}

	h, err := env.WS.Core().Range(part, "paragraph.replace at "+id, p, p)
	if err != nil {
		return err
	}
	keepNodes := map[*tree.Node]bool{}
	var keepList []kept
	for _, ref := range sent {
		k := pieces[ref]
		if k.inRun {
			r, err := isolateObject(h, k.nodes[0])
			if err != nil {
				return err
			}
			k.nodes = []*tree.Node{r}
			pieces[ref] = k
		}
		for _, n := range k.nodes {
			keepNodes[n] = true
		}
		keepList = append(keepList, k)
	}
	tail := contentEnd(p)
	if err := deleteLive(env, h, p, keepNodes); err != nil {
		return err
	}

	opt := build.Options{Link: env.hyperlink, EquationRunProperties: eqTemplate, ComplexFields: env.Mode == ModeTrack}
	place := func(container, before *tree.Node, seg []html.Atom) error {
		if len(seg) == 0 {
			return nil
		}
		content, err := build.Inline(h, container, before, seg, opt)
		if err != nil {
			return buildRejection(err, "html")
		}
		for _, g := range content.Groups {
			if _, err := env.Rev.Insert(h, g); err != nil {
				return err
			}
		}
		return nil
	}
	segs := splitAtKept(atoms)
	if ordered {
		// Each new segment goes just before the kept piece after it, the
		// last after the paragraph's old content.
		for i, k := range keepList {
			container, before := p, topChild(p, k.nodes[0])
			if i > 0 && topChild(p, keepList[i-1].nodes[0]) == before && before != k.nodes[0] {
				container, before = k.nodes[0].Parent(), k.nodes[0]
			}
			if err := place(container, before, segs[i]); err != nil {
				return err
			}
		}
		if err := place(p, tail, segs[len(keepList)]); err != nil {
			return err
		}
	} else {
		// Direct mode only: the kept pieces move after the old content in
		// the html's order, with the new segments between them.
		for i, ref := range sent {
			if err := place(p, tail, segs[i]); err != nil {
				return err
			}
			for _, n := range pieces[ref].nodes {
				var err error
				if tail != nil {
					err = h.PutBefore(n, tail)
				} else {
					err = h.Append(p, n)
				}
				if err != nil {
					return err
				}
			}
		}
		if err := place(p, tail, segs[len(sent)]); err != nil {
			return err
		}
	}
	if c.Attrs != nil {
		if err := replaceParagraphProperties(env, h, p, *c.Attrs, style); err != nil {
			return err
		}
	}
	for _, d := range o.Delete {
		env.Deleted = append(env.Deleted, canonicalContentRef(d))
	}
	return nil
}

// isKept reports whether atom a sends back existing content.
func isKept(a html.Atom) bool {
	return a.Kind == html.AtomOpaque || a.Kind == html.AtomEquation && a.AtomObject != nil && a.Ref != ""
}

// trackDeletable reports whether a placeholder can be deleted as a
// tracked change: its runs go into w:del (an object of a run, a complex
// field) or are deleted inside it (a simple field).
func trackDeletable(ph model.Placeholder) bool {
	return ph.InRun || ph.Kind == "field"
}

// replaceBlocker is docxdriver's paragraph_blocker, less what the
// placeholders now keep and the author's own pending insertions: a
// comment range or reference, or another author's tracked change in the
// content (paragraph-mark revisions concern the break and
// stay).
func replaceBlocker(p *tree.Node, author string) string {
	label := ""
	var walk func(c *tree.Node, depth int)
	walk = func(c *tree.Node, depth int) {
		if depth > tree.MaxDepth || label != "" {
			return
		}
		for _, n := range c.Elements() {
			switch {
			case model.IsW(n, "pPr"), model.IsW(n, "p"):
			case model.IsW(n, "commentRangeStart"), model.IsW(n, "commentRangeEnd"), model.IsW(n, "commentReference"):
				label = "comment"
			case model.IsW(n, "ins") && model.AttrOr(n, "author") == author:
				// The author's own pending insertion is edited, not
				// revised (Word's rule; the Reviser removes what it
				// deletes there).
				walk(n, depth+1)
			case model.IsRevIns(n), model.IsRevDel(n), model.IsW(n, "rPrChange"):
				label = "tracked change"
			default:
				walk(n, depth+1)
			}
			if label != "" {
				return
			}
		}
	}
	walk(p, 0)
	return label
}

// inDocumentOrder reports whether sent lists its addresses in the order
// their content has in the paragraph. Equations and placeholders are
// numbered separately, so the order is the nodes'.
func inDocumentOrder(p *tree.Node, sent []string, pieces map[string]kept) bool {
	pos := map[*tree.Node]int{}
	i := 0
	p.Walk(func(n *tree.Node) bool {
		pos[n] = i
		i++
		return true
	})
	last := -1
	for _, ref := range sent {
		at := pos[pieces[ref].nodes[0]]
		if at < last {
			return false
		}
		last = at
	}
	return true
}

// splitAtKept splits atoms at the kept pieces: n kept pieces give n+1
// segments of new content.
func splitAtKept(atoms []html.Atom) [][]html.Atom {
	out := [][]html.Atom{nil}
	for _, a := range atoms {
		if isKept(a) {
			out = append(out, nil)
			continue
		}
		out[len(out)-1] = append(out[len(out)-1], a)
	}
	return out
}

// topChild is the child of p that holds n.
func topChild(p, n *tree.Node) *tree.Node {
	for n != nil && n.Parent() != p {
		n = n.Parent()
	}
	return n
}

// contentEnd is where content added after a paragraph's old content goes:
// the element after its last content child (a run, a wrapper, an
// equation: not properties or a marker), nil for its end; for a
// paragraph without content, its first child after w:pPr (typed.rs
// content_insert_point).
func contentEnd(p *tree.Node) *tree.Node {
	var last *tree.Node
	for _, ch := range p.Elements() {
		switch model.Classify(ch) {
		case model.KindProps, model.KindMarker:
		default:
			if !model.IsW(ch, "p") {
				last = ch
			}
		}
	}
	if last == nil {
		for _, ch := range p.Elements() {
			if !model.IsW(ch, "pPr") {
				return ch
			}
		}
		return nil
	}
	return model.NextElement(last)
}

// isolateObject splits the run around obj, an object inside it, so that
// obj stands in a run of its own (the run's properties copied to each
// half), and returns that run.
func isolateObject(h *core.Handle, obj *tree.Node) (*tree.Node, error) {
	r := obj.Parent()
	isProps := func(n *tree.Node) bool { return model.IsW(n, "rPr") }
	idx := func(r *tree.Node) int {
		for i, ch := range r.Children() {
			if ch == obj {
				return i
			}
		}
		return -1
	}
	before := false
	for _, ch := range r.Children() {
		if ch == obj {
			break
		}
		if model.IsElement(ch) && !isProps(ch) {
			before = true
		}
	}
	if before {
		right, err := h.SplitAt(r, idx(r), isProps)
		if err != nil {
			return nil, err
		}
		r = right
	}
	after := false
	seen := false
	for _, ch := range r.Children() {
		if ch == obj {
			seen = true
			continue
		}
		if seen && model.IsElement(ch) {
			after = true
		}
	}
	if after {
		if _, err := h.SplitAt(r, idx(r)+1, isProps); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// deleteLive records the deletion of the live content of container c
// (the paragraph, or a wrapper in it) but the kept nodes: runs and
// equations go into the author's w:del; hyperlinks, fields, smart tags,
// custom XML and bidi wrappers are descended into; a content control,
// ruby, alternate content or unknown element (a placeholder delete
// lists) is removed outright, which track mode refused beforehand.
// Markers and properties stay.
func deleteLive(env *Env, h *core.Handle, c *tree.Node, keep map[*tree.Node]bool) error {
	for _, n := range c.Elements() {
		switch {
		case keep[n], model.IsW(n, "pPr"), model.IsW(n, "p"):
		case model.IsW(n, "r") || model.IsLocal(n, "oMath") || model.IsLocal(n, "oMathPara"):
			if onlyProps(n) {
				continue
			}
			if err := env.Rev.Delete(h, []*tree.Node{n}); err != nil {
				return err
			}
		case model.IsW(n, "hyperlink"), model.IsW(n, "fldSimple"), model.IsW(n, "smartTag"), model.IsW(n, "customXml"),
			model.IsW(n, "dir"), model.IsW(n, "bdo"):
			if env.Mode == ModeDirect && !holdsAny(n, keep) {
				// Directly, the wrapper goes with its content; its markers
				// (a bookmark inside a link) stay, in its place.
				for _, m := range n.Elements() {
					if model.Classify(m) == model.KindMarker {
						if err := h.PutBefore(m, n); err != nil {
							return err
						}
					}
				}
				if err := h.RemoveNode(n); err != nil {
					return err
				}
				continue
			}
			if model.IsW(n, "fldSimple") {
				// Tracked, a deleted simple field is its complex field
				// runs (a w:del holds runs).
				if err := expandSimpleField(h, n); err != nil {
					return err
				}
				return deleteLive(env, h, c, keep)
			}
			if err := deleteLive(env, h, n, keep); err != nil {
				return err
			}
		case model.IsW(n, "sdt"), model.IsW(n, "ruby"), model.IsLocal(n, "AlternateContent"), model.Classify(n) == model.KindUnknown:
			if err := h.RemoveNode(n); err != nil {
				return err
			}
		}
	}
	return nil
}

// holdsAny reports whether n holds one of the kept nodes.
func holdsAny(n *tree.Node, keep map[*tree.Node]bool) bool {
	found := false
	n.Walk(func(c *tree.Node) bool {
		if keep[c] {
			found = true
		}
		return !found
	})
	return found
}

// onlyProps reports whether run r holds nothing but properties and
// markers (a bookmark's run, a field's code): it is not content.
func onlyProps(r *tree.Node) bool {
	if !model.IsW(r, "r") {
		return false
	}
	for _, ch := range r.Elements() {
		switch model.Classify(ch) {
		case model.KindProps, model.KindMarker:
		default:
			return false
		}
	}
	return true
}

// replaceParagraphProperties gives p the style and paragraph CSS of the
// html's block element where they differ from what it has, as one
// tracked property change.
func replaceParagraphProperties(env *Env, h *core.Handle, p *tree.Node, a html.ParaAttrs, style string) error {
	current := ""
	if ppr := model.ChildW(p, "pPr"); ppr != nil {
		if ps := model.ChildW(ppr, "pStyle"); ps != nil {
			current = model.AttrOr(ps, "val")
		}
	}
	css := ""
	if a.CSS.OK {
		css, _ = html.ParagraphProperties(a.CSS.V) //nolint:errcheck // checked before any change
	}
	if style == current && css == "" {
		return nil
	}
	ppr, err := h.EnsureIn(p, docxedit.W("pPr"))
	if err != nil {
		return err
	}
	_, err = env.Rev.Property(h, ppr, func() error {
		if style != current {
			if style == "" {
				if ps := model.ChildW(ppr, "pStyle"); ps != nil {
					return h.RemoveNode(ps)
				}
			} else if err := setStyle(h, p, style); err != nil {
				return err
			}
		}
		if css == "" {
			return nil
		}
		nodes, err := h.Workspace().Parse(ppr, []byte(css), map[string]string{"w": model.NSW})
		if err != nil {
			return err
		}
		for _, n := range nodes {
			if old := model.ChildW(ppr, n.Local()); old != nil {
				if err := mergeAttrs(h, old, n); err != nil {
					return err
				}
				continue
			}
			if err := h.PlaceNew(ppr, n); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

// mergeAttrs gives the existing property element old the attribute
// values of n that differ from its own, keeping its other attributes: an
// echo of read's CSS changes nothing. An exact line height read shows
// in points is the same as an at-least one of the same height (read
// writes both as points).
func mergeAttrs(h *core.Handle, old, n *tree.Node) error {
	for _, a := range n.Attrs() {
		local := a.Name
		if i := strings.IndexByte(local, ':'); i >= 0 {
			local = local[i+1:]
		}
		if v, ok := model.Attr(old, local); ok && (v == a.Value || local == "lineRule" && v != "auto") {
			continue
		}
		if err := h.SetNodeAttr(old, qname(old, local), a.Value); err != nil {
			return err
		}
	}
	return nil
}
