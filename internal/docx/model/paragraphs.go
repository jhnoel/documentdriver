package model

import (
	"slices"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// View is which text of a paragraph to project, by tracked-changes
// semantics.
type View int

// The views.
const (
	// Current is the text with every change accepted: inserted text shown,
	// deleted text hidden.
	Current View = iota
	// Baseline is the text with every change rejected: inserted text
	// hidden, deleted text shown.
	Baseline
	// All shows both (deleted text still occupies its positions).
	All
)

// ViewNamed returns the view a command's internal view name selects:
// "baseline", "all", and anything else (or nothing) is Current.
func ViewNamed(name string) View {
	switch name {
	case "baseline":
		return Baseline
	case "all":
		return All
	}
	return Current
}

// Paragraph is a story paragraph and its 1-based index in its story.
type Paragraph struct {
	Index int
	Node  *tree.Node
}

// BodyMilestones are the body-level elements that can sit between
// paragraphs and are not blocks themselves.
var BodyMilestones = []string{
	"bookmarkStart", "bookmarkEnd", "commentRangeStart", "commentRangeEnd",
	"commentReference", "permStart", "permEnd", "proofErr",
	"moveFromRangeStart", "moveFromRangeEnd", "moveToRangeStart", "moveToRangeEnd",
}

// isMilestone reports whether n is a body milestone element listed in BodyMilestones.
func isMilestone(n *tree.Node) bool {
	return slices.ContainsFunc(BodyMilestones, func(l string) bool { return IsW(n, l) })
}

// Paragraphs returns every story paragraph of the body, indexed 1-based.
func (x *XML) Paragraphs() []Paragraph {
	b := x.Body()
	if b == nil {
		return nil
	}
	return ParagraphsIn(b)
}

// ParagraphsIn returns every story-level w:p under container (a body,
// header or footer), in document order, indexed 1-based. Paragraphs of a
// textbox or drawing inside a paragraph belong to another story and are
// not included; of an mc:AlternateContent only the effective branch is.
func ParagraphsIn(container *tree.Node) []Paragraph {
	var nodes []*tree.Node
	collectStoryParagraphs(container, &nodes)
	out := make([]Paragraph, len(nodes))
	for i, n := range nodes {
		out[i] = Paragraph{Index: i + 1, Node: n}
	}
	return out
}

// collectStoryParagraphs recurses only over a tree the tokenizer built, so
// its depth is bounded by tree.MaxDepth.
func collectStoryParagraphs(container *tree.Node, out *[]*tree.Node) {
	for c := range container.ChildrenSeq() {
		if !IsElement(c) {
			continue
		}
		if IsLocal(c, "AlternateContent") {
			if b := AlternateContentBranch(c); b != nil {
				collectStoryParagraphs(b, out)
			}
			continue
		}
		if IsW(c, "p") {
			*out = append(*out, c)
			continue
		}
		collectStoryParagraphs(c, out)
	}
}

// AllParagraphs returns every w:p of the part in document order, nested
// ones included.
func (x *XML) AllParagraphs() []*tree.Node {
	r := x.Root()
	if r == nil {
		return nil
	}
	return Descendants(r, func(n *tree.Node) bool { return IsW(n, "p") })
}

// AlternateContentBranch returns the effective branch of an
// mc:AlternateContent: its mc:Fallback. docxdriver advertises none of the
// extension namespaces a Choice requires, so without a Fallback there is
// no effective content (nil).
func AlternateContentBranch(n *tree.Node) *tree.Node {
	if !IsLocal(n, "AlternateContent") {
		return nil
	}
	return ChildLocal(n, "Fallback")
}

// ParagraphStyle returns the paragraph's w:pPr/w:pStyle/@w:val.
func ParagraphStyle(p *tree.Node) (string, bool) {
	ppr := ChildW(p, "pPr")
	if ppr == nil {
		return "", false
	}
	s := ChildW(ppr, "pStyle")
	if s == nil {
		return "", false
	}
	return Attr(s, "val")
}

// IsRevIns reports whether n is an insertion wrapper (w:ins or w:moveTo).
func IsRevIns(n *tree.Node) bool { return IsW(n, "ins") || IsW(n, "moveTo") }

// IsRevDel reports whether n is a deletion wrapper (w:del or w:moveFrom).
func IsRevDel(n *tree.Node) bool { return IsW(n, "del") || IsW(n, "moveFrom") }

// markRPr returns the paragraph mark's run properties, w:pPr/w:rPr, or nil.
func markRPr(p *tree.Node) *tree.Node {
	ppr := ChildW(p, "pPr")
	if ppr == nil {
		return nil
	}
	return ChildW(ppr, "rPr")
}

// ParagraphMarkIns returns the paragraph mark's insertion marker
// (w:pPr/w:rPr/w:ins or w:moveTo), or nil.
func ParagraphMarkIns(p *tree.Node) *tree.Node {
	rpr := markRPr(p)
	if rpr == nil {
		return nil
	}
	if m := ChildW(rpr, "ins"); m != nil {
		return m
	}
	return ChildW(rpr, "moveTo")
}

// ParagraphMarkDel returns the paragraph mark's deletion marker
// (w:pPr/w:rPr/w:del or w:moveFrom), or nil.
func ParagraphMarkDel(p *tree.Node) *tree.Node {
	rpr := markRPr(p)
	if rpr == nil {
		return nil
	}
	if m := ChildW(rpr, "del"); m != nil {
		return m
	}
	return ChildW(rpr, "moveFrom")
}

// ParagraphJoinTarget returns the paragraph a removed paragraph mark joins
// p into: the next block past milestones, when it is a paragraph.
func ParagraphJoinTarget(p *tree.Node) *tree.Node {
	for n := p.NextSibling(); n != nil; n = n.NextSibling() {
		if !IsElement(n) || isMilestone(n) {
			continue
		}
		if IsW(n, "p") {
			return n
		}
		return nil
	}
	return nil
}

// RejectedInsertionVanishes reports whether rejecting p's inserted mark
// removes the paragraph outright: nothing joins it, everything in it (and
// in the run of joined insertions after it) is inserted, and its story
// stays well formed without it.
func RejectedInsertionVanishes(p *tree.Node) bool {
	cur := p
	for {
		if ParagraphMarkIns(cur) == nil || !whollyInserted(cur) {
			return false
		}
		next := ParagraphJoinTarget(cur)
		if next == nil {
			break
		}
		cur = next
	}
	return storySurvivesWithout(p) && storySurvivesWithout(cur)
}

// whollyInserted reports whether every element child of p is pPr or an insertion wrapper.
func whollyInserted(p *tree.Node) bool {
	for c := range p.ChildrenSeq() {
		if IsElement(c) && !IsW(c, "pPr") && !IsRevIns(c) {
			return false
		}
	}
	return true
}

// storySurvivesWithout reports whether the story stays well formed without p.
func storySurvivesWithout(p *tree.Node) bool {
	parent := p.Parent()
	if parent == nil {
		return false
	}
	block := func(n *tree.Node) bool { return IsElement(n) && !isMilestone(n) }
	for n := p.PrevSibling(); n != nil; n = n.PrevSibling() {
		if block(n) {
			if IsW(n, "p") {
				return true
			}
			break
		}
	}
	if IsW(parent, "body") {
		return false
	}
	for n := p.NextSibling(); n != nil; n = n.NextSibling() {
		if block(n) {
			return IsW(n, "p") || IsW(n, "tbl")
		}
	}
	return false
}

// RowRevision returns a table row's w:trPr/w:ins or w:del marker, or nil.
func RowRevision(row *tree.Node) *tree.Node {
	trpr := ChildW(row, "trPr")
	if trpr == nil {
		return nil
	}
	if m := ChildW(trpr, "ins"); m != nil {
		return m
	}
	return ChildW(trpr, "del")
}

// RowGoes reports whether settling removes row: a deleted row on accept,
// an inserted row on reject.
func RowGoes(row *tree.Node, accept bool) bool {
	m := RowRevision(row)
	return m != nil && IsW(m, "del") == accept
}

// ParagraphText returns the paragraph's text in view. Text, breaks and
// equations count as LeafText says. Nested story
// paragraphs and paragraph properties contribute nothing; of an
// mc:AlternateContent only the effective branch does.
func ParagraphText(p *tree.Node, v View) string {
	var b strings.Builder
	collectText(p, v, false, false, &b)
	return b.String()
}

// collectText appends the text of n's descendants in view v to b, honouring insertions and deletions.
func collectText(n *tree.Node, v View, inIns, inDel bool, b *strings.Builder) {
	for c := range n.ChildrenSeq() {
		switch {
		case !IsElement(c), IsW(c, "p"), IsW(c, "pPr"):
		case IsRevIns(c):
			collectText(c, v, true, inDel, b)
		case IsRevDel(c):
			collectText(c, v, inIns, true, b)
		case isLeaf(c):
			text, deleted, _ := LeafText(c)
			if !deleted && v == Current && !inDel || v == Baseline && !inIns || v == All {
				b.WriteString(text)
			}
		case IsLocal(c, "AlternateContent"):
			if br := AlternateContentBranch(c); br != nil {
				collectText(br, v, inIns, inDel, b)
			}
		default:
			collectText(c, v, inIns, inDel, b)
		}
	}
}

// revisionIDTags are the element families carrying a revision w:id; the
// last four, the move range markers, pair a start and an end under one id.
var revisionIDTags = []string{
	"ins", "del", "cellIns", "cellDel", "cellMerge", "moveFrom", "moveTo", "rPrChange",
	"pPrChange", "tblPrChange", "trPrChange", "tcPrChange", "sectPrChange", "tblGridChange",
	"numberingChange", "tblPrExChange",
	"moveFromRangeStart", "moveFromRangeEnd", "moveToRangeStart", "moveToRangeEnd",
}

const revisionRangeMarkers = 16

// isRevisionTag reports whether n is a WordprocessingML element named in tags.
func isRevisionTag(n *tree.Node, tags []string) bool {
	return slices.ContainsFunc(tags, func(t string) bool { return IsW(n, t) })
}

// RevisionElements returns the revision elements carrying a w:id, except
// the move range markers, in document order.
func (x *XML) RevisionElements() []*tree.Node {
	r := x.Root()
	if r == nil {
		return nil
	}
	return Descendants(r, func(n *tree.Node) bool {
		if !isRevisionTag(n, revisionIDTags[:revisionRangeMarkers]) {
			return false
		}
		_, ok := Attr(n, "id")
		return ok
	})
}

// MaxRevisionID returns the highest numeric revision id in the part (body,
// else the document element), and false when there is none.
func (x *XML) MaxRevisionID() (uint64, bool) {
	scope := x.Body()
	if scope == nil {
		scope = x.Root()
	}
	if scope == nil {
		return 0, false
	}
	var best uint64
	found := false
	scope.Walk(func(n *tree.Node) bool {
		if n != scope && isRevisionTag(n, revisionIDTags) {
			if v, ok := Attr(n, "id"); ok {
				if id, err := strconv.ParseUint(v, 10, 64); err == nil {
					best, found = max(best, id), true
				}
			}
		}
		return true
	})
	return best, found
}
