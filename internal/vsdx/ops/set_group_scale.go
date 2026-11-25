package ops

import (
	"strconv"

	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// groupScaleCells are the cells of a group's child that Visio's Group
// command binds to the group's size.
var groupScaleCells = []string{vsd.CellPinX, vsd.CellPinY, vsd.CellWidth, vsd.CellHeight, vsd.CellBeginX, vsd.CellBeginY, vsd.CellEndX, vsd.CellEndY}

// scaleGroupChildren evaluates, after s.Box resized the group at
// s.At, the children cells whose own formula is Visio's
// Sheet.<group>!Width*k or Sheet.<group>!Height*k (groupScaleFormula):
// each gets the group's new Width or Height times k, and the child's
// dependents are recomputed (RecomputeShape); a child that is itself a
// group scales its children in turn. A cell with any other formula
// keeps its value, as the contract's group rule says. It returns the
// children it changed and RecomputeShape's stale cells.
func scaleGroupChildren(ws *edit.Workspace, s *ObjectSetSpec, aliases map[string]string) ([]string, []string, error) {
	if s.Box == nil {
		return nil, nil, nil
	}
	addr, err := resolveShape(s.At, aliases)
	if err != nil {
		return nil, nil, err
	}
	return scaleGroup(ws, addr, 0)
}

// scaleGroup scales the children of the group at addr (scaleGroupChildren),
// depth levels below the resized group.
func scaleGroup(ws *edit.Workspace, addr string, depth int) ([]string, []string, error) {
	if depth > 64 {
		return nil, nil, nil
	}
	a, err := vsd.Parse(addr)
	if err != nil {
		return nil, nil, nil
	}
	st, err := loadStyleShape(ws, a, addr)
	if err != nil {
		return nil, nil, nil
	}
	if typ, _ := st.node.Attr("Type"); typ != vsd.ShapeTypeGroup {
		return nil, nil, nil
	}
	gw, okW := styleNumberCell(st, vsd.CellWidth)
	gh, okH := styleNumberCell(st, vsd.CellHeight)
	if !okW || !okH {
		return nil, nil, nil
	}
	pageAddr := vsd.PageID(a.Container, a.PageDup)
	var changed, stale []string
	for shapes := range st.node.ElementsSeq() {
		if shapes.Local() != vsd.ElemShapes {
			continue
		}
		for c := range shapes.ElementsSeq() {
			if c.Local() != vsd.ElemShape {
				continue
			}
			idText, _ := c.Attr("ID")
			id, err := strconv.ParseUint(idText, 10, 32)
			if err != nil {
				continue
			}
			child := vsd.ShapeID(pageAddr, uint32(id), 1) //nolint:gosec // G115: parsed with bitSize 32
			cshape := textShapeOf(c)
			var cells []edit.Cell
			var names []string
			for _, name := range groupScaleCells {
				cell, origin, ok := st.res.Cell(cshape, sheet.ShapeCell(name))
				if !ok || origin != inherit.Local {
					continue
				}
				side, k, ok := groupScaleFormula(cell.F, a.Shape)
				if !ok {
					continue
				}
				v := gw * k
				if side == "Height" {
					v = gh * k
				}
				if vsd.FormatInches(v) == cell.V {
					continue
				}
				cells = append(cells, edit.Cell{N: name, V: vsd.FormatInches(v), U: cell.U, F: cell.F})
				names = append(names, name)
			}
			if len(cells) == 0 {
				continue
			}
			if err := ws.SetCells(child, cells); err != nil {
				return nil, nil, moveEditError(err)
			}
			st2, err := ws.RecomputeShape(child, names)
			if err != nil {
				return nil, nil, moveEditError(err)
			}
			stale = append(stale, st2...)
			changed = append(changed, child)
			if typ, _ := c.Attr("Type"); typ == vsd.ShapeTypeGroup {
				more, moreStale, err := scaleGroup(ws, child, depth+1)
				if err != nil {
					return nil, nil, err
				}
				changed = append(changed, more...)
				stale = append(stale, moreStale...)
			}
		}
	}
	return changed, stale, nil
}
