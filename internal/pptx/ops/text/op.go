package text

import (
	"fmt"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/edit"
)

// Object is what the text operations need from an edit.Handle, which
// implements it (ReplaceParagraphs is the mutator the text operations
// added to it).
type Object interface {
	// Branches returns the number of mc:AlternateContent branch elements
	// of the object (1 outside mc:AlternateContent).
	Branches() int
	// Elem returns the element of branch i (0 is the projected branch)
	// for reading.
	Elem(i int) *tree.Node
	// Ensure makes sure the element at parent has a child named c, in
	// every branch, placed in schema order (edit.Handle.Ensure).
	Ensure(parent edit.Path, c edit.Name) error
	// ReplaceParagraphs replaces, in each branch i with frags[i] not nil,
	// the children of the object's p:txBody from its first a:p to its
	// last a:p inclusive (whatever lies between them included) with the
	// nodes parsed from frags[i]: a sequence of a:p elements whose
	// prefixes are in scope at the p:txBody or declared on the elements
	// themselves. When the p:txBody has no a:p, the nodes go after
	// a:lstStyle, else after a:bodyPr. a:bodyPr and a:lstStyle are never
	// touched. A nil frags[i] leaves branch i as it is. Nothing changes
	// unless every listed branch can be changed: each must have a
	// p:txBody with an a:bodyPr and each fragment must parse as one or
	// more a:p elements.
	ReplaceParagraphs(frags [][]byte) error
}

// Rels is what text.set needs of the relationships of the object's part:
// reading hyperlink targets (Ctx; colours are not displayed, so Color
// returns ("", true)), finding or adding an external hyperlink
// relationship, and removing relationships nothing refers to any more.
type Rels interface {
	Ctx
	// Hyperlink returns the id of an external hyperlink relationship to
	// target: an existing one with the same target (PowerPoint shares one
	// among the runs of a link), else a new one with the next free id.
	Hyperlink(target string) (string, error)
	// Prune removes each of the relationships rids that nothing in the
	// part refers to any more.
	Prune(rids []string) error
}

// linkIDs returns the relationship ids of the click hyperlinks in body.
func linkIDs(body *tree.Node) []string {
	var out []string
	if body == nil {
		return nil
	}
	var walk func(n *tree.Node)
	walk = func(n *tree.Node) {
		for _, c := range n.Elements() {
			if derive.Is(c, derive.DML, "hlinkClick") {
				if id, ok := derive.AttrNS(c, derive.REL, "id"); ok && id != "" {
					out = append(out, id)
				}
				continue
			}
			walk(c)
		}
	}
	walk(body)
	return out
}

// txBody returns the p:txBody of an object element, or nil.
func txBody(e *tree.Node) *tree.Node { return derive.Child(e, derive.PML, "txBody") }

// Set is text.set: it makes the object's text body show html. The
// object's a:bodyPr and a:lstStyle are kept as they are; a shape without
// a text body gets one with an empty a:bodyPr. When the body already
// shows html (colours compared as written, not as displayed), nothing is
// touched. After writing it reads every branch back and fails with
// ErrInexpressible unless each shows html. Without the part's
// relationships, hyperlinks show as unresolved and none can be created:
// see SetWith.
func Set(o Object, html string) error { return SetWith(o, html, nil) }

// SetWith is Set with the relationships of the object's part (an
// edit.PartRels from Handle.Rels), through which hyperlinks are read,
// reused or added; relationships of links the body no longer holds are
// removed when nothing else in the part refers to them.
func SetWith(o Object, html string, rels Rels) error {
	want, err := Parse(html)
	if err != nil {
		return err
	}
	need := false
	for i := range o.Branches() {
		if txBody(o.Elem(i)) == nil {
			need = true
		}
	}
	if need {
		if err := o.Ensure(nil, edit.P("txBody")); err != nil {
			return fmt.Errorf("text.set: %w", err)
		}
		if err := o.Ensure(edit.Path{edit.P("txBody")}, edit.A("bodyPr")); err != nil {
			return fmt.Errorf("text.set: %w", err)
		}
	}
	var ctx Ctx
	if rels != nil {
		ctx = rels
	}
	frags := make([][]byte, o.Branches())
	dirty := false
	var before []string
	for i := range frags {
		body := txBody(o.Elem(i))
		before = append(before, linkIDs(body)...)
		frag, changed, err := BuildWith(body, want, rels)
		if err != nil {
			return fmt.Errorf("text.set: branch %d: %w", i, err)
		}
		if changed {
			frags[i], dirty = frag, true
		}
	}
	if !dirty {
		return nil
	}
	if err := o.ReplaceParagraphs(frags); err != nil {
		return fmt.Errorf("text.set: %w", err)
	}
	k := key(want)
	for i := range o.Branches() {
		body := txBody(o.Elem(i))
		if body == nil {
			return fmt.Errorf("text.set: %w: branch %d has no text body after writing", ErrInexpressible, i)
		}
		if got := key(ReadBodyWith(body, ctx)); got != k {
			return fmt.Errorf("text.set: %w: branch %d reads back as %s, want %s", ErrInexpressible, i, got, k)
		}
	}
	if rels != nil && len(before) > 0 {
		if err := rels.Prune(before); err != nil {
			return fmt.Errorf("text.set: %w", err)
		}
	}
	return nil
}

// ReplaceText is text.replace on an object: Replace in every branch,
// which must hold the same number of matches. It returns the number of
// matches found (per branch). occurrence picks one match, counting from 1
// (0: the only one). Replacing a text with itself checks the
// matches and touches nothing.
func ReplaceText(o Object, find, with string, all bool, occurrence int) (int, error) {
	frags := make([][]byte, o.Branches())
	n := -1
	for i := range frags {
		body := txBody(o.Elem(i))
		if body == nil {
			return 0, fmt.Errorf("text.replace: branch %d has no text body", i)
		}
		frag, k, err := Replace(body, find, with, all, occurrence)
		if err != nil {
			return 0, fmt.Errorf("text.replace: branch %d: %w", i, err)
		}
		if n >= 0 && k != n {
			return 0, fmt.Errorf("text.replace: %w: %d matches in branch 0, %d in branch %d", edit.ErrBranch, n, k, i)
		}
		n = k
		frags[i] = frag
	}
	if find == with {
		return n, nil
	}
	if err := o.ReplaceParagraphs(frags); err != nil {
		return 0, fmt.Errorf("text.replace: %w", err)
	}
	return n, nil
}
