package model

import (
	"fmt"
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
// object, its resolver shape, and the frame it sits in.
type placed struct {
	obj   *Object
	s     *inherit.Shape
	frame place.Frame
}

// linker carries one linkConnects pass: the page, the resolver, the
// placed shapes by stored ID, the page height and the index of each
// connector's connection.
type linker struct {
	p     *Page
	r     *inherit.Resolver
	byID  map[uint32]*placed
	pageH float64
	index map[uint32]int
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
	l := &linker{p: p, r: r, byID: byID, pageH: pageH, index: map[uint32]int{}}
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
				Detail: fmt.Sprintf("%s names shape %d, which is not on the page", glueName, toID)})
			continue
		}
		l.stale(fromPl, begin, which, toPl, toCell, glue)
	}
}

// stale reports a stale-connector hazard on the connector of fromPl
// when its begin (begin true, which "from") or end (which "to") is
// farther than place.GlueTolerance from its point-glue site or outside
// its whole-shape target's box. An end or site that cannot be derived
// is left unreported: staleness is unknown, not false.
func (l *linker) stale(fromPl *placed, begin bool, which string, toPl *placed, toCell, glue string) {
	xName, yName := vsd.CellEndX, vsd.CellEndY
	if begin {
		xName, yName = vsd.CellBeginX, vsd.CellBeginY
	}
	pt, _, reason := endPoint(l.r, fromPl.s, fromPl.frame, l.pageH, xName, yName)
	if reason != "" || pt == nil {
		return
	}
	end := *pt
	t, _, ok := transformOf(l.r, toPl.s)
	if !ok {
		return
	}
	if toCell == vsd.ToCellPin {
		box, err := place.Box(toPl.frame, t, l.pageH)
		if err != nil {
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
				Detail: fmt.Sprintf("%s end is %s pt outside shape %s", which, geom.FormatNum(d), glue)})
		}
		return
	}
	n, ok := siteNumber(toCell)
	if !ok {
		return
	}
	var site *geom.Pt
	for _, s := range sitesOf(l.r, toPl.s, toPl.frame, t, l.pageH) {
		if s.N == n {
			pt := s.Pt
			site = &pt
			break
		}
	}
	if site == nil {
		return
	}
	if d := math.Hypot(end.X-site.X, end.Y-site.Y); d > place.GlueTolerance {
		l.p.Hazards = append(l.p.Hazards, Hazard{Kind: "stale-connector", ID: fromPl.obj.ID,
			Detail: fmt.Sprintf("%s end is %s pt from site %s", which, geom.FormatNum(d), glue)})
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
