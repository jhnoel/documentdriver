package view

import (
	"math"
	"strings"

	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/model"
	"github.com/jhnoel/documentdriver/internal/pptx/show"
)

// Detail levels.
const (
	Default = ""
	Sites   = "sites"
	Full    = "full"
)

// Options select what a view shows.
type Options struct {
	// Detail is Default, Sites or Full.
	Detail string
	// Units is "pt" (default) or "emu".
	Units string
	// IDs limits output to these short ids and the descendants of any
	// listed group. Empty means all.
	IDs []string
	// Region, if set, limits output to objects whose slide-space bounding
	// box intersects x, y, w, h (in Units).
	Region *[4]float64
	// Slide is the slide's name. With it, object ids are shown as full
	// addresses (slide3/s17), the form a plan's `at` takes. JSON and SVG
	// set it from the slide they render.
	Slide string
	// Base is the hash of the deck file ("sha256:<hex>"), shown first
	// when set.
	Base string
}

// addr is an object id as the views show it: the full address when the
// slide is known.
func (o Options) addr(id string) string {
	if o.Slide == "" || id == "" {
		return id
	}
	return o.Slide + "/" + id
}

// emu reports whether the selected units are EMU.
func (o Options) emu() bool { return o.Units == "emu" }

// sites reports whether the selected detail shows connection sites.
func (o Options) sites() bool { return o.Detail == Sites || o.Detail == Full }

// full reports whether the selected detail is full.
func (o Options) full() bool { return o.Detail == Full }

// unitName is the units label shown in both views.
func (o Options) unitName() string {
	if o.emu() {
		return "emu"
	}
	return "pt"
}

// num formats an EMU value in the selected units.
func (o Options) num(emu float64) string {
	if o.emu() {
		return derive.FormatNum(emu)
	}
	return derive.FormatNum(emu / derive.EMUPerPoint)
}

// toEMU converts a value in the selected units to EMU.
func (o Options) toEMU(v float64) float64 {
	if o.emu() {
		return v
	}
	return v * model.EMUPerPoint
}

// rot formats a rotation in degrees.
func rot(d float64) string { return derive.FormatNum(d) }

// Select returns the objects chosen by o.IDs (with their descendants), in
// document order; every object when o.IDs is empty.
func Select(s *model.Slide, ids []string) []*model.Object {
	return selected(s, Options{IDs: ids})
}

// selected returns the objects to show, in document order.
func selected(s *model.Slide, o Options) []*model.Object {
	byID := map[string]*model.Object{}
	for _, ob := range s.Objects {
		byID[ob.ID] = ob
	}
	var ids map[string]bool
	if len(o.IDs) > 0 {
		ids = map[string]bool{}
		for _, id := range o.IDs {
			// An id is given as shown (slide3/s12) or short (s12).
			if _, short, ok := strings.Cut(id, "/"); ok {
				id = short
			}
			ids[id] = true
		}
	}
	inIDs := func(ob *model.Object) bool {
		if ids == nil {
			return true
		}
		for p := ob; p != nil; p = byID[p.Parent] {
			if ids[p.ID] {
				return true
			}
			if p.Parent == "" {
				break
			}
		}
		return false
	}
	var out []*model.Object
	for _, ob := range s.Objects {
		if !inIDs(ob) {
			continue
		}
		if o.Region != nil && !intersects(ob, o) {
			continue
		}
		out = append(out, ob)
	}
	return out
}

// intersects reports whether an object's placed box intersects the selected region.
func intersects(ob *model.Object, o Options) bool {
	if ob.Box == nil || ob.Stored == nil {
		return false
	}
	c := derive.Corners(derive.Placed{M: ob.M}, ob.Stored.W, ob.Stored.H)
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, p := range c {
		minX, maxX = math.Min(minX, p.X), math.Max(maxX, p.X)
		minY, maxY = math.Min(minY, p.Y), math.Max(maxY, p.Y)
	}
	r := o.Region
	rx, ry := o.toEMU(r[0]), o.toEMU(r[1])
	rw, rh := o.toEMU(r[2]), o.toEMU(r[3])
	return maxX >= rx && minX <= rx+rw && maxY >= ry && minY <= ry+rh
}

// adjValue shows an adjust formula as the views show it.
func adjValue(f string) string { return show.AdjValue(f) }

// isConnectorShape reports whether an object is shown with endpoints
// rather than a box.
func isConnectorShape(ob *model.Object) bool { return ob.Kind == model.Connector }

// slideOmits reports whether the slide records an omission of prop.
func slideOmits(s *model.Slide, prop string) bool {
	for _, x := range s.Omitted {
		if x.Prop == prop {
			return true
		}
	}
	return false
}

// flipName is "h", "v", "hv" or "".
func flipName(x *derive.Xfrm) string {
	s := ""
	if x.FlipH {
		s += "h"
	}
	if x.FlipV {
		s += "v"
	}
	return s
}

// arrowString is the compact arrow form shared by both views.
func arrowString(a *model.Arrow) string { return show.Arrow(a) }
