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
	"paths takes the place of points and closed",
	"a shape of several paths takes no line begin or end",
}

// checkPathAdd reports the first path rule object.add breaks
// (pathAddRules): points and closed belong to a polyline or curve,
// which takes them instead of box or pin, needs enough finite [x, y]
// points spanning a positive width and height, and fills only when
// closed. It returns the message, the field (points[i] for a bad
// point) and true, or false.
func checkPathAdd(s *ObjectAddSpec) (string, string, bool) {
	if !isPathGeom(s.Geom) {
		switch {
		case s.Points != nil || s.Closed:
			return pathAddRules[0], "points", true
		case s.Paths != nil:
			return pathAddRules[0], "paths", true
		}
		return "", "", false
	}
	if s.Box != nil {
		return pathAddRules[1], "box", true
	}
	if s.Pin != nil {
		return pathAddRules[1], "pin", true
	}
	if s.Paths != nil {
		return checkPaths(s)
	}
	if s.Points == nil {
		return pathAddRules[2], "points", true
	}
	if msg, field, bad := checkOnePath(s.Points, s.Closed, "points"); bad {
		return msg, field, true
	}
	pts := path.Normalize(planPoints(s.Points), s.Closed)
	if msg, bad := checkSpan(pts); bad {
		return msg, "points", true
	}
	if !s.Closed && s.Fill != nil && !s.Fill.None {
		return pathAddRules[6], "fill", true
	}
	return "", "", false
}

// checkOnePath reports the first rule one path's points break, at
// field (points, or paths[i].points): each a finite [x, y] pair, and
// enough of them for closed.
func checkOnePath(raw [][]float64, closed bool, field string) (string, string, bool) {
	for i, p := range raw {
		if len(p) != 2 || !addFinite(p) {
			return pathAddRules[3], field + "[" + strconv.Itoa(i) + "]", true
		}
	}
	if len(path.Normalize(planPoints(raw), closed)) < path.MinPoints(closed) {
		return pathAddRules[4], field, true
	}
	return "", "", false
}

// checkSpan reports the span rule broken by points pts (all of a
// shape's paths together): a positive, finite width and height.
func checkSpan(pts []geom.Pt) (string, bool) {
	lo, hi := path.Bounds(pts)
	if !(hi.X-lo.X > 0) || !(hi.Y-lo.Y > 0) || math.IsInf(hi.X-lo.X, 0) || math.IsInf(hi.Y-lo.Y, 0) {
		return pathAddRules[5], true
	}
	return "", false
}

// checkPaths reports the first rule a shape of several paths breaks:
// no points or closed beside paths, each path's points as points'
// rules, a positive span over every path, a fill only when some path
// is closed, and no line begin or end.
func checkPaths(s *ObjectAddSpec) (string, string, bool) {
	if s.Points != nil || s.Closed {
		return pathAddRules[7], "paths", true
	}
	var all []geom.Pt
	anyClosed := false
	for i, p := range s.Paths {
		field := "paths[" + strconv.Itoa(i) + "].points"
		if msg, f, bad := checkOnePath(p.Points, p.Closed, field); bad {
			return msg, f, true
		}
		all = append(all, path.Normalize(planPoints(p.Points), p.Closed)...)
		anyClosed = anyClosed || p.Closed
	}
	if msg, bad := checkSpan(all); bad {
		return msg, "paths", true
	}
	if !anyClosed && s.Fill != nil && !s.Fill.None {
		return pathAddRules[6], "fill", true
	}
	if len(s.Paths) > 1 && s.Line != nil && (s.Line.Begin != nil || s.Line.End != nil) {
		return pathAddRules[8], "line", true
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
