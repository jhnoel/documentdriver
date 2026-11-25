package apply

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/plan"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/model"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
	"github.com/jhnoel/documentdriver/internal/vsdx/read"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// lawFixtureNames are the designer-committed V1 to V17 fixtures the
// write laws run on, in fixture order.
var lawFixtureNames = []string{
	"v1-minimal", "v2-master", "v3-styles", "v4-groups", "v5-glue", "v6-data",
	"v7-text", "v8-pages", "v9-rotation", "v10-defects", "v11-ns2011",
	"v12-stencil", "v12-vsdm", "v13-scale", "v15-foreign", "v17-orientation",
}

// lawPage is one readable page of a law document: its read model,
// the part that holds it, the pages.xml part and whether the page
// is scaled.
type lawPage struct {
	page      *model.Page
	part      string
	pagesPart string
	scaled    bool
}

// lawPages returns every readable page of the drawing at path
// through read --page (read.PageModel with full detail, so origins
// and sites are present), in pages.xml order. A page that cannot
// be read is skipped, not an error: it has no targets.
func lawPages(path string) ([]lawPage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	sum, err := read.Document(data, "BASE")
	if err != nil {
		return nil, err
	}
	pkg, err := opc.Open(path)
	if err != nil {
		return nil, err
	}
	defer pkg.Close()
	d, err := doc.Open(pkg)
	if err != nil {
		return nil, err
	}
	pagesPart := lawPagesPart(pkg, d)
	var out []lawPage
	for i := range sum.Pages {
		pm, err := read.PageModel(data, sum.Pages[i].ID, read.Options{Detail: "full"})
		if err != nil {
			continue
		}
		part := ""
		if i < len(d.Pages) {
			part = d.Pages[i].Part
		}
		scaled := false
		for _, n := range pm.Notes {
			if strings.HasPrefix(n.Reason, "drawing scale") {
				scaled = true
			}
		}
		out = append(out, lawPage{page: pm, part: part, pagesPart: pagesPart, scaled: scaled})
	}
	return out, nil
}

// lawPagesPart returns the pages.xml part of d, or the conventional
// name when the relationship cannot be resolved (the page suites
// only need a part name the harness can find in the package).
func lawPagesPart(pkg *opc.Package, d *doc.Document) string {
	if d == nil {
		return "/visio/pages/pages.xml"
	}
	rels, err := pkg.Rels(d.Part)
	if err == nil {
		for _, rel := range rels.All() {
			if rel.External() || !ctypes.Is(rel.Type, ctypes.RelPages) {
				continue
			}
			if part, err := rels.TargetPart(rel); err == nil {
				return part
			}
		}
	}
	return "/visio/pages/pages.xml"
}

// lawApply applies ops as one plan to src, writing dst (in place
// when dst is src), through apply.Run.
func lawApply(src, dst string, opsIn []laws.Op) error {
	specs := make([]ops.Spec, len(opsIn))
	for i, o := range opsIn {
		spec, ok := o.(ops.Spec)
		if !ok {
			return fmt.Errorf("law op %T is not an ops.Spec", o)
		}
		specs[i] = spec
	}
	opt := Options{}
	if dst != src {
		opt.Out = dst
	}
	_, err := Run(src, &ops.Plan{Ops: specs}, opt)
	return err
}

// lawFail returns an operation that fails when applied: an
// object.set on a shape the drawing does not have.
func lawFail() laws.Op {
	return &ops.ObjectSetSpec{At: "page0/s999999", Box: []float64{0, 0, 72, 36}}
}

// lawRefused reports whether err is a documented refusal: the
// operation_failed refusals of docs/vsdx-contract-draft.md section 3
// (formula-bound cells, containers, lists and callouts, members of
// containers, scaled-page geometry, data-linked rows, and the other
// operation_failed refusals the help <op> lists). Any other code
// (not_found, invalid_output, internal, unreadable_target) is a
// failure of the law, not a refusal.
func lawRefused(err error) bool {
	var ce *cli.Error
	if !errors.As(err, &ce) {
		return false
	}
	return ce.Code == cli.CodeOperationFailed
}

// lawShapeAddr returns the shape address a law target names: the
// part before any "#" end suffix and any "/prop:" suffix.
func lawShapeAddr(addr string) string {
	if i := strings.Index(addr, "#"); i >= 0 {
		addr = addr[:i]
	}
	if a, err := vsd.Parse(addr); err == nil {
		switch a.Kind {
		case vsd.KindShape:
			return vsd.ShapeID(vsd.PageID(a.Container, a.PageDup), a.Shape, a.Dup)
		case vsd.KindProp:
			return vsd.ShapeID(vsd.PageID(a.Container, a.PageDup), a.Shape, a.Dup)
		}
	}
	return addr
}

// lawEndOf returns the end ("from" or "to") and the alternate glue
// a glue or ends target encodes after "#", and whether it has one.
func lawEndOf(addr string) (shape, end, alt string, ok bool) {
	shape, rest, found := strings.Cut(addr, "#")
	if !found {
		return "", "", "", false
	}
	end, alt, _ = strings.Cut(rest, "|")
	return shape, end, alt, true
}

// lawOwnsShape reports whether unit e belongs to the shape a law
// target names: the Shape unit with its stored ID.
func lawOwnsShape(t laws.Target, e *tree.Node) bool {
	return lawOwns(t, e, false)
}

// lawOwnsConnector reports whether unit e belongs to the connector
// a law target names: its Shape unit, and its Connect rows
// (FromSheet is the connector).
func lawOwnsConnector(t laws.Target, e *tree.Node) bool {
	return lawOwns(t, e, true)
}

// lawOwns reports whether unit e belongs to the shape of t: a
// Shape with its ID, and, when connector is set, a Connect whose
// FromSheet is the shape. Container units (Shapes, Connects) are
// never owned: their own bytes mask their nested units.
func lawOwns(t laws.Target, e *tree.Node, connector bool) bool {
	if e == nil || e.Kind() != tree.ElementNode {
		return false
	}
	addr := lawShapeAddr(t.Addr)
	a, err := vsd.Parse(addr)
	if err != nil || a.Kind != vsd.KindShape {
		return false
	}
	id := strconv.FormatUint(uint64(a.Shape), 10)
	switch e.Local() {
	case vsd.ElemShape:
		v, _ := e.Attr("ID")
		return v == id
	case vsd.ElemConnect:
		if !connector {
			return false
		}
		v, _ := e.Attr("FromSheet")
		return v == id
	}
	return false
}

// lawOwnsPage reports whether unit e belongs to a page target: the
// pages.xml part has no units, so the harness never compares its
// units; owning everything is the honest answer for it.
func lawOwnsPage(_ laws.Target, _ *tree.Node) bool { return true }

var _ plan.Units = glueUnits{}

// glueUnits divides a page part into Shape units and the Shapes
// and Connects containers, for the glue suite: a glue write adds
// or removes a Connect row inside the Connects container, so an
// individual Connect cannot be a unit (the harness requires the
// unit count to stay the same), while the container can be owned
// by the connector whose glue it is.
type glueUnits struct{}

// HasUnits implements plan.Units for glueUnits: page and master
// contents parts.
func (glueUnits) HasUnits(d *tree.Document) bool {
	r := d.Root()
	return r != nil && (r.Local() == vsd.ElemPageContents || r.Local() == vsd.ElemMasterContents)
}

// IsUnit implements plan.Units for glueUnits.
func (glueUnits) IsUnit(e *tree.Node) bool {
	if e == nil || e.Kind() != tree.ElementNode || !vsd.IsMain(e.Space()) {
		return false
	}
	switch e.Local() {
	case vsd.ElemShape, vsd.ElemShapes, vsd.ElemConnects:
		return true
	}
	return false
}

// Describe implements plan.Units for glueUnits.
func (glueUnits) Describe(e *tree.Node) string {
	if e.Local() == vsd.ElemShape {
		if id, ok := e.Attr("ID"); ok {
			return "shape s" + id
		}
	}
	return e.Name()
}

// lawOwnsGlue reports whether unit e belongs to the connector a
// glue target names: its Shape unit and the Connects container
// holding its Connect rows.
func lawOwnsGlue(t laws.Target, e *tree.Node) bool {
	if e == nil || e.Kind() != tree.ElementNode {
		return false
	}
	if e.Local() == vsd.ElemConnects {
		return true
	}
	return lawOwnsShape(t, e)
}

// lawWritableKind reports whether an object kind is settable by
// object.set style and transform writes: shapes, groups and
// connectors, not pictures or other read-only kinds.
func lawWritableKind(k model.Kind) bool {
	return k == model.Shape || k == model.Group || k == model.Connector
}

// boxValue is an object's view box with the origin read shows it
// from, for the box suite's sticky undo.
type boxValue struct {
	box    [4]float64
	origin string
}

// boxSuite is the object box suite: object.set box, varied by
// +-1 pt in x, undone by object.move +-d.
func boxSuite() *laws.Suite[boxValue] {
	return &laws.Suite[boxValue]{
		Name: "box",
		Read: func(path string) ([]laws.Value[boxValue], error) {
			pages, err := lawPages(path)
			if err != nil {
				return nil, err
			}
			var out []laws.Value[boxValue]
			for _, lp := range pages {
				if lp.scaled {
					continue
				}
				glued := lawGlueTargets(lp)
				dup := lawDuplicateIDs(lp)
				for _, o := range lp.page.Objects {
					if o.Box == nil || (o.Kind != model.Shape && o.Kind != model.Group) || glued[lawShapeIDOf(o.ID)] || dup[lawShapeIDOf(o.ID)] {
						continue
					}
					out = append(out, laws.Value[boxValue]{
						Target: laws.Target{Part: lp.part, Addr: o.ID},
						V:      boxValue{box: [4]float64{o.Box.X, o.Box.Y, o.Box.W, o.Box.H}, origin: string(o.Origins["box"])},
					})
				}
			}
			return out, nil
		},
		Show: func(v boxValue) string {
			return fmt.Sprintf("[%s %s %s %s]", geom.FormatNum(v.box[0]), geom.FormatNum(v.box[1]), geom.FormatNum(v.box[2]), geom.FormatNum(v.box[3]))
		},
		Set: func(t laws.Target, v boxValue) laws.Op {
			return &ops.ObjectSetSpec{At: t.Addr, Box: []float64{v.box[0], v.box[1], v.box[2], v.box[3]}}
		},
		Vary: func(rng *rand.Rand, _ laws.Target, cur boxValue) boxValue {
			d := 1.0
			if rng.IntN(2) == 0 {
				d = -1
			}
			cur.box[0] += d
			return cur
		},
		Apply:   lawApply,
		Refused: lawRefused,
		Units:   edit.Units(),
		Owns:    lawOwnsShape,
		Fail:    lawFail,
		Undo: func(rng *rand.Rand, t laws.Target) (laws.Op, laws.Op) {
			dx := float64(rng.IntN(2000)-1000) / 100
			dy := float64(rng.IntN(2000)-1000) / 100
			mv := func(x, y float64) laws.Op { return &ops.ObjectMoveSpec{At: lawShapeAddr(t.Addr), DX: x, DY: y} }
			return mv(dx, dy), mv(-dx, -dy)
		},
		Sticky: func(v laws.Value[boxValue]) bool { return v.V.origin != "local" && v.V.origin != "" },
		Budget: 3,
	}
}

// fillValue is a fill as read shows it: none, a hex colour, or a
// pattern (its name or number) in that colour over a clear (bgNone) or
// coloured (bgHex) background.
type fillValue struct {
	none    bool
	hex     string
	pattern string
	bgNone  bool
	bgHex   string
}

// fillSuite is the fill suite: object.set fill.
func fillSuite() *laws.Suite[fillValue] {
	return &laws.Suite[fillValue]{
		Name: "fill",
		Read: func(path string) ([]laws.Value[fillValue], error) {
			pages, err := lawPages(path)
			if err != nil {
				return nil, err
			}
			var out []laws.Value[fillValue]
			for _, lp := range pages {
				for _, o := range lp.page.Objects {
					if o.Fill == nil || !lawWritableKind(o.Kind) {
						continue
					}
					f := o.Fill
					if f.Pattern != "" && !f.BgNone && f.BgHex == "" {
						// A background read does not derive cannot
						// be sent back (docs/contract.md §3).
						continue
					}
					out = append(out, laws.Value[fillValue]{
						Target: laws.Target{Part: lp.part, Addr: o.ID},
						V:      fillValue{none: f.None, hex: f.Hex, pattern: f.Pattern, bgNone: f.BgNone, bgHex: f.BgHex},
					})
				}
			}
			return out, nil
		},
		Show: func(v fillValue) string {
			switch {
			case v.none:
				return "none"
			case v.pattern == "":
				return v.hex
			case v.bgNone:
				return v.hex + " " + v.pattern + " on none"
			}
			return v.hex + " " + v.pattern + " on " + v.bgHex
		},
		Set: func(t laws.Target, v fillValue) laws.Op {
			spec := &ops.PaintSpec{None: v.none, Hex: v.hex, BgNone: v.bgNone, BgHex: v.bgHex}
			if v.pattern != "" {
				spec.Pattern = v.pattern
				if n, ok := vsd.FillPatternIndex[v.pattern]; ok {
					spec.Pattern = n
				}
			}
			return &ops.ObjectSetSpec{At: t.Addr, Fill: spec}
		},
		Vary: func(_ *rand.Rand, _ laws.Target, cur fillValue) fillValue {
			// none and pattern vary to a solid colour, solid to a
			// pattern, so the suite writes and reads back all three.
			switch {
			case cur.none, cur.pattern != "" && cur.hex != "#00FF00":
				return fillValue{hex: "#00FF00"}
			case cur.pattern != "":
				return fillValue{hex: "#FF0000"}
			case cur.hex == "#000000":
				return fillValue{hex: "#FF0000", pattern: "cross_hatch", bgHex: "#FFFFFF"}
			}
			return fillValue{hex: "#000000", pattern: "hatch_up", bgNone: true}
		},
		Apply:   lawApply,
		Refused: lawRefused,
		Units:   edit.Units(),
		Owns:    lawOwnsShape,
		Fail:    lawFail,
		Budget:  3,
	}
}

// lineValue is a line as read shows it.
type lineValue struct {
	none  bool
	w     float64
	hasW  bool
	color string
	dash  string
}

// lineSuite is the line suite: object.set line.
func lineSuite() *laws.Suite[lineValue] {
	return &laws.Suite[lineValue]{
		Name: "line",
		Read: func(path string) ([]laws.Value[lineValue], error) {
			pages, err := lawPages(path)
			if err != nil {
				return nil, err
			}
			var out []laws.Value[lineValue]
			for _, lp := range pages {
				for _, o := range lp.page.Objects {
					if o.Line == nil || !lawWritableKind(o.Kind) {
						continue
					}
					out = append(out, laws.Value[lineValue]{
						Target: laws.Target{Part: lp.part, Addr: o.ID},
						V:      lineValue{none: o.Line.None, w: o.Line.W, hasW: o.Line.HasW, color: o.Line.Color, dash: o.Line.Dash},
					})
				}
			}
			return out, nil
		},
		Show: func(v lineValue) string {
			if v.none {
				return "none"
			}
			w := ""
			if v.hasW {
				w = geom.FormatNum(v.w)
			}
			return fmt.Sprintf("w=%s color=%s dash=%s", w, v.color, v.dash)
		},
		Set: func(t laws.Target, v lineValue) laws.Op {
			if v.none {
				return &ops.ObjectSetSpec{At: t.Addr, Line: &ops.LineSpec{None: true}}
			}
			spec := &ops.LineSpec{Color: v.color, Dash: v.dash}
			if v.hasW {
				w := v.w
				spec.W = &w
			}
			return &ops.ObjectSetSpec{At: t.Addr, Line: spec}
		},
		Vary: func(_ *rand.Rand, _ laws.Target, cur lineValue) lineValue {
			w := cur.w + 1.5
			if !cur.hasW || cur.none {
				w = 1.5
			}
			return lineValue{w: w, hasW: true, color: "#00FF00", dash: "1"}
		},
		Apply:   lawApply,
		Refused: lawRefused,
		Units:   edit.Units(),
		Owns:    lawOwnsShape,
		Fail:    lawFail,
		Budget:  3,
	}
}

// nameSuite is the name suite: object.set name.
func nameSuite() *laws.Suite[string] {
	return &laws.Suite[string]{
		Name: "name",
		Read: func(path string) ([]laws.Value[string], error) {
			pages, err := lawPages(path)
			if err != nil {
				return nil, err
			}
			var out []laws.Value[string]
			for _, lp := range pages {
				for _, o := range lp.page.Objects {
					if !lawWritableKind(o.Kind) {
						continue
					}
					out = append(out, laws.Value[string]{Target: laws.Target{Part: lp.part, Addr: o.ID}, V: o.Name})
				}
			}
			return out, nil
		},
		Show: func(v string) string { return strconv.Quote(v) },
		Set: func(t laws.Target, v string) laws.Op {
			name := v
			return &ops.ObjectSetSpec{At: t.Addr, Name: &name}
		},
		Vary: func(rng *rand.Rand, _ laws.Target, cur string) string {
			if cur == "" {
				return fmt.Sprintf("Law %d", rng.IntN(10000))
			}
			return fmt.Sprintf("%s law %d", cur, rng.IntN(10000))
		},
		Apply:   lawApply,
		Refused: lawRefused,
		Units:   edit.Units(),
		Owns:    lawOwnsShape,
		Fail:    lawFail,
		Budget:  3,
	}
}

// rotSuite is the rotation suite: object.set rot.
func rotSuite() *laws.Suite[float64] {
	return &laws.Suite[float64]{
		Name: "rot",
		Read: func(path string) ([]laws.Value[float64], error) {
			pages, err := lawPages(path)
			if err != nil {
				return nil, err
			}
			var out []laws.Value[float64]
			for _, lp := range pages {
				if lp.scaled {
					continue
				}
				glued := lawGlueTargets(lp)
				dup := lawDuplicateIDs(lp)
				for _, o := range lp.page.Objects {
					if o.Box == nil || (o.Kind != model.Shape && o.Kind != model.Group) || glued[lawShapeIDOf(o.ID)] || dup[lawShapeIDOf(o.ID)] {
						continue
					}
					out = append(out, laws.Value[float64]{Target: laws.Target{Part: lp.part, Addr: o.ID}, V: o.Box.Rot})
				}
			}
			return out, nil
		},
		Show: func(v float64) string { return geom.FormatNum(geom.NormDeg(v)) },
		Set: func(t laws.Target, v float64) laws.Op {
			rot := v
			return &ops.ObjectSetSpec{At: t.Addr, Rot: &rot}
		},
		Vary:    func(_ *rand.Rand, _ laws.Target, cur float64) float64 { return geom.NormDeg(cur + 30) },
		Apply:   lawApply,
		Refused: lawRefused,
		Units:   edit.Units(),
		Owns:    lawOwnsShape,
		Fail:    lawFail,
		Budget:  3,
	}
}

// flipsValue is the pair of flips as read shows them.
type flipsValue struct {
	h, v bool
}

// flipsSuite is the flips suite: object.set flip_h and flip_v.
func flipsSuite() *laws.Suite[flipsValue] {
	return &laws.Suite[flipsValue]{
		Name: "flips",
		Read: func(path string) ([]laws.Value[flipsValue], error) {
			pages, err := lawPages(path)
			if err != nil {
				return nil, err
			}
			var out []laws.Value[flipsValue]
			for _, lp := range pages {
				if lp.scaled {
					continue
				}
				glued := lawGlueTargets(lp)
				dup := lawDuplicateIDs(lp)
				for _, o := range lp.page.Objects {
					if o.Box == nil || (o.Kind != model.Shape && o.Kind != model.Group) || glued[lawShapeIDOf(o.ID)] || dup[lawShapeIDOf(o.ID)] {
						continue
					}
					out = append(out, laws.Value[flipsValue]{
						Target: laws.Target{Part: lp.part, Addr: o.ID},
						V:      flipsValue{h: o.Box.FlipH, v: o.Box.FlipV},
					})
				}
			}
			return out, nil
		},
		Show: func(v flipsValue) string { return fmt.Sprintf("h=%v v=%v", v.h, v.v) },
		Set: func(t laws.Target, v flipsValue) laws.Op {
			h, vv := v.h, v.v
			return &ops.ObjectSetSpec{At: t.Addr, FlipH: &h, FlipV: &vv}
		},
		Vary:    func(_ *rand.Rand, _ laws.Target, cur flipsValue) flipsValue { return flipsValue{h: !cur.h, v: cur.v} },
		Apply:   lawApply,
		Refused: lawRefused,
		Units:   edit.Units(),
		Owns:    lawOwnsShape,
		Fail:    lawFail,
		Budget:  3,
	}
}

// endsSuite is the connector ends suite: one target per free end,
// object.set from or to. A glued end is not a target: sending it
// back as a free point frees it, so it has no identity.
func endsSuite() *laws.Suite[[2]float64] {
	return &laws.Suite[[2]float64]{
		Name: "ends",
		Read: func(path string) ([]laws.Value[[2]float64], error) {
			pages, err := lawPages(path)
			if err != nil {
				return nil, err
			}
			var out []laws.Value[[2]float64]
			for _, lp := range pages {
				if lp.scaled {
					continue
				}
				glued := lawGlueTargets(lp)
				dup := lawDuplicateIDs(lp)
				for _, o := range lp.page.Objects {
					if o.Kind != model.Connector || glued[lawShapeIDOf(o.ID)] || dup[lawShapeIDOf(o.ID)] {
						continue
					}
					if o.From != nil && o.FromGlue == "" {
						out = append(out, laws.Value[[2]float64]{
							Target: laws.Target{Part: lp.part, Addr: o.ID + "#from"},
							V:      [2]float64{o.From.X, o.From.Y},
						})
					}
					if o.To != nil && o.ToGlue == "" {
						out = append(out, laws.Value[[2]float64]{
							Target: laws.Target{Part: lp.part, Addr: o.ID + "#to"},
							V:      [2]float64{o.To.X, o.To.Y},
						})
					}
				}
			}
			return out, nil
		},
		Show: func(v [2]float64) string { return fmt.Sprintf("[%s %s]", geom.FormatNum(v[0]), geom.FormatNum(v[1])) },
		Set: func(t laws.Target, v [2]float64) laws.Op {
			shape, end, _, _ := lawEndOf(t.Addr)
			pt := []float64{v[0], v[1]}
			if end == "from" {
				return &ops.ObjectSetSpec{At: shape, From: pt}
			}
			return &ops.ObjectSetSpec{At: shape, To: pt}
		},
		Vary:    func(_ *rand.Rand, _ laws.Target, cur [2]float64) [2]float64 { return [2]float64{cur[0] + 10, cur[1]} },
		Apply:   lawApply,
		Refused: lawRefused,
		Units:   edit.Units(),
		Owns:    lawOwnsConnector,
		Fail:    lawFail,
		Budget:  3,
	}
}

// glueSuite is the glue suite: one target per connector end that
// is free (identity "none") or glued with its end formula stored
// (identity the glue read shows), object.set from_glue or to_glue,
// varied to "none" for a glued end and to a connection point on
// the page for a free end. A glued end whose formula is not stored
// (V5) is not a target: writing its glue back adds the formula and
// is not an identity. The alternate glue is recorded in the target
// address because Vary receives only the target and the current
// value.
func glueSuite() *laws.Suite[string] {
	return &laws.Suite[string]{
		Name: "glue",
		Read: func(path string) ([]laws.Value[string], error) {
			pages, err := lawPages(path)
			if err != nil {
				return nil, err
			}
			var out []laws.Value[string]
			for _, lp := range pages {
				if lp.scaled {
					continue
				}
				alt := glueAlt(lp)
				for _, o := range lp.page.Objects {
					if o.Kind != model.Connector {
						continue
					}
					for _, end := range []struct {
						name, glue string
					}{{"from", o.FromGlue}, {"to", o.ToGlue}} {
						a := alt
						if end.glue != "" {
							// A glued end is a target only when its
							// end cell stores the glue formula, so
							// setting the glue back is an identity
							// and the varied (freed) end is still a
							// target. A dangling glue has no shape
							// to set back to.
							if strings.HasPrefix(end.glue, "#") || lawEndFormula(o, end.name) == "" {
								continue
							}
						} else if a == "" {
							continue
						}
						out = append(out, laws.Value[string]{
							Target: laws.Target{Part: lp.part, Addr: o.ID + "#" + end.name + "|" + a},
							V:      end.glue,
						})
					}
				}
			}
			return out, nil
		},
		Show: func(v string) string {
			if v == "" {
				return "none"
			}
			return v
		},
		Set: func(t laws.Target, v string) laws.Op {
			shape, end, _, _ := lawEndOf(t.Addr)
			glue := v
			if glue == "" {
				glue = "none"
			}
			if end == "from" {
				return &ops.ObjectSetSpec{At: shape, FromGlue: &glue}
			}
			return &ops.ObjectSetSpec{At: shape, ToGlue: &glue}
		},
		Vary: func(_ *rand.Rand, t laws.Target, cur string) string {
			if cur != "" {
				return "none"
			}
			_, _, alt, _ := lawEndOf(t.Addr)
			return alt
		},
		Apply:   lawApply,
		Refused: lawRefused,
		Units:   glueUnits{},
		Owns:    lawOwnsGlue,
		Fail:    lawFail,
		Budget:  3,
	}
}

// lawGlueTargets returns the stored shape IDs a connector on the
// page is glued to (from the page's connections and the objects'
// glue strings): moving one leaves its connectors stale, so a
// geometry write to it adds the recalculation part and the
// untargeted law, which holds every other part byte for byte,
// cannot hold for it. Geometry suites exclude these shapes; the
// stale write is the documented recalculation policy, not an
// untargeted touch.
func lawGlueTargets(lp lawPage) map[uint32]bool {
	out := map[uint32]bool{}
	add := func(addr string) {
		if a, err := vsd.Parse(addr); err == nil && a.Kind == vsd.KindShape {
			out[a.Shape] = true
		}
	}
	for _, c := range lp.page.Connections {
		add(c.From)
		add(c.To)
	}
	for _, o := range lp.page.Objects {
		for _, g := range []string{o.FromGlue, o.ToGlue} {
			if g == "" {
				continue
			}
			head, _, _ := strings.Cut(g, ":")
			if strings.HasPrefix(head, "s") {
				if n, err := strconv.ParseUint(head[1:], 10, 32); err == nil {
					out[uint32(n)] = true
				}
			}
		}
	}
	return out
}

// lawDuplicateIDs returns the stored IDs that occur more than
// once on the page: geometry writes to a duplicate are ambiguous
// in the stored Connect rows, so geometry suites exclude them.
func lawDuplicateIDs(lp lawPage) map[uint32]bool {
	seen := map[uint32]int{}
	for _, o := range lp.page.Objects {
		seen[lawShapeIDOf(o.ID)]++
	}
	out := map[uint32]bool{}
	for id, n := range seen {
		if n > 1 {
			out[id] = true
		}
	}
	return out
}

// lawShapeIDOf returns the stored ID of a shape address, or 0.
func lawShapeIDOf(addr string) uint32 {
	if a, err := vsd.Parse(addr); err == nil {
		return a.Shape
	}
	return 0
}

// lawThePageDependent reports whether any object on the page stores
// a cell formula naming ThePage!PageWidth or ThePage!PageHeight:
// changing the page size or orientation leaves it stale, adding
// the recalculation part, so page size and orientation suites
// exclude such pages.
func lawThePageDependent(lp lawPage) bool {
	for _, o := range lp.page.Objects {
		for _, c := range o.Cells {
			if strings.Contains(c.F, "ThePage!PageWidth") || strings.Contains(c.F, "ThePage!PageHeight") {
				return true
			}
		}
	}
	return false
}

// lawEndFormula returns the stored formula of one connector end
// cell (BeginX for from, EndX for to), or "" when it stores none.
func lawEndFormula(o *model.Object, end string) string {
	name := "BeginX"
	if end == "to" {
		name = "EndX"
	}
	for _, c := range o.Cells {
		if c.Key == name {
			return c.F
		}
	}
	return ""
}

// glueAlt returns a point glue on the page (the first shape with a
// connection site, its first site), or "" when the page has none.
func glueAlt(lp lawPage) string {
	for _, o := range lp.page.Objects {
		if o.Kind != model.Shape && o.Kind != model.Group {
			continue
		}
		if len(o.Sites) == 0 {
			continue
		}
		if a, err := vsd.Parse(o.ID); err == nil {
			return fmt.Sprintf("s%d:%d", a.Shape, o.Sites[0].N)
		}
	}
	return ""
}

// textSuite is the text suite: text.set with the html read shows.
func textSuite() *laws.Suite[string] {
	return &laws.Suite[string]{
		Name: "text",
		Read: func(path string) ([]laws.Value[string], error) {
			pages, err := lawPages(path)
			if err != nil {
				return nil, err
			}
			var out []laws.Value[string]
			for _, lp := range pages {
				for _, o := range lp.page.Objects {
					if !o.HasText || !lawWritableKind(o.Kind) {
						continue
					}
					out = append(out, laws.Value[string]{Target: laws.Target{Part: lp.part, Addr: o.ID}, V: o.HTML})
				}
			}
			return out, nil
		},
		Show: func(v string) string { return v },
		Set: func(t laws.Target, v string) laws.Op {
			return &ops.TextSetSpec{At: t.Addr, HTML: v}
		},
		Vary: func(rng *rand.Rand, _ laws.Target, _ string) string {
			return fmt.Sprintf("<p>Law %d</p>", rng.IntN(10000))
		},
		Apply:   lawApply,
		Refused: lawRefused,
		Units:   edit.Units(),
		Owns:    lawOwnsShape,
		Fail:    lawFail,
		Budget:  3,
	}
}

// textBlockValue is a text block as the model derives it: each
// member with whether it is derived.
type textBlockValue struct {
	valign        string
	hasVAlign     bool
	margins       [4]float64
	hasMargins    bool
	hasBg, bgNone bool
	bgHex         string
	bgTrans       float64
	box           geom.Box
	hasBox        bool
	// shape is the object's own box (not shown), from which Vary
	// places a block on an object that has none.
	shape    geom.Box
	hasShape bool
}

// textBlockSuite is the text block suite: object.set text_block, one
// target per object with text, its effective (inherited included)
// valign, margins and background.
func textBlockSuite() *laws.Suite[textBlockValue] {
	return &laws.Suite[textBlockValue]{
		Name: "text_block",
		Read: func(path string) ([]laws.Value[textBlockValue], error) {
			pages, err := lawPages(path)
			if err != nil {
				return nil, err
			}
			var out []laws.Value[textBlockValue]
			for _, lp := range pages {
				for _, o := range lp.page.Objects {
					tb := o.TextBlock
					if tb == nil || !lawWritableKind(o.Kind) {
						continue
					}
					out = append(out, laws.Value[textBlockValue]{Target: laws.Target{Part: lp.part, Addr: o.ID}, V: textBlockValue{
						valign: tb.VAlign, hasVAlign: tb.HasVAlign, margins: tb.Margins, hasMargins: tb.HasMargins,
						hasBg: tb.HasBg, bgNone: tb.BgNone, bgHex: tb.BgHex, bgTrans: tb.BgTrans,
						box: tb.Box, hasBox: tb.HasBox,
					}})
					if o.Box != nil {
						out[len(out)-1].V.shape, out[len(out)-1].V.hasShape = *o.Box, true
					}
				}
			}
			return out, nil
		},
		Show: func(v textBlockValue) string {
			s := v.valign + fmt.Sprintf(" %v", v.margins)
			if v.hasBox {
				b := v.box
				s += " box " + geom.FormatNum(b.X) + " " + geom.FormatNum(b.Y) + " " + geom.FormatNum(b.W) + " " + geom.FormatNum(b.H) + " at " + geom.FormatNum(geom.NormDeg(b.Rot))
			}
			switch {
			case !v.hasBg:
			case v.bgNone:
				s += " bg none"
			default:
				s += fmt.Sprintf(" bg %s %v%%", v.bgHex, v.bgTrans)
			}
			return s
		},
		Set: func(t laws.Target, v textBlockValue) laws.Op {
			spec := &ops.TextBlockSpec{}
			if v.hasVAlign {
				spec.VAlign = v.valign
			}
			if v.hasMargins {
				spec.Margins = v.margins[:]
			}
			if v.hasBox {
				spec.Box = []float64{v.box.X, v.box.Y, v.box.W, v.box.H}
				a := v.box.Rot
				spec.Angle = &a
			}
			if v.hasBg {
				tr := v.bgTrans
				spec.Background = &ops.TextBgSpec{None: v.bgNone, Hex: v.bgHex, Transparency: &tr}
			}
			return &ops.ObjectSetSpec{At: t.Addr, TextBlock: spec}
		},
		Vary: func(rng *rand.Rand, _ laws.Target, cur textBlockValue) textBlockValue {
			next := map[string]string{"top": "middle", "middle": "bottom", "bottom": "top", "": "top"}
			m := float64(rng.IntN(20))
			out := textBlockValue{valign: next[cur.valign], hasVAlign: true, margins: [4]float64{m, m + 1, m + 2, m + 3}, hasMargins: true, hasBg: true}
			switch {
			case cur.hasBox:
				// Move the block 6 pt right and turn it 15 degrees.
				out.box, out.hasBox = cur.box, true
				out.box.X += 6
				out.box.Rot = geom.NormDeg(cur.box.Rot + 15)
			case cur.hasShape && cur.shape.W > 1 && cur.shape.H > 1:
				// Put the block over the shape's top edge, half its
				// height, as a region title.
				b := cur.shape
				out.box = geom.Box{X: b.X, Y: b.Y - b.H/4, W: b.W, H: b.H / 2, Rot: b.Rot}
				out.hasBox = true
			}
			if cur.hasBg && !cur.bgNone {
				out.bgNone = true
			} else {
				out.bgHex, out.bgTrans = "#FFFF00", 25
			}
			return out
		},
		Apply:   lawApply,
		Refused: lawRefused,
		Units:   edit.Units(),
		Owns:    lawOwnsShape,
		Fail:    lawFail,
		Budget:  3,
	}
}

// propValue is one shape data value as read shows it, with its type.
type propValue struct {
	typ    string
	value  any
	format string
}

// propSuite is the prop value suite: prop.set, one target per
// row, varied by type. Data-linked rows are not targets: setting
// one back is refused, so they have no identity.
func propSuite() *laws.Suite[propValue] {
	return &laws.Suite[propValue]{
		Name: "prop",
		Read: func(path string) ([]laws.Value[propValue], error) {
			pages, err := lawPages(path)
			if err != nil {
				return nil, err
			}
			var out []laws.Value[propValue]
			for _, lp := range pages {
				for _, o := range lp.page.Objects {
					if !lawWritableKind(o.Kind) {
						continue
					}
					for _, pr := range o.Props {
						if pr.Linked || pr.Value == nil {
							continue
						}
						switch pr.Type {
						case "number", "currency":
							if _, ok := pr.Value.(float64); !ok {
								continue
							}
						case "boolean":
							if _, ok := pr.Value.(bool); !ok {
								continue
							}
						case "date":
							if _, ok := pr.Value.(map[string]string); !ok {
								continue
							}
						}
						out = append(out, laws.Value[propValue]{
							Target: laws.Target{Part: lp.part, Addr: pr.ID},
							V:      propValue{typ: pr.Type, value: pr.Value, format: pr.Format},
						})
					}
				}
			}
			return out, nil
		},
		Show: func(v propValue) string { return fmt.Sprintf("%s=%#v", v.typ, v.value) },
		Set: func(t laws.Target, v propValue) laws.Op {
			return &ops.PropSetSpec{At: t.Addr, Value: v.value}
		},
		Vary: func(_ *rand.Rand, _ laws.Target, cur propValue) propValue {
			out := cur
			switch cur.typ {
			case "number", "currency":
				if f, ok := cur.value.(float64); ok {
					out.value = f + 1.5
				} else {
					out.value = float64(1.5)
				}
			case "boolean":
				if b, ok := cur.value.(bool); ok {
					out.value = !b
				} else {
					out.value = true
				}
			case "date":
				s := ""
				if m, ok := cur.value.(map[string]string); ok {
					s = m["date"]
				}
				if len(s) >= 4 && s[:4] != "2025" {
					out.value = map[string]string{"date": "2025" + s[4:]}
				} else if len(s) >= 4 {
					out.value = map[string]string{"date": "2026" + s[4:]}
				} else {
					out.value = map[string]string{"date": "2025-01-01T00:00:00"}
				}
			default:
				if s, ok := cur.value.(string); ok && s != "" {
					out.value = s + " law"
				} else {
					out.value = "law"
				}
			}
			return out
		},
		Apply:   lawApply,
		Refused: lawRefused,
		Units:   edit.Units(),
		Owns:    lawOwnsShape,
		Fail:    lawFail,
		Budget:  3,
	}
}

// pageNameSuite is the page name suite: page.rename.
func pageNameSuite() *laws.Suite[string] {
	return &laws.Suite[string]{
		Name: "page-name",
		Read: func(path string) ([]laws.Value[string], error) {
			pages, err := lawPages(path)
			if err != nil {
				return nil, err
			}
			var out []laws.Value[string]
			for _, lp := range pages {
				out = append(out, laws.Value[string]{
					Target: laws.Target{Part: lp.pagesPart, Addr: lp.page.ID},
					V:      lp.page.Name,
				})
			}
			return out, nil
		},
		Show: func(v string) string { return strconv.Quote(v) },
		Set: func(t laws.Target, v string) laws.Op {
			return &ops.PageRenameSpec{At: t.Addr, Name: v}
		},
		Vary: func(rng *rand.Rand, _ laws.Target, cur string) string {
			return fmt.Sprintf("%s law %d", cur, rng.IntN(10000))
		},
		Apply:   lawApply,
		Refused: lawRefused,
		Units:   edit.Units(),
		Owns:    lawOwnsPage,
		Fail:    lawFail,
		Budget:  3,
	}
}

// pageSizeSuite is the page size suite: page.set size. A page
// whose print orientation disagrees with its size orientation is
// not a target: setting its size materialises the print cell, so
// it has no size identity.
func pageSizeSuite() *laws.Suite[[2]float64] {
	return &laws.Suite[[2]float64]{
		Name: "page-size",
		Read: func(path string) ([]laws.Value[[2]float64], error) {
			pages, err := lawPages(path)
			if err != nil {
				return nil, err
			}
			var out []laws.Value[[2]float64]
			for _, lp := range pages {
				if !lp.page.HasSize || lawThePageDependent(lp) {
					continue
				}
				if lp.page.Orientation != "" && lp.page.PrintOrientation != "" && lp.page.PrintOrientation != lp.page.Orientation {
					continue
				}
				out = append(out, laws.Value[[2]float64]{
					Target: laws.Target{Part: lp.pagesPart, Addr: lp.page.ID},
					V:      [2]float64{lp.page.W * vsd.PointsPerInch, lp.page.H * vsd.PointsPerInch},
				})
			}
			return out, nil
		},
		Show: func(v [2]float64) string { return fmt.Sprintf("[%s %s]", geom.FormatNum(v[0]), geom.FormatNum(v[1])) },
		Set: func(t laws.Target, v [2]float64) laws.Op {
			return &ops.PageSetSpec{At: t.Addr, Size: []float64{v[0], v[1]}}
		},
		Vary: func(_ *rand.Rand, _ laws.Target, cur [2]float64) [2]float64 {
			return [2]float64{cur[0] + 36, cur[1]}
		},
		Apply:   lawApply,
		Refused: lawRefused,
		Units:   edit.Units(),
		Owns:    lawOwnsPage,
		Fail:    lawFail,
		Budget:  3,
	}
}

// pageOrientationSuite is the page orientation suite: page.set
// orientation. A page whose print orientation disagrees with its
// size orientation is not a target, as for page size.
func pageOrientationSuite() *laws.Suite[string] {
	return &laws.Suite[string]{
		Name: "page-orientation",
		Read: func(path string) ([]laws.Value[string], error) {
			pages, err := lawPages(path)
			if err != nil {
				return nil, err
			}
			var out []laws.Value[string]
			for _, lp := range pages {
				if lp.page.Orientation == "" || lawThePageDependent(lp) {
					continue
				}
				if lp.page.PrintOrientation != "" && lp.page.PrintOrientation != lp.page.Orientation {
					continue
				}
				out = append(out, laws.Value[string]{
					Target: laws.Target{Part: lp.pagesPart, Addr: lp.page.ID},
					V:      lp.page.Orientation,
				})
			}
			return out, nil
		},
		Show: func(v string) string { return v },
		Set: func(t laws.Target, v string) laws.Op {
			return &ops.PageSetSpec{At: t.Addr, Orientation: v}
		},
		Vary: func(_ *rand.Rand, _ laws.Target, cur string) string {
			if cur == "portrait" {
				return "landscape"
			}
			return "portrait"
		},
		Apply:   lawApply,
		Refused: lawRefused,
		Units:   edit.Units(),
		Owns:    lawOwnsPage,
		Fail:    lawFail,
		Budget:  3,
	}
}

// lawSuiteRun is one law suite's check, as a test subtest.
type lawSuiteRun struct {
	name string
	run  func(t *testing.T, src string, rep *laws.Report)
}

// lawSuiteRuns returns every CC14 law suite as a check.
func lawSuiteRuns() []lawSuiteRun {
	mk := func(name string, check func(t *testing.T, src string, rep *laws.Report)) lawSuiteRun {
		return lawSuiteRun{name: name, run: check}
	}
	return []lawSuiteRun{
		mk("box", func(t *testing.T, src string, rep *laws.Report) { boxSuite().Check(t, src, 11, rep) }),
		mk("fill", func(t *testing.T, src string, rep *laws.Report) { fillSuite().Check(t, src, 12, rep) }),
		mk("line", func(t *testing.T, src string, rep *laws.Report) { lineSuite().Check(t, src, 13, rep) }),
		mk("name", func(t *testing.T, src string, rep *laws.Report) { nameSuite().Check(t, src, 14, rep) }),
		mk("rot", func(t *testing.T, src string, rep *laws.Report) { rotSuite().Check(t, src, 15, rep) }),
		mk("flips", func(t *testing.T, src string, rep *laws.Report) { flipsSuite().Check(t, src, 16, rep) }),
		mk("ends", func(t *testing.T, src string, rep *laws.Report) { endsSuite().Check(t, src, 17, rep) }),
		mk("glue", func(t *testing.T, src string, rep *laws.Report) { glueSuite().Check(t, src, 18, rep) }),
		mk("text", func(t *testing.T, src string, rep *laws.Report) { textSuite().Check(t, src, 19, rep) }),
		mk("prop", func(t *testing.T, src string, rep *laws.Report) { propSuite().Check(t, src, 20, rep) }),
		mk("page-name", func(t *testing.T, src string, rep *laws.Report) { pageNameSuite().Check(t, src, 21, rep) }),
		mk("page-size", func(t *testing.T, src string, rep *laws.Report) { pageSizeSuite().Check(t, src, 22, rep) }),
		mk("page-orientation", func(t *testing.T, src string, rep *laws.Report) { pageOrientationSuite().Check(t, src, 23, rep) }),
		mk("text_block", func(t *testing.T, src string, rep *laws.Report) { textBlockSuite().Check(t, src, 24, rep) }),
	}
}

// TestLawsWrite runs every CC14 law suite on every V1 to V17
// fixture and logs each suite's report, requiring targets over the
// fixtures as a whole. The harness runs identity (every read
// target, inherited included, written back as a no-op), inverse
// (varied values read back, untargeted units checked), atomicity
// and sequencing (including box's object.move undo pair) on each.
func TestLawsWrite(t *testing.T) {
	totals := map[string]*laws.Report{}
	for _, name := range lawSuiteRuns() {
		totals[name.name] = &laws.Report{}
	}
	for _, fx := range lawFixtureNames {
		t.Run(fx, func(t *testing.T) {
			src := vsdxtest.Fixture(t, fx)
			for _, s := range lawSuiteRuns() {
				rep := totals[s.name]
				t.Run(s.name, func(t *testing.T) { s.run(t, src, rep) })
			}
		})
	}
	t.Run("totals", func(t *testing.T) {
		for _, s := range lawSuiteRuns() {
			rep := totals[s.name]
			t.Logf("%s: %s", s.name, rep.String())
			if rep.Targets == 0 {
				t.Errorf("%s: no targets over V1-V17", s.name)
			}
		}
	})
}

// lawLocalCell reports whether the object at target on the
// drawing at path stores a local cell key (its own sheet), which
// is how a varied box/rot/flips write materialises a master-held
// component as F="Inh" while the composite box origin stays
// master.
func lawLocalCell(path string, t laws.Target, key string) (bool, error) {
	pages, err := lawPages(path)
	if err != nil {
		return false, err
	}
	shape := lawShapeAddr(t.Addr)
	for _, lp := range pages {
		for _, o := range lp.page.Objects {
			if o.ID != shape {
				continue
			}
			for _, c := range o.Cells {
				if c.Key == key {
					return true, nil
				}
			}
			return false, nil
		}
	}
	return false, nil
}

// lawOriginOf returns the origin read shows for the property of
// target t on the page model of the drawing at path, or "" when
// the property has no origin (name, page properties).
func lawOriginOf(path string, t laws.Target, prop string) (string, error) {
	pages, err := lawPages(path)
	if err != nil {
		return "", err
	}
	shape := lawShapeAddr(t.Addr)
	for _, lp := range pages {
		if prop == "prop" {
			for _, o := range lp.page.Objects {
				for _, pr := range o.Props {
					if pr.ID == t.Addr {
						return string(pr.Origin), nil
					}
				}
			}
			continue
		}
		for _, o := range lp.page.Objects {
			if o.ID != shape {
				continue
			}
			if prop == "rot" || prop == "flips" {
				return string(o.Origins["box"]), nil
			}
			if prop == "from" || prop == "to" {
				_, end, _, ok := lawEndOf(t.Addr)
				if !ok {
					end = prop
				}
				return string(o.Origins[end]), nil
			}
			return string(o.Origins[prop]), nil
		}
	}
	return "", nil
}

// checkOriginLocal applies Vary to the first budget targets of
// suite s on src and checks the property reads back with origin
// local, skipping refused targets.
func checkOriginLocal[V any](t *testing.T, src string, s *laws.Suite[V], prop string, budget int) {
	t.Helper()
	vals, err := s.Read(src)
	if err != nil {
		t.Fatalf("%s: read: %v", s.Name, err)
	}
	rng := rand.New(rand.NewPCG(99, 7))
	for i, v := range vals {
		if i >= budget {
			break
		}
		want := s.Vary(rng, v.Target, v.V)
		dst := filepath.Join(t.TempDir(), "out.vsdx")
		if err := s.Apply(src, dst, []laws.Op{s.Set(v.Target, want)}); err != nil {
			if s.Refused(err) {
				continue
			}
			t.Errorf("%s: %s: set: %v", s.Name, v.Target.Addr, err)
			continue
		}
		origin, err := lawOriginOf(dst, v.Target, prop)
		if err != nil {
			t.Errorf("%s: %s: read back: %v", s.Name, v.Target.Addr, err)
			continue
		}
		if origin == "local" || origin == "" {
			continue
		}
		// Box, rot and flips are composite: a master-held Width
		// keeps the box origin master even when the varied Pin,
		// Angle or Flip cell is written local (materialised
		// F="Inh"). The local cell is the origin the chunk pins.
		cell := ""
		switch prop {
		case "box":
			cell = "PinX"
		case "rot":
			cell = "Angle"
		case "flips":
			cell = "FlipX"
		}
		if cell != "" {
			if ok, cerr := lawLocalCell(dst, v.Target, cell); cerr == nil && ok {
				continue
			}
		}
		t.Errorf("%s: %s: varied value reads back with origin %q, want local", s.Name, v.Target.Addr, origin)
	}
}

// TestLawsInverseOriginLocal checks the CC14 origin row: a varied
// value set by the inverse law reads back with origin local, for
// every suite whose property has an origin (box, fill, line, rot
// and flips through box, ends, text, prop). No suite pins an
// inherited value: identity accepts inherited origins, inverse
// materialises them local.
func TestLawsInverseOriginLocal(t *testing.T) {
	for _, fx := range lawFixtureNames {
		t.Run(fx, func(t *testing.T) {
			src := vsdxtest.Fixture(t, fx)
			t.Run("box", func(t *testing.T) { checkOriginLocal(t, src, boxSuite(), "box", 3) })
			t.Run("fill", func(t *testing.T) { checkOriginLocal(t, src, fillSuite(), "fill", 3) })
			t.Run("line", func(t *testing.T) { checkOriginLocal(t, src, lineSuite(), "line", 3) })
			t.Run("rot", func(t *testing.T) { checkOriginLocal(t, src, rotSuite(), "rot", 3) })
			t.Run("flips", func(t *testing.T) { checkOriginLocal(t, src, flipsSuite(), "flips", 3) })
			t.Run("ends", func(t *testing.T) { checkOriginLocal(t, src, endsSuite(), "to", 3) })
			t.Run("text", func(t *testing.T) { checkOriginLocal(t, src, textSuite(), "html", 3) })
			t.Run("prop", func(t *testing.T) { checkOriginLocal(t, src, propSuite(), "prop", 3) })
		})
	}
}

// TestLawsUntargetedPlantedBug proves the untargeted law catches a
// write that also touches a neighbour's cell, then removes the
// bug: it checks laws.Untargeted on a real box write (the target's
// own unit may change, its neighbour's may not), and on the same
// write with a neighbour shape wrongly claimed as owned the check
// would pass, so the ownership by shape ID above is what makes
// the planted bug fail. The planted write itself is built by
// writing the neighbour's box as well in one plan and confirming
// Untargeted with the first target's ownership reports the
// neighbour.
func TestLawsUntargetedPlantedBug(t *testing.T) {
	src := vsdxtest.Fixture(t, "v1-minimal")
	// A second shape is added by the test itself through a plan,
	// so the neighbour exists in both sides of the comparison.
	add := filepath.Join(t.TempDir(), "two.vsdx")
	plan := &ops.Plan{Ops: []ops.Spec{&ops.ObjectAddSpec{At: "page0", Geom: "rect", Box: []float64{216, 126, 72, 36}}}}
	if _, err := Run(src, plan, Options{Out: add}); err != nil {
		t.Fatalf("add neighbour: %v", err)
	}
	// The planted bug: one plan writing both shapes' boxes.
	both := filepath.Join(t.TempDir(), "both.vsdx")
	planted := &ops.Plan{Ops: []ops.Spec{
		&ops.ObjectSetSpec{At: "page0/s1", Box: []float64{108, 126, 72, 36}},
		&ops.ObjectSetSpec{At: "page0/s2", Box: []float64{217, 126, 72, 36}},
	}}
	// The first op is identity, the second is the neighbour touch;
	// run only the neighbour touch as the planted write.
	planted.Ops = planted.Ops[1:]
	if _, err := Run(add, planted, Options{Out: both}); err != nil {
		t.Fatalf("planted write: %v", err)
	}
	// Untargeted with s1's ownership must report s2's change.
	target := laws.Target{Part: "/visio/pages/page1.xml", Addr: "page0/s1"}
	if err := laws.Untargeted(add, both, target.Part, edit.Units(), func(e *tree.Node) bool { return lawOwnsShape(target, e) }); err == nil {
		t.Error("planted bug (neighbour box written) went unnoticed by the untargeted law")
	} else {
		t.Logf("planted bug caught: %v", err)
	}
	// And the honest single-target write passes the same check.
	honest := filepath.Join(t.TempDir(), "honest.vsdx")
	honestPlan := &ops.Plan{Ops: []ops.Spec{&ops.ObjectSetSpec{At: "page0/s1", Box: []float64{109, 126, 72, 36}}}}
	if _, err := Run(add, honestPlan, Options{Out: honest}); err != nil {
		t.Fatalf("honest write: %v", err)
	}
	if err := laws.Untargeted(add, honest, target.Part, edit.Units(), func(e *tree.Node) bool { return lawOwnsShape(target, e) }); err != nil {
		t.Errorf("honest write failed the untargeted law: %v", err)
	}
}
