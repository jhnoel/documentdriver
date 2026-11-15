package revise

import (
	"slices"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// ReferencedNumIDs returns the list instances (w:numId values) the given
// part roots reference (lists.rs referenced_num_ids, over the main
// document and the package's other WordprocessingML parts but the
// numbering part).
func ReferencedNumIDs(roots []*tree.Node) map[string]bool {
	ids := map[string]bool{}
	for _, r := range roots {
		r.Walk(func(n *tree.Node) bool {
			if model.IsW(n, "numId") {
				if v, ok := model.Attr(n, "val"); ok {
					ids[v] = true
				}
			}
			return true
		})
	}
	return ids
}

// Orphaned returns the ids in before that after lacks: list instances a
// rejection orphaned.
func Orphaned(before, after map[string]bool) map[string]bool {
	out := map[string]bool{}
	for id := range before {
		if !after[id] {
			out[id] = true
		}
	}
	return out
}

// DropOrphanedLists removes from the numbering part (root is its
// w:numbering) the list instances named in orphaned, and each abstract
// definition they used that no instance uses any more and no numbering
// style links (lists.rs drop_orphaned_lists): reject-all takes back the
// definitions a rejected list change added. It returns how many elements
// it removed.
func DropOrphanedLists(h *edit.Handle, root *tree.Node, orphaned map[string]bool) (int, error) {
	if len(orphaned) == 0 || root == nil {
		return 0, nil
	}
	removed := 0
	abstracts := map[string]bool{}
	for _, num := range slices.Clone(root.Children()) {
		if !model.IsW(num, "num") || !orphaned[model.AttrOr(num, "numId")] {
			continue
		}
		if a := model.ChildW(num, "abstractNumId"); a != nil {
			if v, ok := model.Attr(a, "val"); ok {
				abstracts[v] = true
			}
		}
		if err := h.RemoveNode(num); err != nil {
			return removed, err
		}
		removed++
	}
	used := map[string]bool{}
	for _, num := range root.Children() {
		if model.IsW(num, "num") {
			if a := model.ChildW(num, "abstractNumId"); a != nil {
				if v, ok := model.Attr(a, "val"); ok {
					used[v] = true
				}
			}
		}
	}
	for _, def := range slices.Clone(root.Children()) {
		if !model.IsW(def, "abstractNum") {
			continue
		}
		id, ok := model.Attr(def, "abstractNumId")
		linked := model.ChildW(def, "styleLink") != nil || model.ChildW(def, "numStyleLink") != nil
		if !ok || !abstracts[id] || used[id] || linked {
			continue
		}
		if err := h.RemoveNode(def); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}
