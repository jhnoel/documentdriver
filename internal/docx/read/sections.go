package read

import (
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/view"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// boundaryParagraph is the paragraph whose w:pPr holds sect, or nil.
func boundaryParagraph(sect *tree.Node) *tree.Node {
	ppr := sect.Parent()
	if ppr == nil {
		return nil
	}
	p := ppr.Parent()
	if p == nil || !model.IsW(p, "p") {
		return nil
	}
	return p
}

// sectionInventory lists the body's sections (commands/sections.rs
// inventory): handle (by the address of the paragraph ending it), the
// paragraph range, page size, margins, columns, page numbering, break
// type, title page and header and footer stories. A body without a final
// w:sectPr gets an implicit final section. With original, a tracked
// section property change's snapshot stands in.
func sectionInventory(st *model.Document, original bool) []any {
	x := st.XML
	ids := st.ParaIDs()
	chrome := view.ChromeMap(st.Pkg, x)
	paragraphs := x.Paragraphs()
	position := make(map[*tree.Node]int, len(paragraphs))
	for i, p := range paragraphs {
		position[p.Node] = i
	}
	values := []any{}
	start := 0
	for i, direct := range view.SectPrNodes(x) {
		anchor := boundaryParagraph(direct)
		var id any
		handle := "section:final"
		if anchor != nil {
			if pid, ok := ids.IDs[anchor]; ok {
				id = pid.ID
				handle = "section:" + pid.ID
			}
		}
		end := len(paragraphs)
		if anchor != nil {
			if pos, ok := position[anchor]; ok {
				end = pos + 1
			}
		}
		sect := direct
		if original {
			if ch := model.ChildW(direct, "sectPrChange"); ch != nil {
				if old := model.ChildW(ch, "sectPr"); old != nil {
					sect = old
				}
			}
		}
		attrs := func(name string) any {
			n := model.ChildW(sect, name)
			if n == nil {
				return nil
			}
			o := view.AttrMap(n)
			if name == "cols" {
				defs := []any{}
				for c := range n.ChildrenSeq() {
					if !model.IsW(c, "col") {
						continue
					}
					defs = append(defs, Object{"width_twips": attrOrNil(c, "w"), "spacing_twips": attrOrNil(c, "space")})
				}
				o["definitions"] = defs
			}
			return o
		}
		stories := []any{}
		if i < len(chrome) {
			for _, s := range chrome[i].Slots {
				stories = append(stories, Object{"scope": s.Scope, "kind": s.Kind, "part": s.Part, "explicit": s.Explicit, "inherited": !s.Explicit})
			}
		}
		var breakType any
		if t := model.ChildW(sect, "type"); t != nil {
			breakType = attrOrNil(t, "val")
		}
		firstPage := false
		if tp := model.ChildW(sect, "titlePg"); tp != nil {
			v, ok := model.Attr(tp, "val")
			firstPage = !ok || (v != "0" && v != "false" && v != "off")
		}
		values = append(values, Object{
			"id": handle, "index": i + 1, "boundary_after": id, "paragraph_start": start,
			"paragraph_end": end, "final": anchor == nil, "page_size": attrs("pgSz"),
			"margins": attrs("pgMar"), "columns": attrs("cols"), "page_numbering": attrs("pgNumType"),
			"break_type": breakType, "first_page": firstPage, "stories": stories,
		})
		start = end
	}
	if b := x.Body(); b == nil || model.ChildW(b, "sectPr") == nil {
		stories := []any{}
		if len(chrome) > 0 {
			for _, s := range chrome[len(chrome)-1].Slots {
				stories = append(stories, Object{"scope": s.Scope, "kind": s.Kind, "part": s.Part, "explicit": false, "inherited": true})
			}
		}
		values = append(values, Object{
			"id": "section:final", "index": len(values) + 1, "boundary_after": nil,
			"paragraph_start": start, "paragraph_end": len(paragraphs), "final": true, "implicit": true,
			"page_size": nil, "margins": nil, "columns": nil, "page_numbering": nil, "break_type": nil,
			"first_page": false, "stories": stories,
		})
	}
	return values
}

// attrOrNil returns a node's attribute, or nil when absent.
func attrOrNil(n *tree.Node, local string) any {
	if v, ok := model.Attr(n, local); ok {
		return v
	}
	return nil
}
