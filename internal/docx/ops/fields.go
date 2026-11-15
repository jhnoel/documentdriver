package ops

import (
	"errors"

	"github.com/jhnoel/documentdriver/internal/docx/html"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// expandSimpleFields rewrites each w:fldSimple in container c (a
// paragraph or a wrapper in it, held by h) as the equivalent complex
// field runs (headers.rs expand_simple_fields): a tracked deletion holds
// runs, so a deleted field is those runs, and accepting leaves no empty
// w:fldSimple behind for Word to recompute. The result runs are moved,
// not copied; the field's first run's properties are copied to the new
// runs.
func expandSimpleFields(h *core.Handle, c *tree.Node) error {
	for _, n := range c.Elements() {
		switch {
		case model.IsW(n, "fldSimple"):
			if err := expandSimpleField(h, n); err != nil {
				return err
			}
		case model.IsW(n, "hyperlink"), model.IsW(n, "smartTag"), model.IsW(n, "customXml"), model.IsW(n, "dir"), model.IsW(n, "bdo"):
			if err := expandSimpleFields(h, n); err != nil {
				return err
			}
		}
	}
	return nil
}

// expandSimpleField rewrites one w:fldSimple as complex field runs.
func expandSimpleField(h *core.Handle, fs *tree.Node) error {
	parent := fs.Parent()
	rpr := ""
	if r := model.ChildW(fs, "r"); r != nil {
		if p := model.ChildW(r, "rPr"); p != nil {
			rpr = string(p.Bytes())
		}
	}
	dirty := ""
	if v, ok := model.Attr(fs, "dirty"); ok {
		dirty = ` w:dirty="` + html.EscapeAttr(v) + `"`
	}
	run := func(inner string) error {
		nodes, err := h.Workspace().Parse(parent, []byte(`<w:r>`+rpr+inner+`</w:r>`), map[string]string{"w": model.NSW})
		if err != nil || len(nodes) != 1 {
			return errors.Join(errors.New("ops: a field run did not parse"), err)
		}
		return h.PlaceAt(parent, nodes[0], fs)
	}
	if err := run(`<w:fldChar w:fldCharType="begin"` + dirty + `/>`); err != nil {
		return err
	}
	if err := run(`<w:instrText xml:space="preserve"> ` + string(tree.EscapeText(model.AttrOr(fs, "instr"))) + ` </w:instrText>`); err != nil {
		return err
	}
	if err := run(`<w:fldChar w:fldCharType="separate"/>`); err != nil {
		return err
	}
	for _, ch := range fs.Elements() {
		if err := h.PutBefore(ch, fs); err != nil {
			return err
		}
	}
	if err := run(`<w:fldChar w:fldCharType="end"/>`); err != nil {
		return err
	}
	return h.RemoveNode(fs)
}
