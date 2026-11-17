package model

import (
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// paint derives fill and line: FillPattern 0 is fill "none", otherwise
// FillForegnd through doc.Document.Color; LinePattern 0 is line "none",
// otherwise LineWeight in points, LineColor through Color and
// LinePattern as Dash. "Themed" and unresolvable colours omit the
// colour with the reason. Chunk M2b.
func paint(o *Object, r *inherit.Resolver, s *inherit.Shape) {
	if o == nil {
		return
	}
	paintFill(o, r, s)
	paintLine(o, r, s)
}

// paintFill sets o's fill from the effective FillPattern and
// FillForegnd cells of s: a pattern of 0 is fill none, otherwise the
// foreground colour through paintColor. A themed or unresolvable
// foreground, or a pattern that is not a number, leaves the fill out
// with an omission; a foreground no sheet stores leaves it out
// silently when no pattern is stored either. The fill's origin is the
// most inherited of the pattern and foreground cells' origins.
func paintFill(o *Object, r *inherit.Resolver, s *inherit.Shape) {
	pattern, patternOrigin, patternOk := r.Cell(s, sheet.ShapeCell(vsd.CellFillPattern))
	if patternOk {
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
	setOrigin(o, "fill", boxOrigin(patternOrigin, foreOrigin))
}

// paintLine sets o's line from the effective LinePattern, LineWeight
// and LineColor cells of s: a pattern of 0 is line none, otherwise the
// weight in points (stored inches through vsd.PointsPerInch), the
// colour through paintColor and the pattern as stored for Dash. A
// themed or unresolvable colour omits only the colour, keeping the
// weight and dash; a cell that is not a number omits that part. A line
// no sheet stores at all is left out. The line's origin is the most
// inherited of the origins of the parts it shows.
func paintLine(o *Object, r *inherit.Resolver, s *inherit.Shape) {
	pattern, patternOrigin, patternOk := r.Cell(s, sheet.ShapeCell(vsd.CellLinePattern))
	if patternOk {
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
	var origins []inherit.Origin
	if weightOk {
		if n, ok := vsd.Number(weight.V); ok {
			line.W = n * vsd.PointsPerInch
			line.HasW = true
			origins = append(origins, weightOrigin)
		} else {
			o.Omitted = append(o.Omitted, Omission{Prop: "line", Reason: vsd.CellLineWeight + " not a number"})
		}
	}
	if colorOk {
		if color.V == vsd.ValueThemed {
			o.Omitted = append(o.Omitted, Omission{Prop: "line", Reason: "themed value not derived"})
		} else if hex, ok := paintColor(r, color.V); ok {
			line.Color = hex
			origins = append(origins, colorOrigin)
		} else {
			o.Omitted = append(o.Omitted, Omission{Prop: "line", Reason: "colour " + color.V + " not resolved"})
		}
	}
	if patternOk {
		line.Dash = pattern.V
		origins = append(origins, patternOrigin)
	}
	o.Line = line
	if origin := boxOrigin(origins...); origin != "" {
		setOrigin(o, "line", origin)
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
