package build

import (
	"strings"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/html"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Para is one paragraph of block HTML: its open-tag data and its atoms.
type Para struct {
	Attrs html.ParaAttrs
	Atoms []html.Atom
}

// Paragraphs splits items into paragraphs. Table structure is refused
// (create builds tables through Blocks; the operations have their own
// table lowering), as are revision markup and list markers a new
// paragraph cannot carry. List membership rides in Attrs.List
// (Lists.Apply writes it); a list item of several paragraphs and a level
// past the ninth are refused.
func Paragraphs(items []html.Item) ([]Para, error) {
	blocks, err := collect(items, false)
	if err != nil {
		return nil, err
	}
	out := make([]Para, len(blocks))
	for i, b := range blocks {
		out[i] = *b.Para
	}
	return out, nil
}

// StyleOf is the paragraph style a paragraph's open tag names: Heading1
// to Heading6 for h1 to h6, a <p>'s class, or "" for none.
func StyleOf(a html.ParaAttrs) string {
	if strings.HasPrefix(a.Tag, "h") && len(a.Tag) == 2 {
		return "Heading" + a.Tag[1:]
	}
	if a.Class.OK {
		return a.Class.V
	}
	return ""
}

// Properties gives paragraph p (placed, held by h) the properties its
// open tag asks for: the style (StyleOf) and the paragraph CSS
// (alignment, spacing, indentation). Each property goes where the schema
// puts it in w:pPr (docxdriver wrote w:jc before w:spacing and w:ind,
// against CT_PPrBase's order). Nothing is written for a plain <p>.
func Properties(h *core.Handle, p *tree.Node, a html.ParaAttrs) error {
	style := StyleOf(a)
	css := ""
	if a.CSS.OK {
		var err error
		if css, err = html.ParagraphProperties(a.CSS.V); err != nil {
			return contentErr("%s", err.Error())
		}
	}
	if style == "" && css == "" {
		return nil
	}
	ppr, err := h.EnsureIn(p, docxedit.W("pPr"))
	if err != nil {
		return err
	}
	if style != "" {
		ps, err := h.EnsureIn(ppr, docxedit.W("pStyle"))
		if err != nil {
			return err
		}
		if err := h.SetNodeAttr(ps, wq(ps, "val"), style); err != nil {
			return err
		}
	}
	if css == "" {
		return nil
	}
	nodes, err := h.Workspace().Parse(ppr, []byte(css), nsW)
	if err != nil {
		return err
	}
	for _, n := range nodes {
		if err := h.PlaceNew(ppr, n); err != nil {
			return err
		}
	}
	return nil
}
