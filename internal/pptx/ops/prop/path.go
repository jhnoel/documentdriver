package prop

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/edit"
	"github.com/jhnoel/documentdriver/internal/pptx/show"
)

// PathItem is one a:path as the views show it: absolute commands in EMU
// relative to the slide-space box's top-left (before rotation and flips),
// the fill mode ("" for norm) and whether the path is unstroked.
type PathItem struct {
	Cmds     []derive.PathCmd
	Fill     string
	NoStroke bool
}

// PathVal is a custom geometry's paths, one item per a:path.
type PathVal []PathItem

var (
	pathCust    = edit.Path{edit.P("spPr"), edit.A("custGeom")}
	pathPathLst = edit.Path{edit.P("spPr"), edit.A("custGeom"), edit.A("pathLst")}
	pathCxnLst  = edit.Path{edit.P("spPr"), edit.A("custGeom"), edit.A("cxnLst")}
	pathRect    = edit.Path{edit.P("spPr"), edit.A("custGeom"), edit.A("rect")}
)

// custLists are a new custom geometry's lists, in schema order, before
// a:pathLst: written empty, as PowerPoint writes them (and pdraw).
var custLists = []string{"avLst", "gdLst", "ahLst", "cxnLst"}

// newRect is the text rectangle of a new custom geometry: the whole box,
// by the builtin guides, as PowerPoint writes it for freeforms whose
// sites it derives (every corpus freeform with connsite guides).
var newRect = [4]string{"l", "t", "r", "b"}

// Path returns the codec of a custom geometry's paths, shown in units u
// as the views show "path": derive.CustomPaths over the object's
// inheritance chain, scaled by its groups. A write makes the object's
// own geometry a custom geometry (replacing a preset; a new a:custGeom
// gets empty avLst, gdLst, ahLst and cxnLst and the rect l t r b, as
// PowerPoint writes freeforms) and its a:pathLst hold exactly the paths
// given; adjust values, guides, handles, sites and the text rectangle of
// an existing custom geometry are kept. Each path keeps its coordinate
// space (w, h) when the new points are expressible in it, else takes the
// shape's stored extent, so coordinates are whole EMU of the shape's own
// space. Commands whose points are unchanged keep their bytes.
func Path(u Units) Codec[PathVal] {
	return Codec[PathVal]{
		Name:  "path",
		Read:  readPath,
		Write: func(h *edit.Handle, v PathVal) error { return writePath(h, v, u) },
		Show:  func(v PathVal) string { return ShowPath(v, u) },
	}
}

// ShowPath shows paths as the views' "path" value in units u.
func ShowPath(v PathVal, u Units) string {
	return string(show.Paths(shownPaths(v), u.Length))
}

// shownPaths converts path items into the shown form used by the views.
func shownPaths(v PathVal) []derive.ShownPath {
	sp := make([]derive.ShownPath, len(v))
	for i, p := range v {
		sp[i] = derive.ShownPath{D: derive.FormatPath(p.Cmds, derive.FormatNum), Fill: p.Fill, NoStroke: p.NoStroke}
	}
	return sp
}

// custFrame is what reading a custom geometry of branch i needs: the
// geometry as written, the stored extent and the display scale (the
// read model's: the axis scales of the placed map, or 1 without a box).
type custFrame struct {
	cg     derive.CustomGeom
	w, h   float64
	kx, ky float64
	placed bool
}

// readCustFrame reads branch i's custom geometry with its extent and display scale.
func readCustFrame(h *edit.Handle, i int) (custFrame, error) {
	chain, err := h.Chain(i)
	if err != nil {
		return custFrame{}, err
	}
	g, _, ok := derive.ReadGeom(chain)
	if !ok || !g.Custom {
		return custFrame{}, errors.New("not a custom geometry")
	}
	tr := transformRead(h, i)
	if tr.StoredErr != nil {
		return custFrame{}, fmt.Errorf("no transform: path extent unknown (%v)", tr.StoredErr)
	}
	f := custFrame{cg: derive.ReadCustomGeom(g.Elem), kx: 1, ky: 1}
	s := tr.Stored.Float()
	f.w, f.h = s.W, s.H
	if tr.PlaceErr == nil {
		f.kx, f.ky = derive.AxisScales(tr.Placed.M)
		f.placed = true
	}
	return f, nil
}

// readPath reads branch i's custom geometry paths as shown.
func readPath(h *edit.Handle, i int) derive.Val[PathVal] {
	f, err := readCustFrame(h, i)
	if err != nil {
		return derive.Missing[PathVal](err.Error())
	}
	paths, err := derive.CustomPaths(f.cg, f.w, f.h, f.kx, f.ky, derive.FormatNum)
	if err != nil {
		return derive.Missing[PathVal](err.Error())
	}
	empty := true
	for _, p := range paths {
		if p.D != "" {
			empty = false
		}
	}
	if empty {
		return derive.Missing[PathVal]("custom geometry has no path commands")
	}
	out := make(PathVal, len(paths))
	for k, p := range paths {
		out[k] = PathItem{Fill: p.Fill, NoStroke: p.NoStroke}
		if p.D == "" {
			continue
		}
		cmds, err := derive.ParsePath(p.D)
		if err != nil {
			return derive.Missing[PathVal](err.Error())
		}
		out[k].Cmds = cmds
	}
	return derive.Got(out, derive.FromSlide)
}

// EnsureCustom makes every branch's own geometry a custom geometry: a
// preset (or no geometry of its own) is replaced by a new a:custGeom
// holding empty avLst, gdLst, ahLst and cxnLst, the rect l t r b and an
// empty pathLst; an existing a:custGeom is kept as it is.
func EnsureCustom(h *edit.Handle) error {
	n := h.Branches()
	fresh := make([]edit.Path, n)
	need := false
	for i := range n {
		if h.FindIn(i, pathSpPr) == nil {
			return fmt.Errorf("%s has no shape properties", h.Elem(i).Name())
		}
		if _, custom := ownGeom(h, i); !custom {
			fresh[i] = pathCust
			need = true
		}
	}
	if !need {
		return nil
	}
	spPrs := make([]edit.Path, n)
	for i, p := range fresh {
		if p != nil {
			spPrs[i] = pathSpPr
		}
	}
	if err := h.EnsureEach(spPrs, edit.A("custGeom")); err != nil {
		return err
	}
	for _, l := range append(append([]string{}, custLists...), "rect", "pathLst") {
		if err := h.EnsureEach(fresh, edit.A(l)); err != nil {
			return err
		}
	}
	edits := make([][]edit.AttrEdit, n)
	for i, p := range fresh {
		if p == nil {
			continue
		}
		for k, a := range []string{"l", "t", "r", "b"} {
			edits[i] = append(edits[i], edit.AttrEdit{Path: pathRect, Name: a, Value: newRect[k]})
		}
	}
	return h.ApplyEach(edits)
}

// ErrNoPath is returned when a custom geometry is written without paths.
var ErrNoPath = errors.New("prop: a custom geometry needs at least one path")

// writePath writes the wanted paths as the object's custom geometry.
func writePath(h *edit.Handle, want PathVal, u Units) error {
	if len(want) == 0 {
		return ErrNoPath
	}
	for k, p := range want {
		if p.Fill != "" {
			if err := edit.CheckAttr("a:CT_Path2D", "fill", p.Fill); err != nil {
				return fmt.Errorf("path %d fill: %w", k, err)
			}
		}
	}
	if err := EnsureCustom(h); err != nil {
		return err
	}
	if err := h.Ensure(pathCust, edit.A("pathLst")); err != nil {
		return err
	}
	frags := make([][]byte, h.Branches())
	for i := range h.Branches() {
		frag, err := buildPathList(h, i, want, u)
		if err != nil {
			return err
		}
		frags[i] = frag
	}
	return h.ReplaceChildren(pathPathLst, frags)
}

// buildPathList returns the new content of branch i's a:pathLst for
// want, or nil when it already holds exactly that.
func buildPathList(h *edit.Handle, i int, want PathVal, u Units) ([]byte, error) {
	pl := h.FindIn(i, pathPathLst)
	tr := transformRead(h, i)
	if tr.StoredErr != nil {
		return nil, fmt.Errorf("%w: the path is relative to the shape's box, which is not derived: %v", ErrNotDerived, tr.StoredErr)
	}
	if tr.PlaceErr != nil {
		return nil, fmt.Errorf("%w: %v", ErrInexpressible, tr.PlaceErr)
	}
	s := tr.Stored.Float()
	kx, ky := derive.AxisScales(tr.Placed.M)
	if kx == 0 || ky == 0 {
		return nil, fmt.Errorf("%w: the shape's groups collapse an axis", ErrInexpressible)
	}
	old := pl.Elements()
	var b bytes.Buffer
	for k, p := range want {
		var prev *tree.Node
		if k < len(old) && derive.Is(old[k], derive.DML, "path") {
			prev = old[k]
		}
		x, err := BuildPath(prev, pl.Prefix(), p, s.W, s.H, kx, ky, u)
		if err != nil {
			return nil, fmt.Errorf("path %d: %w", k, err)
		}
		b.Write(x)
	}
	var cur bytes.Buffer
	for _, c := range pl.Children() {
		cur.Write(c.Bytes())
	}
	if bytes.Equal(cur.Bytes(), b.Bytes()) {
		return nil, nil
	}
	return b.Bytes(), nil
}

// BuildPath writes one a:path for p, an object whose stored extent is
// w x h and whose display scale is kx, ky, with the DrawingML prefix
// pfx. prev, the a:path it replaces (nil for a new one), lends its
// attributes and its coordinate space when the points are expressible in
// it (they read back as shown), and the bytes of each command whose
// points are unchanged. Otherwise the path's space is the stored extent,
// in which points are whole EMU of the shape's own coordinates.
func BuildPath(prev *tree.Node, pfx string, p PathItem, w, h, kx, ky float64, u Units) ([]byte, error) {
	name := func(local string) string {
		if pfx == "" {
			return local
		}
		return pfx + ":" + local
	}
	wantShown := show.PathUnits(derive.FormatPath(p.Cmds, derive.FormatNum), u.Length)
	// Candidate coordinate spaces: the previous path's own, then the
	// extent.
	type space struct {
		pw, ph int64
		keep   bool // the previous path's w and h stay as written
	}
	var spaces []space
	if prev != nil {
		// An absent w or h is the extent itself (derive's scale 1).
		lit := func(name string, ext float64) (int64, bool) {
			if _, ok := prev.Attr(name); !ok {
				return int64(math.Round(ext)), true
			}
			return literalAttr(prev, name)
		}
		pw, okw := lit("w", w)
		ph, okh := lit("h", h)
		if okw && okh {
			spaces = append(spaces, space{pw, ph, true})
		}
	}
	spaces = append(spaces, space{int64(math.Round(w)), int64(math.Round(h)), false})
	var pts [][]int64
	var sp space
	found := false
	for _, c := range spaces {
		got, shown := placePoints(p.Cmds, c.pw, c.ph, w, h, kx, ky)
		if show.PathUnits(shown, u.Length) == wantShown {
			pts, sp, found = got, c, true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("%w: the points do not round to whole EMU of the shape's coordinates as given", ErrInexpressible)
	}
	for _, xs := range pts {
		for _, v := range xs {
			if err := edit.CheckAttr("a:CT_AdjPoint2D", "x", strconv.FormatInt(v, 10)); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrInexpressible, err)
			}
		}
	}
	var b bytes.Buffer
	// Start tag: the previous path's attributes in their order, with w,
	// h, fill and stroke set as wanted; new attributes in schema order.
	attrs := [][2]string{}
	if prev != nil {
		for _, a := range prev.Attrs() {
			attrs = append(attrs, [2]string{a.Name, a.Value})
		}
	}
	set := func(k, v string) {
		for i := range attrs {
			if attrs[i][0] == k {
				attrs[i][1] = v
				return
			}
		}
		attrs = append(attrs, [2]string{k, v})
	}
	unset := func(k string) {
		for i := range attrs {
			if attrs[i][0] == k {
				attrs = append(attrs[:i], attrs[i+1:]...)
				return
			}
		}
	}
	get := func(k string) (string, bool) {
		for _, a := range attrs {
			if a[0] == k {
				return a[1], true
			}
		}
		return "", false
	}
	if !sp.keep {
		set("w", strconv.FormatInt(sp.pw, 10))
		set("h", strconv.FormatInt(sp.ph, 10))
	}
	switch cur, ok := get("fill"); {
	case p.Fill == "" && ok && cur != "norm":
		unset("fill")
	case p.Fill != "" && cur != p.Fill:
		set("fill", p.Fill)
	}
	cur, ok := get("stroke")
	stroked := !ok || cur == "1" || cur == "true"
	switch {
	case p.NoStroke && stroked:
		set("stroke", "0")
	case !p.NoStroke && !stroked:
		unset("stroke")
	}
	same := prev != nil && len(attrs) == len(prev.Attrs())
	if same {
		for k, a := range prev.Attrs() {
			if attrs[k][0] != a.Name || attrs[k][1] != a.Value {
				same = false
			}
		}
	}
	var olds []*tree.Node
	if prev != nil {
		olds = prev.Elements()
	}
	var body bytes.Buffer
	for k, c := range p.Cmds {
		if k < len(olds) && sameCmd(olds[k], c.Op, pts[k]) {
			body.Write(olds[k].Bytes())
			continue
		}
		op := map[byte]string{'M': "moveTo", 'L': "lnTo", 'C': "cubicBezTo", 'Q': "quadBezTo", 'Z': "close"}[c.Op]
		if c.Op == 'Z' {
			body.WriteString("<" + name(op) + "/>")
			continue
		}
		body.WriteString("<" + name(op) + ">")
		for j := 0; j < len(pts[k]); j += 2 {
			body.WriteString("<" + name("pt") + ` x="` + strconv.FormatInt(pts[k][j], 10) + `" y="` + strconv.FormatInt(pts[k][j+1], 10) + `"/>`)
		}
		body.WriteString("</" + name(op) + ">")
	}
	// An unchanged start tag keeps its bytes (and its end tag), unless it
	// closes itself and the path now has commands.
	if same && !(bytes.HasSuffix(prev.StartTag(), []byte("/>")) && body.Len() > 0) {
		b.Write(prev.StartTag())
		if bytes.HasSuffix(prev.StartTag(), []byte("/>")) {
			return b.Bytes(), nil
		}
		b.Write(body.Bytes())
		b.Write(prev.EndTag())
		return b.Bytes(), nil
	}
	b.WriteString("<" + name("path"))
	for _, a := range attrs {
		b.WriteString(" " + a[0] + `="` + escAttr(a[1]) + `"`)
	}
	if body.Len() == 0 {
		b.WriteString("/>")
		return b.Bytes(), nil
	}
	b.WriteString(">")
	b.Write(body.Bytes())
	b.WriteString("</" + name("path") + ">")
	return b.Bytes(), nil
}

var attrEsc = strings.NewReplacer("&", "&amp;", "<", "&lt;", `"`, "&quot;")

// escAttr escapes s for use inside a double-quoted attribute value.
func escAttr(s string) string { return attrEsc.Replace(s) }

// literalAttr reads an attribute that holds a whole number.
func literalAttr(n *tree.Node, name string) (int64, bool) {
	v, ok := n.Attr(name)
	if !ok {
		return 0, false
	}
	x, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	return x, err == nil
}

// placePoints maps commands (EMU relative to the box) into a path space
// pw x ph over a stored extent w x h shown at scale kx, ky, rounding to
// whole units, and returns the points (x, y pairs per command) and the
// path they show, computed as derive.CustomPaths computes it.
func placePoints(cmds []derive.PathCmd, pw, ph int64, w, h, kx, ky float64) ([][]int64, string) {
	// derive.pathScale: the path's space onto the extent (1 for a zero
	// path extent).
	sx, sy := 1.0, 1.0
	if pw != 0 {
		sx = w / float64(pw)
	}
	if ph != 0 {
		sy = h / float64(ph)
	}
	out := make([][]int64, len(cmds))
	back := make([]derive.PathCmd, len(cmds))
	for k, c := range cmds {
		back[k].Op = c.Op
		for _, p := range c.Pts {
			x := int64(math.Round(p.X / kx / sx))
			y := int64(math.Round(p.Y / ky / sy))
			out[k] = append(out[k], x, y)
			back[k].Pts = append(back[k].Pts, derive.Pt{X: float64(x) * sx * kx, Y: float64(y) * sy * ky})
		}
	}
	// derive formats each number once, in EMU.
	return out, reformat(back)
}

// reformat formats commands as derive.CustomPaths does, then reads them
// back, so that a comparison sees the same rounding as the view.
func reformat(cmds []derive.PathCmd) string {
	return derive.FormatPath(cmds, derive.FormatNum)
}

// sameCmd reports whether an existing command element has op and exactly
// the literal points pts.
func sameCmd(n *tree.Node, op byte, pts []int64) bool {
	want := map[byte]string{'M': "moveTo", 'L': "lnTo", 'C': "cubicBezTo", 'Q': "quadBezTo", 'Z': "close"}[op]
	if !derive.Is(n, derive.DML, want) {
		return false
	}
	ps := n.Elements()
	if len(ps)*2 != len(pts) {
		return false
	}
	for j, pt := range ps {
		x, okx := literalAttr(pt, "x")
		y, oky := literalAttr(pt, "y")
		if !okx || !oky || x != pts[2*j] || y != pts[2*j+1] {
			return false
		}
	}
	return true
}
