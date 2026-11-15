package revise

import (
	"fmt"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Span edits (replace_text, revise_text and text.insert): the deletion of
// an isolated span's units and the insertion of new runs at a point.

// DeleteUnits records the deletion of units, the maximal elements of an
// isolated span (span.Isolated.Units) in document order, which may have
// different parents (delete_tracked_range): runs and paragraph-level
// equations go into w:del wrappers (Delete), consecutive siblings sharing
// one; wrappers a deletion cannot hold (hyperlinks, fields, smart tags,
// custom XML, bidi wrappers, insertions) have their content deleted
// inside them, so another author's insertion gets a nested w:del; content
// already deleted stays as it is, and the author's own pending insertion
// is removed outright. Range markers, properties and content the engine
// does not know keep their place.
func (r *Reviser) DeleteUnits(h *edit.Handle, units []*tree.Node) error {
	var group []*tree.Node
	flush := func() error {
		if len(group) == 0 {
			return nil
		}
		g := group
		group = nil
		return r.Delete(h, g)
	}
	var visit func(n *tree.Node) error
	visit = func(n *tree.Node) error {
		switch {
		case deletable(n):
			if len(group) > 0 && !adjacent(group[len(group)-1], n) {
				if err := flush(); err != nil {
					return err
				}
			}
			group = append(group, n)
			return nil
		case model.IsRevDel(n):
			return flush()
		case isDescended(n):
			if err := flush(); err != nil {
				return err
			}
			for _, c := range n.Elements() {
				if err := visit(c); err != nil {
					return err
				}
			}
			return flush()
		}
		// A marker or anything else between deleted runs stays outside
		// the deletion, as docxdriver leaves it.
		return flush()
	}
	for _, u := range units {
		if u.Parent() == nil {
			continue
		}
		if err := visit(u); err != nil {
			return err
		}
	}
	return flush()
}

// adjacent reports whether b is a's next element sibling.
func adjacent(a, b *tree.Node) bool {
	if a.Parent() != b.Parent() {
		return false
	}
	return a.NextSibling() != nil && model.NextElement(a) == b
}

// OwnInsertion returns the author's own pending insertion (w:ins or
// w:moveTo) at or above n, below the paragraph, or nil.
func (r *Reviser) OwnInsertion(n *tree.Node) *tree.Node { return r.ownInsertion(n, nil) }

// InsertPlaced records the insertion of nodes, consecutive siblings just
// placed in a paragraph (new runs), and returns the w:ins that holds
// them. Content placed inside the author's own pending insertion is
// already part of it (Word edits its own pending text in place): nothing
// is recorded. Otherwise, unless join is false, new content beside the
// author's own pending insertion extends it (docxdriver's merge_own_ins:
// the left neighbour first), and anything else is wrapped in a new w:ins.
func (r *Reviser) InsertPlaced(h *edit.Handle, nodes []*tree.Node, join bool) (*tree.Node, error) {
	if err := contiguous(nodes); err != nil {
		return nil, err
	}
	if own := r.ownInsertion(nodes[0].Parent(), nil); own != nil {
		return own, nil
	}
	if join {
		if left := model.PrevElement(nodes[0]); left != nil && r.ownWrapper(left) {
			for _, n := range nodes {
				if err := h.Append(left, n); err != nil {
					return nil, fmt.Errorf("%w: %w", ErrRevise, err)
				}
			}
			return left, nil
		}
		if right := model.NextElement(nodes[len(nodes)-1]); right != nil && r.ownWrapper(right) {
			for i := len(nodes) - 1; i >= 0; i-- {
				if err := h.Prepend(right, nodes[i]); err != nil {
					return nil, fmt.Errorf("%w: %w", ErrRevise, err)
				}
			}
			return right, nil
		}
	}
	return r.Insert(h, nodes)
}

// ownWrapper reports whether n is a w:ins the author wrote.
func (r *Reviser) ownWrapper(n *tree.Node) bool {
	return model.IsW(n, "ins") && model.AttrOr(n, "author") == r.author
}
