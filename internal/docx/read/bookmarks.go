package read

import (
	"slices"
	"strings"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// storyRelationships are the relationship types (by their last path
// segment) of the stories besides the body, in listing order.
var storyRelationships = []string{"header", "footer", "footnotes", "endnotes", "comments"}

// bookmarkTextLimit caps the characters of bookmarked text listed.
const bookmarkTextLimit = 200

// storyParts returns the other story parts the main document relates to,
// in relationship-type order, then relationship order: headers, footers,
// footnotes, endnotes and comments. Targets are joined to the document's
// directory as written.
func storyParts(pkg *model.Package) []string {
	main := pkg.DocumentPartName()
	x, ok := pkg.Part(model.RelsPartFor(main))
	if !ok || x.Root() == nil {
		return nil
	}
	var parts []string
	for _, kind := range storyRelationships {
		for rel := range x.Root().ChildrenSeq() {
			if !model.IsLocal(rel, "Relationship") || model.AttrOr(rel, "TargetMode") == "External" {
				continue
			}
			t, ok := model.Attr(rel, "Type")
			if !ok || t[strings.LastIndexByte(t, '/')+1:] != kind {
				continue
			}
			target, ok := model.Attr(rel, "Target")
			if !ok {
				continue
			}
			// Resolved like every other target ("." and ".." segments);
			// docxdriver joins it to the directory as written, so a target
			// such as "./header1.xml" names no part there.
			part := model.ResolveRelTarget(main, target)
			if pkg.Has(part) && !slices.Contains(parts, part) {
				parts = append(parts, part)
			}
		}
	}
	return parts
}

type bookmarkEntry struct {
	name, id, part string
	start, end     *string
	startPending   bool
	text           strings.Builder
	chars          int
	truncated      bool
}

// push appends s to the entry text until the character limit.
func (e *bookmarkEntry) push(s string) {
	for _, r := range s {
		if e.chars >= bookmarkTextLimit {
			e.truncated = true
			return
		}
		e.text.WriteRune(r)
		e.chars++
	}
}

type bookmarkWalk struct {
	part     string
	paraID   func(*tree.Node) *string
	entries  []*bookmarkEntry
	open     []int
	lastPara *string
}

// node walks children of n; para is the enclosing paragraph's id when
// inside one (inPara). It recurses only over a tree the tokenizer built.
func (w *bookmarkWalk) node(n *tree.Node, para *string, inPara bool) {
	for c := range n.ChildrenSeq() {
		if !model.IsElement(c) {
			continue
		}
		switch {
		case model.IsW(c, "p"):
			id := w.paraID(c)
			for _, i := range w.open {
				e := w.entries[i]
				if e.startPending {
					e.start, e.startPending = id, false
				} else if e.text.Len() > 0 && !strings.HasSuffix(e.text.String(), "\n") {
					e.push("\n")
				}
			}
			w.node(c, id, true)
			w.lastPara = id
			continue
		case model.IsW(c, "bookmarkStart"):
			e := &bookmarkEntry{
				name: model.AttrOr(c, "name"), id: model.AttrOr(c, "id"), part: w.part,
				startPending: !inPara,
			}
			if inPara {
				e.start = para
			}
			w.open = append(w.open, len(w.entries))
			w.entries = append(w.entries, e)
			continue
		case model.IsW(c, "bookmarkEnd"):
			id := model.AttrOr(c, "id")
			for pos, i := range w.open {
				e := w.entries[i]
				if e.id != id {
					continue
				}
				w.open = append(w.open[:pos], w.open[pos+1:]...)
				e.end = w.lastPara
				if inPara {
					e.end = para
				}
				if e.startPending {
					e.start, e.startPending = e.end, false
				}
				break
			}
			continue
		case model.IsW(c, "pPr"), model.IsW(c, "rPr"), model.IsW(c, "delText"):
			continue
		}
		var text string
		has := true
		switch {
		case model.IsW(c, "t"):
			text = model.TextContent(c)
		case model.IsW(c, "tab") && model.IsW(c.Parent(), "r"):
			text = "\t"
		case (model.IsW(c, "br") || model.IsW(c, "cr")) && model.IsW(c.Parent(), "r"):
			text = "\n"
		default:
			has = false
		}
		if has {
			for _, i := range w.open {
				w.entries[i].push(text)
			}
			continue
		}
		if model.IsLocal(c, "AlternateContent") {
			if b := model.AlternateContentBranch(c); b != nil {
				w.node(b, para, inPara)
			}
			continue
		}
		w.node(c, para, inPara)
	}
}

// walkStory returns one story's bookmark entries in document order.
func walkStory(x *model.XML, part string, paraID func(*tree.Node) *string) []*bookmarkEntry {
	r := x.Root()
	if r == nil {
		return nil
	}
	w := &bookmarkWalk{part: part, paraID: paraID}
	w.node(r, nil, false)
	return w.entries
}

// listBookmarks is the bookmarks read kind: every bookmark of every story
// with its name, w:id, whether it is hidden, its start and end paragraph
// ids, its current-view text (at most 200 characters, then "…") and part.
// Body paragraphs have their resolved addresses; other stories' paragraphs
// their w14:paraId as written, uppercased.
func listBookmarks(d *model.Document) Object {
	pkg, x := d.Pkg, d.XML
	main := pkg.DocumentPartName()
	idx := d.ParaIDs()
	entries := walkStory(x, main, func(n *tree.Node) *string {
		if id, ok := idx.IDs[n]; ok {
			return &id.ID
		}
		return nil
	})
	for _, part := range storyParts(pkg) {
		sx, ok := pkg.Part(part)
		if !ok {
			continue
		}
		entries = append(entries, walkStory(sx, part, func(n *tree.Node) *string {
			if v, ok := model.Attr(n, "paraId"); ok {
				u := rustUpper(v)
				return &u
			}
			return nil
		})...)
	}
	list := []any{}
	for _, e := range entries {
		text := strings.TrimRight(e.text.String(), "\n")
		if e.truncated {
			text += "…"
		}
		list = append(list, Object{
			"id": "bookmark:" + e.name, "name": e.name, "ooxml_id": e.id, "hidden": strings.HasPrefix(e.name, "_"),
			"start": optString(e.start), "end": optString(e.end), "text": text, "part": e.part,
		})
	}
	return Object{"bookmarks": list}
}

// optString returns an optional string as a JSON value, or nil.
func optString(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

// rustUpper uppercases s as Rust's str::to_uppercase does for the
// characters whose uppercase is longer than one character and that a
// paragraph id could plausibly hold (ß and the Latin ligatures); every
// other character takes its simple uppercase mapping.
func rustUpper(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case 'ß':
			b.WriteString("SS")
		case 'ﬀ':
			b.WriteString("FF")
		case 'ﬁ':
			b.WriteString("FI")
		case 'ﬂ':
			b.WriteString("FL")
		case 'ﬃ':
			b.WriteString("FFI")
		case 'ﬄ':
			b.WriteString("FFL")
		case 'ﬅ', 'ﬆ':
			b.WriteString("ST")
		case 'ŉ':
			b.WriteString("ʼN")
		case 'ǰ':
			b.WriteString("J̌")
		default:
			b.WriteString(strings.ToUpper(string(r)))
		}
	}
	return b.String()
}
