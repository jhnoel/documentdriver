package model

import (
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/path"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// paint derives fill and line: FillPattern 0 is fill "none", otherwise
// FillForegnd through doc.Document.Color; LinePattern 0 is line "none",
// otherwise LineWeight in points, LineColor through Color and
// LinePattern as Dash. A Themed pattern or weight, and unresolvable
// colours, omit with the reason. Chunk M2b, MF3.
func paint(o *Object, r *inherit.Resolver, s *inherit.Shape) {
	if o == nil {
		return
	}
	paintFill(o, r, s)
	paintLine(o, r, s)
	if o.Kind == Connector || ((o.Geom == path.Polyline || o.Geom == path.Curve) && !o.Closed) {
		paintLineEnds(o, r, s)
	}
	if o.Kind == Shape || o.Kind == Group {
		paintShadow(o, r, s)
	}
}

// paintFill sets o's fill from the effective FillPattern and
// FillForegnd cells of s: a pattern of 0 is fill none, otherwise the
// foreground colour through paintColor, and a pattern from 2 to 24
// also its name and background (paintBackground). A Themed pattern (checked
// before the pattern is parsed as a number), a themed or
// unresolvable foreground, or a pattern that is not a number, leaves
// the fill out with an omission; a foreground no sheet stores leaves
// it out silently when no pattern is stored either. The fill's origin
// is the most inherited of the pattern and foreground cells' origins.
func paintFill(o *Object, r *inherit.Resolver, s *inherit.Shape) {
	patternN := 1.0
	pattern, patternOrigin, patternOk := r.Cell(s, sheet.ShapeCell(vsd.CellFillPattern))
	if patternOk {
		if pattern.V == vsd.ValueThemed {
			o.Omitted = append(o.Omitted, Omission{Prop: "fill", Reason: "themed value not derived"})
			return
		}
		n, ok := vsd.Number(pattern.V)
		if !ok {
			o.Omitted = append(o.Omitted, Omission{Prop: "fill", Reason: vsd.CellFillPattern + " not a number"})
			return
		}
		if n == 0 {
			o.Fill = &Paint{None: true}
			setOrigin(o, "fill", patternOrigin)
			return
		}
		patternN = n
	}
	fore, foreOrigin, foreOk := r.Cell(s, sheet.ShapeCell(vsd.CellFillForegnd))
	if !foreOk {
		if patternOk {
			o.Omitted = append(o.Omitted, Omission{Prop: "fill", Reason: vsd.CellFillForegnd + " not stored"})
		}
		return
	}
	if fore.V == vsd.ValueThemed {
		o.Omitted = append(o.Omitted, Omission{Prop: "fill", Reason: "themed value not derived"})
		return
	}
	hex, ok := paintColor(r, fore.V)
	if !ok {
		o.Omitted = append(o.Omitted, Omission{Prop: "fill", Reason: "colour " + fore.V + " not resolved"})
		return
	}
	o.Fill = &Paint{Hex: hex}
	best := boxOrigin(patternOrigin, foreOrigin)
	if patternN >= vsd.FillPatternMin && patternN <= vsd.FillPatternMax && patternN == float64(int(patternN)) {
		key := vsd.FormatInches(patternN)
		o.Fill.Pattern = key
		if name, named := vsd.FillPatternNames[key]; named {
			o.Fill.Pattern = name
		}
		best = boxOrigin(best, paintBackground(o, r, s))
	}
	setOrigin(o, "fill", best)
}

// paintBackground sets the background of o's pattern fill from the
// effective FillBkgndTrans and FillBkgnd cells: a transparency of 1
// (fully clear) is BgNone; otherwise the background colour through
// paintColor, left out with a fill.bg omission when it is themed,
// unresolvable or not stored, and noted as fill.bg when a partial
// transparency is not shown. It returns the origin of the cells used.
func paintBackground(o *Object, r *inherit.Resolver, s *inherit.Shape) inherit.Origin {
	var best inherit.Origin
	trans := 0.0
	if t, to, ok := r.Cell(s, sheet.ShapeCell(vsd.CellFillBkgndTrans)); ok {
		v, isNum := vsd.Number(t.V)
		if !isNum {
			o.Omitted = append(o.Omitted, Omission{Prop: "fill.bg", Reason: shadowReason(t.V)})
			return best
		}
		trans, best = v, to
	}
	if trans >= 1 {
		o.Fill.BgNone = true
		return best
	}
	bg, bo, ok := r.Cell(s, sheet.ShapeCell(vsd.CellFillBkgnd))
	if !ok {
		o.Omitted = append(o.Omitted, Omission{Prop: "fill.bg", Reason: vsd.CellFillBkgnd + " not stored"})
		return best
	}
	hex, okc := paintColor(r, bg.V)
	if !okc || bg.V == vsd.ValueThemed {
		o.Omitted = append(o.Omitted, Omission{Prop: "fill.bg", Reason: shadowReason(bg.V)})
		return best
	}
	o.Fill.BgHex = hex
	if trans > 0 {
		o.Omitted = append(o.Omitted, Omission{Prop: "fill.bg", Reason: vsd.CellFillBkgndTrans + " " + vsd.FormatInches(trans) + " not shown"})
	}
	return boxOrigin(best, bo)
}

// paintLine sets o's line from the effective LinePattern, LineWeight
// and LineColor cells of s: a pattern of 0 is line none, otherwise the
// weight in points (stored inches through vsd.PointsPerInch), the
// colour through paintColor and the pattern as stored for Dash. A
// Themed pattern (checked before the pattern is parsed as a number)
// leaves the whole line out; a Themed weight (checked before it is
// parsed as a number) or a themed or unresolvable colour omits only
// that part, keeping the rest; a cell that is not a number omits that
// part. A line no sheet stores at all is left out. The line's origin
// is the most inherited of the origins of the parts it shows.
func paintLine(o *Object, r *inherit.Resolver, s *inherit.Shape) {
	pattern, patternOrigin, patternOk := r.Cell(s, sheet.ShapeCell(vsd.CellLinePattern))
	if patternOk {
		if pattern.V == vsd.ValueThemed {
			o.Omitted = append(o.Omitted, Omission{Prop: "line", Reason: "themed value not derived"})
			return
		}
		n, ok := vsd.Number(pattern.V)
		if !ok {
			o.Omitted = append(o.Omitted, Omission{Prop: "line", Reason: vsd.CellLinePattern + " not a number"})
			return
		}
		if n == 0 {
			o.Line = &Line{None: true}
			setOrigin(o, "line", patternOrigin)
			return
		}
	}
	weight, weightOrigin, weightOk := r.Cell(s, sheet.ShapeCell(vsd.CellLineWeight))
	color, colorOrigin, colorOk := r.Cell(s, sheet.ShapeCell(vsd.CellLineColor))
	if !patternOk && !weightOk && !colorOk {
		return
	}
	line := &Line{}
	// best folds the origins of the parts that are shown, one at a
	// time (boxOrigin is the maximum over its arguments, so a fold
	// equals the fold of the whole list), rather than building a list.
	var best inherit.Origin
	if weightOk {
		if weight.V == vsd.ValueThemed {
			o.Omitted = append(o.Omitted, Omission{Prop: "line", Reason: "themed value not derived"})
		} else if n, ok := vsd.Number(weight.V); ok {
			line.W = n * vsd.PointsPerInch
			line.HasW = true
			best = boxOrigin(best, weightOrigin)
		} else {
			o.Omitted = append(o.Omitted, Omission{Prop: "line", Reason: vsd.CellLineWeight + " not a number"})
		}
	}
	if colorOk {
		if color.V == vsd.ValueThemed {
			o.Omitted = append(o.Omitted, Omission{Prop: "line", Reason: "themed value not derived"})
		} else if hex, ok := paintColor(r, color.V); ok {
			line.Color = hex
			best = boxOrigin(best, colorOrigin)
		} else {
			o.Omitted = append(o.Omitted, Omission{Prop: "line", Reason: "colour " + color.V + " not resolved"})
		}
	}
	if patternOk {
		line.Dash = pattern.V
		best = boxOrigin(best, patternOrigin)
	}
	o.Line = line
	if best != "" {
		setOrigin(o, "line", best)
	}
}

// paintColor resolves a stored colour value v for resolver r through
// the document's colour table (Resolver.Doc().Color), so a stored hex
// is normalised, a document or built-in index is resolved, and
// "Themed" and an index no table holds report false. A nil resolver
// resolves only a stored hex.
func paintColor(r *inherit.Resolver, v string) (string, bool) {
	if r == nil {
		return vsd.Color(v)
	}
	return r.Doc().Color(v)
}

// paintLineEnds sets a connector's line ends (docs/contract.md §8):
// for each of Begin and End, the effective arrow cell, shown only when
// it is not 0; a themed arrow omits line.begin or line.end, a themed
// size omits only line.begin.size or line.end.size. A line that is
// none shows no ends. The ends share the line's origin key.
func paintLineEnds(o *Object, r *inherit.Resolver, s *inherit.Shape) {
	if o.Line != nil && o.Line.None {
		return
	}
	for _, e := range []struct {
		prop, arrow, size string
		set               func(l *Line, v *LineEnd)
	}{
		{"line.begin", vsd.CellBeginArrow, vsd.CellBeginArrowSize, func(l *Line, v *LineEnd) { l.Begin = v }},
		{"line.end", vsd.CellEndArrow, vsd.CellEndArrowSize, func(l *Line, v *LineEnd) { l.End = v }},
	} {
		cell, origin, ok := r.Cell(s, sheet.ShapeCell(e.arrow))
		if !ok {
			continue
		}
		if cell.V == vsd.ValueThemed {
			o.Omitted = append(o.Omitted, Omission{Prop: e.prop, Reason: "themed value not derived"})
			continue
		}
		n, isNum := vsd.Number(cell.V)
		if !isNum {
			o.Omitted = append(o.Omitted, Omission{Prop: e.prop, Reason: e.arrow + " not a number"})
			continue
		}
		if n == 0 {
			continue
		}
		key := vsd.FormatInches(n)
		end := &LineEnd{Arrow: key}
		if name, named := vsd.ArrowNames[key]; named {
			end.Arrow = name
		}
		best := origin
		if sz, szOrigin, has := r.Cell(s, sheet.ShapeCell(e.size)); has {
			if sz.V == vsd.ValueThemed {
				o.Omitted = append(o.Omitted, Omission{Prop: e.prop + ".size", Reason: "themed value not derived"})
			} else if v, okNum := vsd.Number(sz.V); okNum {
				end.Size, end.HasSize = v, true
				best = boxOrigin(best, szOrigin)
			}
		}
		if o.Line == nil {
			o.Line = &Line{}
		}
		e.set(o.Line, end)
		setOrigin(o, "line", boxOrigin(o.Origins["line"], best))
	}
}

// paintShadow sets a shape's or group's shadow (docs/contract.md §8)
// from the effective ShdwPattern: 0 or absent is no shadow; themed or
// not a number omits shadow; otherwise colour (ShdwForegnd),
// transparency (ShdwForegndTrans as percent) and, when ShapeShdwType
// is 1, the offset (ShapeShdwOffsetX/Y, inches y up, shown in points
// y down), each omitted alone when themed or not derived. A non-zero
// blur, oblique angle or scale is named in omitted.
func paintShadow(o *Object, r *inherit.Resolver, s *inherit.Shape) {
	pattern, origin, ok := r.Cell(s, sheet.ShapeCell(vsd.CellShdwPattern))
	if !ok {
		return
	}
	if pattern.V == vsd.ValueThemed {
		o.Omitted = append(o.Omitted, Omission{Prop: "shadow", Reason: "themed value not derived"})
		return
	}
	n, isNum := vsd.Number(pattern.V)
	if !isNum {
		o.Omitted = append(o.Omitted, Omission{Prop: "shadow", Reason: vsd.CellShdwPattern + " not a number"})
		return
	}
	if n == 0 {
		return
	}
	sh := &Shadow{}
	best := origin
	if c, co, has := r.Cell(s, sheet.ShapeCell(vsd.CellShdwForegnd)); has {
		if hex, okc := paintColor(r, c.V); okc && c.V != vsd.ValueThemed {
			sh.Color, sh.HasColor = hex, true
			best = boxOrigin(best, co)
		} else {
			o.Omitted = append(o.Omitted, Omission{Prop: "shadow.color", Reason: shadowReason(c.V)})
		}
	}
	if t, to, has := r.Cell(s, sheet.ShapeCell(vsd.CellShdwForegndTrans)); has {
		if v, okt := vsd.Number(t.V); okt {
			sh.Transparency, sh.HasTrans = v*100, true
			best = boxOrigin(best, to)
		} else {
			o.Omitted = append(o.Omitted, Omission{Prop: "shadow.transparency", Reason: shadowReason(t.V)})
		}
	}
	typ, _, hasType := r.Cell(s, sheet.ShapeCell(vsd.CellShapeShdwType))
	tn, typeNum := vsd.Number(typ.V)
	if hasType && typeNum && tn == 1 {
		x, xo, hasX := r.Cell(s, sheet.ShapeCell(vsd.CellShapeShdwOffsetX))
		y, yo, hasY := r.Cell(s, sheet.ShapeCell(vsd.CellShapeShdwOffsetY))
		xv, okx := vsd.Number(x.V)
		yv, oky := vsd.Number(y.V)
		if hasX && hasY && okx && oky {
			sh.Offset, sh.HasOffset = [2]float64{xv * vsd.PointsPerInch, -yv * vsd.PointsPerInch}, true
			best = boxOrigin(best, xo, yo)
		} else {
			o.Omitted = append(o.Omitted, Omission{Prop: "shadow.offset", Reason: "themed value not derived"})
		}
	} else {
		o.Omitted = append(o.Omitted, Omission{Prop: "shadow.offset", Reason: "page default shadow not derived"})
	}
	for _, c := range []string{vsd.CellShapeShdwBlur, vsd.CellShapeShdwOblique} {
		if v, _, has := r.Cell(s, sheet.ShapeCell(c)); has {
			if f, okf := vsd.Number(v.V); okf && f != 0 {
				o.Omitted = append(o.Omitted, Omission{Prop: "shadow", Reason: c + " " + v.V + " not shown"})
			}
		}
	}
	if v, _, has := r.Cell(s, sheet.ShapeCell(vsd.CellShapeShdwScale)); has {
		if f, okf := vsd.Number(v.V); okf && f != 1 {
			o.Omitted = append(o.Omitted, Omission{Prop: "shadow", Reason: vsd.CellShapeShdwScale + " " + v.V + " not shown"})
		}
	}
	o.Shadow = sh
	setOrigin(o, "shadow", best)
}

// shadowReason is the omission reason of a shadow member stored as v.
func shadowReason(v string) string {
	if v == vsd.ValueThemed {
		return "themed value not derived"
	}
	return "value " + v + " not derived"
}
