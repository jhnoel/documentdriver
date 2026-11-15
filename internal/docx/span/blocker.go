package span

import (
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// blockerLabels are the structures a span op may not cross, by local name
// in any namespace, with docxdriver's labels (typed.rs BLOCKERS): an edit
// across one cannot keep it whole. The objects among them (drawings, note
// references) count one character, so a selection that covers one is
// visible in the selected text.
var blockerLabels = map[string]string{
	"commentRangeStart": "comment",
	"commentRangeEnd":   "comment",
	"commentReference":  "comment",
	"fldSimple":         "field",
	"fldChar":           "field",
	"drawing":           "drawing",
	"object":            "drawing",
	"pict":              "drawing",
	"footnoteReference": "note",
	"endnoteReference":  "note",
	"sdt":               "structured document tag",
}

// BlockerLabel returns the blocker label of element n alone, and whether
// it is a blocker: one of docxdriver's blocking structures.
func BlockerLabel(n *tree.Node) (string, bool) {
	if !model.IsElement(n) {
		return "", false
	}
	l, ok := blockerLabels[n.Local()]
	return l, ok
}

// Blocker returns the first of docxdriver's blocking structures in nodes
// or anywhere below them, in document order (as docxdriver scans), and
// its label.
func Blocker(nodes []*tree.Node) (label string, at *tree.Node, ok bool) {
	for _, u := range nodes {
		var found *tree.Node
		u.Walk(func(n *tree.Node) bool {
			if found != nil {
				return false
			}
			if _, ok := BlockerLabel(n); ok {
				found = n
			}
			return found == nil
		})
		if found != nil {
			l, _ := BlockerLabel(found)
			return l, found, true
		}
	}
	return "", nil, false
}

// Unknown returns the paragraph content in nodes or below them that the
// taxonomy does not name (model.KindUnknown), outside objects and
// properties, in document order. Isolation never rewrites it; an
// operation that would drop or rewrite the span's content (a direct
// replacement) must keep each whole or refuse, while one that only marks
// the span (a comment, a bookmark, a tracked deletion) leaves it as it is.
func Unknown(nodes []*tree.Node) []*tree.Node {
	var out []*tree.Node
	var walk func(n *tree.Node)
	walk = func(n *tree.Node) {
		switch model.Classify(n) {
		case model.KindUnknown:
			out = append(out, n)
		case model.KindWrapper:
			if model.IsLocal(n, "AlternateContent") {
				if b := model.AlternateContentBranch(n); b != nil {
					for c := range b.ElementsSeq() {
						walk(c)
					}
				}
				return
			}
			for c := range n.ElementsSeq() {
				walk(c)
			}
		}
	}
	for _, u := range nodes {
		walk(u)
	}
	return out
}

// Blocker returns the first of docxdriver's blocking structures in the
// isolated span, and its label.
func (s *Isolated) Blocker() (label string, at *tree.Node, ok bool) { return Blocker(s.Units) }

// Unknown returns the unknown content in the isolated span (Unknown).
func (s *Isolated) Unknown() []*tree.Node { return Unknown(s.Units) }

// Crossed returns the first blocking structure the isolated span crosses
// the edge of (docxdriver's scan between the selection's paragraph-level
// anchors meets it): one that encloses some of the units but not all of
// them, and its label. A span wholly inside one (text in a content
// control) does not cross it.
func (s *Isolated) Crossed() (label string, at *tree.Node, ok bool) {
	for _, u := range s.Units {
		for a := u.Parent(); a != nil && a != s.Para; a = a.Parent() {
			l, blocks := BlockerLabel(a)
			if !blocks {
				continue
			}
			for _, v := range s.Units {
				if !model.Within(v, a) {
					return l, a, true
				}
			}
		}
	}
	return "", nil, false
}
