package read

import (
	"strings"
	"unicode/utf8"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// commentSpan is a comment range in one paragraph's all-view offsets.
type commentSpan struct {
	id         string
	start, end int
}

// documentCommentAnchors maps each comment id to its ranges, paragraph by
// paragraph, in the final-view selection space (commands/reading.rs
// comment_anchors): the paragraph address, start and end, the selected
// text and its occurrence, the all-view locator, and the marked text when
// it differs from the selection. A range open at a paragraph's end
// continues at the start of the next.
func documentCommentAnchors(st *model.Document) map[string][]Object {
	ids := st.ParaIDs()
	out := map[string][]Object{}
	var active []commentSpan // open ranges: id and start, in opening order
	for _, p := range st.XML.Paragraphs() {
		for i := range active {
			active[i].start = 0
		}
		off := 0
		var spans []commentSpan
		walkAnchors(p.Node, &off, &active, &spans)
		for _, a := range active {
			spans = append(spans, commentSpan{a.id, a.start, off})
		}
		id, ok := ids.IDs[p.Node]
		if !ok {
			continue
		}
		segs := model.BuildSegments(p.Node)
		current := currentText(segs)
		all := model.AllText(segs)
		toCurrent := func(b int) int {
			n := 0
			for _, s := range segs {
				if s.Wrap != model.WrapDel {
					n += max(min(b, s.End)-s.Start, 0)
				}
			}
			return n
		}
		for _, sp := range spans {
			start, end := toCurrent(sp.start), toCurrent(sp.end)
			sel := model.CharSlice(current, start, end)
			var occurrence any
			if sel != "" {
				occurrence = occurrenceAt(current, sel, start)
			}
			r := Object{"at": id.ID, "start": start, "end": end, "select": sel, "occurrence": occurrence}
			if marked := model.CharSlice(all, sp.start, sp.end); marked != sel {
				r["marked_text"] = marked
			}
			out[sp.id] = append(out[sp.id], r)
		}
	}
	return out
}

// occurrenceAt is the 1-based index of the non-overlapping match of sel in
// s that starts at character start, or nil when none does.
func occurrenceAt(s, sel string, start int) any {
	n, from := 0, 0
	for {
		i := strings.Index(s[from:], sel)
		if i < 0 {
			return nil
		}
		n++
		at := from + i
		if utf8.RuneCountInString(s[:at]) == start {
			return n
		}
		from = at + len(sel)
	}
}

// walkAnchors records comment ranges and counts all-view text offsets
// below n, skipping nested paragraphs and paragraph properties. It
// recurses only over a tree the tokenizer built.
func walkAnchors(n *tree.Node, off *int, active, spans *[]commentSpan) {
	for c := range n.ChildrenSeq() {
		switch {
		case model.IsW(c, "p"), model.IsW(c, "pPr"):
		case model.IsW(c, "commentRangeStart"):
			if id, ok := model.Attr(c, "id"); ok {
				found := false
				for i := range *active {
					if (*active)[i].id == id {
						(*active)[i].start, found = *off, true
					}
				}
				if !found {
					*active = append(*active, commentSpan{id: id, start: *off})
				}
			}
		case model.IsW(c, "commentRangeEnd"):
			if id, ok := model.Attr(c, "id"); ok {
				for i, a := range *active {
					if a.id == id {
						*spans = append(*spans, commentSpan{id, a.start, *off})
						*active = append((*active)[:i], (*active)[i+1:]...)
						break
					}
				}
			}
		case isLeafText(c):
			text, _, _ := model.LeafText(c)
			*off += utf8.RuneCountInString(text)
		case model.IsLocal(c, "AlternateContent"):
			if b := model.AlternateContentBranch(c); b != nil {
				walkAnchors(b, off, active, spans)
			}
		default:
			walkAnchors(c, off, active, spans)
		}
	}
}

// isLeafText reports whether n is a text leaf element.
func isLeafText(n *tree.Node) bool {
	_, _, ok := model.LeafText(n)
	return ok
}
