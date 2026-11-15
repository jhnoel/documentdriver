package model

import (
	"strings"
	"unicode/utf8"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// RevWrap is the tracked-change wrapper a segment sits in.
type RevWrap int

// The wrappers.
const (
	WrapNone RevWrap = iota
	WrapIns
	WrapDel
)

// Segment is one text-bearing leaf (see LeafText) of a paragraph with
// its character span in the paragraph's all-view text, where deleted text
// still occupies positions. Offsets count Unicode scalar values.
type Segment struct {
	Text    string
	Leaf    *tree.Node // the w:t, w:delText, w:br, w:cr or equation element
	Run     *tree.Node // the enclosing w:r, or nil (see BuildSegments)
	Wrapper *tree.Node // the enclosing w:ins/w:del wrapper, or nil
	Wrap    RevWrap
	Start   int
	End     int
}

// BuildSegments returns the paragraph's segments in document order. Nested
// story paragraphs and paragraph properties occupy no offsets; of an
// mc:AlternateContent only the effective branch does.
//
// docxdriver 0.5.2 panics ("text element inside a run") on a w:t or
// w:delText that is not inside a w:r of the paragraph; here such a leaf
// gets a nil Run instead.
func BuildSegments(p *tree.Node) []Segment {
	var out []Segment
	off := 0
	collectSegments(p, nil, WrapNone, &off, &out)
	return out
}

// collectSegments appends the leaf segments of n to out, advancing off by rune counts.
func collectSegments(n, wrapper *tree.Node, wrap RevWrap, off *int, out *[]Segment) {
	for c := range n.ChildrenSeq() {
		switch {
		case !IsElement(c), IsW(c, "p"), IsW(c, "pPr"):
		case IsRevIns(c):
			collectSegments(c, c, WrapIns, off, out)
		case IsRevDel(c):
			collectSegments(c, c, WrapDel, off, out)
		case isLeaf(c):
			text, _, _ := LeafText(c)
			n := utf8.RuneCountInString(text)
			*out = append(*out, Segment{
				Text: text, Leaf: c, Run: enclosingRun(c), Wrapper: wrapper, Wrap: wrap,
				Start: *off, End: *off + n,
			})
			*off += n
		case IsLocal(c, "AlternateContent"):
			if b := AlternateContentBranch(c); b != nil {
				collectSegments(b, wrapper, wrap, off, out)
			}
		default:
			collectSegments(c, wrapper, wrap, off, out)
		}
	}
}

// enclosingRun returns the nearest w:r ancestor of n below its paragraph.
func enclosingRun(n *tree.Node) *tree.Node {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if IsW(p, "r") {
			return p
		}
		if IsW(p, "p") {
			return nil
		}
	}
	return nil
}

// AllText concatenates the segments' text.
func AllText(segs []Segment) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteString(s.Text)
	}
	return b.String()
}

// CharSlice returns the characters [start, end) of s, counting Unicode
// scalar values; out-of-range bounds are clipped.
func CharSlice(s string, start, end int) string {
	if end <= start {
		return ""
	}
	i, b0, b1 := 0, len(s), len(s)
	for pos := range s {
		if i == start {
			b0 = pos
		}
		if i == end {
			b1 = pos
			break
		}
		i++
	}
	if b0 > b1 {
		return ""
	}
	return s[b0:b1]
}
