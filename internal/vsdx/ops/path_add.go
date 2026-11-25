package ops

import (
	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/vsdx/path"
	"github.com/jhnoel/documentdriver/internal/vsdx/place"
)

// pathBoxSpec returns a copy of a polyline or curve object.add s whose
// box is the bounds of its points, or of every path's (in the plan's
// units), for placing the shape as a box is placed, and the paths it
// stores (path.Normalize: a closed path's repeated last point dropped),
// one for points and closed. The rules have
// checked the points.
func pathBoxSpec(s *ObjectAddSpec) (*ObjectAddSpec, []path.Subpath) {
	var subs []path.Subpath
	if s.Paths != nil {
		for _, p := range s.Paths {
			subs = append(subs, path.Subpath{Closed: p.Closed, Points: path.Normalize(planPoints(p.Points), p.Closed)})
		}
	} else {
		subs = []path.Subpath{{Closed: s.Closed, Points: path.Normalize(planPoints(s.Points), s.Closed)}}
	}
	var all []geom.Pt
	for _, sp := range subs {
		all = append(all, sp.Points...)
	}
	lo, hi := path.Bounds(all)
	c := *s
	c.Box = []float64{lo.X, lo.Y, hi.X - lo.X, hi.Y - lo.Y}
	return &c, subs
}

// relPoints maps view points pts (in the plan's units, scaled to
// points by scale) into the local space of a shape with transform tr
// placed in frame on a page height inches high, as fractions of its
// Width and Height (y up): the inverse of the map read uses to show
// them (place.Site). A transform that cannot be inverted is
// operation_failed at points.
func relPoints(pts []geom.Pt, scale float64, tr place.Transform, frame place.Frame, height float64) ([]geom.Pt, error) {
	m := tr.Local().Then(frame.M).Then(place.View(height))
	inv, ok := m.Invert()
	if !ok || !(tr.Width > 0) || !(tr.Height > 0) {
		return nil, cli.Errorf(cli.CodeOperationFailed, "points", "the points cannot be placed in this frame")
	}
	rel := make([]geom.Pt, len(pts))
	for i, p := range pts {
		l := inv.Apply(geom.Pt{X: p.X * scale, Y: p.Y * scale})
		rel[i] = geom.Pt{X: l.X / tr.Width, Y: l.Y / tr.Height}
	}
	return rel, nil
}
