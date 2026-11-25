package ops

import (
	"math"
	"strconv"

	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/vsdx/path"
)

// pathAddRules are the rules checkPathAdd adds to object.add's, in
// the order it returns them (help object.add prints them as rules:).
var pathAddRules = []string{
	"points and closed go with geom polyline or curve",
	"polyline and curve take points, not box or pin",
	"polyline and curve need points",
	"points must be [x, y] pairs of finite numbers",
	"an open path needs at least 2 points, a closed one 3 (a last point repeating the first is dropped)",
	"points must span a positive width and height (draw a straight line with kind connector)",
	"an open path takes no fill but none: fill needs closed",
}

// checkPathAdd reports the first path rule object.add breaks
// (pathAddRules): points and closed belong to a polyline or curve,
// which takes them instead of box or pin, needs enough finite [x, y]
// points spanning a positive width and height, and fills only when
// closed. It returns the message, the field (points[i] for a bad
// point) and true, or false.
func checkPathAdd(s *ObjectAddSpec) (string, string, bool) {
	if !isPathGeom(s.Geom) {
		if s.Points != nil || s.Closed {
			return pathAddRules[0], "points", true
		}
		return "", "", false
	}
	if s.Box != nil {
		return pathAddRules[1], "box", true
	}
	if s.Pin != nil {
		return pathAddRules[1], "pin", true
	}
	if s.Points == nil {
		return pathAddRules[2], "points", true
	}
	for i, p := range s.Points {
		if len(p) != 2 || !addFinite(p) {
			return pathAddRules[3], "points[" + strconv.Itoa(i) + "]", true
		}
	}
	pts := path.Normalize(planPoints(s.Points), s.Closed)
	if len(pts) < path.MinPoints(s.Closed) {
		return pathAddRules[4], "points", true
	}
	lo, hi := path.Bounds(pts)
	if !(hi.X-lo.X > 0) || !(hi.Y-lo.Y > 0) || math.IsInf(hi.X-lo.X, 0) || math.IsInf(hi.Y-lo.Y, 0) {
		return pathAddRules[5], "points", true
	}
	if !s.Closed && s.Fill != nil && !s.Fill.None {
		return pathAddRules[6], "fill", true
	}
	return "", "", false
}

// planPoints converts a plan's [x, y] pairs to points, in the plan's
// units; the pairs must already be checked.
func planPoints(raw [][]float64) []geom.Pt {
	pts := make([]geom.Pt, len(raw))
	for i, p := range raw {
		pts[i] = geom.Pt{X: p[0], Y: p[1]}
	}
	return pts
}
