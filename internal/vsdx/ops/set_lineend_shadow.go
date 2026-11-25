package ops

import (
	"maps"
	"slices"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/path"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// styleIsConnector reports whether st is a 1-D shape (it has an
// effective BeginX cell), the kind read shows as connector.
func styleIsConnector(st *styleShape) bool {
	_, _, ok := st.res.Cell(st.shape, sheet.ShapeCell(vsd.CellBeginX))
	return ok
}

// styleIsOpenPath reports whether st is a master-less shape whose
// geometry is an open polyline or curve in the form vsdx writes
// (path.Parse), the shapes read shows line ends on besides connectors.
func styleIsOpenPath(st *styleShape) bool {
	if st.shape == nil || st.shape.HasMaster || st.shape.HasMShape {
		return false
	}
	g, ok := path.Parse(st.node)
	return ok && (g.Kind == path.Polyline || g.Kind == path.Curve) && !g.Closed
}

// lineEndCells returns the cells that write line.begin and line.end
// of s on st (docs/contract.md §3, Line ends): a name maps to its
// stored index, an index string is accepted only when it equals the
// shown arrow (and then writes nothing), a size left out keeps the
// size, and a value equal to the effective one writes nothing. A
// line end on a shape that is neither a connector nor an open path, an index the table
// does not name, and a cell bound to a formula vsdx does not evaluate
// are operation_failed at the field.
func lineEndCells(st *styleShape, s *ObjectSetSpec) ([]edit.Cell, error) {
	if s.Line == nil || (s.Line.Begin == nil && s.Line.End == nil) {
		return nil, nil
	}
	var cells []edit.Cell
	for _, e := range []struct {
		spec        *LineEndSpec
		field       string
		arrow, size string
	}{
		{s.Line.Begin, "line.begin", vsd.CellBeginArrow, vsd.CellBeginArrowSize},
		{s.Line.End, "line.end", vsd.CellEndArrow, vsd.CellEndArrowSize},
	} {
		if e.spec == nil {
			continue
		}
		if !styleIsConnector(st) && !styleIsOpenPath(st) {
			return nil, cli.Errorf(cli.CodeOperationFailed, e.field, "%s is not a connector or an open path: line ends apply to connectors (1-D shapes) and open polylines and curves only", st.addr)
		}
		cur, hasCur := styleStoredCell(st, e.arrow)
		curKey := ""
		if n, ok := vsd.Number(cur); hasCur && ok {
			curKey = vsd.FormatInches(n)
		} else if !hasCur {
			curKey = "0"
		}
		want, named := vsd.ArrowIndex[e.spec.Arrow]
		if !named {
			if e.spec.Arrow != curKey || curKey == "" {
				names := slices.Sorted(maps.Keys(vsd.ArrowIndex))
				er := cli.Errorf(cli.CodeOperationFailed, e.field, "arrow %q is not a name vsdx writes", e.spec.Arrow)
				er.Hint = "names: " + strings.Join(names, ", ")
				return nil, er
			}
			want = curKey
		}
		if want != curKey {
			if c, ok := styleRefused(st, e.arrow); ok {
				return nil, formulaRefusal(st.addr, e.field, []formulaCell{c})
			}
			cells = append(cells, edit.Cell{N: e.arrow, V: want})
		}
		if e.spec.Size != nil {
			got, ok := styleNumberCell(st, e.size)
			if !ok || got != *e.spec.Size {
				if c, refused := styleRefused(st, e.size); refused {
					return nil, formulaRefusal(st.addr, e.field, []formulaCell{c})
				}
				cells = append(cells, edit.Cell{N: e.size, V: geom.FormatNum(*e.spec.Size)})
			}
		}
	}
	return cells, nil
}

// shadowCells returns the cells that write the shadow of s on st
// (docs/contract.md §3, Shadows): "none" stores ShdwPattern 0; an
// object stores ShdwPattern 1, ShdwForegnd, ShdwForegndTrans,
// ShapeShdwType 1 and the offsets (inches, y up), each only when the
// shown value differs. On an object with no shadow every member is
// needed. A connector, and a cell bound to a formula vsdx does not
// evaluate, are operation_failed at shadow.
func shadowCells(st *styleShape, s *ObjectSetSpec, scale float64) ([]edit.Cell, error) {
	sh := s.Shadow
	if sh == nil {
		return nil, nil
	}
	if styleIsConnector(st) {
		return nil, cli.Errorf(cli.CodeOperationFailed, "shadow", "%s is a connector: shadow applies to shapes and groups", st.addr)
	}
	pattern, patternOk := styleNumberCell(st, vsd.CellShdwPattern)
	shown := patternOk && pattern != 0
	var cells []edit.Cell
	add := func(name, v string) error {
		if c, ok := styleRefused(st, name); ok {
			return formulaRefusal(st.addr, "shadow", []formulaCell{c})
		}
		cells = append(cells, edit.Cell{N: name, V: v})
		return nil
	}
	if sh.None {
		if shown {
			if err := add(vsd.CellShdwPattern, "0"); err != nil {
				return nil, err
			}
		}
		return cells, nil
	}
	if !shown {
		var missing []string
		if sh.Color == "" {
			missing = append(missing, "color")
		}
		if sh.Transparency == nil {
			missing = append(missing, "transparency")
		}
		if sh.Offset == nil {
			missing = append(missing, "offset")
		}
		if len(missing) > 0 {
			return nil, cli.Errorf(cli.CodeOperationFailed, "shadow", "%s shows no shadow: shadow needs %s", st.addr, strings.Join(missing, ", "))
		}
		if err := add(vsd.CellShdwPattern, "1"); err != nil {
			return nil, err
		}
	}
	if sh.Color != "" {
		if got, ok := styleColorCell(st, vsd.CellShdwForegnd); !ok || got != sh.Color {
			if err := add(vsd.CellShdwForegnd, sh.Color); err != nil {
				return nil, err
			}
		}
	}
	if sh.Transparency != nil {
		want := *sh.Transparency / 100
		if got, ok := styleNumberCell(st, vsd.CellShdwForegndTrans); !ok || geom.FormatNum(got*100) != geom.FormatNum(*sh.Transparency) {
			if err := add(vsd.CellShdwForegndTrans, vsd.FormatInches(want)); err != nil {
				return nil, err
			}
		}
	}
	if sh.Offset != nil {
		typ, typOk := styleNumberCell(st, vsd.CellShapeShdwType)
		x, xOk := styleNumberCell(st, vsd.CellShapeShdwOffsetX)
		y, yOk := styleNumberCell(st, vsd.CellShapeShdwOffsetY)
		dx, dy := sh.Offset[0]*scale, sh.Offset[1]*scale
		if !typOk || typ != 1 {
			if err := add(vsd.CellShapeShdwType, "1"); err != nil {
				return nil, err
			}
		}
		if !xOk || geom.FormatNum(x*vsd.PointsPerInch) != geom.FormatNum(dx) {
			if err := add(vsd.CellShapeShdwOffsetX, vsd.FormatInches(dx/vsd.PointsPerInch)); err != nil {
				return nil, err
			}
		}
		if !yOk || geom.FormatNum(-y*vsd.PointsPerInch) != geom.FormatNum(dy) {
			if err := add(vsd.CellShapeShdwOffsetY, vsd.FormatInches(-dy/vsd.PointsPerInch)); err != nil {
				return nil, err
			}
		}
	}
	return cells, nil
}

// addExtras writes the line ends and shadow of an object.add on the
// shape it added (changed[0]) through setStyle, so object.add and
// object.set share one write and one set of refusals. A connector
// added with kind connector has no arrowheads unless s gives them.
func addExtras(ws *edit.Workspace, s *ObjectAddSpec, changed []string, aliases map[string]string, units string) error {
	hasEnds := s.Line != nil && (s.Line.Begin != nil || s.Line.End != nil)
	if (!hasEnds && s.Shadow == nil) || len(changed) == 0 {
		return nil
	}
	set := &ObjectSetSpec{At: changed[0], Shadow: s.Shadow}
	if hasEnds {
		set.Line = &LineSpec{Begin: s.Line.Begin, End: s.Line.End}
	}
	_, _, err := setStyle(ws, set, aliases, units)
	return err
}
