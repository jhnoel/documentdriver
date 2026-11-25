package ops

import (
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ascii"
	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/ids"
	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/formula"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// minGroupInches is the smallest side a new group is given (a group of
// shapes that all lie on one line would otherwise have no width or
// height to scale its children by).
const minGroupInches = 0.01

// groupMember is one shape group.create wraps: its address, its loaded
// sheet and its transform in the container's space.
type groupMember struct {
	addr string
	st   *styleShape
	oneD bool
}

// Run implements group.create: it wraps the shapes at At (resolved
// through aliases), all of one container, in a new group, as Visio's
// Group command writes one: a Shape of Type Group with LineStyle,
// FillStyle and TextStyle 3, its PinX and PinY at the centre of the
// shapes' bounding box in the container (the corners of each 2-D
// shape's box and each 1-D shape's ends), Width and Height that box's
// size (at least minGroupInches), LocPinX Width*0.5 and LocPinY
// Height*0.5, no rotation or flip, ResizeMode 0, no geometry. The
// group stands where the frontmost shape stood; the shapes move into
// it in paint order and keep their IDs. Each shape's place is
// rewritten into the group's space: a 2-D shape's PinX, PinY, Width
// and Height, a 1-D shape's BeginX, BeginY, EndX and EndY, with
// Visio's formulas Sheet.<group>!Width*k and Sheet.<group>!Height*k
// (so resizing the group scales them, in Visio and in object.set),
// except a cell whose formula vsdx does not evaluate (a glued end, a
// connector's (BeginX+EndX)/2 pin), whose value is moved and formula
// kept. Changed: the group, then the shapes. The alias names the
// group.
func (s *GroupCreateSpec) Run(ws *edit.Workspace, aliases map[string]string, _ string, _ ids.Source) (string, []string, []string, string, error) {
	var addrs []string
	for i, raw := range s.At {
		field := "at[" + strconv.Itoa(i) + "]"
		addr, err := ResolveAlias(raw, aliases, field)
		if err != nil {
			return "", nil, nil, "", err
		}
		if err := targetRefusal(ws, addr, field, true); err != nil {
			return "", nil, nil, "", err
		}
		if err := inheritedSubShapeRefusal(ws, addr, field); err != nil {
			return "", nil, nil, "", err
		}
		addrs = append(addrs, addr)
	}
	d := ws.Doc()
	if d.Kind == doc.Stencil {
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s is in a stencil: stencils cannot be edited", addrs[0])
	}
	first, err := vsd.Parse(addrs[0])
	if err != nil {
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "at[0]", "%s is not a shape address", addrs[0])
	}
	pageAddr := vsd.PageID(first.Container, first.PageDup)
	r := inherit.New(d)
	if err := scaledRefusal(r, d, first.Container, first.PageDup, pageAddr, "at"); err != nil {
		return "", nil, nil, "", err
	}
	pg, err := addLoadPage(ws, r, first.Container, first.PageDup)
	if err != nil {
		return "", nil, nil, "", err
	}
	wanted := map[string]bool{}
	for _, a := range addrs {
		wanted[a] = true
	}
	located := addLocate(pg.root, pageAddr, wanted, r)
	var members []groupMember
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for i, addr := range addrs {
		field := "at[" + strconv.Itoa(i) + "]"
		a, err := vsd.Parse(addr)
		if err != nil || a.Container != first.Container || a.PageDup != first.PageDup {
			return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, field, "%s is not on %s: group shapes of one container", addr, pageAddr)
		}
		loc, ok := located[addr]
		if !ok {
			return "", nil, nil, "", cli.Errorf(cli.CodeNotFound, field, "no %s: not found", addr)
		}
		if !loc.ok {
			return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, field, "%s: its transform is not derived, so it cannot be grouped", addr)
		}
		st, err := loadShapeSheet(ws, a, addr)
		if err != nil {
			return "", nil, nil, "", err
		}
		m := groupMember{addr: addr, st: st}
		if _, ok := styleNumberCell(st, vsd.CellBeginX); ok {
			m.oneD = true
		}
		members = append(members, m)
		t := loc.t
		local := t.Local()
		for _, c := range [4]geom.Pt{{}, {X: t.Width}, {X: t.Width, Y: t.Height}, {Y: t.Height}} {
			p := local.Apply(c)
			x0, y0, x1, y1 = min(x0, p.X), min(y0, p.Y), max(x1, p.X), max(y1, p.Y)
		}
	}
	if x1-x0 < minGroupInches {
		mid := (x0 + x1) / 2
		x0, x1 = mid-minGroupInches/2, mid+minGroupInches/2
	}
	if y1-y0 < minGroupInches {
		mid := (y0 + y1) / 2
		y0, y1 = mid-minGroupInches/2, mid+minGroupInches/2
	}
	w, h := x1-x0, y1-y0
	gid, err := ws.NextShapeID(pageAddr)
	if err != nil {
		return "", nil, nil, "", err
	}
	group, err := ws.GroupShapes(addrs, groupFragment(s.Name, x0+w/2, y0+h/2, w, h))
	if err != nil {
		if errors.Is(err, edit.ErrNotFound) {
			return "", nil, nil, "", cli.Errorf(cli.CodeNotFound, "at", "%s", err)
		}
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s", err)
	}
	if ga, err := vsd.Parse(group); err != nil || ga.Shape != gid {
		return "", nil, nil, "", cli.Errorf(cli.CodeInternal, "", "group.create: the group is %s, want ID %d", group, gid)
	}
	changed := []string{group}
	sheetRef := "Sheet." + strconv.FormatUint(uint64(gid), 10) + "!"
	for _, m := range members {
		cells := groupMemberCells(m, x0, y0, w, h, sheetRef)
		if err := ws.SetCells(m.addr, cells); err != nil {
			return "", nil, nil, "", moveEditError(err)
		}
		changed = append(changed, m.addr)
	}
	return group, changed, nil, "", nil
}

// groupFragment is the new group's Shape element, without its ID:
// Visio's cells for a group whose centre is (pinX, pinY) in its
// container, w by h inches, and an empty Shapes.
func groupFragment(name *string, pinX, pinY, w, h float64) []byte {
	var b strings.Builder
	b.WriteString("<Shape")
	if name != nil && *name != "" {
		n := xmlscan.EscapeAttr(*name, '\'')
		b.WriteString(" NameU='" + n + "' Name='" + n + "'")
	}
	b.WriteString(" Type='" + vsd.ShapeTypeGroup + "' LineStyle='3' FillStyle='3' TextStyle='3'>")
	cell := func(n, v, f string) {
		b.WriteString("<Cell N='" + n + "' V='" + v + "'")
		if f != "" {
			b.WriteString(" F='" + f + "'")
		}
		b.WriteString("/>")
	}
	cell(vsd.CellPinX, vsd.FormatInches(pinX), "")
	cell(vsd.CellPinY, vsd.FormatInches(pinY), "")
	cell(vsd.CellWidth, vsd.FormatInches(w), "")
	cell(vsd.CellHeight, vsd.FormatInches(h), "")
	cell(vsd.CellLocPinX, vsd.FormatInches(w/2), "Width*0.5")
	cell(vsd.CellLocPinY, vsd.FormatInches(h/2), "Height*0.5")
	cell(vsd.CellAngle, "0", "")
	cell(vsd.CellFlipX, "0", "")
	cell(vsd.CellFlipY, "0", "")
	cell("ResizeMode", "0", "")
	b.WriteString("<Shapes/></Shape>")
	return []byte(b.String())
}

// groupMemberCells returns the cells that put member m in the group
// whose lower-left corner is (x0, y0) in the container and whose size
// is w by h inches (sheetRef "Sheet.<id>!"): each place cell moved by
// the corner, with Visio's Sheet.<id>!Width*k or Height*k formula
// where the cell's formula is a constant (or none), else its value
// moved and its formula kept (an inherited one as F="Inh").
func groupMemberCells(m groupMember, x0, y0, w, h float64, sheetRef string) []edit.Cell {
	type place struct {
		name  string
		shift float64
		side  string
		size  float64
	}
	var places []place
	if m.oneD {
		places = []place{
			{vsd.CellBeginX, x0, "Width", w}, {vsd.CellBeginY, y0, "Height", h},
			{vsd.CellEndX, x0, "Width", w}, {vsd.CellEndY, y0, "Height", h},
			{vsd.CellPinX, x0, "", 0}, {vsd.CellPinY, y0, "", 0},
		}
	} else {
		places = []place{
			{vsd.CellPinX, x0, "Width", w}, {vsd.CellPinY, y0, "Height", h},
			{vsd.CellWidth, 0, "Width", w}, {vsd.CellHeight, 0, "Height", h},
		}
	}
	var cells []edit.Cell
	for _, p := range places {
		v, ok := styleNumberCell(m.st, p.name)
		if !ok {
			continue
		}
		nv := v - p.shift
		f, origin := textCellFormula(m.st, p.name)
		c := edit.Cell{N: p.name, V: vsd.FormatInches(nv)}
		if u, ok := storedUnit(m.st, p.name); ok {
			c.U = u
		}
		switch {
		case p.side != "" && (f == "" || formula.Allowed(f)) && p.size > 0:
			c.F = sheetRef + p.side + "*" + strconv.FormatFloat(nv/p.size, 'g', 12, 64)
		case f == "" || formula.Allowed(f):
		case origin == inherit.Local:
			c.F = f
		default:
			c.F = vsd.FormulaInherited
		}
		cells = append(cells, c)
	}
	return cells
}

// storedUnit returns the U of the effective cell name of st, when it
// has one.
func storedUnit(st *styleShape, name string) (string, bool) {
	c, _, ok := st.res.Cell(st.shape, sheet.ShapeCell(name))
	if !ok || c.U == "" {
		return "", false
	}
	return c.U, true
}

// groupScaleFormula parses Visio's group-relative formula
// Sheet.<id>!Width*k or Sheet.<id>!Height*k (spaces and case as Visio
// reads them): the side ("Width" or "Height") and k, for a group of
// stored ID id.
func groupScaleFormula(f string, id uint32) (string, float64, bool) {
	s := ascii.Lower(strings.ReplaceAll(f, " ", ""))
	prefix := "sheet." + strconv.FormatUint(uint64(id), 10) + "!"
	rest, ok := strings.CutPrefix(s, prefix)
	if !ok {
		return "", 0, false
	}
	for _, side := range []string{"width", "height"} {
		if num, ok := strings.CutPrefix(rest, side+"*"); ok {
			k, err := strconv.ParseFloat(num, 64)
			if err != nil || math.IsNaN(k) || math.IsInf(k, 0) {
				return "", 0, false
			}
			if side == "width" {
				return "Width", k, true
			}
			return "Height", k, true
		}
	}
	return "", 0, false
}
