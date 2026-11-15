package span

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Isolation errors: a span end that cannot become an element boundary.
var (
	// ErrBareText is a span end inside a text leaf that is not in a run
	// (docxdriver panics on one): there is no run to split.
	ErrBareText = errors.New("span: a span end falls inside text that is not in a run")
	// ErrAlternateContent is a span end inside an mc:AlternateContent:
	// splitting its effective branch alone would make the branches
	// disagree.
	ErrAlternateContent = errors.New("span: a span end falls inside an mc:AlternateContent")
	// ErrNestedLeaf is a span end inside a text leaf that is not a direct
	// child of its run.
	ErrNestedLeaf = errors.New("span: a span end falls inside text nested below its run")
	// ErrUnknown is a span end inside an element the taxonomy does not
	// name (model.KindUnknown): splitting below it would rewrite content
	// the engine cannot vouch for, so it is preserved and the span
	// refused.
	ErrUnknown = errors.New("span: a span end falls inside an element the engine does not know")
	// ErrRunOrder is a span end in a run whose w:rPr is not its first
	// child, on the side the split would leave without it.
	ErrRunOrder = errors.New("span: a span end splits a run whose properties follow its content")
)

// Isolation refusals are about the document, not the engine: an
// operation reports them as its own error (the span cannot be made whole
// elements without rewriting markup), and the document is unchanged.

// Isolated is a span whose ends are element boundaries.
type Isolated struct {
	Span
	// Units are the maximal elements wholly inside the span, in document
	// order: runs, wrappers (revision wrappers, hyperlinks, fields,
	// content controls) whose content is all inside, paragraph-level
	// objects, and the zero-width markers between the first and the last
	// character (never those before the first or after the last, which
	// stay outside). Units may have different parents where the span
	// crosses a wrapper's edge. Empty for an empty span.
	Units []*tree.Node
}

// Isolate makes span s of paragraph s.Para whole elements: the run (and
// text leaf) holding each end is split there, with its run properties
// copied, through h, a handle on the paragraph (internal/docx/edit
// Workspace.Target), which declares the change. An end between two run
// children splits the run there; markers between the end's characters and
// the next go to the outside. An empty span splits at its point and has no
// units. Text, views and formatting are unchanged: every character keeps
// its run properties and wrappers, and every marker its place.
//
// ids, when given, allocates revision ids: a split run whose properties
// record a pending format change (w:rPrChange) gives the copy in its new
// half a fresh id, so the two records do not share one. Without it the
// copy keeps the id.
func Isolate(h *core.Handle, s Span, ids ...func() (string, error)) (*Isolated, error) {
	var fresh func() (string, error)
	if len(ids) > 0 {
		fresh = ids[0]
	}
	if n := Read(s.Para).Len(); s.Start < 0 || s.End < s.Start || s.End > n {
		return nil, fmt.Errorf("%w: [%d, %d) of %d characters", ErrRange, s.Start, s.End, n)
	}
	// Every refusal is found before anything changes.
	if err := mixed(Read(s.Para), s); err != nil {
		return nil, err
	}
	for _, dry := range []bool{true, false} {
		if err := cut(h, fresh, s.Para, s.End, true, dry); err != nil {
			return nil, err
		}
		if !s.Empty() {
			if err := cut(h, fresh, s.Para, s.Start, false, dry); err != nil {
				return nil, err
			}
		}
	}
	if s.Empty() {
		return &Isolated{Span: s}, nil
	}
	units, err := unitsOf(Read(s.Para), s)
	if err != nil {
		return nil, err
	}
	return &Isolated{Span: s, Units: units}, nil
}

// cut makes all-view offset off a run boundary. end says which side is
// the span's inside: the characters before off (an end) or after it (a
// start); markers at off go to the other side.
// With dry set it only checks that the split is possible.
func cut(h *core.Handle, ids func() (string, error), p *tree.Node, off int, end, dry bool) error {
	segs := Read(p).Segs
	for _, s := range segs {
		if s.Start < off && off < s.End {
			return splitInside(h, ids, p, s, off, dry)
		}
	}
	var before, after *model.Segment
	for i := range segs {
		s := &segs[i]
		if s.Start < s.End && s.End == off {
			before = s
		}
		if s.Start < s.End && s.Start == off && after == nil {
			after = s
		}
	}
	switch {
	case end && before != nil:
		return splitAround(h, ids, p, *before, true, dry)
	case !end && after != nil:
		return splitAround(h, ids, p, *after, false, dry)
	case before != nil: // the point of an empty span at a paragraph's end
		return splitAround(h, ids, p, *before, true, dry)
	case after != nil:
		return splitAround(h, ids, p, *after, false, dry)
	}
	return nil
}

// isRPr reports whether n is a run properties element.
func isRPr(n *tree.Node) bool { return model.IsW(n, "rPr") }

// splittable checks that splitting run before child index at rewrites
// only known content: no element the taxonomy does not name encloses the
// run (below the paragraph), and the run's properties, if any, come before
// at so both halves keep them.
func splittable(run, p *tree.Node, at int) error {
	if inAlternate(run, p) {
		return ErrAlternateContent
	}
	for a := run.Parent(); a != nil && a != p; a = a.Parent() {
		if model.Classify(a) == model.KindUnknown {
			return ErrUnknown
		}
	}
	for i, c := range run.Children() {
		if isRPr(c) && i >= at {
			return ErrRunOrder
		}
	}
	return nil
}

// runChild returns the child of run that holds leaf.
func runChild(run, leaf *tree.Node) *tree.Node {
	c := leaf
	for c.Parent() != run {
		c = c.Parent()
	}
	return c
}

// inAlternate reports whether an mc:AlternateContent lies on the path
// from n up to (not including) top.
func inAlternate(n, top *tree.Node) bool {
	for a := n.Parent(); a != nil && a != top; a = a.Parent() {
		if model.IsLocal(a, "AlternateContent") {
			return true
		}
	}
	return false
}

// splitAround splits leaf s's run right after the run child holding it
// (after) or right before it, when the run holds other content on that
// side. A leaf outside any run (an equation) is already a boundary.
func splitAround(h *core.Handle, ids func() (string, error), p *tree.Node, s model.Segment, after, dry bool) error {
	run := s.Run
	if run == nil {
		return nil
	}
	child := runChild(run, s.Leaf)
	kids := run.Children()
	i := slices.Index(kids, child)
	rest := kids[:i]
	at := i
	if after {
		rest, at = kids[i+1:], i+1
	}
	if !slices.ContainsFunc(rest, func(c *tree.Node) bool { return model.IsElement(c) && !isRPr(c) }) {
		return nil
	}
	if err := splittable(run, p, at); err != nil || dry {
		return err
	}
	right, err := h.SplitAt(run, at, isRPr)
	if err != nil {
		return err
	}
	return renumberCopy(h, ids, right)
}

// splitInside splits text leaf s at all-view offset off, then its run
// between the halves.
func splitInside(h *core.Handle, ids func() (string, error), p *tree.Node, s model.Segment, off int, dry bool) error {
	switch {
	case s.Run == nil:
		return ErrBareText
	case inAlternate(s.Leaf, p):
		return ErrAlternateContent
	case s.Leaf.Parent() != s.Run, s.Leaf.Text() != s.Text:
		// Nested below the run, or text the read model normalises
		// differently (a CR in CDATA): no split point to trust.
		return ErrNestedLeaf
	}
	// The run splits right after the leaf's left half.
	if err := splittable(s.Run, p, slices.Index(s.Run.Children(), s.Leaf)+1); err != nil || dry {
		return err
	}
	b := 0
	for range off - s.Start {
		_, size := utf8.DecodeRuneInString(s.Text[b:])
		b += size
	}
	right, err := h.SplitText(s.Leaf, b)
	if err != nil {
		return err
	}
	for _, half := range []*tree.Node{s.Leaf, right} {
		if err := preserveSpace(h, half); err != nil {
			return err
		}
	}
	kids := s.Run.Children()
	half, err := h.SplitAt(s.Run, slices.Index(kids, right), isRPr)
	if err != nil {
		return err
	}
	return renumberCopy(h, ids, half)
}

// preserveSpace marks a text half whose text starts or ends with white
// space xml:space="preserve", as Word needs to keep it.
func preserveSpace(h *core.Handle, t *tree.Node) error {
	s := t.Text()
	if s == "" || strings.TrimSpace(s) == s {
		return nil
	}
	if v, ok := t.Attr("xml:space"); ok && v == "preserve" {
		return nil
	}
	return h.SetNodeAttr(t, "xml:space", "preserve")
}

// mixed refuses a span that would cut an element the engine may not
// split (an mc:AlternateContent or an unknown element) into an inside and
// an outside part. Run splits create siblings, so this holds before and
// after them.
func mixed(p *Para, s Span) error {
	if s.Empty() {
		return nil
	}
	in, out := map[*tree.Node]bool{}, map[*tree.Node]bool{}
	for _, g := range p.Segs {
		if g.Start == g.End {
			continue
		}
		m := out
		if g.Start >= s.Start && g.End <= s.End {
			m = in
		}
		for a := g.Leaf.Parent(); a != nil && a != p.Node; a = a.Parent() {
			m[a] = true
		}
	}
	for a := range in {
		if !out[a] {
			continue
		}
		if model.IsLocal(a, "AlternateContent") || inAlternate(a, p.Node) {
			return ErrAlternateContent
		}
		if model.Classify(a) == model.KindUnknown {
			return ErrUnknown
		}
	}
	return nil
}

// unitsOf returns the maximal elements wholly inside span s of an isolated
// paragraph.
func unitsOf(p *Para, s Span) ([]*tree.Node, error) {
	type count struct{ in, out int }
	counts := map[*tree.Node]*count{}
	for _, g := range p.Segs {
		if g.Start == g.End {
			continue
		}
		inside := g.Start >= s.Start && g.End <= s.End
		for a := g.Leaf; a != nil && a != p.Node; a = a.Parent() {
			c := counts[a]
			if c == nil {
				c = &count{}
				counts[a] = c
			}
			if inside {
				c.in++
			} else {
				c.out++
			}
		}
	}
	type entry struct {
		n    *tree.Node
		zero bool
	}
	var ents []entry
	var walk func(n *tree.Node) error
	walk = func(n *tree.Node) error {
		for c := range n.ChildrenSeq() {
			if !model.IsElement(c) {
				continue
			}
			switch model.Classify(c) {
			case model.KindProps, model.KindStory:
				continue
			}
			k := counts[c]
			switch {
			case k == nil:
				ents = append(ents, entry{c, true})
			case k.out == 0:
				ents = append(ents, entry{c, false})
			case k.in > 0:
				if model.IsLocal(c, "AlternateContent") {
					return ErrAlternateContent
				}
				if model.Classify(c) == model.KindUnknown {
					return ErrUnknown
				}
				if err := walk(c); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(p.Node); err != nil {
		return nil, err
	}
	first := slices.IndexFunc(ents, func(e entry) bool { return !e.zero })
	if first < 0 {
		return nil, nil
	}
	last := len(ents) - 1
	for ents[last].zero {
		last--
	}
	out := make([]*tree.Node, 0, last-first+1)
	for _, e := range ents[first : last+1] {
		out = append(out, e.n)
	}
	return out, nil
}

// renumberCopy gives the pending format change in run's properties (a
// copy a split made) a fresh revision id from ids, when ids is set.
func renumberCopy(h *core.Handle, ids func() (string, error), run *tree.Node) error {
	if ids == nil {
		return nil
	}
	rpr := model.ChildW(run, "rPr")
	if rpr == nil {
		return nil
	}
	ch := model.ChildW(rpr, "rPrChange")
	if ch == nil {
		return nil
	}
	for _, a := range ch.Attrs() {
		if a.Name == "id" || strings.HasSuffix(a.Name, ":id") && !strings.HasPrefix(a.Name, "xmlns:") {
			id, err := ids()
			if err != nil {
				return err
			}
			return h.SetNodeAttr(ch, a.Name, id)
		}
	}
	return nil
}
