package model

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/place"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// placed is what the walk (M1) keeps of each shape for the glue pass:
// its object, the frame it sits in, its transform (ok false when not
// derived) and its sites.
type placed struct {
	obj   *Object
	frame place.Frame
	t     place.Transform
	hasT  bool
	sites []Site
}

// linker carries one linkConnects pass: the page, the placed shapes
// by stored ID, the page height and the lazily indexed sheets of
// shapes whose derived values are absent (one index per shape, not
// one per Connect).
type linker struct {
	p      *Page
	byID   map[uint32]*placed
	pageH  float64
	sheets map[uint32]*sheet.Sheet
	index  map[uint32]int
}

// linkConnects reads the page's Connects element (nil when none) into
// p.Connections, each connector's FromGlue and ToGlue, and each other
// shape's Glued (non-end FromCells), and adds the hazards:
// dangling-glue for a Connect naming a missing shape, stale-connector
// for a glued end farther than place.GlueTolerance (math.Hypot in view
// points) from its site (point glue) or outside its target's box
// (whole-shape glue). A Connect naming no FromSheet is a note. Chunk M4.
func linkConnects(p *Page, connects *tree.Node, byID map[uint32]*placed, pageH float64) {
	if p == nil || connects == nil {
		return
	}
	l := &linker{p: p, byID: byID, pageH: pageH, index: map[uint32]int{}}
	for c := range connects.ElementsSeq() {
		if c.Local() != vsd.ElemConnect {
			continue
		}
		if gluedOf(p, c, byID) {
			continue
		}
		var fromID, toID uint32
		var fromOK bool
		fromCell, toCell := "", ""
		for name, value := range c.AttrsSeq() {
			switch name {
			case "FromSheet":
				if u, err := strconv.ParseUint(value, 10, 32); err == nil {
					fromID, fromOK = uint32(u), true
				}
			case "ToSheet":
				if u, err := strconv.ParseUint(value, 10, 32); err == nil {
					toID = uint32(u)
				}
			case "FromCell":
				fromCell = value
			case "ToCell":
				toCell = value
			}
		}
		begin := fromCell == vsd.FromCellBegin
		if !begin && fromCell != vsd.FromCellEnd {
			continue
		}
		fromPl, found := byID[fromID]
		if !fromOK || fromID == 0 || !found || fromPl == nil || fromPl.obj == nil {
			p.Notes = append(p.Notes, doc.Note{At: p.ID, Reason: "connect from sheet names no shape"})
			continue
		}
		toPl, found := byID[toID]
		resolved := found && toPl != nil && toPl.obj != nil
		glue := vsd.Glue(toID, toCell, resolved)
		which := "end"
		if begin {
			which = "begin"
		}
		idx, seen := l.index[fromID]
		if !seen {
			idx = len(p.Connections)
			l.index[fromID] = idx
			p.Connections = append(p.Connections, Connection{Connector: fromPl.obj.ID})
		}
		conn := &p.Connections[idx]
		if begin {
			fromPl.obj.FromGlue, conn.FromGlue = glue, glue
			if resolved {
				conn.From = toPl.obj.ID
			}
		} else {
			fromPl.obj.ToGlue, conn.ToGlue = glue, glue
			if resolved {
				conn.To = toPl.obj.ID
			}
		}
		if !resolved {
			p.Hazards = append(p.Hazards, Hazard{Kind: "dangling-glue", ID: fromPl.obj.ID,
				Detail: fmt.Sprintf("%s glued to id %d, which is not on the page", which, toID)})
			continue
		}
		l.stale(fromPl, fromID, begin, which, toPl, toID, toCell)
	}
}

// stale reports a stale-connector hazard on the connector of fromPl
// when its begin (begin true) or end is farther than place.GlueTolerance
// from its point-glue site or outside its whole-shape target's box.
// An end or site that cannot be derived is left unreported: staleness
// is unknown, not false.
func (l *linker) stale(fromPl *placed, fromID uint32, begin bool, which string, toPl *placed, toID uint32, toCell string) {
	end, ok := l.endPoint(fromPl, fromID, begin)
	if !ok {
		return
	}
	if toCell == vsd.ToCellPin {
		box, ok := l.boxOf(toPl, toID)
		if !ok {
			return
		}
		inv, ok := box.Matrix().Invert()
		if !ok {
			return
		}
		local := inv.Apply(end)
		dx, dy := 0.0, 0.0
		if local.X < 0 {
			dx = -local.X
		} else if local.X > box.W {
			dx = local.X - box.W
		}
		if local.Y < 0 {
			dy = -local.Y
		} else if local.Y > box.H {
			dy = local.Y - box.H
		}
		if d := math.Hypot(dx, dy); d > place.GlueTolerance {
			l.p.Hazards = append(l.p.Hazards, Hazard{Kind: "stale-connector", ID: fromPl.obj.ID,
				Detail: fmt.Sprintf("%s is %s pt outside %s", which, geom.FormatNum(d), toPl.obj.ID)})
		}
		return
	}
	n, ok := siteNumber(toCell)
	if !ok {
		return
	}
	site, ok := l.sitePoint(toPl, toID, n)
	if !ok {
		return
	}
	if d := math.Hypot(end.X-site.X, end.Y-site.Y); d > place.GlueTolerance {
		l.p.Hazards = append(l.p.Hazards, Hazard{Kind: "stale-connector", ID: fromPl.obj.ID,
			Detail: fmt.Sprintf("%s is %s pt from %s:%d", which, geom.FormatNum(d), toPl.obj.ID, n)})
	}
}

// siteNumber returns the 1-based point number of a Connections.Xn
// ToCell, or false for any other ToCell.
func siteNumber(toCell string) (int, bool) {
	rest, ok := strings.CutPrefix(toCell, vsd.ToCellConnectionPfx)
	if !ok || rest == "" {
		return 0, false
	}
	for i := 0; i < len(rest); i++ {
		if rest[i] < '0' || rest[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

// endPoint returns the view point of the begin or end of fromPl's
// shape: the object's derived end, or its stored BeginX/BeginY or
// EndX/EndY through place.Point when the end was not derived.
func (l *linker) endPoint(fromPl *placed, fromID uint32, begin bool) (geom.Pt, bool) {
	if begin && fromPl.obj.From != nil {
		return *fromPl.obj.From, true
	}
	if !begin && fromPl.obj.To != nil {
		return *fromPl.obj.To, true
	}
	sh := l.sheetOf(fromPl, fromID)
	if sh == nil {
		return geom.Pt{}, false
	}
	xName, yName := vsd.CellEndX, vsd.CellEndY
	if begin {
		xName, yName = vsd.CellBeginX, vsd.CellBeginY
	}
	x, okX := cellNum(sh, sheet.ShapeCell(xName))
	y, okY := cellNum(sh, sheet.ShapeCell(yName))
	if !okX || !okY {
		return geom.Pt{}, false
	}
	return place.Point(fromPl.frame, x, y, l.pageH), true
}

// boxOf returns the view box of toPl's shape: the object's derived
// box, or place.Box of its transform when the box was not derived.
func (l *linker) boxOf(toPl *placed, toID uint32) (geom.Box, bool) {
	if toPl.obj.Box != nil {
		return *toPl.obj.Box, true
	}
	t, ok := l.transform(toPl, toID)
	if !ok {
		return geom.Box{}, false
	}
	box, err := place.Box(toPl.frame, t, l.pageH)
	if err != nil {
		return geom.Box{}, false
	}
	return box, true
}

// sitePoint returns the view point of connection point n of toPl's
// shape: a derived site, or place.Site of the row's stored local
// X and Y through its transform when the site was not derived.
func (l *linker) sitePoint(toPl *placed, toID uint32, n int) (geom.Pt, bool) {
	for _, s := range toPl.sites {
		if s.N == n {
			return s.Pt, true
		}
	}
	for _, s := range toPl.obj.Sites {
		if s.N == n {
			return s.Pt, true
		}
	}
	sh := l.sheetOf(toPl, toID)
	if sh == nil {
		return geom.Pt{}, false
	}
	var x, y float64
	found := false
	for _, key := range sh.Rows(vsd.SectionConnection) {
		if key.RowIX != n-1 {
			continue
		}
		xc, okX := cellNum(sh, key.WithCell("X"))
		yc, okY := cellNum(sh, key.WithCell("Y"))
		if okX && okY {
			x, y, found = xc, yc, true
		}
		break
	}
	if !found {
		return geom.Pt{}, false
	}
	t, ok := l.transform(toPl, toID)
	if !ok {
		return geom.Pt{}, false
	}
	return place.Site(toPl.frame, t, x, y, l.pageH), true
}

// transform returns the effective transform of pl's shape: the one
// the walk derived, or the shape's own stored transform cells when
// none was derived.
func (l *linker) transform(pl *placed, id uint32) (place.Transform, bool) {
	if pl.hasT {
		return pl.t, true
	}
	sh := l.sheetOf(pl, id)
	if sh == nil {
		return place.Transform{}, false
	}
	var t place.Transform
	nums := []struct {
		name string
		dst  *float64
	}{
		{vsd.CellPinX, &t.PinX}, {vsd.CellPinY, &t.PinY},
		{vsd.CellWidth, &t.Width}, {vsd.CellHeight, &t.Height},
		{vsd.CellLocPinX, &t.LocPinX}, {vsd.CellLocPinY, &t.LocPinY},
		{vsd.CellAngle, &t.Angle},
	}
	for _, num := range nums {
		v, ok := cellNum(sh, sheet.ShapeCell(num.name))
		if !ok {
			return place.Transform{}, false
		}
		*num.dst = v
	}
	for _, flip := range []struct {
		name string
		dst  *bool
	}{{vsd.CellFlipX, &t.FlipX}, {vsd.CellFlipY, &t.FlipY}} {
		c, ok := sh.Cell(sheet.ShapeCell(flip.name))
		if !ok {
			continue
		}
		b, ok := vsd.Bool(c.V)
		if !ok {
			return place.Transform{}, false
		}
		*flip.dst = b
	}
	return t, true
}

// sheetOf returns the indexed sheet of pl's shape, indexing its node
// at most once per linkConnects pass, or nil when it has no node.
func (l *linker) sheetOf(pl *placed, id uint32) *sheet.Sheet {
	if l.sheets == nil {
		l.sheets = map[uint32]*sheet.Sheet{}
	}
	if sh, ok := l.sheets[id]; ok {
		return sh
	}
	var sh *sheet.Sheet
	if pl.obj != nil && pl.obj.Node != nil {
		sh, _ = sheet.Index(pl.obj.Node)
	}
	l.sheets[id] = sh
	return sh
}

// cellNum returns the number stored at key of sh, or false when the
// cell is absent or holds no number.
func cellNum(sh *sheet.Sheet, key sheet.Key) (float64, bool) {
	if sh == nil {
		return 0, false
	}
	c, ok := sh.Cell(key)
	if !ok {
		return 0, false
	}
	return vsd.Number(c.V)
}
