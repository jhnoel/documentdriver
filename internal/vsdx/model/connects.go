package model

import (
	"math"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/place"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// placed is what the walk keeps of each shape for the glue pass: its
// object, the frame it sits in, and the effective master it carries
// for a later re-index. It keeps no indexed sheet: the walk releases
// each shape's sheet once its object is built, and the glue pass
// indexes on demand only the shapes a Connect names. The s field is
// kept for the unit tests, which build placed values by hand with a
// sheet already indexed; the walk never sets it.
type placed struct {
	obj       *Object
	s         *inherit.Shape
	frame     place.Frame
	master    uint32
	hasMaster bool
}

// linker carries one linkConnects pass: the page, the resolver, the
// placed shapes by stored ID, the page height, the index of each
// connector's connection, memo, which holds a re-indexed sheet per
// named shape, each indexed at most once for the pass, and left,
// which counts how many remaining Connect rows still name a shape,
// so a memoised sheet is released as soon as its last use is done.
type linker struct {
	p     *Page
	r     *inherit.Resolver
	byID  map[uint32]*placed
	pageH float64
	index map[uint32]int
	memo  map[uint32]*inherit.Shape
	left  map[uint32]int
}

// release drops the memoised sheet of id once its last naming
// Connect row has been handled, so the pass holds only the sheets
// of shapes still to come.
func (l *linker) release(id uint32) {
	if l.left[id] > 0 {
		l.left[id]--
	}
	if l.left[id] == 0 {
		delete(l.memo, id)
	}
}

// countNames counts, for every Connect row of connects, how often a
// stored shape ID is named as FromSheet or ToSheet: the release
// schedule of one linkConnects pass.
func countNames(connects *tree.Node) map[uint32]int {
	left := map[uint32]int{}
	for c := range connects.ElementsSeq() {
		if c.Local() != vsd.ElemConnect {
			continue
		}
		for name, value := range c.AttrsSeq() {
			if name != "FromSheet" && name != "ToSheet" {
				continue
			}
			if u, err := strconv.ParseUint(value, 10, 32); err == nil && u != 0 {
				left[uint32(u)]++
			}
		}
	}
	return left
}

// shapeOf returns the resolver shape of pl, indexing its node on
// first use and memoising it for the pass: a placed shape the walk
// already indexed (s non-nil, the unit tests) is used as-is, and a
// shape with no node has no sheet. Each shape is indexed at most
// once per pass.
func (l *linker) shapeOf(id uint32, pl *placed) *inherit.Shape {
	if pl == nil {
		return nil
	}
	if pl.s != nil {
		return pl.s
	}
	if s, ok := l.memo[id]; ok {
		return s
	}
	if pl.obj == nil || pl.obj.Node == nil {
		return nil
	}
	s := indexShape(pl.obj.Node, pl.master, pl.hasMaster)
	l.memo[id] = s
	return s
}

// linkConnects reads the page's Connects element (nil when none) into
// p.Connections, each connector's FromGlue and ToGlue, and each other
// shape's Glued (non-end FromCells), and adds the hazards:
// dangling-glue for a Connect naming a missing shape, stale-connector
// for a glued end farther than place.GlueTolerance (math.Hypot in view
// points) from its site (point glue) or outside its target's box
// (whole-shape glue). A Connect naming no FromSheet is a note. Chunk MF2.
func linkConnects(p *Page, r *inherit.Resolver, connects *tree.Node, byID map[uint32]*placed, pageH float64) {
	if p == nil || connects == nil {
		return
	}
	l := &linker{p: p, r: r, byID: byID, pageH: pageH, index: map[uint32]int{}, memo: map[uint32]*inherit.Shape{}, left: countNames(connects)}
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
		which := "to"
		glueName := "to_glue"
		if begin {
			which = "from"
			glueName = "from_glue"
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
				Detail: glueName + " names shape " + strconv.FormatUint(uint64(toID), 10) + ", which is not on the page"})
			continue
		}
		l.stale(fromID, fromPl, begin, which, toID, toPl, toCell, glue)
		l.release(fromID)
		l.release(toID)
	}
}

// stale reports a stale-connector hazard on the connector of fromPl
// when its begin (begin true, which "from") or end (which "to") is
// farther than place.GlueTolerance from its point-glue site or outside
// its whole-shape target's box. An end or site that cannot be derived
// is left unreported: staleness is unknown, not false. The end, the
// target's box and its sites are read from the objects the walk
// already derived (From, To, Box, Sites, computed from the same
// cells in the same frame); only when a value was not derived there
// is the shape re-indexed on demand (shapeOf) and recomputed as
// before, so a shape a Connect names is indexed only when the
// staleness check truly needs it.
func (l *linker) stale(fromID uint32, fromPl *placed, begin bool, which string, toID uint32, toPl *placed, toCell, glue string) {
	end, ok := l.endOf(fromID, fromPl, begin)
	if !ok {
		return
	}
	if toCell == vsd.ToCellPin {
		box, ok := l.boxOf(toID, toPl)
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
				Detail: which + " end is " + geom.FormatNum(d) + " pt outside shape " + glue})
		}
		return
	}
	n, ok := siteNumber(toCell)
	if !ok {
		return
	}
	site, found := l.sitePoint(toID, toPl, n)
	if !found {
		return
	}
	if d := math.Hypot(end.X-site.X, end.Y-site.Y); d > place.GlueTolerance {
		l.p.Hazards = append(l.p.Hazards, Hazard{Kind: "stale-connector", ID: fromPl.obj.ID,
			Detail: which + " end is " + geom.FormatNum(d) + " pt from site " + glue})
	}
}

// endOf returns the connector's begin (begin true) or end view point:
// the From or To the walk derived, or, when that was not derived,
// the end recomputed from the shape indexed on demand. It reports
// false when the end cannot be derived.
func (l *linker) endOf(fromID uint32, fromPl *placed, begin bool) (geom.Pt, bool) {
	obj := fromPl.obj
	if begin && obj.From != nil {
		return *obj.From, true
	}
	if !begin && obj.To != nil {
		return *obj.To, true
	}
	xName, yName := vsd.CellEndX, vsd.CellEndY
	if begin {
		xName, yName = vsd.CellBeginX, vsd.CellBeginY
	}
	pt, _, reason := endPoint(l.r, l.shapeOf(fromID, fromPl), fromPl.frame, l.pageH, xName, yName)
	if reason != "" || pt == nil {
		return geom.Pt{}, false
	}
	return *pt, true
}

// boxOf returns the target's view box: the Box the walk derived, or,
// when that was not derived, the box of the shape indexed on demand.
// It reports false when the box cannot be derived.
func (l *linker) boxOf(toID uint32, toPl *placed) (geom.Box, bool) {
	if toPl.obj.Box != nil {
		return *toPl.obj.Box, true
	}
	t, _, ok := transformOf(l.r, l.shapeOf(toID, toPl))
	if !ok {
		return geom.Box{}, false
	}
	box, err := place.Box(toPl.frame, t, l.pageH)
	if err != nil {
		return geom.Box{}, false
	}
	return box, true
}

// sitePoint returns the target's connection site numbered n: the Sites
// the walk derived (with Sites or Full), or, when they were not
// derived, the one site of the shape indexed on demand (siteAt, so no
// site list is built just to scan it). It reports false when the
// target has no Connection row to glue to.
func (l *linker) sitePoint(toID uint32, toPl *placed, n int) (geom.Pt, bool) {
	if toPl.obj.Sites != nil {
		for _, s := range toPl.obj.Sites {
			if s.N == n {
				return s.Pt, true
			}
		}
		return geom.Pt{}, false
	}
	t, _, ok := transformOf(l.r, l.shapeOf(toID, toPl))
	if !ok {
		return geom.Pt{}, false
	}
	return siteAt(l.r, l.shapeOf(toID, toPl), toPl.frame, t, l.pageH, n)
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
