package ops

import (
	"math"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/geom"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/formula"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// styleShape is what setStyle reads of its target before writing: the
// shape's address in the page, the page part that holds it, its node in
// the working tree, and its resolver shape for effective cells.
type styleShape struct {
	addr  string
	part  string
	node  *tree.Node
	res   *inherit.Resolver
	shape *inherit.Shape
}

// setStyle applies the style fields of s (fill, line and name) to the
// shape at s.At, which it resolves through aliases, and returns the
// changed address when it wrote anything. Fill "none" writes
// FillPattern 0, a hex writes FillForegnd with FillPattern 1 and a
// pattern fill its cells (fillCells); line
// "none" writes LinePattern 0 and a line writes LineWeight in inches
// for w (in the plan's units), LineColor for color and LinePattern for
// dash, adding LinePattern 1 when a weight or colour would otherwise
// stay hidden under a none line; name writes Name and NameU. A cell
// whose effective formula is outside formula.Allowed is refused with
// operation_failed at at, naming the cell and formula, before anything
// is written, and a field whose effective value already equals the
// wanted one writes nothing. Foreign, guide, master and stencil
// shapes are operation_failed at at, a missing shape is not_found at
// at, and an unreadable page part is unreadable_target.
func setStyle(ws *edit.Workspace, s *ObjectSetSpec, aliases map[string]string, units string) (changed []string, stale string, err error) {
	if s.Fill == nil && s.Line == nil && s.Name == nil && s.Shadow == nil {
		return nil, "", nil
	}
	addr, err := resolveShape(s.At, aliases)
	if err != nil {
		return nil, "", err
	}
	scale := 1.0
	if units == "in" {
		scale = vsd.PointsPerInch
	}
	hex, err := styleFillHexSpec(s)
	if err != nil {
		return nil, "", err
	}
	color, err := styleLineSpec(s)
	if err != nil {
		return nil, "", err
	}
	a, err := vsd.Parse(addr)
	if err != nil {
		return nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s is not a shape address", addr)
	}
	st, err := loadStyleShape(ws, a, addr)
	if err != nil {
		return nil, "", err
	}
	if err := shapeRefusal(st.res, st.shape, addr, "at", false); err != nil {
		return nil, "", err
	}
	cells, err := styleCells(st, s, hex, color, scale)
	if err != nil {
		return nil, "", err
	}
	ends, err := lineEndCells(st, s)
	if err != nil {
		return nil, "", err
	}
	shadow, err := shadowCells(st, s, scale)
	if err != nil {
		return nil, "", err
	}
	cells = append(append(cells, ends...), shadow...)
	curName, _ := st.node.Attr("Name")
	nameDone := s.Name == nil || curName == *s.Name
	if len(cells) == 0 && nameDone {
		return nil, "", nil
	}
	if len(cells) > 0 {
		if err := ws.SetCells(addr, cells); err != nil {
			return nil, "", err
		}
	}
	if !nameDone {
		if err := styleSetName(ws, st, *s.Name); err != nil {
			return nil, "", err
		}
	}
	return []string{addr}, "", nil
}

// styleFillHexSpec validates and normalises the fill of s: "" when
// s has no fill or fill none, otherwise the upper-case hex, with an
// operation_failed fault at fill for a colour that is not #RRGGBB.
func styleFillHexSpec(s *ObjectSetSpec) (string, error) {
	if s.Fill == nil || s.Fill.None {
		return "", nil
	}
	hex, ok := vsd.Color(s.Fill.Hex)
	if !ok {
		return "", cli.Errorf(cli.CodeOperationFailed, "fill", "hex %q is not #RRGGBB", s.Fill.Hex)
	}
	return hex, nil
}

// styleLineSpec validates and normalises the line of s and returns its
// colour in upper case, or "" when it has none: an operation_failed
// fault at line for a weight that is not finite or is negative, or a
// colour that is not #RRGGBB.
func styleLineSpec(s *ObjectSetSpec) (string, error) {
	if s.Line == nil || s.Line.None {
		return "", nil
	}
	if s.Line.W != nil {
		w := *s.Line.W
		if math.IsNaN(w) || math.IsInf(w, 0) || w < 0 {
			return "", cli.Errorf(cli.CodeOperationFailed, "line", "w %v is not a finite non-negative number", w)
		}
	}
	if s.Line.Color == "" {
		return "", nil
	}
	hex, ok := vsd.Color(s.Line.Color)
	if !ok {
		return "", cli.Errorf(cli.CodeOperationFailed, "line", "color %q is not #RRGGBB", s.Line.Color)
	}
	return hex, nil
}

// loadStyleShape reads the target shape a of address addr from the
// working tree of its page: the shape node from the core's tree (so a
// prior op's writes are visible), its page part, and its resolver
// shape, refusing stencils, Foreign and guide shapes, and reporting a
// missing page or shape as not_found at at and an unreadable page part
// as unreadable_target.
func loadStyleShape(ws *edit.Workspace, a vsd.Address, addr string) (*styleShape, error) {
	d, err := doc.Open(ws.Package())
	if err != nil {
		return nil, err
	}
	if d.Kind == doc.Stencil {
		return nil, cli.Errorf(cli.CodeOperationFailed, "at", "%s is in a stencil: stencils cannot be edited", addr)
	}
	var entry doc.PageEntry
	found := false
	depth := 0
	for _, p := range d.Pages {
		if p.ID != a.Container {
			continue
		}
		depth++
		if depth == a.PageDup {
			entry, found = p, true
			break
		}
	}
	if !found {
		return nil, cli.Errorf(cli.CodeNotFound, "at", "no %s: not found", vsd.PageID(a.Container, a.PageDup))
	}
	if entry.Part == "" {
		return nil, &edit.Unreadable{Target: vsd.PageID(entry.ID, depth), Detail: "it has no part"}
	}
	working, err := ws.Core().Tree(entry.Part)
	if err != nil {
		return nil, &edit.Unreadable{Target: vsd.PageID(entry.ID, depth), Detail: err.Error()}
	}
	root := working.Root()
	if root == nil || !vsd.IsMain(root.Space()) || root.Local() != vsd.ElemPageContents {
		return nil, &edit.Unreadable{Target: vsd.PageID(entry.ID, depth), Detail: "it is not a " + vsd.ElemPageContents + " part"}
	}
	node := findTextShape(root, a.Shape, a.Dup)
	if node == nil {
		return nil, cli.Errorf(cli.CodeNotFound, "at", "no %s: not found", addr)
	}
	if typ, _ := node.Attr("Type"); typ == vsd.ShapeTypeForeign || typ == vsd.ShapeTypeGuide {
		return nil, cli.Errorf(cli.CodeOperationFailed, "at", "%s is a %s shape: style cannot be set", addr, typ)
	}
	return &styleShape{addr: addr, part: entry.Part, node: node, res: inherit.New(d), shape: textShapeOf(node)}, nil
}

// styleCells builds the cells setStyle writes for the fill and line
// of s on st, in Visio order, skipping a field whose effective value
// already equals the wanted one, and reports an operation_failed
// fault when a cell to be written is outside formula.Allowed, in the
// formula refusal form at fill or line.
func styleCells(st *styleShape, s *ObjectSetSpec, hex, color string, scale float64) ([]edit.Cell, error) {
	var cells []edit.Cell
	if s.Fill != nil {
		if s.Fill.None {
			if !stylePatternIs(st, vsd.CellFillPattern, 0) {
				if c, ok := styleRefused(st, vsd.CellFillPattern); ok {
					return nil, formulaRefusal(st.addr, "fill", []formulaCell{c})
				}
				cells = append(cells, edit.Cell{N: vsd.CellFillPattern, V: "0"})
			}
		} else if !styleFillMatches(st, s.Fill, hex) {
			fill := fillCells(s.Fill, hex)
			for _, c := range fill {
				if fc, ok := styleRefused(st, c.N); ok {
					return nil, formulaRefusal(st.addr, "fill", []formulaCell{fc})
				}
			}
			cells = append(cells, fill...)
		}
	}
	if s.Line == nil {
		return cells, nil
	}
	if s.Line.None {
		if !stylePatternIs(st, vsd.CellLinePattern, 0) {
			if c, ok := styleRefused(st, vsd.CellLinePattern); ok {
				return nil, formulaRefusal(st.addr, "line", []formulaCell{c})
			}
			cells = append(cells, edit.Cell{N: vsd.CellLinePattern, V: "0"})
		}
		return cells, nil
	}
	return styleLineCells(st, s, color, scale, cells)
}

// styleLineCells builds the cells for a non-none line of s appended to
// cells: the present w, color and dash, with LinePattern 1 when a
// weight or colour would stay hidden under a none or themed pattern.
func styleLineCells(st *styleShape, s *ObjectSetSpec, color string, scale float64, cells []edit.Cell) ([]edit.Cell, error) {
	if s.Line.W == nil && color == "" && s.Line.Dash == "" {
		return cells, nil
	}
	none := stylePatternIs(st, vsd.CellLinePattern, 0)
	themed := stylePatternThemed(st, vsd.CellLinePattern)
	if !none && !themed && styleLineMatches(st, s, color, scale) {
		return cells, nil
	}
	if color != "" {
		if c, ok := styleRefused(st, vsd.CellLineColor); ok {
			return nil, formulaRefusal(st.addr, "line", []formulaCell{c})
		}
		cells = append(cells, edit.Cell{N: vsd.CellLineColor, V: color})
	}
	if s.Line.W != nil {
		if c, ok := styleRefused(st, vsd.CellLineWeight); ok {
			return nil, formulaRefusal(st.addr, "line", []formulaCell{c})
		}
		cells = append(cells, edit.Cell{N: vsd.CellLineWeight, V: vsd.FormatInches(*s.Line.W * scale / vsd.PointsPerInch)})
	}
	switch {
	case s.Line.Dash != "":
		if c, ok := styleRefused(st, vsd.CellLinePattern); ok {
			return nil, formulaRefusal(st.addr, "line", []formulaCell{c})
		}
		cells = append(cells, edit.Cell{N: vsd.CellLinePattern, V: s.Line.Dash})
	case (none || themed) && (s.Line.W != nil || color != ""):
		if c, ok := styleRefused(st, vsd.CellLinePattern); ok {
			return nil, formulaRefusal(st.addr, "line", []formulaCell{c})
		}
		cells = append(cells, edit.Cell{N: vsd.CellLinePattern, V: "1"})
	}
	return cells, nil
}

// styleLineMatches reports whether every present field of the line of
// s already equals the effective line of st.
func styleLineMatches(st *styleShape, s *ObjectSetSpec, color string, scale float64) bool {
	if s.Line.W != nil {
		got, ok := styleNumberCell(st, vsd.CellLineWeight)
		if !ok || geom.FormatNum(got*vsd.PointsPerInch) != geom.FormatNum(*s.Line.W*scale) {
			return false
		}
	}
	if color != "" {
		got, ok := styleColorCell(st, vsd.CellLineColor)
		if !ok || got != color {
			return false
		}
	}
	if s.Line.Dash != "" {
		got, ok := styleStoredCell(st, vsd.CellLinePattern)
		if !ok || got != s.Line.Dash {
			return false
		}
	}
	return true
}

// stylePatternIs reports whether the effective cell name of st is the
// number want (a stored 0 for a none fill or line).
func stylePatternIs(st *styleShape, name string, want float64) bool {
	got, ok := styleNumberCell(st, name)
	return ok && got == want
}

// stylePatternThemed reports whether the effective cell name of st
// stores the themed value.
func stylePatternThemed(st *styleShape, name string) bool {
	got, ok := styleStoredCell(st, name)
	return ok && got == vsd.ValueThemed
}

// styleFillHex returns the effective fill hex of st as read shows it,
// and false when the fill is none, themed or unresolvable.
func styleFillHex(st *styleShape) (string, bool) {
	if stylePatternIs(st, vsd.CellFillPattern, 0) || stylePatternThemed(st, vsd.CellFillPattern) {
		return "", false
	}
	if _, ok := styleStoredCell(st, vsd.CellFillPattern); ok {
		if _, number := styleNumberCell(st, vsd.CellFillPattern); !number {
			return "", false
		}
	}
	return styleColorCell(st, vsd.CellFillForegnd)
}

// styleNumberCell returns the effective number of cell name of st,
// and false when it is absent, themed or not a number.
func styleNumberCell(st *styleShape, name string) (float64, bool) {
	cell, _, ok := st.res.Cell(st.shape, sheet.ShapeCell(name))
	if !ok || cell.V == vsd.ValueThemed {
		return 0, false
	}
	return vsd.Number(cell.V)
}

// styleStoredCell returns the effective stored V of cell name of st,
// and false when no sheet stores it.
func styleStoredCell(st *styleShape, name string) (string, bool) {
	cell, _, ok := st.res.Cell(st.shape, sheet.ShapeCell(name))
	if !ok {
		return "", false
	}
	return cell.V, true
}

// styleColorCell returns the effective colour of cell name of st in
// upper-case hex, and false when it is absent, themed or unresolvable.
func styleColorCell(st *styleShape, name string) (string, bool) {
	v, ok := styleStoredCell(st, name)
	if !ok || v == vsd.ValueThemed {
		return "", false
	}
	return st.res.Doc().Color(v)
}

// styleRefused reports whether the effective cell name of st may
// not be written: its formula is outside formula.Allowed. It
// returns the cell for the formula refusal form.
func styleRefused(st *styleShape, name string) (formulaCell, bool) {
	cell, _, ok := st.res.Cell(st.shape, sheet.ShapeCell(name))
	if !ok || formula.Allowed(cell.F) {
		return formulaCell{}, false
	}
	return formulaCell{name: name, formula: cell.F}, true
}

// styleSetName writes name as the Name and NameU attributes of st's
// shape, removing both when name is empty.
func styleSetName(ws *edit.Workspace, st *styleShape, name string) error {
	h := ws.Core().Target(st.part, st.addr, []*tree.Node{st.node})
	if name == "" {
		if err := h.RemoveAttr(core.Path{}, "NameU"); err != nil {
			return err
		}
		return h.RemoveAttr(core.Path{}, "Name")
	}
	if err := h.SetAttr(core.Path{}, "NameU", name); err != nil {
		return err
	}
	return h.SetAttr(core.Path{}, "Name", name)
}
