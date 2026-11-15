package revise

import (
	"slices"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/schema"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// propChanges are the property-change records and the snapshot each keeps
// of the properties before the change (numberingChange keeps none).
var propChanges = map[string]string{
	"rPrChange": "rPr", "pPrChange": "pPr", "sectPrChange": "sectPr", "tblPrChange": "tblPr",
	"trPrChange": "trPr", "tcPrChange": "tcPr", "tblGridChange": "tblGrid", "numberingChange": "",
}

// isRecord reports whether n is a property-change record, by local name
// in any namespace, as docxdriver matches revision kinds.
func isRecord(n *tree.Node) bool {
	if !model.IsElement(n) {
		return false
	}
	_, ok := propChanges[n.Local()]
	return ok
}

// nextRecord returns the first record at or after from in document order,
// within scope, that ok accepts.
func nextRecord(from, scope *tree.Node, ok func(*tree.Node) bool) *tree.Node {
	var found *tree.Node
	visit := func(n *tree.Node) bool {
		if found != nil {
			return false
		}
		if n != scope && isRecord(n) && ok(n) {
			found = n
			return false
		}
		return true
	}
	from.Walk(visit)
	for n := from; found == nil && n != scope && n.Parent() != nil; n = n.Parent() {
		for s := n.NextSibling(); s != nil; s = s.NextSibling() {
			if found != nil {
				break
			}
			s.Walk(visit)
		}
	}
	return found
}

// properties settles the selected property-change records one at a time,
// first first: accept drops the record, reject restores its snapshot into
// the live container. Resolving a record changes only its container, so
// the scan resumes at the container's parent; rejecting can bring a
// record out of a snapshot (an rPrChange in a pPrChange's snapshot), which
// the scan then meets.
func (s *settler) properties(scope *tree.Node) error {
	from := scope
	for {
		r := nextRecord(from, scope, s.picked)
		if r == nil {
			return nil
		}
		from = scope
		if p := r.Parent(); p != nil && p.Parent() != nil {
			from = p.Parent()
		}
		if err := s.resolveProperty(r); err != nil {
			return err
		}
		s.count++
	}
}

// resolveProperty settles one property-change record (revisions.rs
// resolve_property_change).
func (s *settler) resolveProperty(r *tree.Node) error {
	inner := propChanges[r.Local()]
	parent := r.Parent()
	if s.accept || inner == "" || parent == nil {
		return s.remove(r)
	}
	snapshot := model.ChildW(r, inner)
	if snapshot == nil {
		return s.remove(r)
	}
	old := slices.Clone(snapshot.Children())
	// A paragraph mark's rPr snapshot can hold the mark's own revision
	// (w:del, w:ins, w:moveFrom, w:moveTo): restoring it would bring back a
	// revision this settle already resolved, so reject-all would leave one.
	// docxdriver restores it (n830205.docx keeps a w:del); it is dropped.
	// The mark's current revision, when this settle leaves it pending
	// (another author's, under a narrowed selection), stays in place: it
	// is CT_ParaRPr's first child, before the restored properties.
	markRevision := func(c *tree.Node) bool {
		return model.IsW(c, "ins") || model.IsW(c, "del") || model.IsW(c, "moveFrom") || model.IsW(c, "moveTo")
	}
	if inner == "rPr" {
		old = slices.DeleteFunc(old, markRevision)
	}
	current := slices.Clone(parent.Children())
	if inner == "rPr" {
		current = slices.DeleteFunc(current, markRevision)
	}
	var retained []*tree.Node
	for _, c := range current {
		if inner == "pPr" && (model.IsW(c, "rPr") || model.IsW(c, "sectPr")) ||
			inner == "sectPr" && (model.IsW(c, "headerReference") || model.IsW(c, "footerReference")) {
			retained = append(retained, c)
		}
	}
	// Imported documents can put sectPr before rPr: the CT_PPr tail goes
	// back in schema order.
	slices.SortStableFunc(retained, func(a, b *tree.Node) int {
		return boolInt(model.IsW(a, "sectPr")) - boolInt(model.IsW(b, "sectPr"))
	})
	appendAll := func(nodes []*tree.Node) error {
		for _, c := range nodes {
			if c.Kind() != tree.ElementNode {
				continue
			}
			if err := s.h.Append(parent, c); err != nil {
				return wrap(err)
			}
		}
		return nil
	}
	if inner == "sectPr" {
		if err := appendAll(retained); err != nil {
			return err
		}
	}
	if err := appendAll(old); err != nil {
		return err
	}
	for _, c := range current {
		if !slices.Contains(retained, c) {
			if err := s.remove(c); err != nil {
				return err
			}
		}
	}
	if inner != "sectPr" {
		if err := appendAll(retained); err != nil {
			return err
		}
	}
	// A row or cell that had no properties before the change gets none
	// back, rather than an empty optional container.
	if (inner == "trPr" || inner == "tcPr") && len(parent.Children()) == 0 && len(parent.Attrs()) == 0 {
		return s.remove(parent)
	}
	return nil
}

// boolInt returns 1 for true and 0 for false.
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// recordAttrs are the attributes of a new property-change record named
// record under bag: w:id, then w:author and w:date where the record's
// schema type has them (w:tblGridChange is CT_Markup: an id alone).
func recordAttrs(bag *tree.Node, record, author, date string) [][2]string {
	out := [][2]string{{"w:id", "0"}}
	typ := ""
	if bt, ok := docxedit.Type(bag); ok {
		typ, _ = schema.ChildType(bt, "w:"+record)
	}
	for _, a := range [][2]string{{"author", author}, {"date", date}} {
		if _, ok := schema.AttrType(typ, a[0]); ok || typ == "" {
			out = append(out, [2]string{"w:" + a[0], a[1]})
		}
	}
	return out
}
