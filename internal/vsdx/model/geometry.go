package model

import (
	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/place"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// geometry derives the box (2-D, and 1-D with Full), the ends (1-D) and
// the sites (with Sites or Full) from the effective transform cells and
// Connection rows, through place.Box, place.Point and place.Site, and
// sets the box and ends origins. A cell that is absent or not a number
// omits the property with its reason. Chunk M2a.
func geometry(o *Object, r *inherit.Resolver, s *inherit.Shape, f place.Frame, pageH float64, opt Options) {
	if o == nil {
		return
	}
	_, _, beginOK := r.Cell(s, sheet.ShapeCell(vsd.CellBeginX))
	oneD := o.Kind == Connector || beginOK
	if oneD {
		if from, origin, reason := endPoint(r, s, f, pageH, vsd.CellBeginX, vsd.CellBeginY); reason != "" {
			o.Omitted = append(o.Omitted, Omission{Prop: "from", Reason: reason})
		} else {
			o.From = from
			if opt.Full {
				setOrigin(o, "from", origin)
			}
		}
		if to, origin, reason := endPoint(r, s, f, pageH, vsd.CellEndX, vsd.CellEndY); reason != "" {
			o.Omitted = append(o.Omitted, Omission{Prop: "to", Reason: reason})
		} else {
			o.To = to
			if opt.Full {
				setOrigin(o, "to", origin)
			}
		}
	}
	t, missing, hasT := transformOf(r, s)
	if !oneD || opt.Full {
		switch {
		case !hasT:
			o.Omitted = append(o.Omitted, Omission{Prop: "box", Reason: missingReason(r, s, missing)})
		default:
			if box, err := place.Box(f, t, pageH); err != nil {
				o.Omitted = append(o.Omitted, Omission{Prop: "box", Reason: err.Error()})
			} else {
				o.Box = &box
				if opt.Full {
					setOrigin(o, "box", transformOrigin(r, s))
				}
			}
		}
	}
	if (opt.Sites || opt.Full) && hasT {
		o.Sites = sitesOf(r, s, f, t, pageH)
	}
}

// transformOf reads a shape's effective transform cells; ok is false
// (with the missing or unreadable cell named) when one cannot be
// derived. Chunk M2a.
func transformOf(r *inherit.Resolver, s *inherit.Shape) (t place.Transform, missing string, ok bool) {
	if !numInto(r, s, vsd.CellPinX, &t.PinX) {
		return place.Transform{}, vsd.CellPinX, false
	}
	if !numInto(r, s, vsd.CellPinY, &t.PinY) {
		return place.Transform{}, vsd.CellPinY, false
	}
	if !numInto(r, s, vsd.CellWidth, &t.Width) {
		return place.Transform{}, vsd.CellWidth, false
	}
	if !numInto(r, s, vsd.CellHeight, &t.Height) {
		return place.Transform{}, vsd.CellHeight, false
	}
	if !numInto(r, s, vsd.CellLocPinX, &t.LocPinX) {
		return place.Transform{}, vsd.CellLocPinX, false
	}
	if !numInto(r, s, vsd.CellLocPinY, &t.LocPinY) {
		return place.Transform{}, vsd.CellLocPinY, false
	}
	if !numInto(r, s, vsd.CellAngle, &t.Angle) {
		return place.Transform{}, vsd.CellAngle, false
	}
	if !boolInto(r, s, vsd.CellFlipX, &t.FlipX) {
		return place.Transform{}, vsd.CellFlipX, false
	}
	if !boolInto(r, s, vsd.CellFlipY, &t.FlipY) {
		return place.Transform{}, vsd.CellFlipY, false
	}
	return t, "", true
}

// numInto stores the effective number of the shape-level cell name for
// s in dst, reporting false when it is absent or not a number.
func numInto(r *inherit.Resolver, s *inherit.Shape, name string, dst *float64) bool {
	v, _, reason := numCell(r, s, name)
	if reason != "" {
		return false
	}
	*dst = v
	return true
}

// boolInto stores the effective boolean of the shape-level cell name
// for s in dst, reporting false when it is absent or not a boolean.
func boolInto(r *inherit.Resolver, s *inherit.Shape, name string, dst *bool) bool {
	v, _, reason := boolCell(r, s, name)
	if reason != "" {
		return false
	}
	*dst = v
	return true
}

// numCell reads the effective number of the shape-level cell name for s;
// reason is "" on success, otherwise the omission reason naming the cell
// ("PinX not stored" when no sheet stores it, "PinX not a number" when
// the stored V does not parse).
func numCell(r *inherit.Resolver, s *inherit.Shape, name string) (float64, inherit.Origin, string) {
	cell, origin, ok := r.Cell(s, sheet.ShapeCell(name))
	if !ok {
		return 0, "", name + " not stored"
	}
	v, ok := vsd.Number(cell.V)
	if !ok {
		return 0, "", name + " not a number"
	}
	return v, origin, ""
}

// boolCell reads the effective boolean of the shape-level cell name for s;
// reason is "" on success, otherwise the omission reason naming the cell.
func boolCell(r *inherit.Resolver, s *inherit.Shape, name string) (bool, inherit.Origin, string) {
	cell, origin, ok := r.Cell(s, sheet.ShapeCell(name))
	if !ok {
		return false, "", name + " not stored"
	}
	v, ok := vsd.Bool(cell.V)
	if !ok {
		return false, "", name + " not a boolean"
	}
	return v, origin, ""
}

// missingReason returns the omission reason for the transform cell named
// missing, which transformOf already found unreadable.
func missingReason(r *inherit.Resolver, s *inherit.Shape, missing string) string {
	var reason string
	if missing == vsd.CellFlipX || missing == vsd.CellFlipY {
		_, _, reason = boolCell(r, s, missing)
	} else {
		_, _, reason = numCell(r, s, missing)
	}
	if reason == "" {
		return missing + " not stored"
	}
	return reason
}

// endPoint returns the view point of the 1-D end whose coordinates are
// the shape-level cells xName and yName in f's space, with the most
// inherited of their origins; reason is non-empty (naming the first
// missing or unreadable coordinate) when the end cannot be derived.
func endPoint(r *inherit.Resolver, s *inherit.Shape, f place.Frame, pageH float64, xName, yName string) (*geom.Pt, inherit.Origin, string) {
	x, ox, reason := numCell(r, s, xName)
	if reason != "" {
		return nil, "", reason
	}
	y, oy, reason := numCell(r, s, yName)
	if reason != "" {
		return nil, "", reason
	}
	pt := place.Point(f, x, y, pageH)
	return &pt, mostInherited([]inherit.Origin{ox, oy}), ""
}

// sitesOf returns the connection sites of s in row order: for each
// effective Connection row the point of its X and Y cells through
// place.Site, numbered by row IX+1; rows whose X or Y cannot be read
// are left out.
func sitesOf(r *inherit.Resolver, s *inherit.Shape, f place.Frame, t place.Transform, pageH float64) []Site {
	rows := r.Rows(s, vsd.SectionConnection)
	if len(rows) == 0 {
		return nil
	}
	sites := make([]Site, 0, len(rows))
	for _, row := range rows {
		if row.Key.RowIX < 0 {
			continue
		}
		x, ok := rowNumber(r, s, row.Key, "X")
		if !ok {
			continue
		}
		y, ok := rowNumber(r, s, row.Key, "Y")
		if !ok {
			continue
		}
		sites = append(sites, Site{N: row.Key.RowIX + 1, Pt: place.Site(f, t, x, y, pageH)})
	}
	return sites
}

// rowNumber reads the effective number of cell cellName of the row
// named by key for s, reporting false when it is absent or not a number.
func rowNumber(r *inherit.Resolver, s *inherit.Shape, key sheet.Key, cellName string) (float64, bool) {
	cell, _, ok := r.Cell(s, key.WithCell(cellName))
	if !ok {
		return 0, false
	}
	return vsd.Number(cell.V)
}

// transformOrigin returns the most inherited origin of the transform
// cells of s, as box's origin.
func transformOrigin(r *inherit.Resolver, s *inherit.Shape) inherit.Origin {
	names := [9]string{vsd.CellPinX, vsd.CellPinY, vsd.CellWidth, vsd.CellHeight, vsd.CellLocPinX, vsd.CellLocPinY, vsd.CellAngle, vsd.CellFlipX, vsd.CellFlipY}
	origins := make([]inherit.Origin, 0, len(names))
	for _, name := range names {
		_, origin, _ := r.Cell(s, sheet.ShapeCell(name))
		origins = append(origins, origin)
	}
	return mostInherited(origins)
}

// setOrigin records origin for prop on o, creating the origins map when
// o has none.
func setOrigin(o *Object, prop string, origin inherit.Origin) {
	if o.Origins == nil {
		o.Origins = map[string]inherit.Origin{}
	}
	o.Origins[prop] = origin
}

// mostInherited returns the most inherited of origins, in the order
// local < inherited < master < style (docs/vsdx-design.md, decision a),
// or "" when origins holds none.
func mostInherited(origins []inherit.Origin) inherit.Origin {
	best := inherit.Origin("")
	bestRank := -1
	for _, origin := range origins {
		if rank := originRank(origin); rank > bestRank {
			best, bestRank = origin, rank
		}
	}
	return best
}

// originRank ranks an origin for mostInherited: local 0, inherited 1,
// master 2, style 3, and -1 for anything else.
func originRank(origin inherit.Origin) int {
	switch origin {
	case inherit.Local:
		return 0
	case inherit.Inherited:
		return 1
	case inherit.Master:
		return 2
	case inherit.Style:
		return 3
	}
	return -1
}
