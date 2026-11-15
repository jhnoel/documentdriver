package edit

import "github.com/jhnoel/documentdriver/internal/ooxml/tree"

// Insertion order after one anchor. Blocks a plan inserts after the same
// anchor keep the plan's order (docxdriver's insert_after_in_order): the
// second goes after the first, not between the anchor and the first.

// InsertAfter returns the element a new block inserted after anchor goes
// after: anchor itself, or the last block an earlier insertion after it
// (or after one of those) placed, as long as each is still anchor's
// sibling.
func (r *Registry) InsertAfter(anchor *tree.Node) *tree.Node {
	target := anchor
	seen := map[*tree.Node]bool{}
	for {
		next, ok := r.after[target]
		if !ok || seen[next] || next.Parent() == nil || next.Parent() != anchor.Parent() {
			return target
		}
		seen[next] = true
		target = next
	}
}

// NoteInsertedAfter records that block n was inserted after anchor.
func (r *Registry) NoteInsertedAfter(anchor, n *tree.Node) {
	if r.after == nil {
		r.after = map[*tree.Node]*tree.Node{}
	}
	r.after[anchor] = n
}
