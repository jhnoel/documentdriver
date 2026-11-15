package revise

import (
	"bytes"
	"errors"
	"fmt"
	"slices"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Paragraph deletion (docxdriver 0.5.2's delete_paragraphs, typed.rs
// wrap_live_content_in_del, previous_joinable and layout.rs
// adopt_paragraph_properties). A deleted paragraph's live content goes into
// paragraph-local w:del wrappers and its mark is deleted, so accepting
// joins it into the next paragraph. A mark that cannot join forward (the
// last paragraph of the body, one before a table, one ending a section)
// stays; the deletion takes the previous paragraph's mark instead, and the
// paragraph adopts the previous paragraph's properties (its own recorded
// in a w:pPrChange), so accepting leaves no empty paragraph behind.

// ErrStructuredTag is a deleted paragraph holding a content control
// (w:sdt) in its live content: what to delete in it is not determined.
var ErrStructuredTag = errors.New("the paragraph contains a structured document tag (w:sdt) — paragraph.delete requires unambiguous paragraph content; remove the tag first")

// SetJoin sets the hook called before a paragraph join the reviser makes
// moves content (deleting the author's own inserted mark, and accepting a
// deleted mark in direct mode): the workspace's JoinParagraphs, which
// records the join and moves the identity. Without it joins are not
// recorded.
func (r *Reviser) SetJoin(join func(h *edit.Handle, survivor, absorbed *tree.Node) error) {
	r.join = join
}

// EndsSection reports whether paragraph p carries a section break
// (w:pPr/w:sectPr).
func EndsSection(p *tree.Node) bool {
	ppr := model.ChildW(p, "pPr")
	return ppr != nil && model.ChildW(ppr, "sectPr") != nil
}

// deletable is paragraph content a w:del wraps: runs and equations.
// A deletion is a run-level wrapper (CT_RunTrackChange), so hyperlinks,
// fields, smart tags, custom XML, bidi wrappers and insertions are
// descended into instead; equations at paragraph level are run-level
// content too (EG_MathContent), deleted with the paragraph.
func deletable(n *tree.Node) bool {
	return model.IsW(n, "r") || model.IsLocal(n, "oMath") || model.IsLocal(n, "oMathPara")
}

// descended are the wrappers whose content is deleted inside them.
var descended = []string{"hyperlink", "fldSimple", "smartTag", "customXml", "dir", "bdo", "ins", "moveTo"}

// isDescended reports whether n is a wrapper to descend into.
func isDescended(n *tree.Node) bool {
	return slices.ContainsFunc(descended, func(l string) bool { return model.IsW(n, l) })
}

// ContentBlocker returns ErrStructuredTag when the live content of
// paragraph p holds a content control. It is checked before anything
// changes.
func ContentBlocker(p *tree.Node) error {
	var found bool
	var walk func(c *tree.Node)
	walk = func(c *tree.Node) {
		for _, n := range c.Children() {
			switch {
			case found || !model.IsElement(n) || model.IsRevDel(n):
			case model.IsW(n, "sdt"):
				found = true
			case isDescended(n):
				walk(n)
			}
		}
	}
	walk(p)
	if found {
		return ErrStructuredTag
	}
	return nil
}

// DeleteContent records the deletion of paragraph p's live content,
// paragraph-locally (wrap_live_content_in_del): runs and equations go
// into w:del wrappers (Delete), inside hyperlinks, fields and other
// wrappers that cannot be deleted whole; content inside another author's
// insertion gets a nested w:del (Word's edit within an edit), and the
// author's own pending insertion is removed outright. Existing deletions
// are left as they are, and so are range markers, properties and content
// the engine does not know. The paragraph mark is not touched.
func (r *Reviser) DeleteContent(h *edit.Handle, p *tree.Node) error {
	if err := ContentBlocker(p); err != nil {
		return err
	}
	return r.deleteIn(h, p)
}

// deleteIn records the deletion of container c's deletable content.
func (r *Reviser) deleteIn(h *edit.Handle, c *tree.Node) error {
	for _, n := range slices.Clone(c.Children()) {
		var err error
		switch {
		case deletable(n):
			// One w:del per run, as Word writes a deleted paragraph and
			// docxdriver does: each run's deletion is listed alone.
			err = r.Delete(h, []*tree.Node{n})
		case isDescended(n):
			// Delete removes the author's own insertion it empties.
			err = r.deleteIn(h, n)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// hasParagraphSibling reports whether some element sibling of p is
// another paragraph: the walk beside p, before it and after it,
// following the tree's sibling links (no slice of the parent's
// children is built).
func hasParagraphSibling(p *tree.Node) bool {
	isOther := func(n *tree.Node) bool { return n != p && model.IsW(n, "p") }
	for s := model.NextElement(p); s != nil; s = model.NextElement(s) {
		if isOther(s) {
			return true
		}
	}
	for s := model.PrevElement(p); s != nil; s = model.PrevElement(s) {
		if isOther(s) {
			return true
		}
	}
	return false
}

// OwnParagraph reports whether paragraph p is wholly the author's pending
// insertion, with nothing else to keep (is_own_inserted_paragraph): its
// mark is the author's w:ins, it ends no section, another paragraph is
// beside it, and its content is the author's insertions of plain runs
// (properties, text, tabs and breaks). Deleting it removes it outright,
// as Word does.
func (r *Reviser) OwnParagraph(p *tree.Node) bool {
	own := func(n *tree.Node) bool { return model.IsW(n, "ins") && model.AttrOr(n, "author") == r.author }
	if m := model.ParagraphMarkIns(p); m == nil || !own(m) || EndsSection(p) {
		return false
	}
	if !hasParagraphSibling(p) {
		return false
	}
	for _, c := range p.Elements() {
		if model.IsW(c, "pPr") {
			continue
		}
		if !own(c) {
			return false
		}
		for _, run := range c.Elements() {
			if !model.IsW(run, "r") {
				return false
			}
			for _, leaf := range run.Elements() {
				if !slices.ContainsFunc([]string{"rPr", "t", "tab", "br"}, func(l string) bool { return model.IsW(leaf, l) }) {
					return false
				}
			}
		}
	}
	return true
}

// PreviousJoinable returns the paragraph whose mark a deletion of tail
// takes when tail's own mark cannot join forward (previous_joinable): the
// nearest earlier paragraph, past milestones and the deletion's targets
// whose marks are deleted. It is nil when that is not a paragraph (a
// table, the start of the story), ends a section (joining it would remove
// the break) or has a revised mark.
func PreviousJoinable(tail *tree.Node, targets map[*tree.Node]bool) *tree.Node {
	for n := tail.PrevSibling(); n != nil; n = n.PrevSibling() {
		if !model.IsElement(n) || slices.ContainsFunc(model.BodyMilestones, func(l string) bool { return model.IsW(n, l) }) {
			continue
		}
		if !model.IsW(n, "p") || EndsSection(n) {
			return nil
		}
		if targets[n] && model.ParagraphMarkDel(n) != nil {
			continue
		}
		if model.ParagraphMarkIns(n) != nil || model.ParagraphMarkDel(n) != nil {
			return nil
		}
		return n
	}
	return nil
}

// pprBase are the children of a paragraph's w:pPr a property snapshot
// holds: all but the mark's run properties, the section break and a
// change record.
func pprBase(p *tree.Node) []*tree.Node {
	ppr := model.ChildW(p, "pPr")
	if ppr == nil {
		return nil
	}
	return base(ppr, recordOf["pPr"].keep)
}

// concat returns the concatenated bytes of nodes.
func concat(nodes []*tree.Node) []byte {
	var b bytes.Buffer
	for _, n := range nodes {
		b.Write(n.Bytes())
	}
	return b.Bytes()
}

// AdoptProperties gives paragraph target a copy of source's paragraph
// properties (adopt_paragraph_properties), recording target's own in a
// w:pPrChange when they differ; target's mark run properties and section
// break stay. It reports false, changing nothing, when the properties
// differ and target already has a pending property change.
func (r *Reviser) AdoptProperties(h *edit.Handle, target, source *tree.Node) (bool, error) {
	oldNodes, nextNodes := pprBase(target), pprBase(source)
	old, next := concat(oldNodes), concat(nextNodes)
	if bytes.Equal(old, next) {
		return true, nil
	}
	ppr := model.ChildW(target, "pPr")
	if ppr != nil && model.ChildW(ppr, "pPrChange") != nil {
		return false, nil
	}
	if ppr == nil {
		var err error
		if ppr, err = h.EnsureIn(target, docxedit.W("pPr")); err != nil {
			return false, fmt.Errorf("%w: %w", ErrRevise, err)
		}
	}
	q := func(local string) string { return wName(ppr, local) }
	var frag bytes.Buffer
	frag.WriteString("<" + q("pPr") + ">")
	frag.Write(next)
	kept := slices.Clone(nextNodes)
	for _, keep := range []string{"rPr", "sectPr"} {
		if c := model.ChildW(ppr, keep); c != nil {
			frag.Write(c.Bytes())
			kept = append(kept, c)
		}
	}
	frag.WriteString("<" + q("pPrChange"))
	for _, a := range r.attrs(ppr) {
		frag.WriteString(" " + a[0] + `="` + string(tree.EscapeText(a[1])) + `"`)
	}
	frag.WriteString("><" + q("pPr") + ">")
	frag.Write(old)
	frag.WriteString("</" + q("pPr") + "></" + q("pPrChange") + "></" + q("pPr") + ">")
	nodes, err := h.Workspace().Parse(ppr, frag.Bytes(), nil)
	if err != nil || len(nodes) != 1 {
		return false, fmt.Errorf("%w: the adopted properties do not parse: %w", ErrRevise, err)
	}
	// Every element but the record is a copy: the adopted properties of
	// source's, the mark properties, section break and snapshot of
	// target's own, so faults they had stay theirs.
	ws := h.Workspace()
	ws.RecordCopy(nodes[0], ppr)
	change := model.ChildW(nodes[0], "pPrChange")
	for i, c := range nodes[0].Elements() {
		if c != change && i < len(kept) {
			ws.RecordCopy(c, kept[i])
		}
	}
	ws.RecordCopies(model.ChildW(change, "pPr"), oldNodes)
	if err := h.ReplaceWith(ppr, nodes[0]); err != nil {
		return false, fmt.Errorf("%w: %w", ErrRevise, err)
	}
	r.note(h, change)
	return true, nil
}

// Deletion is what DeleteParagraphs did.
type Deletion struct {
	// Marked counts the paragraphs whose content was marked deleted,
	// Removed the author's own pending paragraphs removed outright.
	Marked, Removed int
	// Breaks counts the paragraph marks deleted (the targets' own, and
	// previous paragraphs' taken for a target that cannot join forward).
	Breaks int
	// KeptEmpty counts the targets left as an empty paragraph: their mark
	// cannot join forward and no previous paragraph can join them.
	KeptEmpty int
	// Adopted are the targets that took a previous paragraph's mark, in
	// order.
	Adopted []Adoption
}

// Adoption is a deleted paragraph that took the mark and adopted the
// properties of the previous paragraph, Source.
type Adoption struct {
	Target, Source *tree.Node
}

// DeleteParagraphs deletes paragraphs ps (distinct, in document order,
// in the story h covers) as one change (delete_paragraphs): the author's
// own pending paragraphs go outright; every other one has its live
// content deleted (DeleteContent) and its mark deleted when the next
// block is a paragraph it can join and it ends no section. A target whose
// mark cannot join forward takes the previous joinable paragraph's mark
// instead and adopts that paragraph's properties (AdoptProperties), so
// accepting joins the previous paragraph into it: the survivor keeps its
// section break and no empty paragraph is left. Content controls are
// refused before anything changes.
func (r *Reviser) DeleteParagraphs(h *edit.Handle, ps []*tree.Node) (Deletion, error) {
	var d Deletion
	for _, p := range ps {
		if !r.OwnParagraph(p) {
			if err := ContentBlocker(p); err != nil {
				return d, err
			}
		}
	}
	targets := map[*tree.Node]bool{}
	var tails []*tree.Node
	for _, p := range ps {
		targets[p] = true
		if r.OwnParagraph(p) {
			if err := h.RemoveNode(p); err != nil {
				return d, fmt.Errorf("%w: %w", ErrRevise, err)
			}
			d.Removed++
			continue
		}
		d.Marked++
		if err := r.DeleteContent(h, p); err != nil {
			return d, err
		}
		if model.ParagraphMarkDel(p) != nil {
			continue
		}
		if !EndsSection(p) && model.ParagraphJoinTarget(p) != nil {
			if _, err := r.MarkParagraph(h, p, "del"); err != nil {
				return d, err
			}
			d.Breaks++
			continue
		}
		tails = append(tails, p)
	}
	for _, tail := range tails {
		prev := PreviousJoinable(tail, targets)
		if prev == nil {
			d.KeptEmpty++
			continue
		}
		ok, err := r.AdoptProperties(h, tail, prev)
		if err != nil {
			return d, err
		}
		if !ok {
			d.KeptEmpty++
			continue
		}
		if _, err := r.MarkParagraph(h, prev, "del"); err != nil {
			return d, err
		}
		d.Adopted = append(d.Adopted, Adoption{tail, prev})
		d.Breaks++
	}
	return d, nil
}
