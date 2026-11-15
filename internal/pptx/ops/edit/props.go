package edit

import (
	"fmt"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
)

// extLst is the DrawingML extension list the pptx extension helpers use.
var extLst = A("extLst")

// AppendExt adds an extension to the element at parent in every branch:
// its a:extLst (made through Ensure, where the schema puts it) gains, at
// the end, an a:ext with the given uri holding the nodes parsed from
// frag (core.Handle.AppendExt). frag's prefixes resolve through ns,
// mapped to the namespace family member in scope, or through
// declarations in frag itself, which is how PowerPoint writes extension
// content (asvg:svgBlip declares asvg). It fails, changing nothing,
// unless every branch has the parent and none has an extension with that
// uri already, and the fragment parses as elements only.
func (h *Handle) AppendExt(parent Path, uri string, frag []byte, ns map[string]string) error {
	return h.Handle.AppendExt(parent, extLst, uri, frag, ns)
}

// ExtChild returns, for reading, the child named (space, local) of the
// a:ext with the given uri in the a:extLst of the element at p in branch
// i, or nil.
func (h *Handle) ExtChild(i int, p Path, uri, space, local string) *tree.Node {
	return h.Handle.ExtChild(i, p, extLst, uri, space, local)
}

// SetExtRelAttr sets the relationship-id attribute local of the child
// (space, local name child) of the extension uri under the element at p,
// in every branch. It fails, changing nothing, unless every branch has
// that child.
func (h *Handle) SetExtRelAttr(p Path, uri, space, child, local, value string) error {
	return h.Handle.SetExtRelAttr(p, extLst, uri, space, child, local, value)
}

// RemoveExt removes the a:ext with the given uri from the a:extLst of the
// element at p in every branch, and the a:extLst when nothing is left in
// it. It changes nothing when no branch has the extension, and fails,
// changing nothing, when only some do.
func (h *Handle) RemoveExt(p Path, uri string) error { return h.Handle.RemoveExt(p, extLst, uri) }

// SetGuide sets the fmla attribute of the child of the element at p whose
// name attribute is name, in every branch. Setting the formula a guide
// already has changes nothing. It fails, changing nothing, unless every
// branch has the guide.
func (h *Handle) SetGuide(p Path, name, fmla string) error {
	named := func(c *tree.Node) bool { v, _ := c.Attr("name"); return v == name }
	return h.SetChildAttr(p, named, fmt.Sprintf("guide %q", name), "fmla", fmla)
}

// CNvPr returns branch i's cNvPr for reading, nil if it has none.
func (h *Handle) CNvPr(i int) *tree.Node { return cNvPr(h.Elem(i)) }

// SetCNvPrAttr sets an attribute of the cNvPr in every branch.
func (h *Handle) SetCNvPrAttr(name, value string) error {
	return h.SetAttrAt(cNvPr, "cNvPr", name, value)
}

// RemoveCNvPrAttr removes an attribute of the cNvPr in every branch (a
// branch without it is left as it is).
func (h *Handle) RemoveCNvPrAttr(name string) error {
	return h.RemoveAttrAt(cNvPr, "cNvPr", name)
}

// ResolveGlue resolves a glue id on the handle's slide (see
// Workspace.ResolveGlue).
func (h *Handle) ResolveGlue(raw string) (Ref, error) { return h.w.ResolveGlue(h.ref.part, raw) }

// GlueID returns the id glue to ref must write. It fails when ref's id is
// shared by several objects on the slide: such glue is ambiguous, so it
// is never written.
func (w *Workspace) GlueID(ref Ref) (string, error) {
	if _, err := w.ResolveGlue(ref.part, ref.id); err != nil {
		return "", fmt.Errorf("cannot glue to %s: %w", ref.Short(), err)
	}
	return ref.id, nil
}

// Groups returns the groups enclosing branch i, innermost first, for
// reading.
func (h *Handle) Groups(i int) []*tree.Node {
	var out []*tree.Node
	for p := h.Elem(i).Parent(); p != nil && !derive.Is(p, derive.PML, "spTree"); p = p.Parent() {
		if isGroup(p) {
			out = append(out, p)
		}
	}
	return out
}

// ColorCtx returns the colour context of the handle's slide, as the read
// model builds it (derive.SlideColorCtx).
func (h *Handle) ColorCtx() (derive.ColorCtx, error) { return h.w.ColorCtx(h.ref.part) }

// ColorCtx returns the colour context of a slide part: its colour map
// after overrides, and its theme's colour and format schemes.
func (w *Workspace) ColorCtx(part string) (derive.ColorCtx, error) {
	sld, err := w.tree(part)
	if err != nil {
		return derive.ColorCtx{}, err
	}
	var lay, mst, theme *tree.Node
	layName, layDoc, err := w.related(part, ctypes.RelSlideLayout)
	if err != nil {
		return derive.ColorCtx{}, err
	}
	if layDoc != nil {
		lay = layDoc.Root()
		mName, mDoc, err := w.related(layName, ctypes.RelSlideMaster)
		if err != nil {
			return derive.ColorCtx{}, err
		}
		if mDoc != nil {
			mst = mDoc.Root()
			_, tDoc, err := w.related(mName, ctypes.RelTheme)
			if err != nil {
				return derive.ColorCtx{}, err
			}
			if tDoc != nil {
				theme = tDoc.Root()
			}
		}
	}
	return derive.SlideColorCtx(sld.Root(), lay, mst, theme), nil
}
