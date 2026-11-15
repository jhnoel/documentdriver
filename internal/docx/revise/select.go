package revise

import (
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Selection is the Options.Select of a settle narrowed to some
// paragraphs, to one author's revisions, or to both: paragraphs, when
// not nil, keeps the revisions that belong to one of them (Owners);
// author, when not nil, keeps those whose w:author is *author (and a
// table's grid change with that author's table changes). With
// neither it returns nil, which selects every revision.
func Selection(paragraphs map[*tree.Node]bool, author *string) func(*tree.Node) bool {
	if paragraphs == nil && author == nil {
		return nil
	}
	return func(n *tree.Node) bool {
		if author != nil && !byAuthor(n, *author) {
			return false
		}
		if paragraphs == nil {
			return true
		}
		for _, p := range Owners(n) {
			if paragraphs[p] {
				return true
			}
		}
		return false
	}
}

// Owners are the paragraphs a revision element belongs to, nearest
// first: every w:p it sits in, so that a revision in a text box belongs
// to the text box's paragraph and to the story paragraph anchoring it
// (where the revision listing and Settle place it). Row and cell markers
// and table, row and cell property records belong to no paragraph (nil),
// even in a table inside a text box: settling them changes a whole row
// or cell. A paragraph's content wrappers, its mark's insertion or
// deletion and the property records in its w:pPr (its section's
// included) and runs belong to it.
func Owners(n *tree.Node) []*tree.Node {
	var out []*tree.Node
	for a := n.Parent(); a != nil; a = a.Parent() {
		switch {
		case model.IsW(a, "p"):
			out = append(out, a)
		case len(out) == 0 && (model.IsW(a, "tr") || model.IsW(a, "tc") || model.IsW(a, "tbl")):
			return nil
		}
	}
	return out
}

// tableRecords are the table, row and cell revisions a table's
// w:tblGridChange goes with.
var tableRecords = map[string]bool{
	"tblPrChange": true, "trPrChange": true, "tcPrChange": true,
	"cellIns": true, "cellDel": true, "cellMerge": true,
}

// byAuthor reports whether revision n is author's. A w:tblGridChange
// has no author (CT_Markup): it is the author's when its table has a
// table, row or cell revision of theirs, so that settling those (cell
// spans, inserted cells) settles the grid they were made against too.
func byAuthor(n *tree.Node, author string) bool {
	if !model.IsW(n, "tblGridChange") {
		return model.AttrOr(n, "author") == author
	}
	table := ancestor(n, "tbl")
	if table == nil {
		return false
	}
	return model.Descendants(table, func(d *tree.Node) bool {
		return model.IsElement(d) && model.IsW(d, d.Local()) && tableRecords[d.Local()] &&
			model.AttrOr(d, "author") == author && ancestor(d, "tbl") == table
	}) != nil
}
