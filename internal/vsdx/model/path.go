package model

import (
	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/path"
	"github.com/jhnoel/documentdriver/internal/vsdx/place"
)

// pathOf sets o's geom form (docs/contract.md §8): for a master-less
// 2-D shape whose own Geometry is a polyline or curve in the form vsdx
// writes (path.Parse), the form's name, whether it is closed and its
// points in the view (each relative point scaled
// by Width and Height and placed through the shape's transform, as a
// connection site is). Rect and ellipse are not shown (read never
// showed them, and a master instance's geometry is not derived); a
// master instance, a shape whose transform
// cannot be read and any other geometry show no geom.
func pathOf(o *Object, r *inherit.Resolver, s *inherit.Shape, f place.Frame, pageH float64) {
	if o == nil || o.Kind != Shape || o.Node == nil || s == nil || s.HasMaster || s.HasMShape {
		return
	}
	g, ok := path.Parse(o.Node)
	if !ok || (g.Kind != path.Polyline && g.Kind != path.Curve) {
		return
	}
	t, _, hasT := transformOf(r, s)
	if !hasT {
		return
	}
	o.Geom, o.Closed = g.Kind, g.Closed
	if len(g.Points) == 0 {
		return
	}
	o.Points = make([]geom.Pt, len(g.Points))
	for i, p := range g.Points {
		o.Points[i] = place.Site(f, t, p.X*t.Width, p.Y*t.Height, pageH)
	}
}
