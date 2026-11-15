package read

import (
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// propChanges are the property-change record elements.
var propChanges = []string{
	"rPrChange", "pPrChange", "sectPrChange", "tblPrChange", "trPrChange",
	"tcPrChange", "tblGridChange", "numberingChange",
}

// revisionKind is the kind name of a revision element by its local name,
// and false for other elements.
func revisionKind(n *tree.Node) (string, bool) {
	if !model.IsElement(n) {
		return "", false
	}
	l := n.Local()
	if l == "ins" || l == "del" || l == "moveFrom" || l == "moveTo" || slices.Contains(propChanges, l) {
		return l, true
	}
	return "", false
}

type revisionEntry struct {
	id, kind, author, date string
	paragraph              int
	locator, text          string
}

// entryFor returns a revision entry from node n's revision attributes.
func entryFor(n *tree.Node, kind string, paragraph int, locator, text string) revisionEntry {
	return revisionEntry{
		id: model.AttrOr(n, "id"), kind: kind, author: model.AttrOr(n, "author"),
		date: model.AttrOr(n, "date"), paragraph: paragraph, locator: locator, text: text,
	}
}

// collectRevisions lists the tracked changes of the story paragraphs
// under container: content wrappers (with their all-view span and text),
// paragraph-mark insertions and deletions, property changes, then table
// row insertions and deletions.
func collectRevisions(container *tree.Node) []revisionEntry {
	var out []revisionEntry
	for _, p := range model.ParagraphsIn(container) {
		segs := model.BuildSegments(p.Node)
		loc := func(a, b int) string {
			return "p" + strconv.Itoa(p.Index) + ":" + strconv.Itoa(a) + "-" + strconv.Itoa(b)
		}
		wrappers := model.Descendants(p.Node, func(n *tree.Node) bool {
			return (model.IsRevIns(n) || model.IsRevDel(n)) && !model.InsidePPr(n, p.Node)
		})
		for _, w := range wrappers {
			start, end, seen := 0, 0, false
			var text strings.Builder
			for _, s := range segs {
				if !model.Within(s.Leaf, w) {
					continue
				}
				if !seen {
					start, end, seen = s.Start, s.End, true
				} else {
					start, end = min(start, s.Start), max(end, s.End)
				}
				text.WriteString(s.Text)
			}
			fallback := "del"
			if model.IsRevIns(w) {
				fallback = "ins"
			}
			kind, ok := revisionKind(w)
			if !ok {
				kind = fallback
			}
			out = append(out, entryFor(w, kind, p.Index, loc(start, end), text.String()))
		}
		for _, m := range []*tree.Node{model.ParagraphMarkIns(p.Node), model.ParagraphMarkDel(p.Node)} {
			if m == nil {
				continue
			}
			kind, ok := revisionKind(m)
			if !ok {
				kind = "ins"
			}
			n := utf8.RuneCountInString(model.AllText(segs))
			out = append(out, entryFor(m, kind, p.Index, loc(n, n), ""))
		}
		props := model.Descendants(p.Node, func(n *tree.Node) bool {
			k, ok := revisionKind(n)
			return ok && k != "ins" && k != "del" && k != "moveFrom" && k != "moveTo"
		})
		for _, n := range props {
			kind, _ := revisionKind(n)
			out = append(out, entryFor(n, kind, p.Index, loc(0, 0), ""))
		}
	}
	rows := model.Descendants(container, func(n *tree.Node) bool { return model.IsW(n, "tr") })
	for _, row := range rows {
		m := model.RowRevision(row)
		if m == nil {
			continue
		}
		kind, ok := revisionKind(m)
		if !ok {
			kind = "ins"
		}
		out = append(out, entryFor(m, kind, 0, "row", ""))
	}
	return out
}

// listRevisions is the revisions read kind: the body's tracked changes,
// then those of every header and footer part the sections reference.
func listRevisions(pkg *model.Package, x *model.XML) Object {
	revisions := []any{}
	add := func(entries []revisionEntry, part string) {
		for _, e := range entries {
			o := Object{
				"id": "revision:" + e.id, "kind": e.kind, "author": e.author, "date": e.date,
				"paragraph_index": e.paragraph, "locator": e.locator, "text": e.text,
			}
			if part != "" {
				o["part"] = part
			}
			revisions = append(revisions, o)
		}
	}
	if b := x.Body(); b != nil {
		add(collectRevisions(b), "")
	}
	for _, name := range chromePartNames(pkg, x) {
		px, ok := pkg.Part(name)
		if !ok || px.Root() == nil {
			continue
		}
		add(collectRevisions(px.Root()), name)
	}
	return Object{"revisions": revisions}
}
