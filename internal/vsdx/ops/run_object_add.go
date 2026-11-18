package ops

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/ids"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/emit"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/place"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/text"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// addConnPt is one connection point of a shape: its row index (IX, the
// point is IX+1) and its local coordinates in inches, y up.
type addConnPt struct {
	ix int
	x  float64
	y  float64
}

// addLocated is the frame data of one wanted shape: its transform in
// its parent space, the frame of that space, its stored type, and its
// connection points in row order.
type addLocated struct {
	t   place.Transform
	f   place.Frame
	ok  bool
	typ string
	pts []addConnPt
}

// addGlue is one parsed glue reference: the target shape's address,
// its stored ID, and the connection point (1-based) when hasPoint holds.
type addGlue struct {
	target   string
	id       uint32
	point    int
	hasPoint bool
}

// addPage is one page's height and written tree, for placement.
type addPage struct {
	height float64
	root   *tree.Node
}

// Run adds one shape or connector and returns its address. It resolves
// at through aliases, places the shape with place.Unplace, emits with
// emit, inserts through the edit layer, then applies text and fill/line.
func (s *ObjectAddSpec) Run(ws *edit.Workspace, aliases map[string]string, units string, idSrc ids.Source) (string, []string, []string, string, error) {
	_ = idSrc
	scale := 1.0
	if units == "in" {
		scale = vsd.PointsPerInch
	}
	at, err := addAddr(s.At, aliases, "at")
	if err != nil {
		return "", nil, nil, "", err
	}
	a, err := vsd.Parse(at)
	if err != nil {
		return "", nil, nil, "", cli.Errorf(cli.CodeNotFound, "at", "%s", err.Error())
	}
	if a.InMaster {
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s names a master shape; masters are read only", at)
	}
	var pageAddr, parent string
	switch a.Kind {
	case vsd.KindPage:
		pageAddr, parent = at, ""
	case vsd.KindShape:
		pageAddr, parent = vsd.PageID(a.Container, a.PageDup), at
	default:
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s: want a page or group address", at)
	}
	d, err := doc.Open(ws.Package())
	if err != nil {
		return "", nil, nil, "", err
	}
	r := inherit.New(d)
	var masterID uint32
	var masterEntry *doc.MasterEntry
	var locX, locY float64
	if s.Master != "" {
		raw, err := addAddr(s.Master, aliases, "master")
		if err != nil {
			return "", nil, nil, "", err
		}
		masterID, masterEntry, err = addMaster(d, raw)
		if err != nil {
			return "", nil, nil, "", err
		}
		locX, locY, err = addMasterLoc(r, masterID)
		if err != nil {
			return "", nil, nil, "", err
		}
	}
	pg, err := addLoadPage(ws, r, a.Container, a.PageDup)
	if err != nil {
		return "", nil, nil, "", err
	}
	if a.Kind == vsd.KindShape {
		if node := findTextShape(pg.root, a.Shape, a.Dup); node != nil {
			if err := nodeRefusal(r, node, at, "at", false); err != nil {
				return "", nil, nil, "", err
			}
		}
	}
	if err := scaledRefusal(r, d, a.Container, a.PageDup, pageAddr, "at"); err != nil {
		return "", nil, nil, "", err
	}
	if s.Kind == "connector" {
		return s.addConnector(ws, aliases, r, pg, pageAddr, parent, a.Container, a.PageDup, scale)
	}
	return s.addShape(ws, r, d, pg, pageAddr, parent, scale, masterID, masterEntry, locX, locY)
}

// addAddr resolves one address field through aliases: a $name becomes
// the address an earlier op named, with any suffix that may follow
// it, anything else is kept.
func addAddr(raw string, aliases map[string]string, field string) (string, error) {
	return ResolveAlias(raw, aliases, field)
}

// addMaster resolves one master reference to its stored ID: a master
// address (master2) or the name read shows it under.
func addMaster(d *doc.Document, raw string) (uint32, *doc.MasterEntry, error) {
	if a, err := vsd.Parse(raw); err == nil && a.Kind == vsd.KindMaster && !a.InMaster {
		entry, ok := d.Master(a.Container)
		if !ok {
			return 0, nil, cli.Errorf(cli.CodeNotFound, "master", "no master %s: not found", raw)
		}
		if entry.Part == "" {
			e := cli.Errorf(cli.CodeUnreadableTarget, "master", "%s cannot be edited safely", vsd.MasterID(entry.ID))
			e.Detail = "it has no part"
			return 0, nil, e
		}
		return entry.ID, entry, nil
	}
	for i := range d.Masters {
		entry := &d.Masters[i]
		if raw == entry.Name || (entry.NameU != "" && raw == entry.NameU) {
			if entry.Part == "" {
				e := cli.Errorf(cli.CodeUnreadableTarget, "master", "%s cannot be edited safely", vsd.MasterID(entry.ID))
				e.Detail = "it has no part"
				return 0, nil, e
			}
			return entry.ID, entry, nil
		}
	}
	return 0, nil, cli.Errorf(cli.CodeNotFound, "master", "no master %q: not found", raw)
}

// addMasterLoc returns the LocPin fractions of the master's top shape
// and refuses a 1-D top, whose transform is its ends, not a box.
func addMasterLoc(r *inherit.Resolver, masterID uint32) (float64, float64, error) {
	top, ok := r.MasterShape(&inherit.Shape{Master: masterID, HasMaster: true})
	if !ok || top == nil || top.Sheet == nil {
		e := cli.Errorf(cli.CodeUnreadableTarget, "master", "%s cannot be edited safely", vsd.MasterID(masterID))
		e.Detail = "it has no readable top shape"
		return 0, 0, e
	}
	if _, ok := top.Sheet.Cell(sheet.ShapeCell(vsd.CellBeginX)); ok {
		return 0, 0, cli.Errorf(cli.CodeOperationFailed, "master", "%s is a 1-D shape; use kind connector for a straight connector", vsd.MasterID(masterID))
	}
	width, height, locX, locY := 0.0, 0.0, 0.0, 0.0
	if err := addNumberCell(r, top, vsd.CellWidth, &width); err != nil {
		return 0, 0, err
	}
	if err := addNumberCell(r, top, vsd.CellHeight, &height); err != nil {
		return 0, 0, err
	}
	if err := addNumberCell(r, top, vsd.CellLocPinX, &locX); err != nil {
		return 0, 0, err
	}
	if err := addNumberCell(r, top, vsd.CellLocPinY, &locY); err != nil {
		return 0, 0, err
	}
	if !(width > 0) || !(height > 0) {
		return 0, 0, cli.Errorf(cli.CodeOperationFailed, "master", "%s has no positive size", vsd.MasterID(masterID))
	}
	return locX / width, locY / height, nil
}

// addNumberCell reads the effective number of one shape-level cell into dst.
func addNumberCell(r *inherit.Resolver, h *inherit.Shape, name string, dst *float64) error {
	cell, _, ok := r.Cell(h, sheet.ShapeCell(name))
	if !ok {
		return cli.Errorf(cli.CodeOperationFailed, "master", "%s is not stored", name)
	}
	v, ok := vsd.Number(cell.V)
	if !ok {
		return cli.Errorf(cli.CodeOperationFailed, "master", "%s is not a number", name)
	}
	*dst = v
	return nil
}

// addLoadPage reads one page's height in inches and its written PageContents.
func addLoadPage(ws *edit.Workspace, r *inherit.Resolver, container uint32, dup int) (*addPage, error) {
	pagesPart := ""
	for _, name := range ws.Package().Names() {
		if strings.HasSuffix(name, "/pages.xml") {
			pagesPart = name
			break
		}
	}
	if pagesPart == "" {
		return nil, cli.Errorf(cli.CodeOperationFailed, "at", "the drawing has no pages part")
	}
	pagesDoc, err := ws.Core().Tree(pagesPart)
	if err != nil {
		e := cli.Errorf(cli.CodeUnreadableTarget, "at", "%s cannot be edited safely", vsd.PageID(container, dup))
		e.Detail = err.Error()
		return nil, e
	}
	var pageNode *tree.Node
	seen := 0
	for c := range pagesDoc.Root().ElementsSeq() {
		if c.Local() != vsd.ElemPage || !vsd.IsMain(c.Space()) {
			continue
		}
		v, ok := c.Attr("ID")
		if !ok {
			continue
		}
		id, err := strconv.ParseUint(v, 10, 32)
		if err != nil || uint32(id) != container {
			continue
		}
		seen++
		if seen == dup {
			pageNode = c
			break
		}
	}
	if pageNode == nil {
		return nil, cli.Errorf(cli.CodeNotFound, "at", "no page %s: not found", vsd.PageID(container, dup))
	}
	var sheetNode *tree.Node
	var relID string
	for c := range pageNode.ElementsSeq() {
		if vsd.IsMain(c.Space()) && c.Local() == vsd.ElemPageSheet {
			sheetNode = c
		}
		if vsd.IsMain(c.Space()) && c.Local() == vsd.ElemRel {
			relID, _ = c.AttrNS(vsd.NSRel, "id")
		}
	}
	if sheetNode == nil {
		return nil, cli.Errorf(cli.CodeOperationFailed, "at", "%s has no page sheet; its size cannot be derived", vsd.PageID(container, dup))
	}
	height, err := addPageHeight(r, sheetNode)
	if err != nil {
		return nil, err
	}
	if relID == "" {
		e := cli.Errorf(cli.CodeUnreadableTarget, "at", "%s cannot be edited safely", vsd.PageID(container, dup))
		e.Detail = "it has no part"
		return nil, e
	}
	rels, err := ws.Package().Rels(pagesPart)
	if err != nil {
		e := cli.Errorf(cli.CodeUnreadableTarget, "at", "%s cannot be edited safely", vsd.PageID(container, dup))
		e.Detail = err.Error()
		return nil, e
	}
	rel, ok := rels.Get(relID)
	if !ok {
		e := cli.Errorf(cli.CodeUnreadableTarget, "at", "%s cannot be edited safely", vsd.PageID(container, dup))
		e.Detail = "it has no part"
		return nil, e
	}
	partName, err := rels.TargetPart(rel)
	if err != nil {
		e := cli.Errorf(cli.CodeUnreadableTarget, "at", "%s cannot be edited safely", vsd.PageID(container, dup))
		e.Detail = err.Error()
		return nil, e
	}
	contents, err := ws.Core().Tree(partName)
	if err != nil {
		e := cli.Errorf(cli.CodeUnreadableTarget, "at", "%s cannot be edited safely", vsd.PageID(container, dup))
		e.Detail = err.Error()
		return nil, e
	}
	if contents.Root() == nil || contents.Root().Local() != vsd.ElemPageContents || !vsd.IsMain(contents.Root().Space()) {
		e := cli.Errorf(cli.CodeUnreadableTarget, "at", "%s cannot be edited safely", vsd.PageID(container, dup))
		e.Detail = "it is not a " + vsd.ElemPageContents + " part"
		return nil, e
	}
	return &addPage{height: height, root: contents.Root()}, nil
}

// addPageHeight reads a page sheet's PageHeight through the resolver.
func addPageHeight(r *inherit.Resolver, sheetNode *tree.Node) (float64, error) {
	shape := &inherit.Shape{}
	shape.Sheet, _ = sheet.Index(sheetNode)
	for name, value := range sheetNode.AttrsSeq() {
		var i int
		switch name {
		case "LineStyle":
			i = 0
		case "FillStyle":
			i = 1
		case "TextStyle":
			i = 2
		default:
			continue
		}
		if u, err := strconv.ParseUint(value, 10, 32); err == nil {
			shape.Styles[i], shape.HasStyle[i] = uint32(u), true
		}
	}
	cell, _, ok := r.Cell(shape, sheet.ShapeCell(vsd.CellPageHeight))
	if !ok {
		return 0, cli.Errorf(cli.CodeOperationFailed, "at", "the page size cannot be derived")
	}
	h, ok := vsd.Number(cell.V)
	if !ok || !(h > 0) {
		return 0, cli.Errorf(cli.CodeOperationFailed, "at", "the page size cannot be derived")
	}
	return h, nil
}

// addParseGlue parses one glue reference into its target and point.
func addParseGlue(raw string, aliases map[string]string, pageAddr string, container uint32, dup int, field string) (addGlue, error) {
	head, pointText, hasPoint := strings.Cut(raw, ":")
	point := 0
	if hasPoint {
		n, err := strconv.Atoi(pointText)
		if err != nil || n <= 0 {
			return addGlue{}, cli.Errorf(cli.CodeOperationFailed, field, "%s must name a shape or $name, with an optional point", raw)
		}
		point = n
	}
	if strings.HasPrefix(head, "$") {
		target, err := ResolveAlias(head, aliases, field)
		if err != nil {
			return addGlue{}, err
		}
		a, err := vsd.Parse(target)
		if err != nil || a.Kind != vsd.KindShape || a.InMaster {
			return addGlue{}, cli.Errorf(cli.CodeOperationFailed, field, "%s: want a shape address", target)
		}
		if a.Container != container || a.PageDup != dup {
			return addGlue{}, cli.Errorf(cli.CodeNotFound, field, "%s is not on %s: not found", target, pageAddr)
		}
		return addGlue{target: target, id: a.Shape, point: point, hasPoint: hasPoint}, nil
	}
	if !strings.HasPrefix(head, "s") {
		return addGlue{}, cli.Errorf(cli.CodeOperationFailed, field, "%s must name a shape or $name, with an optional point", raw)
	}
	id, err := strconv.ParseUint(head[1:], 10, 32)
	if err != nil {
		return addGlue{}, cli.Errorf(cli.CodeOperationFailed, field, "%s must name a shape or $name, with an optional point", raw)
	}
	return addGlue{target: vsd.ShapeID(pageAddr, uint32(id), 1), id: uint32(id), point: point, hasPoint: hasPoint}, nil
}

// addShapesChild returns the first Shapes element child of node.
func addShapesChild(node *tree.Node) *tree.Node {
	for c := range node.ElementsSeq() {
		if c.Local() == vsd.ElemShapes && vsd.IsMain(c.Space()) {
			return c
		}
	}
	return nil
}

// addHead builds the resolver shape of one page shape element.
func addHead(node *tree.Node, master uint32, has bool) *inherit.Shape {
	h := &inherit.Shape{Master: master, HasMaster: has}
	h.Sheet, _ = sheet.Index(node)
	for name, value := range node.AttrsSeq() {
		switch name {
		case "Master":
			if u, err := strconv.ParseUint(value, 10, 32); err == nil {
				h.Master, h.HasMaster = uint32(u), true
			}
		case "MasterShape":
			if u, err := strconv.ParseUint(value, 10, 32); err == nil {
				h.MasterShape, h.HasMShape = uint32(u), true
			}
		case "LineStyle":
			if u, err := strconv.ParseUint(value, 10, 32); err == nil {
				h.Styles[0], h.HasStyle[0] = uint32(u), true
			}
		case "FillStyle":
			if u, err := strconv.ParseUint(value, 10, 32); err == nil {
				h.Styles[1], h.HasStyle[1] = uint32(u), true
			}
		case "TextStyle":
			if u, err := strconv.ParseUint(value, 10, 32); err == nil {
				h.Styles[2], h.HasStyle[2] = uint32(u), true
			}
		}
	}
	return h
}

// addTransform reads the effective transform of one shape.
func addTransform(r *inherit.Resolver, h *inherit.Shape) (place.Transform, bool) {
	var t place.Transform
	num := func(name string, dst *float64) bool {
		cell, _, ok := r.Cell(h, sheet.ShapeCell(name))
		if !ok {
			return false
		}
		v, ok := vsd.Number(cell.V)
		if !ok {
			return false
		}
		*dst = v
		return true
	}
	boolean := func(name string, dst *bool) bool {
		cell, _, ok := r.Cell(h, sheet.ShapeCell(name))
		if !ok {
			return false
		}
		v, ok := vsd.Bool(cell.V)
		if !ok {
			return false
		}
		*dst = v
		return true
	}
	if !num(vsd.CellPinX, &t.PinX) || !num(vsd.CellPinY, &t.PinY) || !num(vsd.CellWidth, &t.Width) || !num(vsd.CellHeight, &t.Height) {
		return place.Transform{}, false
	}
	if !num(vsd.CellLocPinX, &t.LocPinX) || !num(vsd.CellLocPinY, &t.LocPinY) || !num(vsd.CellAngle, &t.Angle) {
		return place.Transform{}, false
	}
	if !boolean(vsd.CellFlipX, &t.FlipX) || !boolean(vsd.CellFlipY, &t.FlipY) {
		return place.Transform{}, false
	}
	return t, true
}

// addPoints reads the effective connection points of one shape.
func addPoints(r *inherit.Resolver, h *inherit.Shape) []addConnPt {
	var out []addConnPt
	for _, row := range r.Rows(h, vsd.SectionConnection) {
		if row.Key.RowIX < 0 {
			continue
		}
		xc, _, okX := r.Cell(h, row.Key.WithCell("X"))
		yc, _, okY := r.Cell(h, row.Key.WithCell("Y"))
		if !okX || !okY {
			continue
		}
		x, okX := vsd.Number(xc.V)
		y, okY := vsd.Number(yc.V)
		if !okX || !okY {
			continue
		}
		out = append(out, addConnPt{row.Key.RowIX, x, y})
	}
	return out
}

// addLocate walks the page and returns the frame data of wanted shapes.
func addLocate(root *tree.Node, pageAddr string, wanted map[string]bool, r *inherit.Resolver) map[string]addLocated {
	out := map[string]addLocated{}
	top := addShapesChild(root)
	if top == nil {
		return out
	}
	type item struct {
		node *tree.Node
		f    place.Frame
		m    uint32
		hasM bool
	}
	var stack []item
	push := func(shapes *tree.Node, f place.Frame, m uint32, has bool) {
		for n := shapes.LastChild(); n != nil; n = n.PrevSibling() {
			if n.Kind() == tree.ElementNode && n.Local() == vsd.ElemShape && vsd.IsMain(n.Space()) {
				stack = append(stack, item{n, f, m, has})
			}
		}
	}
	push(top, place.Page, 0, false)
	seen := map[uint32]int{}
	for len(stack) > 0 {
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		node := it.node
		idText, okID := node.Attr("ID")
		id, err := strconv.ParseUint(idText, 10, 32)
		if !okID || err != nil {
			if nested := addShapesChild(node); nested != nil {
				push(nested, it.f, it.m, it.hasM)
			}
			continue
		}
		shapeID := uint32(id)
		seen[shapeID]++
		addr := vsd.ShapeID(pageAddr, shapeID, seen[shapeID])
		nested := addShapesChild(node)
		isGroup := nested != nil
		if wanted[addr] || isGroup {
			h := addHead(node, it.m, it.hasM)
			t, ok := addTransform(r, h)
			if wanted[addr] {
				typ, _ := node.Attr("Type")
				out[addr] = addLocated{t: t, f: it.f, ok: ok, typ: typ, pts: addPoints(r, h)}
			}
			if nested != nil {
				childF := it.f
				if ok {
					childF = it.f.Child(t)
				}
				m, has := it.m, it.hasM
				if h.HasMaster {
					m, has = h.Master, true
				}
				push(nested, childF, m, has)
			}
		}
	}
	return out
}

// addViewToParent maps a view point (points, y down) to parent inches.
func addViewToParent(f place.Frame, view geom.Pt, height float64) (float64, float64, bool) {
	viewInv, ok := place.View(height).Invert()
	if !ok {
		return 0, 0, false
	}
	pagePt := viewInv.Apply(view)
	frameInv, ok := f.M.Invert()
	if !ok {
		return 0, 0, false
	}
	parentPt := frameInv.Apply(pagePt)
	return parentPt.X, parentPt.Y, true
}

// addParentFrame returns the frame of a parent group or the page frame.
func addParentFrame(parent string, located map[string]addLocated) (place.Frame, error) {
	if parent == "" {
		return place.Page, nil
	}
	loc, ok := located[parent]
	if !ok {
		return place.Frame{}, cli.Errorf(cli.CodeNotFound, "at", "no shape %s: not found", parent)
	}
	if loc.typ != vsd.ShapeTypeGroup {
		return place.Frame{}, cli.Errorf(cli.CodeOperationFailed, "at", "%s is not a group", parent)
	}
	if !loc.ok {
		return place.Frame{}, cli.Errorf(cli.CodeOperationFailed, "at", "%s has no readable transform", parent)
	}
	return loc.f.Child(loc.t), nil
}

// addShape adds a master-less or master shape.
func (s *ObjectAddSpec) addShape(ws *edit.Workspace, r *inherit.Resolver, d *doc.Document, pg *addPage, pageAddr, parent string, scale float64, masterID uint32, masterEntry *doc.MasterEntry, locX, locY float64) (string, []string, []string, string, error) {
	located := map[string]addLocated{}
	needWalk := parent != ""
	if needWalk {
		located = addLocate(pg.root, pageAddr, map[string]bool{parent: true}, r)
		if _, ok := located[parent]; !ok {
			return "", nil, nil, "", cli.Errorf(cli.CodeNotFound, "at", "no shape %s: not found", parent)
		}
		if located[parent].typ != vsd.ShapeTypeGroup {
			return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s is not a group", parent)
		}
	}
	frame, err := addParentFrame(parent, located)
	if err != nil {
		return "", nil, nil, "", err
	}
	newID, err := ws.NextShapeID(pageAddr)
	if err != nil {
		return "", nil, nil, "", err
	}
	var frag []byte
	if s.Master != "" {
		frag, err = addMasterShape(ws, d, r, newID, masterID, masterEntry, s, frame, pg.height, locX, locY, scale)
		if err != nil {
			return "", nil, nil, "", err
		}
	} else {
		tr, err := addPlannedTransform(s, frame, pg.height, 0.5, 0.5, scale, "box")
		if err != nil {
			return "", nil, nil, "", err
		}
		shape := emit.Shape{ID: newID, Name: s.Name, Geom: s.Geom, PinX: tr.PinX, PinY: tr.PinY, Width: tr.Width, Height: tr.Height, LocPinX: tr.LocPinX, LocPinY: tr.LocPinY, Angle: tr.Angle, FlipX: tr.FlipX, FlipY: tr.FlipY, Style: 3}
		if s.Fill != nil && !s.Fill.None {
			shape.FillHex = s.Fill.Hex
		}
		if s.Line != nil && !s.Line.None {
			shape.LineHex = s.Line.Color
			if s.Line.W != nil {
				shape.LineWeight = *s.Line.W * scale / vsd.PointsPerInch
			}
		}
		frag, err = emit.ShapeXML(shape)
		if err != nil {
			return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "box", "%s", err.Error())
		}
	}
	addr, err := ws.InsertShape(pageAddr, parent, frag)
	if err != nil {
		return "", nil, nil, "", err
	}
	if err := addText(ws, addr, s.HTML); err != nil {
		return "", nil, nil, "", err
	}
	if err := addPaint(ws, addr, s, true); err != nil {
		return "", nil, nil, "", err
	}
	return addr, []string{addr}, nil, "", nil
}

// addPlannedTransform places one box or pin with Unplace or the pin map.
func addPlannedTransform(s *ObjectAddSpec, frame place.Frame, height, locX, locY, scale float64, field string) (place.Transform, error) {
	if s.Box != nil {
		if !addFinite(s.Box) {
			return place.Transform{}, cli.Errorf(cli.CodeOperationFailed, "box", "box must hold finite numbers")
		}
		want := geom.Box{X: s.Box[0] * scale, Y: s.Box[1] * scale, W: s.Box[2] * scale, H: s.Box[3] * scale}
		tr, err := place.Unplace(frame, want, height, locX, locY)
		if err != nil {
			return place.Transform{}, cli.Errorf(cli.CodeOperationFailed, "box", "the box cannot be placed in %s: %s", field, err.Error())
		}
		return tr, nil
	}
	if !addFinite(s.Pin) {
		return place.Transform{}, cli.Errorf(cli.CodeOperationFailed, "pin", "pin must hold finite numbers")
	}
	if locX == 0.5 && locY == 0.5 && frame == place.Page {
		return addPinTransform(s.Pin, scale, height)
	}
	return addPinFrame(s.Pin, scale, height, frame, locX, locY)
}

// addFinite reports whether every value is finite.
func addFinite(v []float64) bool {
	for _, f := range v {
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return false
		}
	}
	return true
}

// addPinTransform maps a centre pin of a master-less shape with a 1x1
// inch size in the page frame.
func addPinTransform(pin []float64, scale, height float64) (place.Transform, error) {
	view := geom.Pt{X: pin[0] * scale, Y: pin[1] * scale}
	viewInv, ok := place.View(height).Invert()
	if !ok {
		return place.Transform{}, cli.Errorf(cli.CodeOperationFailed, "pin", "the pin cannot be placed")
	}
	p := viewInv.Apply(view)
	return place.Transform{PinX: p.X, PinY: p.Y, Width: 1, Height: 1, LocPinX: 0.5, LocPinY: 0.5}, nil
}

// addPinFrame maps a centre pin of a master instance in a group frame.
func addPinFrame(pin []float64, scale, height float64, frame place.Frame, locX, locY float64) (place.Transform, error) {
	view := geom.Pt{X: pin[0] * scale, Y: pin[1] * scale}
	x, y, ok := addViewToParent(frame, view, height)
	if !ok {
		return place.Transform{}, cli.Errorf(cli.CodeOperationFailed, "pin", "the pin cannot be placed")
	}
	return place.Transform{PinX: x, PinY: y, LocPinX: locX, LocPinY: locY}, nil
}

// addMasterShape builds the XML of one master instance.
func addMasterShape(_ *edit.Workspace, d *doc.Document, _ *inherit.Resolver, newID, masterID uint32, entry *doc.MasterEntry, s *ObjectAddSpec, frame place.Frame, height, locX, locY, scale float64) ([]byte, error) {
	name := s.Name
	if name == "" {
		name = entry.NameU
		if name == "" {
			name = entry.Name
		}
	}
	contents, err := d.MasterContents(masterID)
	if err != nil {
		e := cli.Errorf(cli.CodeUnreadableTarget, "master", "%s cannot be edited safely", vsd.MasterID(masterID))
		e.Detail = err.Error()
		return nil, e
	}
	top := addShapesChild(contents.Root())
	if top == nil {
		return nil, cli.Errorf(cli.CodeOperationFailed, "master", "%s has no shapes", vsd.MasterID(masterID))
	}
	var topShape *tree.Node
	for n := top.LastChild(); n != nil; n = n.PrevSibling() {
		if n.Kind() == tree.ElementNode && n.Local() == vsd.ElemShape {
			topShape = n
		}
	}
	if topShape == nil {
		return nil, cli.Errorf(cli.CodeOperationFailed, "master", "%s has no shapes", vsd.MasterID(masterID))
	}
	if s.Box != nil {
		if !addFinite(s.Box) {
			return nil, cli.Errorf(cli.CodeOperationFailed, "box", "box must hold finite numbers")
		}
		want := geom.Box{X: s.Box[0] * scale, Y: s.Box[1] * scale, W: s.Box[2] * scale, H: s.Box[3] * scale}
		tr, err := place.Unplace(frame, want, height, locX, locY)
		if err != nil {
			return nil, cli.Errorf(cli.CodeOperationFailed, "box", "the box cannot be placed: %s", err.Error())
		}
		return addInstanceFrag(topShape, d, newID, masterID, name, tr, true), nil
	}
	if !addFinite(s.Pin) {
		return nil, cli.Errorf(cli.CodeOperationFailed, "pin", "pin must hold finite numbers")
	}
	view := geom.Pt{X: s.Pin[0] * scale, Y: s.Pin[1] * scale}
	x, y, ok := addViewToParent(frame, view, height)
	if !ok {
		return nil, cli.Errorf(cli.CodeOperationFailed, "pin", "the pin cannot be placed")
	}
	return addInstanceFrag(topShape, d, newID, masterID, name, place.Transform{PinX: x, PinY: y}, false), nil
}

// addInstanceFrag writes one master instance and its nested instances.
func addInstanceFrag(topShape *tree.Node, _ *doc.Document, newID, masterID uint32, name string, tr place.Transform, hasSize bool) []byte {
	var b strings.Builder
	next := newID
	addInstanceShape(&b, topShape, &next, masterID, name, tr, hasSize, true)
	return []byte(b.String())
}

// addInstanceShape writes one instance shape element for a master shape.
func addInstanceShape(b *strings.Builder, masterNode *tree.Node, next *uint32, masterID uint32, name string, tr place.Transform, hasSize, isTop bool) {
	id := *next
	*next++
	masterShapeID, _ := masterNode.Attr("ID")
	typ, _ := masterNode.Attr("Type")
	if typ == "" {
		typ = "Shape"
	}
	b.WriteString("<" + vsd.ElemShape + " ID='" + strconv.FormatUint(uint64(id), 10) + "'")
	if name != "" && isTop {
		escaped := xmlscan.EscapeAttr(name, '\'')
		b.WriteString(" NameU='" + escaped + "' Name='" + escaped + "'")
	}
	b.WriteString(" Type='" + typ + "'")
	if isTop {
		b.WriteString(" Master='" + strconv.FormatUint(uint64(masterID), 10) + "'")
	} else {
		b.WriteString(" MasterShape='" + masterShapeID + "'")
	}
	b.WriteByte('>')
	if isTop {
		b.WriteString("<Cell N='" + vsd.CellPinX + "' V='" + vsd.FormatInches(tr.PinX) + "'/>")
		b.WriteString("<Cell N='" + vsd.CellPinY + "' V='" + vsd.FormatInches(tr.PinY) + "'/>")
		if hasSize {
			b.WriteString("<Cell N='" + vsd.CellWidth + "' V='" + vsd.FormatInches(tr.Width) + "'/>")
			b.WriteString("<Cell N='" + vsd.CellHeight + "' V='" + vsd.FormatInches(tr.Height) + "'/>")
		}
	}
	nested := addShapesChild(masterNode)
	if nested != nil {
		b.WriteString("<" + vsd.ElemShapes + ">")
		for n := nested.FirstChild(); n != nil; n = n.NextSibling() {
			if n.Kind() == tree.ElementNode && n.Local() == vsd.ElemShape {
				addInstanceShape(b, n, next, masterID, "", place.Transform{}, false, false)
			}
		}
		b.WriteString("</" + vsd.ElemShapes + ">")
	}
	b.WriteString("</" + vsd.ElemShape + ">")
}

// addConnector adds a master-less straight connector.
func (s *ObjectAddSpec) addConnector(ws *edit.Workspace, aliases map[string]string, r *inherit.Resolver, pg *addPage, pageAddr, parent string, container uint32, dup int, scale float64) (string, []string, []string, string, error) {
	located := map[string]addLocated{}
	wanted := map[string]bool{}
	if parent != "" {
		wanted[parent] = true
	}
	var fromGlue, toGlue addGlue
	var err error
	if s.FromGlue != "" {
		fromGlue, err = addParseGlue(s.FromGlue, aliases, pageAddr, container, dup, "from_glue")
		if err != nil {
			return "", nil, nil, "", err
		}
		if !fromGlue.hasPoint {
			return "", nil, nil, "", addWholeGlue("from_glue", s.FromGlue)
		}
		wanted[fromGlue.target] = true
	}
	if s.ToGlue != "" {
		toGlue, err = addParseGlue(s.ToGlue, aliases, pageAddr, container, dup, "to_glue")
		if err != nil {
			return "", nil, nil, "", err
		}
		if !toGlue.hasPoint {
			return "", nil, nil, "", addWholeGlue("to_glue", s.ToGlue)
		}
		wanted[toGlue.target] = true
	}
	if len(wanted) > 0 {
		located = addLocate(pg.root, pageAddr, wanted, r)
	}
	frame, err := addParentFrame(parent, located)
	if err != nil {
		return "", nil, nil, "", err
	}
	beginView, err := addEndView(s.From, s.FromGlue != "", fromGlue, located, "from", "from_glue", pg.height, scale)
	if err != nil {
		return "", nil, nil, "", err
	}
	endView, err := addEndView(s.To, s.ToGlue != "", toGlue, located, "to", "to_glue", pg.height, scale)
	if err != nil {
		return "", nil, nil, "", err
	}
	beginX, beginY, ok := addViewToParent(frame, beginView, pg.height)
	if !ok {
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "from", "the begin cannot be placed")
	}
	endX, endY, ok := addViewToParent(frame, endView, pg.height)
	if !ok {
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "to", "the end cannot be placed")
	}
	newID, err := ws.NextShapeID(pageAddr)
	if err != nil {
		return "", nil, nil, "", err
	}
	begin := emit.End{X: beginX, Y: beginY}
	end := emit.End{X: endX, Y: endY}
	if s.FromGlue != "" {
		begin.Glued, begin.Target, begin.Point = true, fromGlue.id, fromGlue.point
	}
	if s.ToGlue != "" {
		end.Glued, end.Target, end.Point = true, toGlue.id, toGlue.point
	}
	frag, connects, ok := emit.ConnectorXML(newID, begin, end)
	if !ok {
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "to", "the connector ends coincide; give distinct from and to positions")
	}
	if s.Name != "" {
		frag = addNamedConnector(frag, s.Name)
	}
	addr, err := ws.InsertShape(pageAddr, parent, frag)
	if err != nil {
		return "", nil, nil, "", err
	}
	if len(connects) > 0 {
		if err := ws.InsertConnects(pageAddr, connects); err != nil {
			return "", nil, nil, "", err
		}
	}
	if err := addText(ws, addr, s.HTML); err != nil {
		return "", nil, nil, "", err
	}
	if err := addPaint(ws, addr, s, false); err != nil {
		return "", nil, nil, "", err
	}
	return addr, []string{addr}, nil, "", nil
}

// addWholeGlue refuses whole-shape glue on a master-less connector.
func addWholeGlue(field, raw string) error {
	return wholeGlueRefusal(raw, field)
}

// addEndView returns one connector end as a view point in points.
func addEndView(point []float64, glued bool, g addGlue, located map[string]addLocated, field, glueField string, height, scale float64) (geom.Pt, error) {
	if !glued {
		if !addFinite(point) {
			return geom.Pt{}, cli.Errorf(cli.CodeOperationFailed, field, "%s must hold finite numbers", field)
		}
		return geom.Pt{X: point[0] * scale, Y: point[1] * scale}, nil
	}
	loc, ok := located[g.target]
	if !ok {
		return geom.Pt{}, cli.Errorf(cli.CodeNotFound, glueField, "no shape %s: not found", g.target)
	}
	if !loc.ok {
		return geom.Pt{}, cli.Errorf(cli.CodeOperationFailed, glueField, "%s has no readable transform", g.target)
	}
	for _, pt := range loc.pts {
		if pt.ix+1 == g.point {
			return place.Site(loc.f, loc.t, pt.x, pt.y, height), nil
		}
	}
	return geom.Pt{}, cli.Errorf(cli.CodeNotFound, glueField, "connection point %d of %s is not found", g.point, g.target)
}

// addNamedConnector writes Name attributes into one connector fragment.
func addNamedConnector(frag []byte, name string) []byte {
	escaped := xmlscan.EscapeAttr(name, '\'')
	needle := []byte(" Type='Shape'")
	insert := []byte(" NameU='" + escaped + "' Name='" + escaped + "'")
	at := 0
	for i := 0; i+len(needle) <= len(frag); i++ {
		match := true
		for j := range needle {
			if frag[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			at = i
			break
		}
	}
	out := make([]byte, 0, len(frag)+len(insert))
	out = append(out, frag[:at]...)
	out = append(out, insert...)
	out = append(out, frag[at:]...)
	return out
}

// addText writes the HTML of one new shape through emit and SetText.
func addText(ws *edit.Workspace, addr string, html *string) error {
	if html == nil {
		return nil
	}
	body, formats, err := text.ParseHTML(*html)
	if err != nil {
		return cli.Errorf(cli.CodeOperationFailed, "html", "%s", err.Error())
	}
	for _, para := range body.Paras {
		for _, run := range para.Runs {
			if run.Ref != "" {
				return cli.Errorf(cli.CodeNotFound, "html", "field %s is not in %s", run.Ref, addr)
			}
		}
	}
	sections, textEl, err := emit.TextXML(body, formats)
	if err != nil {
		return cli.Errorf(cli.CodeOperationFailed, "html", "%s", err.Error())
	}
	if err := ws.SetText(addr, sections, textEl); err != nil {
		return fmt.Errorf("vsdx object.add text %s: %w", addr, err)
	}
	return nil
}

// addPaint writes fill and line overrides not already in the fragment.
func addPaint(ws *edit.Workspace, addr string, s *ObjectAddSpec, emitted bool) error {
	var cells []edit.Cell
	if s.Fill != nil {
		if s.Fill.None {
			cells = append(cells, edit.Cell{N: vsd.CellFillPattern, V: "0"})
		} else if !emitted {
			cells = append(cells, edit.Cell{N: vsd.CellFillForegnd, V: s.Fill.Hex})
			cells = append(cells, edit.Cell{N: vsd.CellFillPattern, V: "1"})
		}
	}
	if s.Line != nil {
		if s.Line.None {
			cells = append(cells, edit.Cell{N: vsd.CellLinePattern, V: "0"})
		} else {
			if s.Line.Color != "" && !emitted {
				cells = append(cells, edit.Cell{N: vsd.CellLineColor, V: s.Line.Color})
			}
			if s.Line.W != nil && !emitted {
				inches := *s.Line.W / vsd.PointsPerInch
				cells = append(cells, edit.Cell{N: vsd.CellLineWeight, V: vsd.FormatInches(inches)})
			}
			if s.Line.Dash != "" {
				cells = append(cells, edit.Cell{N: vsd.CellLinePattern, V: s.Line.Dash})
			}
		}
	}
	if len(cells) == 0 {
		return nil
	}
	return ws.SetCells(addr, cells)
}
