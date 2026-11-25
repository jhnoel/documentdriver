package ops

import (
	"slices"
	"strings"

	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/ids"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/place"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// Run applies object.set: it calls setTransform, scaleGroupChildren
// (when the transform changed), setStyle, setTextBlock, setPicture and
// setEnds
// for the fields the spec gives, in that order, and merges their
// changed and stale reports into one. Changed holds each changed
// address once, in the order the fields first changed it, and stale
// joins the non-empty stale details with "; ". The first field that
// refuses stops the run with its error, so a refused field's own
// refusal is the one reported; the transaction rolls back any field
// already written. A spec whose fields all already show their wanted
// values changes nothing.
func (s *ObjectSetSpec) Run(ws *edit.Workspace, aliases map[string]string, units string, _ ids.Source) (string, []string, []string, string, error) {
	var changed []string
	var staleParts []string
	resized := false
	for _, step := range []func() ([]string, string, error){
		func() ([]string, string, error) {
			c, stale, err := setTransform(ws, s, aliases, units)
			resized = len(c) > 0
			return c, stale, err
		},
		func() ([]string, string, error) {
			if !resized {
				// The group's box is what it was: nothing to scale.
				return nil, "", nil
			}
			c, stale, err := scaleGroupChildren(ws, s, aliases)
			if len(stale) > 0 {
				return c, strings.Join(stale, ", ") + " left stale", err
			}
			return c, "", err
		},
		func() ([]string, string, error) { return setStyle(ws, s, aliases, units) },
		func() ([]string, string, error) { return setTextBlock(ws, s, aliases, units) },
		func() ([]string, string, error) { return setPicture(ws, s, aliases) },
		func() ([]string, string, error) {
			if endsIdentity(ws, s, aliases, units) {
				return nil, "", nil
			}
			return setEnds(ws, s, aliases, units)
		},
	} {
		c, stale, err := step()
		if err != nil {
			return "", nil, nil, "", err
		}
		for _, addr := range c {
			if !slices.Contains(changed, addr) {
				changed = append(changed, addr)
			}
		}
		if stale != "" {
			staleParts = append(staleParts, stale)
		}
	}
	return "", changed, nil, strings.Join(staleParts, "; "), nil
}

// endsIdentity reports whether every from/to point s gives is
// already the connector end read shows it, in read's form
// (geom.FormatNum points), with no glue given and neither end
// glued, so setEnds writes nothing for those fields (CC4). It
// returns false whenever the ends cannot be read confidently
// (a missing or unreadable shape, a scaled page, a refused
// shape, a glued end), so setEnds reports the proper refusal
// or write instead of this shortcut.
func endsIdentity(ws *edit.Workspace, s *ObjectSetSpec, aliases map[string]string, units string) bool {
	if s.From == nil && s.To == nil {
		return false
	}
	if s.FromGlue != nil || s.ToGlue != nil {
		return false
	}
	shapeAddr, err := resolveShape(s.At, aliases)
	if err != nil {
		return false
	}
	a, err := vsd.Parse(shapeAddr)
	if err != nil {
		return false
	}
	d, err := doc.Open(ws.Package())
	if err != nil || d.Kind == doc.Stencil {
		return false
	}
	scale := 1.0
	if units == "in" {
		scale = vsd.PointsPerInch
	}
	r := inherit.New(d)
	pg, err := addLoadPage(ws, r, a.Container, a.PageDup)
	if err != nil {
		return false
	}
	pageAddr := vsd.PageID(a.Container, a.PageDup)
	located := addLocate(pg.root, pageAddr, map[string]bool{shapeAddr: true}, r)
	loc, ok := located[shapeAddr]
	if !ok || !loc.ok {
		return false
	}
	node := findEndsNode(pg.root, a)
	if node == nil {
		return false
	}
	if err := nodeRefusal(r, node, shapeAddr, "at", false); err != nil {
		return false
	}
	field := "from"
	if s.From == nil {
		field = "to"
	}
	if err := scaledRefusal(r, d, a.Container, a.PageDup, pageAddr, field); err != nil {
		return false
	}
	h := addHead(node, 0, false)
	if _, _, has := r.Cell(h, sheet.ShapeCell(vsd.CellBeginX)); !has {
		return false
	}
	if _, _, _, present := currentGlue(pg.root, a.Shape, true); present {
		return false
	}
	if _, _, _, present := currentGlue(pg.root, a.Shape, false); present {
		return false
	}
	cur := endsCurrent{r: r, h: h}
	beginX, ok := cur.number(vsd.CellBeginX)
	if !ok {
		return false
	}
	beginY, ok := cur.number(vsd.CellBeginY)
	if !ok {
		return false
	}
	endX, ok := cur.number(vsd.CellEndX)
	if !ok {
		return false
	}
	endY, ok := cur.number(vsd.CellEndY)
	if !ok {
		return false
	}
	curBegin := place.Point(loc.f, beginX, beginY, pg.height)
	curEnd := place.Point(loc.f, endX, endY, pg.height)
	if s.From != nil && (geom.FormatNum(curBegin.X) != geom.FormatNum(s.From[0]*scale) || geom.FormatNum(curBegin.Y) != geom.FormatNum(s.From[1]*scale)) {
		return false
	}
	if s.To != nil && (geom.FormatNum(curEnd.X) != geom.FormatNum(s.To[0]*scale) || geom.FormatNum(curEnd.Y) != geom.FormatNum(s.To[1]*scale)) {
		return false
	}
	return true
}
