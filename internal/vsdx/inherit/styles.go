package inherit

import (
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// styleCell walks the style chain of category c from style id, at most
// MaxStyleDepth steps. Chunk H3.
func (r *Resolver) styleCell(c vsd.Category, id uint32, k sheet.Key) (sheet.Cell, bool) {
	if c == vsd.PageCategory {
		for _, sub := range [3]vsd.Category{vsd.LineCategory, vsd.FillCategory, vsd.TextCategory} {
			if cell, ok := r.styleCell(sub, id, k); ok {
				return cell, true
			}
		}
		return sheet.Cell{}, false
	}
	if c == vsd.NoStyle {
		return sheet.Cell{}, false
	}
	var found sheet.Cell
	ok := false
	r.styleWalk(c, id, func(sh *sheet.Sheet) bool {
		if sh == nil {
			return false
		}
		if sh.Deleted(k) {
			return true
		}
		if cell, present := sh.Cell(k); present {
			found, ok = cell, true
			return true
		}
		return false
	})
	return found, ok
}

// styleWalk visits the sheets of the style chain of category c from id
// in chain order, stopping when visit returns true, when the chain
// ends, when a style repeats (a cycle) or when it would pass
// MaxStyleDepth sheets. A cycle or an over-long chain is one note and
// stops the walk; a style id the document does not hold ends the walk
// silently. It reports whether visit stopped it. Chunk H3.
func (r *Resolver) styleWalk(c vsd.Category, id uint32, visit func(*sheet.Sheet) bool) bool {
	if r == nil || r.doc == nil || visit == nil {
		return false
	}
	at := r.doc.Part
	var visited [MaxStyleDepth]uint32
	n := 0
	cur := id
	for {
		for _, seen := range visited[:n] {
			if seen == cur {
				r.noteOnce(at, "style cycle "+chainText(visited[:n], cur))
				return false
			}
		}
		if n >= MaxStyleDepth {
			r.noteOnce(at, "style chain too deep "+chainText(visited[:n], cur))
			return false
		}
		visited[n] = cur
		n++
		sh, ss, ok := r.styleSheet(cur)
		if !ok {
			return false
		}
		if visit(sh) {
			return true
		}
		parent, ok := styleParent(ss, c)
		if !ok {
			return false
		}
		cur = parent
	}
}

// styleSheet returns the memoised sheet and the StyleSheet of style id,
// indexing the style sheet once per id. It reports false when the
// document holds no style sheet of that id. Chunk H3.
func (r *Resolver) styleSheet(id uint32) (*sheet.Sheet, *doc.StyleSheet, bool) {
	if r == nil || r.doc == nil {
		return nil, nil, false
	}
	ss, ok := r.doc.Styles[id]
	if !ok || ss == nil {
		return nil, nil, false
	}
	if r.styles == nil {
		r.styles = map[uint32]*sheet.Sheet{}
	}
	if sh, ok := r.styles[id]; ok {
		return sh, ss, true
	}
	sh, _ := sheet.Index(ss.Node)
	r.styles[id] = sh
	return sh, ss, true
}

// styleParent returns the parent style id of ss of category c, and
// false when ss names none for c. Chunk H3.
func styleParent(ss *doc.StyleSheet, c vsd.Category) (uint32, bool) {
	if ss == nil {
		return 0, false
	}
	switch c {
	case vsd.LineCategory:
		if ss.HasLine {
			return ss.LineStyle, true
		}
	case vsd.FillCategory:
		if ss.HasFill {
			return ss.FillStyle, true
		}
	case vsd.TextCategory:
		if ss.HasText {
			return ss.TextStyle, true
		}
	}
	return 0, false
}

// noteOnce appends the note (at, reason) to the resolver unless it is
// already there, so a cycle met by many cells is noted once. Chunk H3.
func (r *Resolver) noteOnce(at, reason string) {
	for _, n := range r.notes {
		if n.At == at && n.Reason == reason {
			return
		}
	}
	r.notes = append(r.notes, doc.Note{At: at, Reason: reason})
}

// chainText spells a style chain as its stored ids joined by " -> ",
// ending with extra. Chunk H3.
func chainText(visited []uint32, extra uint32) string {
	var b strings.Builder
	ids := append(visited, extra)
	for i, id := range ids {
		if i > 0 {
			b.WriteString(" -> ")
		}
		b.WriteString(strconv.FormatUint(uint64(id), 10))
	}
	return b.String()
}
