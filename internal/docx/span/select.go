package span

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// ErrRange is returned for a character range outside a paragraph's text.
var ErrRange = errors.New("span: range outside the paragraph's text")

// Para is one paragraph of a working tree, read for selection: its
// segments (model.BuildSegments) and, per segment, whether each
// tracked-change view shows it. It is a snapshot: after a mutation, read
// the paragraph again.
type Para struct {
	Node *tree.Node
	// Segs are the paragraph's text leaves in document order, with their
	// all-view offsets.
	Segs  []model.Segment
	shown [][3]bool // per segment, per view
}

// Read reads paragraph p (a w:p of a working tree or of the read model).
func Read(p *tree.Node) *Para {
	segs := model.BuildSegments(p)
	shown := make([][3]bool, len(segs))
	for i, s := range segs {
		inIns, inDel := false, false
		for a := s.Leaf.Parent(); a != nil && a != p; a = a.Parent() {
			inIns = inIns || model.IsRevIns(a)
			inDel = inDel || model.IsRevDel(a)
		}
		_, deleted, _ := model.LeafText(s.Leaf)
		// The rule of model.ParagraphText.
		shown[i] = [3]bool{
			model.Current:  !deleted && !inDel,
			model.Baseline: !inIns,
			model.All:      true,
		}
	}
	return &Para{Node: p, Segs: segs, shown: shown}
}

// Len returns the length of the paragraph's all-view text in characters.
func (p *Para) Len() int {
	if len(p.Segs) == 0 {
		return 0
	}
	return p.Segs[len(p.Segs)-1].End
}

// Shows reports whether view v shows segment i.
func (p *Para) Shows(v model.View, i int) bool { return p.shown[i][v] }

// Text returns the paragraph's text in view v: model.ParagraphText's.
func (p *Para) Text(v model.View) string {
	var b strings.Builder
	for i, s := range p.Segs {
		if p.shown[i][v] {
			b.WriteString(s.Text)
		}
	}
	return b.String()
}

// toAll maps each character of view v's text to its all-view offset.
func (p *Para) toAll(v model.View) []int {
	var out []int
	for i, s := range p.Segs {
		if !p.shown[i][v] {
			continue
		}
		for k := range utf8.RuneCountInString(s.Text) {
			out = append(out, s.Start+k)
		}
	}
	return out
}

// Span is a character range [Start, End) of a paragraph in its all-view
// offsets, where deleted text still occupies positions (the offsets of
// read's revision and comment locators).
type Span struct {
	Para       *tree.Node
	Start, End int
}

// Empty reports whether the span covers no character (an insertion
// point).
func (s Span) Empty() bool { return s.Start >= s.End }

// Select maps characters [start, end) of view v's text to a span. A
// non-empty range runs from the all-view position of its first character
// to just after its last, so content view v hides between them is inside;
// an empty range is the point before character start of the view (after
// its last character at the end).
func (p *Para) Select(v model.View, start, end int) (Span, error) {
	to := p.toAll(v)
	if start < 0 || end < start || end > len(to) {
		return Span{}, fmt.Errorf("%w: [%d, %d) of %d characters", ErrRange, start, end, len(to))
	}
	switch {
	case start < end:
		return Span{p.Node, to[start], to[end-1] + 1}, nil
	case start < len(to):
		return Span{p.Node, to[start], to[start]}, nil
	case len(to) > 0:
		return Span{p.Node, to[len(to)-1] + 1, to[len(to)-1] + 1}, nil
	}
	return Span{p.Node, 0, 0}, nil
}

// SelectAll returns the span of view v's whole text (a whole-paragraph
// anchor).
func (p *Para) SelectAll(v model.View) Span {
	n := len(p.toAll(v))
	s, err := p.Select(v, 0, n)
	if err != nil {
		return Span{p.Node, 0, 0} // unreachable: [0, n) is in range
	}
	return s
}

// Match is one occurrence of a needle in a view's text: its character
// range in that view and its span.
type Match struct {
	Start, End int // in the view's text
	Span       Span
}

// Find returns the non-overlapping occurrences of needle in view v's
// text, left to right (as find and docxdriver's select count them). An
// empty needle matches nothing.
func (p *Para) Find(v model.View, needle string) []Match {
	if needle == "" {
		return nil
	}
	text := p.Text(v)
	n := utf8.RuneCountInString(needle)
	var out []Match
	from, chars := 0, 0
	for {
		i := strings.Index(text[from:], needle)
		if i < 0 {
			return out
		}
		chars += utf8.RuneCountInString(text[from : from+i])
		s, err := p.Select(v, chars, chars+n)
		if err != nil {
			return out // unreachable: a match lies inside the text
		}
		out = append(out, Match{chars, chars + n, s})
		chars += n
		from += i + len(needle)
	}
}

// Offsets maps each character of view v's text to its all-view offset.
func (p *Para) Offsets(v model.View) []int { return p.toAll(v) }

// Point returns the all-view insertion point just after the first n
// characters of view v's text: right after the n-th character (before
// any content the view hides after it), or before the first character
// when n is 0 (after any hidden content before it). With no character
// in the view it is the end of the paragraph.
func (p *Para) Point(v model.View, n int) (int, error) {
	to := p.toAll(v)
	switch {
	case n < 0 || n > len(to):
		return 0, fmt.Errorf("%w: point %d of %d characters", ErrRange, n, len(to))
	case n > 0:
		return to[n-1] + 1, nil
	case len(to) > 0:
		return to[0], nil
	}
	return p.Len(), nil
}
