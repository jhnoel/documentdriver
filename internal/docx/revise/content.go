package revise

import (
	"slices"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// rangeMarkers keep their place, collapsed, when the content around them
// goes: their counterpart may sit outside the removed content.
var rangeMarkers = []string{
	"bookmarkStart", "bookmarkEnd", "commentRangeStart", "commentRangeEnd",
	"moveFromRangeStart", "moveFromRangeEnd", "moveToRangeStart", "moveToRangeEnd",
	"permStart", "permEnd",
}

// isRangeMarker reports whether n is one of the range markers.
func isRangeMarker(n *tree.Node) bool {
	return slices.ContainsFunc(rangeMarkers, func(l string) bool { return model.IsW(n, l) })
}

// below reports whether some node from n up to (not including) top
// satisfies is.
func below(n, top *tree.Node, is func(*tree.Node) bool) bool {
	for a := n; a != nil && a != top; a = a.Parent() {
		if is(a) {
			return true
		}
	}
	return false
}

// otherStory reports whether a is a textbox story or fallback branch.
func otherStory(a *tree.Node) bool {
	return model.IsW(a, "txbxContent") || model.IsLocal(a, "Fallback")
}

// removeContent removes n and its content without breaking what pairs
// with it from outside (segments.rs remove_content): range markers stay in
// place, comment references stay in a bare run, and a field n cuts in two
// loses its remaining field characters and code.
func (s *settler) removeContent(n *tree.Node) error {
	inOwnStory := func(d *tree.Node) bool { return !below(d, n, otherStory) }
	markers := model.Descendants(n, func(d *tree.Node) bool {
		return isRangeMarker(d) && inOwnStory(d) && !below(d, n, func(a *tree.Node) bool { return model.IsW(a, "r") })
	})
	refs := model.Descendants(n, func(d *tree.Node) bool { return model.IsW(d, "commentReference") && inOwnStory(d) })
	orphans := brokenFieldParts(n)
	parent := n.Parent()
	if parent == nil {
		return nil
	}
	for _, m := range markers {
		if err := s.h.PutBefore(m, n); err != nil {
			return wrap(err)
		}
	}
	for _, ref := range refs {
		if err := s.h.PutBefore(ref, n); err != nil {
			return wrap(err)
		}
		if _, err := s.h.Wrap(ref, ref, docxedit.W("r"), nil); err != nil {
			return wrap(err)
		}
	}
	if err := s.remove(n); err != nil {
		return err
	}
	for _, part := range orphans {
		run := part.Parent()
		if run != nil && !model.IsW(run, "r") {
			run = nil
		}
		if err := s.remove(part); err != nil {
			return err
		}
		if run != nil && onlyRunProperties(run) {
			if err := s.remove(run); err != nil {
				return err
			}
		}
	}
	return nil
}

// onlyRunProperties reports whether a run holds only run properties.
func onlyRunProperties(run *tree.Node) bool {
	for _, c := range run.Children() {
		if model.IsElement(c) && !model.IsW(c, "rPr") {
			return false
		}
	}
	return true
}

// brokenFieldParts returns the field characters and code outside n of the
// fields n cuts in two (some, but not all, of their begin, separate and end
// inside it).
func brokenFieldParts(n *tree.Node) []*tree.Node {
	story := ancestorWhere(n, func(a *tree.Node) bool { return model.IsW(a, "txbxContent") })
	if story == nil {
		for r := n; r != nil; r = r.Parent() {
			if r.Parent() != nil && r.Parent().Kind() == tree.DocumentNode {
				story = r
			}
		}
	}
	if story == nil {
		return nil
	}
	inside := func(d *tree.Node) bool { return below(d, nil, func(a *tree.Node) bool { return a == n }) }
	type field struct {
		own, code []*tree.Node
		separated bool
	}
	var fields []*field
	var open []*field
	story.Walk(func(d *tree.Node) bool {
		if d == story {
			return true
		}
		fieldChar := model.IsW(d, "fldChar")
		code := model.IsW(d, "instrText") || model.IsW(d, "delInstrText")
		if !fieldChar && !code {
			return true
		}
		if below(d, story, otherStory) {
			return true
		}
		for _, f := range open {
			if !f.separated {
				f.code = append(f.code, d)
			}
		}
		if !fieldChar {
			return true
		}
		switch model.AttrOr(d, "fldCharType") {
		case "begin":
			f := &field{own: []*tree.Node{d}}
			fields = append(fields, f)
			open = append(open, f)
		case "separate":
			if len(open) > 0 {
				f := open[len(open)-1]
				f.code = dropLast(f.code)
				f.own = append(f.own, d)
				f.separated = true
			}
		case "end":
			if len(open) > 0 {
				f := open[len(open)-1]
				open = open[:len(open)-1]
				if !f.separated {
					f.code = dropLast(f.code)
				}
				f.own = append(f.own, d)
			}
		}
		return true
	})
	var parts []*tree.Node
	for _, f := range fields {
		in := slices.ContainsFunc(f.own, inside)
		out := slices.ContainsFunc(f.own, func(d *tree.Node) bool { return !inside(d) })
		if !in || !out {
			continue
		}
		for _, d := range append(slices.Clone(f.own), f.code...) {
			if !inside(d) && !slices.Contains(parts, d) {
				parts = append(parts, d)
			}
		}
	}
	return parts
}

// ancestorWhere returns the nearest ancestor of n (excluding n) that is
// satisfies, or nil.
func ancestorWhere(n *tree.Node, is func(*tree.Node) bool) *tree.Node {
	for a := n.Parent(); a != nil; a = a.Parent() {
		if is(a) {
			return a
		}
	}
	return nil
}

// undeleteLeaves turns the deleted text and field code under root back
// into live w:t and w:instrText (segments.rs mark_deleted_leaves), except
// inside text boxes. root itself is not renamed.
func (s *settler) undeleteLeaves(root *tree.Node) error {
	return wrap(markLeaves(s.h, root, false))
}

// markLeaves renames the run leaves under root (root excluded) for a
// tracked deletion (del: w:t to w:delText, w:instrText to w:delInstrText)
// or its rejection (the reverse). A text box is its own story with its own
// revisions, so leaves inside one are left alone.
func markLeaves(h *edit.Handle, root *tree.Node, del bool) error {
	from, to := [2]string{"delText", "delInstrText"}, [2]string{"t", "instrText"}
	if del {
		from, to = to, from
	}
	type leaf struct {
		n  *tree.Node
		to string
	}
	var leaves []leaf
	root.Walk(func(d *tree.Node) bool {
		if d == root {
			return true
		}
		k := slices.IndexFunc(from[:], func(l string) bool { return model.IsW(d, l) })
		if k < 0 || below(d, root, func(a *tree.Node) bool { return model.IsW(a, "txbxContent") }) {
			return true
		}
		leaves = append(leaves, leaf{d, to[k]})
		return true
	})
	for _, l := range leaves {
		if l.n.Parent() == nil {
			continue
		}
		if err := h.Rename(l.n, l.to); err != nil {
			return err
		}
	}
	return nil
}

// dropLast removes the last element, if any (Rust's Vec::pop).
func dropLast(s []*tree.Node) []*tree.Node {
	if len(s) == 0 {
		return s
	}
	return s[:len(s)-1]
}
