package ops

import (
	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/ids"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
	"strings"
)

// picturePixelPt is the size of one image pixel in points: 96 pixels
// per inch (docs/contract.md §3, Pictures).
const picturePixelPt = 0.75

// runPicture adds a picture (object.add kind picture): a rect placed
// as box or pin gives (pin alone: the image's pixel size at 96 pixels
// per inch) is made a Foreign shape with the Img cells of its crop and
// a ForeignData that relates to its media part.
func (s *ObjectAddSpec) runPicture(ws *edit.Workspace, aliases map[string]string, units string, idSrc idSource) (string, []string, []string, string, error) {
	if s.img == nil {
		return "", nil, nil, "", cli.Errorf(cli.CodeInvalidOperation, "image", "the image was not checked (CheckImages)")
	}
	in, err := resolveImage(ws, s.img)
	if err != nil {
		return "", nil, nil, "", err
	}
	box := s.Box
	if box == nil {
		scale := 1.0
		if units == "in" {
			scale = vsd.PointsPerInch
		}
		w, h := float64(in.px[0])*picturePixelPt/scale, float64(in.px[1])*picturePixelPt/scale
		box = []float64{s.Pin[0] - w/2, s.Pin[1] - h/2, w, h}
	}
	rect := &ObjectAddSpec{At: s.At, As: s.As, Name: s.Name, Geom: "rect", Box: box}
	addr, changed, deleted, stale, err := rect.runAdd(ws, aliases, units, idSrc)
	if err != nil || len(changed) == 0 {
		return addr, changed, deleted, stale, err
	}
	crop := s.Crop
	if crop == nil {
		crop = []float64{0, 0, 0, 0}
	}
	if err := makePicture(ws, changed[0], in, crop); err != nil {
		return "", nil, nil, "", err
	}
	return addr, changed, deleted, stale, nil
}

// makePicture turns the new rect at addr into a picture of in with
// crop.
func makePicture(ws *edit.Workspace, addr string, in *imageInput, crop []float64) error {
	a, err := vsd.Parse(addr)
	if err != nil {
		return err
	}
	st, err := loadStyleShape(ws, a, addr)
	if err != nil {
		return err
	}
	media, _, err := mediaFor(ws, in)
	if err != nil {
		return err
	}
	relID, err := pageImageRel(ws, st.part, media)
	if err != nil {
		return err
	}
	w, _ := styleNumberCell(st, vsd.CellWidth)
	h, _ := styleNumberCell(st, vsd.CellHeight)
	hd := ws.Core().Target(st.part, addr, []*tree.Node{st.node})
	if err := hd.SetAttr(core.Path{}, "Type", vsd.ShapeTypeForeign); err != nil {
		return err
	}
	// The picture shows no fill and no line in any drawing: it stores
	// LinePattern 0 and FillPattern 0 itself (MS-VSDX visio.xsd
	// LinePattern_Type line 621, FillPattern_Type line 698, both cells of
	// ShapeSheet_Type, line 150), and refers to no style sheet: the
	// LineStyle, FillStyle and TextStyle attributes are optional
	// (ShapeSheet_Type, lines 508-510), so no style ID is assumed.
	for _, n := range []string{"LineStyle", "FillStyle", "TextStyle"} {
		if err := hd.RemoveAttr(core.Path{}, n); err != nil {
			return err
		}
	}
	// A picture has no connection points: nothing in MS-VSDX needs a
	// Connection section on a Foreign shape, and glue to a picture is
	// not a contract operation.
	for c := range st.node.ElementsSeq() {
		if c.Local() == vsd.ElemSection {
			if n, _ := c.Attr("N"); n == "Connection" {
				if err := hd.RemoveNode(c); err != nil {
					return err
				}
				break
			}
		}
	}
	cells := append([]edit.Cell{{N: vsd.CellLinePattern, V: "0"}, {N: vsd.CellFillPattern, V: "0"}}, cropCells(crop, w, h)...)
	// The Img cells and the ForeignData are written as one fragment
	// in the page's single-quote style.
	frag := append(cellsXML(cells), foreignDataXML(in.compression, relID)...)
	nodes, err := ws.Core().Parse(st.node, frag, nil)
	if err != nil {
		return err
	}
	for _, n := range nodes {
		if err := ws.Core().Insert(st.part, core.Into(st.node), n, nil); err != nil {
			return err
		}
	}
	return nil
}

// pictureNode is a picture shape in the working tree: its page part,
// node and the ForeignData element.
type pictureNode struct {
	part          string
	node, foreign *tree.Node
	root          *tree.Node
}

// loadPicture finds the picture at addr for image and crop, refusing
// (operation_failed at field) a shape that is not a picture and a
// picture a master instance inherits.
func loadPicture(ws *edit.Workspace, addr, field string) (*pictureNode, error) {
	a, err := vsd.Parse(addr)
	if err != nil || a.Kind != vsd.KindShape || a.InMaster {
		return nil, cli.Errorf(cli.CodeOperationFailed, "at", "%s is not a page shape", addr)
	}
	d, err := doc.Open(ws.Package())
	if err != nil {
		return nil, err
	}
	var entry *doc.PageEntry
	seen := 0
	for i := range d.Pages {
		if d.Pages[i].ID == a.Container {
			seen++
			if seen == a.PageDup {
				entry = &d.Pages[i]
				break
			}
		}
	}
	if entry == nil || entry.Part == "" {
		return nil, cli.Errorf(cli.CodeNotFound, "at", "no %s: not found", addr)
	}
	t, err := ws.Core().Tree(entry.Part)
	if err != nil {
		return nil, &edit.Unreadable{Target: addr, Detail: err.Error()}
	}
	node := findTextShape(t.Root(), a.Shape, a.Dup)
	if node == nil {
		return nil, cli.Errorf(cli.CodeNotFound, "at", "no %s: not found", addr)
	}
	var foreign *tree.Node
	for c := range node.ElementsSeq() {
		if c.Local() == "ForeignData" {
			foreign = c
		}
	}
	if typ, _ := node.Attr("Type"); typ != vsd.ShapeTypeForeign || foreign == nil || foreignRelID(foreign) == "" {
		return nil, cli.Errorf(cli.CodeOperationFailed, field, "%s is not a picture: image and crop apply to pictures", addr)
	}
	if _, inherited := node.Attr("MasterShape"); inherited {
		return nil, cli.Errorf(cli.CodeOperationFailed, field, "%s is a picture a master instance inherits", addr)
	}
	return &pictureNode{part: entry.Part, node: node, foreign: foreign, root: t.Root()}, nil
}

// setPicture applies object.set image and crop: a new image relates
// the picture to its media part (reused by hash), keeping every cell,
// and releases the old one; a crop writes the Img cells. A value equal
// to what read shows writes nothing.
func setPicture(ws *edit.Workspace, s *ObjectSetSpec, aliases map[string]string) ([]string, string, error) {
	if s.Image == nil && s.Crop == nil {
		return nil, "", nil
	}
	addr, err := resolveShape(s.At, aliases)
	if err != nil {
		return nil, "", err
	}
	field := "image"
	if s.Image == nil {
		field = "crop"
	}
	pn, err := loadPicture(ws, addr, field)
	if err != nil {
		return nil, "", err
	}
	wrote := false
	if s.Image != nil {
		if s.img == nil {
			return nil, "", cli.Errorf(cli.CodeInvalidOperation, "image", "the image was not checked (CheckImages)")
		}
		in, err := resolveImage(ws, s.img)
		if err != nil {
			return nil, "", err
		}
		media, _, err := mediaFor(ws, in)
		if err != nil {
			return nil, "", err
		}
		oldID := foreignRelID(pn.foreign)
		relID, err := pageImageRel(ws, pn.part, media)
		if err != nil {
			return nil, "", err
		}
		if relID != oldID {
			var rel *tree.Node
			for c := range pn.foreign.ElementsSeq() {
				if c.Local() == vsd.ElemRel {
					rel = c
				}
			}
			hd := ws.Core().Target(pn.part, addr, []*tree.Node{pn.node})
			if err := hd.SetNodeAttr(rel, "r:id", relID); err != nil {
				return nil, "", err
			}
			if err := hd.SetNodeAttr(pn.foreign, "CompressionType", in.compression); err != nil {
				return nil, "", err
			}
			if _, err := releaseMedia(ws, pn.part, pn.root, []string{oldID}); err != nil {
				return nil, "", err
			}
			wrote = true
		}
	}
	if s.Crop != nil {
		a, _ := vsd.Parse(addr)
		w, h := 0.0, 0.0
		if st, err := loadCellsOnly(ws, a, addr, pn); err == nil {
			w, h = st[0], st[1]
		}
		want := cropCells(s.Crop, w, h)
		var cells []edit.Cell
		for _, c := range want {
			if cur := cellOf(pn.node, c.N); cur == nil || attrOf(cur, "F") != c.F {
				cells = append(cells, c)
			}
		}
		if len(cells) > 0 {
			if err := ws.SetCells(addr, cells); err != nil {
				return nil, "", err
			}
			wrote = true
		}
	}
	if !wrote {
		return nil, "", nil
	}
	return []string{addr}, "", nil
}

// loadCellsOnly returns the picture's Width and Height in inches as
// stored on its node.
func loadCellsOnly(_ *edit.Workspace, _ vsd.Address, _ string, pn *pictureNode) ([2]float64, error) {
	var out [2]float64
	for i, n := range []string{vsd.CellWidth, vsd.CellHeight} {
		c := cellOf(pn.node, n)
		if c == nil {
			return out, cli.Errorf(cli.CodeOperationFailed, "crop", "the picture stores no %s", n)
		}
		v, ok := vsd.Number(attrOf(c, "V"))
		if !ok {
			return out, cli.Errorf(cli.CodeOperationFailed, "crop", "the picture's %s is not a number", n)
		}
		out[i] = v
	}
	return out, nil
}

// cellOf returns the direct Cell child of n named name.
func cellOf(n *tree.Node, name string) *tree.Node {
	for c := range n.ElementsSeq() {
		if c.Local() == vsd.ElemCell {
			if v, _ := c.Attr("N"); v == name {
				return c
			}
		}
	}
	return nil
}

// attrOf returns attribute name of n, "" when absent.
func attrOf(n *tree.Node, name string) string {
	v, _ := n.Attr(name)
	return v
}

type idSource = ids.Source

// picturePage returns the page part and working root of a's page.
func picturePage(ws *edit.Workspace, a vsd.Address) (*pictureNode, error) {
	d, err := doc.Open(ws.Package())
	if err != nil {
		return nil, err
	}
	seen := 0
	for i := range d.Pages {
		if d.Pages[i].ID != a.Container {
			continue
		}
		seen++
		if seen == a.PageDup && d.Pages[i].Part != "" {
			t, err := ws.Core().Tree(d.Pages[i].Part)
			if err != nil {
				return nil, err
			}
			return &pictureNode{part: d.Pages[i].Part, root: t.Root()}, nil
		}
	}
	return nil, cli.Errorf(cli.CodeNotFound, "at", "no page")
}

// cellsXML writes cells as Cell elements with single-quoted attributes.
func cellsXML(cells []edit.Cell) []byte {
	var b strings.Builder
	for _, c := range cells {
		b.WriteString("<Cell N='" + c.N + "' V='" + c.V + "'")
		if c.F != "" {
			b.WriteString(" F='" + c.F + "'")
		}
		b.WriteString("/>")
	}
	return []byte(b.String())
}
