package revise

import (
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// isPropertyRecord reports whether n is a property-change record.
func isPropertyRecord(n *tree.Node) bool { return isRecord(n) }

// Revisions lists the revisions under scope in the order docxdriver's
// listing gives them (revisions.rs collect_revisions_in): per story
// paragraph its content wrappers, its paragraph-mark insertion and
// deletion, and the property records inside it; then the table rows'
// insertions and deletions. Records outside paragraphs (table, row, cell
// and section properties) and cell insertions and deletions are not
// listed: the listing does not show them and revision_settle cannot
// address them by id.
func Revisions(scope *tree.Node) []*tree.Node {
	var out []*tree.Node
	for _, p := range model.ParagraphsIn(scope) {
		out = append(out, model.Descendants(p.Node, func(n *tree.Node) bool {
			return (model.IsRevIns(n) || model.IsRevDel(n)) && !model.InsidePPr(n, p.Node)
		})...)
		for _, m := range []*tree.Node{model.ParagraphMarkIns(p.Node), model.ParagraphMarkDel(p.Node)} {
			if m != nil {
				out = append(out, m)
			}
		}
		out = append(out, model.Descendants(p.Node, isPropertyRecord)...)
	}
	for _, row := range model.Descendants(scope, func(n *tree.Node) bool { return model.IsW(n, "tr") }) {
		if m := model.RowRevision(row); m != nil {
			out = append(out, m)
		}
	}
	return out
}

// FindRevision returns the first revision Revisions lists under scope
// whose w:id is id, or nil.
func FindRevision(scope *tree.Node, id string) *tree.Node {
	for _, n := range Revisions(scope) {
		if v, ok := model.Attr(n, "id"); ok && v == id {
			return n
		}
	}
	return nil
}
