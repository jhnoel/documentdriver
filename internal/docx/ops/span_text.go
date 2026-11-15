package ops

import (
	"bytes"
	"errors"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/span"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// The text engine of text.replace, text.revise and text.insert: plain
// text replaces ranges of a paragraph's final view (docs/contract.md,
// section 6), word by word, through the span engine (Select, Isolate)
// and the Reviser (DeleteUnits, InsertPlaced). Direct mode is track mode
// with the authored revisions accepted, so both modes run this one path.
//
// What it keeps that docxdriver 0.5.2 does not:
//
//   - the replaced text's run properties, in both modes (W3: docxdriver's
//     direct replacement writes bare runs);
//   - tabs, breaks and objects in a selection (W6, B8): a tab is "\t" and a
//     break "\n" in select and in with, so an unchanged one is kept by the
//     word diff; an object (an equation, a symbol: U+FFFC) the replacement
//     would remove, or a U+FFFC in with where there is none, is refused,
//     never dropped or made up.

// textEdit is one replacement: characters [cs, ce) of the paragraph's
// final view become with. cs == ce is an insertion at that point.
type textEdit struct {
	cs, ce int
	with   string
	// sel and field name the plan fields of the selection and of with,
	// for refusals ("select", "with"; "edits[1].select", ...).
	sel, field string
}

// textTarget is the paragraph a text operation edits, in any story.
type textTarget struct {
	part string
	p    *tree.Node
	id   string
	h    *core.Handle
	// ids allocates the part's revision ids, for split format records.
	ids func() (string, error)
}

// textTargetOf resolves at and opens a region over the paragraph.
func (env *Env) textTargetOf(at ParagraphAddress) (*textTarget, error) {
	part, p, id, err := env.textParagraph(at)
	if err != nil {
		return nil, err
	}
	h, err := env.WS.Core().Range(part, "paragraph "+id, p, p)
	if err != nil {
		return nil, err
	}
	return &textTarget{part: part, p: p, id: id, h: h, ids: func() (string, error) { return env.WS.RevisionID(part) }}, nil
}

// plainSelect is docxdriver's refusal of markup in a selector.
func plainSelect(id, field, sel string) error {
	if strings.Contains(sel, "<") {
		return reject("operation_failed", field, "blocked in paragraph %s: %s must be plain text without HTML markup — select addresses the paragraph's visible text, never markup", id, field)
	}
	return nil
}

// hiddenClause is the not_found clause naming hidden deleted text, with
// its leading " — ", when sel matches in paragraph para's all view
// (model.All shows deleted text as well as current) but not in its
// current view: only then does a deletion explain the miss. It is ""
// otherwise, so a miss the deletion cannot explain is not misled. When
// advise is set the clause also carries the advice to accept or reject
// the deletion before editing that content.
func hiddenClause(para *span.Para, sel string, advise bool) string {
	if len(para.Find(model.All, sel)) == 0 {
		return ""
	}
	clause := " — hidden deleted text is not selectable"
	if advise {
		clause += "; accept or reject the deletion before editing that content"
	}
	return clause
}

// selectOne is the one match of sel in paragraph p's final view
// (occurrence picks one of several), in final-view characters, and how
// many matches there are.
func selectOne(p *tree.Node, id, sel string, occurrence *uint32) (cs, ce, n int, err error) {
	if err := plainSelect(id, "select", sel); err != nil {
		return 0, 0, 0, err
	}
	para := span.Read(p)
	ms := para.Find(model.Current, sel)
	current := para.Text(model.Current)
	switch n = len(ms); {
	case n == 0:
		clause := hiddenClause(para, sel, true)
		return 0, 0, 0, reject("not_found", "select", "blocked in paragraph %s: select matched nothing (current visible text: %q)%s", id, current, clause)
	case occurrence != nil && int(*occurrence) > n:
		return 0, 0, 0, reject("not_found", "occurrence", "blocked in paragraph %s: occurrence %d is out of range: select matched %d times in this paragraph", id, *occurrence, n)
	case occurrence != nil:
		m := ms[*occurrence-1]
		return m.Start, m.End, n, nil
	case n > 1:
		return 0, 0, 0, &Rejection{Code: "ambiguous", Field: "select",
			Msg: "blocked in paragraph " + id + ": select matched " + strconv.Itoa(n) + " times in this paragraph — pass occurrence (1-based) or extend select with more context (current visible text: " + strconv.Quote(current) + ")"}
	}
	return ms[0].Start, ms[0].End, 1, nil
}

// checkWith refuses replacement text the document cannot hold: XML
// cannot store most control characters, and with is plain text, so a
// carriage return or form feed has no meaning there.
func checkWith(field, with string) error {
	for _, r := range with {
		if r == '\t' || r == '\n' {
			continue
		}
		if r < 0x20 || r == 0xFFFE || r == 0xFFFF || (r >= 0xD800 && r <= 0xDFFF) || r == utf8.RuneError {
			return reject("invalid_operation", field, "%s holds the character %U, which a document cannot store as text (a tab is \\t and a line break \\n)", field, r)
		}
	}
	return nil
}

// commentSpans returns the all-view ranges of paragraph p the comment
// ranges in it cover (typed.rs comment_spans, at any depth).
func commentSpans(p *tree.Node) [][2]int {
	segs := span.Read(p).Segs
	var out [][2]int
	open := map[string]int{}
	k := 0
	off := 0
	p.Walk(func(n *tree.Node) bool {
		for k < len(segs) && segs[k].Leaf == n {
			off = segs[k].End
			k++
		}
		switch {
		case model.IsW(n, "commentRangeStart"):
			open[model.AttrOr(n, "id")] = off
		case model.IsW(n, "commentRangeEnd"):
			if s, ok := open[model.AttrOr(n, "id")]; ok {
				out = append(out, [2]int{s, off})
				delete(open, model.AttrOr(n, "id"))
			}
		}
		return true
	})
	return out
}

// directGuard is the direct-mode refusal of a selection that overlaps a
// tracked change or a comment range (docxdriver's untracked replacement
// guards; OpDef.DirectGuards).
func directGuard(p *tree.Node, id string, s span.Span, field string) error {
	for _, g := range span.Read(p).Segs {
		if g.Start < s.End && g.End > s.Start && g.Wrap != model.WrapNone {
			return reject("operation_failed", field, "blocked in paragraph %s: the selection overlaps existing tracked changes — untracked replacement requires accepting or rejecting them first", id)
		}
	}
	for _, c := range commentSpans(p) {
		if s.Start < c[1] && s.End > c[0] {
			return reject("operation_failed", field, "blocked in paragraph %s: the selection intersects a comment range — untracked replacement requires removing or resolving the comment first", id)
		}
	}
	return nil
}

// isolate isolates s through t's handle, as an operation's refusal.
func (t *textTarget) isolate(s span.Span, field string) (*span.Isolated, error) {
	iso, err := span.Isolate(t.h, s, t.ids)
	if err != nil {
		if errors.Is(err, span.ErrRange) {
			return nil, err
		}
		return nil, reject("operation_failed", field, "blocked in paragraph %s: the selection cannot be isolated: %s", t.id, err.Error())
	}
	return iso, nil
}

// checkSelection refuses a selection a replacement may not touch: a
// blocking structure (a comment, field, drawing, note or content
// control) in it, and in direct mode a tracked change or comment range
// it overlaps.
func (env *Env) checkSelection(t *textTarget, s span.Span, field string) error {
	if s.Empty() {
		return nil
	}
	if env.Mode == ModeDirect {
		if err := directGuard(t.p, t.id, s, field); err != nil {
			return err
		}
	}
	iso, err := t.isolate(s, field)
	if err != nil {
		return err
	}
	label, _, bad := iso.Blocker()
	if !bad {
		label, _, bad = iso.Crossed()
	}
	if bad {
		return reject("operation_failed", field, "blocked in paragraph %s: the selection crosses a %s that cannot be safely preserved — update, resolve, or remove it before editing this content", t.id, label)
	}
	return nil
}

// editText applies edits (non-overlapping, in final-view offsets of t's
// paragraph as it stands). Edits
// that would leave the text as it is are dropped first, so a plan that
// changes nothing writes nothing; every other one's selection is checked
// (checkSelection) before any is applied, right to left so the offsets
// of those to the left stay valid.
func (env *Env) editText(t *textTarget, edits []textEdit) error {
	current := []rune(span.Read(t.p).Text(model.Current))
	var live []textEdit
	for _, e := range edits {
		if string(current[e.cs:e.ce]) != e.with {
			live = append(live, e)
		}
	}
	for _, e := range live {
		if err := env.checkSelection(t, selectionOf(t.p, e), e.sel); err != nil {
			return err
		}
	}
	sort.Slice(live, func(i, j int) bool { return live[i].cs > live[j].cs })
	for _, e := range live {
		if err := env.replaceRange(t, e); err != nil {
			return err
		}
	}
	return nil
}

// replaceRange applies one edit: the word diff between the selected text
// and with, each change a tracked deletion and insertion (right to left).
func (env *Env) replaceRange(t *textTarget, e textEdit) error {
	field := e.field
	old := []rune(span.Read(t.p).Text(model.Current))[e.cs:e.ce]
	repl := []rune(e.with)
	changes, err := span.WordDiff(old, repl)
	if errors.Is(err, span.ErrDiffTooLarge) {
		changes, err = []span.Change{{A: 0, B: len(old), C: 0, D: len(repl)}}, nil
	}
	if err != nil {
		return err
	}
	for _, c := range changes {
		if slices.Contains(old[c.A:c.B], span.Object) {
			return reject("operation_failed", field, "blocked in paragraph %s: the replacement would remove an equation or other object (U+FFFC in the selected text) — keep it as U+FFFC in %s at its place, or remove it with its own operation (equation.delete)", t.id, field)
		}
		if slices.Contains(repl[c.C:c.D], span.Object) {
			return reject("operation_failed", field, "blocked in paragraph %s: %s holds U+FFFC where the selection has no object — %s is plain text and cannot create an equation or other object", t.id, field, field)
		}
	}
	for i := len(changes) - 1; i >= 0; i-- {
		c := changes[i]
		if err := env.change(t, e.cs+c.A, e.cs+c.B, string(repl[c.C:c.D]), e.sel); err != nil {
			return err
		}
	}
	return nil
}

// change replaces final-view characters [a, b) of t's paragraph with
// text: a tracked deletion of the range (if any), then a tracked
// insertion of text wearing the run properties of the text it replaces
// or follows (typed.rs typing_rpr: "<b>bold text</b>", text -> words,
// gives "<b>words</b>").
func (env *Env) change(t *textTarget, a, b int, text, field string) error {
	para := span.Read(t.p)
	like := typingRPr(para, a, a < b)
	var point int
	join := true
	if a < b {
		s, err := para.Select(model.Current, a, b)
		if err != nil {
			return err
		}
		iso, err := t.isolate(s, field)
		if err != nil {
			return err
		}
		if u := iso.Unknown(); len(u) > 0 && textUnder(span.Read(t.p), u) {
			return reject("operation_failed", field, "blocked in paragraph %s: the replaced text is inside content the engine does not know (%s); it is left as it is", t.id, u[0].Name())
		}
		own := ownChars(env, span.Read(t.p), s)
		if err := env.Rev.DeleteUnits(t.h, iso.Units); err != nil {
			return err
		}
		point = s.End - own
		join = own == s.End-s.Start
	} else {
		var err error
		if point, err = para.Point(model.Current, a); err != nil {
			return err
		}
	}
	if text == "" {
		return nil
	}
	return env.insertText(t, point, text, like, join)
}

// textUnder reports whether some segment of para lies under one of nodes.
func textUnder(para *span.Para, nodes []*tree.Node) bool {
	for _, s := range para.Segs {
		if s.Start == s.End {
			continue
		}
		for _, n := range nodes {
			if model.Within(s.Leaf, n) {
				return true
			}
		}
	}
	return false
}

// ownChars counts the all-view characters of s inside the author's own
// pending insertion: deleting them removes them.
func ownChars(env *Env, para *span.Para, s span.Span) int {
	n := 0
	for _, g := range para.Segs {
		if g.Start >= s.Start && g.End <= s.End && g.Start < g.End && g.Wrap != model.WrapDel && env.Rev.OwnInsertion(g.Leaf) != nil {
			n += g.End - g.Start
		}
	}
	return n
}

// typingRPr returns the run properties new text at final-view character
// a takes: the replaced text's (replacing), else those of the text just
// before a, else just after; nil for none.
func typingRPr(para *span.Para, a int, replacing bool) *tree.Node {
	to := para.Offsets(model.Current)
	at := -1
	switch {
	case replacing && a < len(to):
		at = to[a]
	case !replacing && a > 0:
		at = to[a-1]
	case !replacing && a < len(to):
		at = to[a]
	}
	if at < 0 {
		return nil
	}
	for _, s := range para.Segs {
		if s.Start <= at && at < s.End {
			if s.Run == nil {
				return nil
			}
			return model.ChildW(s.Run, "rPr")
		}
	}
	return nil
}

// textless reports whether n occupies no text: properties and markers.
func textless(n *tree.Node) bool {
	if !model.IsElement(n) {
		return true
	}
	switch model.Classify(n) {
	case model.KindProps, model.KindMarker:
		return true
	}
	return false
}

// edgeIn reports whether n is the last content of its parent (after: only
// properties and markers follow it), or the first (only they precede it).
func edgeIn(n *tree.Node, after bool) bool {
	if after {
		for s := n.NextSibling(); s != nil; s = s.NextSibling() {
			if !textless(s) {
				return false
			}
		}
	} else {
		for s := n.PrevSibling(); s != nil; s = s.PrevSibling() {
			if !textless(s) {
				return false
			}
		}
	}
	return true
}

// isAlternate reports whether n is AlternateContent content or a branch.
func isAlternate(n *tree.Node) bool {
	return model.IsLocal(n, "AlternateContent") || model.IsLocal(n, "Choice") || model.IsLocal(n, "Fallback")
}

// placement returns where new runs go for all-view point off of t's
// paragraph (isolated there): beside the text just before it (after), or
// else just after it, climbing out of every wrapper the point is at the
// edge of. The author's own pending insertion is entered (its text is
// edited in place); another author's revision wrapper the point falls
// inside is split there, the second half taking a fresh id, so the new
// insertion never nests in it; mc:AlternateContent and unknown elements
// are kept whole.
func (env *Env) placement(t *textTarget, off int) (parent, before *tree.Node, err error) {
	para := span.Read(t.p)
	var left, right *model.Segment
	for i := range para.Segs {
		s := &para.Segs[i]
		if s.Start == s.End {
			continue
		}
		if s.End <= off {
			left = s
		}
		if s.Start >= off && right == nil {
			right = s
		}
	}
	seg, after := left, true
	if seg == nil {
		seg, after = right, false
	}
	if seg == nil {
		return t.p, nil, nil
	}
	n := seg.Leaf
	if seg.Run != nil {
		n = seg.Run
	}
	for n.Parent() != t.p {
		w := n.Parent()
		if w == nil {
			return nil, nil, errors.New("text: the anchor is detached")
		}
		if model.IsW(w, "ins") && model.AttrOr(w, "author") == env.Author {
			break
		}
		if isAlternate(w) || model.Classify(w) == model.KindUnknown || model.IsW(w, "r") {
			n = w
			continue
		}
		if model.IsRevIns(w) || model.IsRevDel(w) {
			if !edgeIn(n, after) {
				if err := env.splitRevision(t, w, n, after); err != nil {
					return nil, nil, err
				}
			}
			n = w
			continue
		}
		if edgeIn(n, after) {
			n = w
			continue
		}
		break
	}
	if after {
		return n.Parent(), model.NextElement(n), nil
	}
	return n.Parent(), n, nil
}

// splitRevision splits revision wrapper w beside its child n (after it,
// or before it) and gives the second half a fresh revision id.
func (env *Env) splitRevision(t *textTarget, w, n *tree.Node, after bool) error {
	i := slices.Index(w.Children(), n)
	if after {
		i++
	}
	right, err := t.h.SplitAt(w, i, nil)
	if err != nil {
		return err
	}
	if _, ok := model.Attr(right, "id"); !ok {
		return nil
	}
	id, err := env.WS.RevisionID(t.part)
	if err != nil {
		return err
	}
	for _, a := range right.Attrs() {
		if a.Name == "id" || strings.HasSuffix(a.Name, ":id") && !strings.HasPrefix(a.Name, "xmlns:") {
			return t.h.SetNodeAttr(right, a.Name, id)
		}
	}
	return nil
}

// insertText inserts text at all-view point off of t's paragraph as one
// new run (tabs and breaks as w:tab and w:br) with a copy of like (run
// properties, nil for none; a pending format change is not copied), and
// records it (Reviser.InsertPlaced; join lets it extend the author's own
// insertion beside it).
func (env *Env) insertText(t *textTarget, off int, text string, like *tree.Node, join bool) error {
	if _, err := span.Isolate(t.h, span.Span{Para: t.p, Start: off, End: off}, t.ids); err != nil {
		return reject("operation_failed", "", "blocked in paragraph %s: the insertion point cannot be isolated: %s", t.id, err.Error())
	}
	parent, before, err := env.placement(t, off)
	if err != nil {
		return err
	}
	var frag bytes.Buffer
	var kept []*tree.Node
	frag.WriteString("<w:r>")
	if like != nil {
		frag.WriteString("<" + like.Name() + ">")
		for _, c := range like.Elements() {
			if model.IsW(c, "rPrChange") {
				continue
			}
			frag.Write(c.Bytes())
			kept = append(kept, c)
		}
		frag.WriteString("</" + like.Name() + ">")
	}
	writeRunText(&frag, text)
	frag.WriteString("</w:r>")
	nodes, err := t.h.Workspace().Parse(parent, frag.Bytes(), map[string]string{"w": model.NSW})
	if err != nil || len(nodes) != 1 {
		return errors.Join(errors.New("text: the new run does not parse"), err)
	}
	run := nodes[0]
	if like != nil {
		t.h.Workspace().RecordCopies(model.ChildW(run, "rPr"), kept)
	}
	if err := t.h.PlaceAt(parent, run, before); err != nil {
		return err
	}
	_, err = env.Rev.InsertPlaced(t.h, []*tree.Node{run}, join)
	return err
}

// writeRunText writes text as a run's content: w:t for characters, w:tab
// for "\t" and w:br for "\n".
func writeRunText(b *bytes.Buffer, text string) {
	for text != "" {
		i := strings.IndexAny(text, "\t\n")
		if i < 0 {
			i = len(text)
		}
		if i > 0 {
			s := text[:i]
			if strings.TrimSpace(s) != s {
				b.WriteString(`<w:t xml:space="preserve">`)
			} else {
				b.WriteString("<w:t>")
			}
			b.Write(tree.EscapeText(s))
			b.WriteString("</w:t>")
		}
		if i == len(text) {
			break
		}
		if text[i] == '\t' {
			b.WriteString("<w:tab/>")
		} else {
			b.WriteString("<w:br/>")
		}
		text = text[i+1:]
	}
}

// spanFind is every non-overlapping match of sel in p's final view.
func spanFind(p *tree.Node, sel string) []span.Match {
	return span.Read(p).Find(model.Current, sel)
}

// selectionOf is the all-view span of edit e (a point for an insertion).
func selectionOf(p *tree.Node, e textEdit) span.Span {
	para := span.Read(p)
	if e.cs == e.ce {
		off, err := para.Point(model.Current, e.cs)
		if err != nil {
			return span.Span{Para: p}
		}
		return span.Span{Para: p, Start: off, End: off}
	}
	s, err := para.Select(model.Current, e.cs, e.ce)
	if err != nil {
		return span.Span{Para: p}
	}
	return s
}
