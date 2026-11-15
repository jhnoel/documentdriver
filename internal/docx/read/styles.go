package read

import (
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// listStyles lists the style definitions of the styles part beside the
// main document part: styleId, type (paragraph when absent), default,
// name, basedOn and next. A package without the part lists none; one whose
// part is not well formed is rejected.
func listStyles(pkg *model.Package) (Object, string) {
	name := pkg.StylesPartName()
	if !pkg.Has(name) {
		return Object{"styles": []any{}}, ""
	}
	x, err := pkg.XML(name)
	if err != nil {
		return nil, "styles.xml parse failed: " + err.Error()
	}
	styles := []any{}
	x.Doc.Node().Walk(func(n *tree.Node) bool {
		if !model.IsW(n, "style") {
			return true
		}
		id, ok := model.Attr(n, "styleId")
		if !ok {
			return true
		}
		typ, ok := model.Attr(n, "type")
		if !ok {
			typ = "paragraph"
		}
		e := Object{"id": id, "type": typ}
		if d, ok := model.Attr(n, "default"); ok && d == "1" {
			e["default"] = true
		}
		for c := range n.ChildrenSeq() {
			var key string
			switch {
			case model.IsW(c, "name"):
				key = "name"
			case model.IsW(c, "basedOn"):
				key = "based_on"
			case model.IsW(c, "next"):
				key = "next"
			default:
				continue
			}
			if v, ok := model.Attr(c, "val"); ok {
				e[key] = v
			}
		}
		styles = append(styles, e)
		return true
	})
	return Object{"styles": styles}, ""
}
