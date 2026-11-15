package grade

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/model"
)

// CheckResult is the outcome of one check.
type CheckResult struct {
	Type   string `json:"type"`
	Pass   bool   `json:"pass"`
	Detail string `json:"detail,omitempty"`
	// ErrPt is the largest geometric deviation the check measured, in
	// points, when it measures one.
	ErrPt *float64 `json:"err_pt,omitempty"`
	// TargetMiss is set when the check failed because an expected target
	// is missing or has the wrong properties (used for wrong-object).
	TargetMiss bool `json:"-"`
}

const pt = model.EMUPerPoint

// env is the state checks run in.
type env struct {
	m       *Matching
	in      *model.Slide
	out     *model.Slide
	aliases map[string][]*model.Object
	targets map[string]bool
}

// sel resolves a selector to output objects.
func (e *env) sel(s string) ([]*model.Object, error) {
	switch {
	case s == "new":
		return e.m.New, nil
	case strings.HasPrefix(s, "$"):
		v, ok := e.aliases[s[1:]]
		if !ok || len(v) == 0 {
			return nil, fmt.Errorf("%s is not bound (an earlier check found nothing)", s)
		}
		return v, nil
	default:
		o := e.m.InToOut[s]
		if o == nil {
			return nil, fmt.Errorf("%s is missing from the output", s)
		}
		return []*model.Object{o}, nil
	}
}

// sels resolves a check's targets to output objects, returning an error if any fails.
func (e *env) sels(c Check) ([]*model.Object, error) {
	ss := c.Targets
	if c.Target != "" {
		ss = append([]string{c.Target}, ss...)
	}
	var out []*model.Object
	for _, s := range ss {
		v, err := e.sel(s)
		if err != nil {
			return nil, err
		}
		out = append(out, v...)
	}
	return out, nil
}

// tolOf returns a check's tolerance in points, defaulting to 0.5.
func tolOf(c Check) float64 {
	if c.Tol > 0 {
		return c.Tol
	}
	return 0.5
}

type box struct{ X, Y, W, H float64 } // points

// boxOf converts an object's EMU box to points, reporting whether it has one.
func boxOf(o *model.Object) (box, bool) {
	if o == nil || o.Box == nil {
		return box{}, false
	}
	b := o.Box
	return box{b.X / pt, b.Y / pt, b.W / pt, b.H / pt}, true
}

// edge returns the named box edge in points, or an error for an unknown name.
func (b box) edge(name string) (float64, error) {
	switch name {
	case "left", "x":
		return b.X, nil
	case "right":
		return b.X + b.W, nil
	case "top", "y":
		return b.Y, nil
	case "bottom":
		return b.Y + b.H, nil
	case "centerx":
		return b.X + b.W/2, nil
	case "centery":
		return b.Y + b.H/2, nil
	case "w":
		return b.W, nil
	case "h":
		return b.H, nil
	}
	return 0, fmt.Errorf("unknown edge %q", name)
}

// normHex returns s as uppercase hex without spaces or a leading #.
func normHex(s string) string {
	return strings.ToUpper(strings.TrimPrefix(strings.TrimSpace(s), "#"))
}

// hexClose reports whether two RRGGBB colours differ by at most 8 per
// channel: displayed hexes of modified colours are approximate.
func hexClose(a, b string) bool {
	a, b = normHex(a), normHex(b)
	if len(a) != 6 || len(b) != 6 {
		return false
	}
	for i := 0; i < 6; i += 2 {
		x, err1 := strconv.ParseUint(a[i:i+2], 16, 8)
		y, err2 := strconv.ParseUint(b[i:i+2], 16, 8)
		if err1 != nil || err2 != nil || math.Abs(float64(x)-float64(y)) > 8 {
			return false
		}
	}
	return true
}

// colorOK matches a colour against a scheme slot (by reference without
// modifiers, or by the slot's resolved hex) or a hex value.
func colorOK(c *model.Color, scheme, hex string, slide *model.Slide) (bool, string) {
	if c == nil {
		return false, "no colour"
	}
	got := c.Kind + ":" + c.Val
	if len(c.Mods) > 0 {
		got += fmt.Sprintf(" with %d modifiers", len(c.Mods))
	}
	got += " (" + c.Hex + ")"
	if scheme != "" {
		if c.Kind == "scheme" && c.Val == scheme && len(c.Mods) == 0 {
			return true, got
		}
		if want := slide.Colors[scheme]; want != "" && hexClose(c.Hex, want) {
			return true, got
		}
		return false, got
	}
	return hexClose(c.Hex, hex), got
}

// geomName returns an object's geometry name, or "custom" or empty if none.
func geomName(o *model.Object) string {
	if o.Geom == nil {
		return ""
	}
	if o.Geom.Custom {
		return "custom"
	}
	return o.Geom.Preset
}

// geomOK reports whether an object's geometry matches any listed name or *-prefix.
func geomOK(o *model.Object, geoms []string) bool {
	if len(geoms) == 0 {
		return true
	}
	g := geomName(o)
	for _, w := range geoms {
		if w == g || (strings.HasSuffix(w, "*") && strings.HasPrefix(g, strings.TrimSuffix(w, "*"))) {
			return true
		}
	}
	return false
}

// normText returns s lowercased with whitespace collapsed to single spaces.
func normText(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// f formats v rounded to two decimal places.
func f(v float64) string { return strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64) }

// errp returns a pointer to v rounded to three decimal places.
func errp(v float64) *float64 { v = math.Round(v*1000) / 1000; return &v }

// run evaluates one check.
func (e *env) run(c Check) (r CheckResult) {
	r = CheckResult{Type: c.Type, Pass: true}
	fail := func(miss bool, format string, a ...any) {
		r.Pass = false
		r.TargetMiss = r.TargetMiss || miss
		if r.Detail != "" {
			r.Detail += "; "
		}
		r.Detail += fmt.Sprintf(format, a...)
	}
	maxErr := -1.0
	measure := func(d float64) {
		if d > maxErr {
			maxErr = d
		}
	}
	defer func() {
		if maxErr >= 0 {
			r.ErrPt = errp(maxErr)
		}
	}()
	tol := tolOf(c)

	switch c.Type {
	case "fill":
		objs, err := e.sels(c)
		if err != nil {
			fail(true, "%v", err)
			break
		}
		for _, o := range objs {
			if o.Fill == nil || o.Fill.Type != "solid" {
				fail(true, "%s: no solid fill", e.m.Label(o))
				continue
			}
			if ok, got := colorOK(o.Fill.Color, c.Scheme, c.Hex, e.out); !ok {
				fail(true, "%s: fill %s", e.m.Label(o), got)
			}
		}
	case "line":
		objs, err := e.sels(c)
		if err != nil {
			fail(true, "%v", err)
			break
		}
		for _, o := range objs {
			if o.Line == nil || o.Line.None {
				fail(true, "%s: no line", e.m.Label(o))
				continue
			}
			if c.WidthPt != nil {
				if o.Line.W == nil || math.Abs(*o.Line.W/pt-*c.WidthPt) > 0.01 {
					w := "none"
					if o.Line.W != nil {
						w = f(*o.Line.W / pt)
					}
					fail(true, "%s: line width %s pt", e.m.Label(o), w)
				}
			}
			if c.Scheme != "" || c.Hex != "" {
				if ok, got := colorOK(o.Line.Color, c.Scheme, c.Hex, e.out); !ok {
					fail(true, "%s: line colour %s", e.m.Label(o), got)
				}
			}
		}
	case "box":
		objs, err := e.sels(c)
		if err != nil {
			fail(true, "%v", err)
			break
		}
		for _, o := range objs {
			b, ok := boxOf(o)
			if !ok {
				fail(true, "%s: no box", e.m.Label(o))
				continue
			}
			want := map[string]*float64{"x": c.X, "y": c.Y, "w": c.W, "h": c.H}
			if in := e.m.OutToIn[o]; in != nil && len(c.Keep) > 0 {
				ib, ok := boxOf(in)
				if ok {
					for _, k := range c.Keep {
						v, _ := ib.edge(k)
						want[k] = &v
					}
				}
			}
			for _, k := range []string{"x", "y", "w", "h"} {
				if want[k] == nil {
					continue
				}
				got, _ := b.edge(k)
				d := math.Abs(got - *want[k])
				measure(d)
				if d > tol {
					fail(true, "%s: %s %s, want %s", e.m.Label(o), k, f(got), f(*want[k]))
				}
			}
		}
	case "align":
		objs, err := e.sels(c)
		if err != nil {
			fail(true, "%v", err)
			break
		}
		var ref *float64
		if c.Value != nil {
			ref = c.Value
		} else if c.Ref != "" {
			ro, err := e.sel(c.Ref)
			if err != nil {
				fail(true, "%v", err)
				break
			}
			rb, ok := boxOf(ro[0])
			if !ok {
				fail(true, "ref has no box")
				break
			}
			v, err := rb.edge(c.Edge)
			if err != nil {
				fail(false, "%v", err)
				break
			}
			ref = &v
		}
		var vals []float64
		for _, o := range objs {
			b, ok := boxOf(o)
			if !ok {
				fail(true, "%s: no box", e.m.Label(o))
				continue
			}
			v, err := b.edge(c.Edge)
			if err != nil {
				fail(false, "%v", err)
				break
			}
			vals = append(vals, v)
			if ref != nil {
				d := math.Abs(v - *ref)
				measure(d)
				if d > tol {
					fail(true, "%s: %s %s, want %s", e.m.Label(o), c.Edge, f(v), f(*ref))
				}
			}
		}
		if ref == nil && len(vals) > 1 {
			lo, hi := vals[0], vals[0]
			for _, v := range vals {
				lo, hi = math.Min(lo, v), math.Max(hi, v)
			}
			measure(hi - lo)
			if hi-lo > tol {
				fail(true, "%s spread %s pt", c.Edge, f(hi-lo))
			}
		}
	case "distribute":
		objs, err := e.sels(c)
		if err != nil {
			fail(true, "%v", err)
			break
		}
		if len(objs) < 3 {
			fail(true, "need at least 3 objects, have %d", len(objs))
			break
		}
		edge := c.Edge
		if edge == "" {
			edge = "centerx"
		}
		var vals []float64
		for _, o := range objs {
			b, ok := boxOf(o)
			if !ok {
				fail(true, "%s: no box", e.m.Label(o))
				break
			}
			v, _ := b.edge(edge)
			vals = append(vals, v)
		}
		if !r.Pass {
			break
		}
		// Targets keep their given order; aliases are already sorted
		// left to right or in sequence order.
		n := len(vals)
		step := (vals[n-1] - vals[0]) / float64(n-1)
		for i, v := range vals {
			d := math.Abs(v - (vals[0] + float64(i)*step))
			measure(d)
			if d > tol {
				fail(true, "item %d %s %s, even spacing puts it at %s", i+1, edge, f(v), f(vals[0]+float64(i)*step))
			}
		}
	case "order":
		objs, err := e.sels(c)
		if err != nil {
			fail(true, "%v", err)
			break
		}
		edge := "centerx"
		if c.Axis == "y" {
			edge = "centery"
		}
		prev := math.Inf(-1)
		for _, o := range objs {
			b, ok := boxOf(o)
			if !ok {
				fail(true, "%s: no box", e.m.Label(o))
				break
			}
			v, _ := b.edge(edge)
			if v <= prev {
				fail(true, "%s is not after the previous object along %s", e.m.Label(o), c.Axis)
			}
			prev = v
		}
	case "same_size":
		objs, err := e.sels(c)
		if err != nil {
			fail(true, "%v", err)
			break
		}
		var b0 box
		for i, o := range objs {
			b, ok := boxOf(o)
			if !ok {
				fail(true, "%s: no box", e.m.Label(o))
				break
			}
			if i == 0 {
				b0 = b
				continue
			}
			d := math.Max(math.Abs(b.W-b0.W), math.Abs(b.H-b0.H))
			measure(d)
			if d > tol {
				fail(true, "%s is %sx%s, first is %sx%s", e.m.Label(o), f(b.W), f(b.H), f(b0.W), f(b0.H))
			}
		}
	case "connector":
		e.connector(c, fail, measure)
	case "no_connector":
		a, err1 := e.sel(c.From)
		b, err2 := e.sel(c.To)
		if err1 != nil || err2 != nil {
			break // a deleted end leaves no connector between them
		}
		for _, o := range e.out.Objects {
			if o.Conn == nil || o.Conn.Start == nil || o.Conn.End == nil {
				continue
			}
			s, t := o.Conn.Start.ID, o.Conn.End.ID
			if (s == a[0].ID && t == b[0].ID) || (s == b[0].ID && t == a[0].ID) {
				fail(true, "%s still joins %s and %s", e.m.Label(o), c.From, c.To)
			}
		}
	case "deleted":
		for _, s := range append([]string{c.Target}, c.Targets...) {
			if s != "" && e.m.InToOut[s] != nil {
				fail(true, "%s is still on the slide", s)
			}
		}
	case "resolved":
		// The target is gone, or both its ends are glued.
		for _, s := range append([]string{c.Target}, c.Targets...) {
			if s == "" {
				continue
			}
			o := e.m.InToOut[s]
			if o != nil && (o.Conn == nil || o.Conn.Start == nil || o.Conn.End == nil) {
				fail(true, "%s is still on the slide with a loose end", s)
			}
		}
	case "new_shape", "new_objects":
		var found []*model.Object
		for _, o := range e.m.New {
			if c.Kind != "" && string(o.Kind) != c.Kind {
				continue
			}
			if c.Kind == "" && c.Type == "new_shape" && o.Kind != model.Shape {
				continue
			}
			if c.Text != "" && !strings.Contains(normText(e.m.Text(o)), normText(c.Text)) {
				continue
			}
			if c.Custom && (o.Geom == nil || !o.Geom.Custom) {
				continue
			}
			if !geomOK(o, c.Geoms) {
				continue
			}
			found = append(found, o)
		}
		want := 1
		if c.Count > 0 {
			want = c.Count
		}
		if len(found) < want {
			fail(true, "found %d new object(s) matching, want %d", len(found), want)
			break
		}
		if c.Type == "new_shape" {
			found = found[:1]
		}
		sortX(found)
		if c.Alias != "" {
			e.aliases[c.Alias] = found
		}
	case "new_line":
		var found []*model.Object
		for _, o := range e.m.New {
			if o.Conn == nil {
				continue
			}
			dx, dy := (o.Conn.To.X-o.Conn.From.X)/pt, (o.Conn.To.Y-o.Conn.From.Y)/pt
			if c.Orient == "h" && (math.Abs(dy) > tolOf(c) || math.Abs(dx) < c.MinLen) {
				continue
			}
			if c.Orient == "v" && (math.Abs(dx) > tolOf(c) || math.Abs(dy) < c.MinLen) {
				continue
			}
			found = append(found, o)
		}
		if len(found) == 0 {
			fail(true, "no new %s line of at least %s pt", c.Orient, f(c.MinLen))
			break
		}
		if c.Alias != "" {
			e.aliases[c.Alias] = found[:1]
		}
	case "sequence":
		var seq []*model.Object
		used := map[*model.Object]bool{}
		for _, t := range c.Texts {
			var hit *model.Object
			for _, o := range e.m.New {
				if !used[o] && normText(e.m.Text(o)) == normText(t) {
					hit = o
					break
				}
			}
			if hit == nil {
				for _, o := range e.m.New {
					if !used[o] && strings.Contains(normText(e.m.Text(o)), normText(t)) {
						hit = o
						break
					}
				}
			}
			if hit == nil {
				fail(true, "no new object with text %q", t)
				continue
			}
			used[hit] = true
			if !geomOK(hit, c.Geoms) {
				fail(true, "%q has geometry %s", t, geomName(hit))
			}
			seq = append(seq, hit)
		}
		if !r.Pass {
			break
		}
		edge := "centerx"
		if c.Axis == "y" {
			edge = "centery"
		}
		prev := math.Inf(-1)
		for i, o := range seq {
			b, ok := boxOf(o)
			if !ok {
				fail(true, "%q has no box", c.Texts[i])
				break
			}
			v, _ := b.edge(edge)
			if v <= prev {
				fail(true, "%q is out of order", c.Texts[i])
			}
			prev = v
		}
		if c.Alias != "" {
			e.aliases[c.Alias] = seq
		}
	case "path":
		objs, err := e.sels(c)
		if err != nil {
			fail(true, "%v", err)
			break
		}
		for _, o := range objs {
			if o.Geom == nil || !o.Geom.Custom {
				fail(true, "%s has no custom geometry", e.m.Label(o))
				continue
			}
			n := pathPoints(o.Geom.Path)
			if n < c.MinPts {
				fail(true, "%s path has %d points, want at least %d", e.m.Label(o), n, c.MinPts)
			}
		}
	case "region":
		objs := e.m.New
		if c.Target != "" || len(c.Targets) > 0 {
			var err error
			if objs, err = e.sels(c); err != nil {
				fail(true, "%v", err)
				break
			}
		}
		if len(objs) == 0 {
			fail(true, "no objects to place")
			break
		}
		rg := box{c.Region[0], c.Region[1], c.Region[2], c.Region[3]}
		for _, o := range objs {
			if o.Parent != "" && e.m.OutToIn[o] == nil {
				// A new child of a new group is placed by its group.
				if p := e.m.OutByID(o.Parent); p != nil && e.m.OutToIn[p] == nil {
					continue
				}
			}
			b, ok := boxOf(o)
			if !ok {
				fail(true, "%s has no box", e.m.Label(o))
				continue
			}
			d := math.Max(math.Max(rg.X-b.X, rg.Y-b.Y), math.Max(b.X+b.W-(rg.X+rg.W), b.Y+b.H-(rg.Y+rg.H)))
			if d > tol {
				measure(d)
				fail(true, "%s [%s %s %s %s] leaves the region by %s pt", e.m.Label(o), f(b.X), f(b.Y), f(b.W), f(b.H), f(d))
			}
		}
	case "text":
		objs, err := e.sels(c)
		if err != nil {
			fail(true, "%v", err)
			break
		}
		for _, o := range objs {
			if !strings.Contains(normText(e.m.Text(o)), normText(c.Text)) {
				fail(true, "%s text %q", e.m.Label(o), e.m.Text(o))
			}
		}
	default:
		fail(false, "unknown check type %q", c.Type)
	}
	return r
}

// sortX sorts objects left to right by box centre x.
func sortX(v []*model.Object) {
	sort.SliceStable(v, func(i, j int) bool {
		a, _ := boxOf(v[i])
		b, _ := boxOf(v[j])
		return a.X+a.W/2 < b.X+b.W/2
	})
}

// pathPoints counts the coordinate pairs in an SVG path subset string.
func pathPoints(p string) int {
	n := 0
	for _, tok := range strings.Fields(p) {
		if _, err := strconv.ParseFloat(tok, 64); err == nil {
			n++
		}
	}
	return n / 2
}

// sideOf classifies a site by its direction from the shape's box centre,
// with the box normalised to a square: right, bottom, left or top. Sites
// on a diagonal (an ellipse's 45-degree sites) belong to both neighbours.
func sideOf(o *model.Object, s derive.Site) map[string]bool {
	b := o.Box
	cx, cy := b.X+b.W/2, b.Y+b.H/2
	dx, dy := s.X-cx, s.Y-cy
	if b.W > 0 {
		dx /= b.W / 2
	}
	if b.H > 0 {
		dy /= b.H / 2
	}
	out := map[string]bool{}
	if math.Hypot(dx, dy) < 1e-6 {
		return out
	}
	ang := math.Atan2(dy, dx) * 180 / math.Pi // y down: 90 is bottom
	near := func(a float64) bool {
		d := math.Mod(math.Abs(ang-a)+360, 360)
		if d > 180 {
			d = 360 - d
		}
		return d <= 45.5
	}
	out["right"], out["bottom"], out["left"], out["top"] = near(0), near(90), near(180), near(-90)
	return out
}

// glueOK checks one glued end against a selector and a site constraint.
func (e *env) glueOK(g *model.Glue, end derive.Pt, target *model.Object, side string, site *int, tol float64) (bool, float64, string) {
	if g == nil || g.ID != target.ID {
		return false, 0, "not glued to it"
	}
	if site != nil && g.Site != *site {
		return false, 0, fmt.Sprintf("glued at site %d, want %d", g.Site, *site)
	}
	var st *derive.Site
	for i := range target.Sites {
		if target.Sites[i].Index == g.Site {
			st = &target.Sites[i]
		}
	}
	if st == nil {
		return false, 0, fmt.Sprintf("site %d is not derivable", g.Site)
	}
	if side != "" && !sideOf(target, *st)[side] {
		return false, 0, fmt.Sprintf("site %d is not on the %s side", g.Site, side)
	}
	d := math.Hypot(end.X-st.X, end.Y-st.Y) / pt
	if d > tol {
		return false, d, fmt.Sprintf("end is %s pt from site %d", f(d), g.Site)
	}
	return true, d, ""
}

// arrow reports whether a is a present, non-none arrowhead.
func arrow(a *model.Arrow) bool { return a != nil && a.Type != "" && a.Type != "none" }

// connector finds a connector, new or targeted, that satisfies c.
func (e *env) connector(c Check, fail func(bool, string, ...any), measure func(float64)) {
	tol := tolOf(c)
	as, err := e.sel(c.From)
	if err != nil {
		fail(true, "%v", err)
		return
	}
	a := as[0]
	var b *model.Object
	if c.To != "" {
		bs, err := e.sel(c.To)
		if err != nil {
			fail(true, "%v", err)
			return
		}
		b = bs[0]
	}
	var best string
	for _, o := range e.out.Objects {
		if o.Kind != model.Connector || o.Conn == nil {
			continue
		}
		// Only new or targeted connectors can satisfy an edit.
		if in := e.m.OutToIn[o]; in != nil && !e.targets[in.ID] {
			continue
		}
		var why []string
		// Try both directions: start at A, or end at A.
		for _, dir := range []bool{true, false} {
			ga, gb := o.Conn.Start, o.Conn.End
			pa, pb := o.Conn.From, o.Conn.To
			headB := o.Line != nil && arrow(o.Line.Tail)
			if !dir {
				ga, gb, pa, pb = gb, ga, pb, pa
				headB = o.Line != nil && arrow(o.Line.Head)
			}
			ok, d1, w := e.glueOK(ga, pa, a, c.FromSide, c.FromSite, tol)
			if !ok {
				why = append(why, "from: "+w)
				continue
			}
			d2 := 0.0
			if b != nil {
				var w2 string
				ok, d2, w2 = e.glueOK(gb, pb, b, c.ToSide, c.ToSite, tol)
				if !ok {
					why = append(why, "to: "+w2)
					continue
				}
			} else if len(c.ToPoint) == 2 {
				d2 = math.Hypot(pb.X/pt-c.ToPoint[0], pb.Y/pt-c.ToPoint[1])
				if d2 > tol {
					why = append(why, fmt.Sprintf("free end at %s,%s, want %s,%s", f(pb.X/pt), f(pb.Y/pt), f(c.ToPoint[0]), f(c.ToPoint[1])))
					continue
				}
			}
			if !geomOK(o, c.Geoms) {
				why = append(why, "geometry "+geomName(o))
				continue
			}
			if c.ArrowAt == "to" && !headB {
				why = append(why, "no arrowhead at the to end")
				continue
			}
			if c.WidthPt != nil && (o.Line == nil || o.Line.W == nil || math.Abs(*o.Line.W/pt-*c.WidthPt) > 0.01) {
				why = append(why, "line width")
				continue
			}
			if c.Hex != "" || c.Scheme != "" {
				var col *model.Color
				if o.Line != nil {
					col = o.Line.Color
				}
				if ok, got := colorOK(col, c.Scheme, c.Hex, e.out); !ok {
					why = append(why, "line colour "+got)
					continue
				}
			}
			d := math.Max(d1, d2)
			measure(d)
			if c.Alias != "" {
				e.aliases[c.Alias] = []*model.Object{o}
			}
			return
		}
		msg := e.m.Label(o) + ": " + strings.Join(why, ", ")
		if best == "" || strings.Count(msg, "not glued") < strings.Count(best, "not glued") {
			best = msg
		}
	}
	if best == "" {
		best = "no new or targeted connector"
	}
	fail(true, "no connector %s -> %s matches (closest: %s)", c.From, orFree(c.To), best)
}

// orFree returns s, or "(free end)" when s is empty.
func orFree(s string) string {
	if s == "" {
		return "(free end)"
	}
	return s
}
