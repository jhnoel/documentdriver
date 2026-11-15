package formula

import (
	"slices"
	"strings"
)

// Translate is the text of a shared-formula child: the master's text,
// written for cell from, with every relative row and column moved by the
// distance from from to to (absolute ones stay; a whole-column reference
// keeps its rows and a whole-row one its columns). Every reference moves,
// on any sheet or workbook. A coordinate pushed off the grid refuses
// (*Refusal): a file Excel wrote never needs it.
func Translate(text string, from, to Cell) (string, error) {
	dr, dc := to.Row-from.Row, to.Col-from.Col
	if dr == 0 && dc == 0 {
		return text, nil
	}
	toks := Lex(text)
	var b strings.Builder
	changed := false
	for i, t := range toks {
		out := t.Text
		if t.Kind == Ref {
			s, err := translateToken(t, dr, dc)
			if err != nil {
				return "", err
			}
			out = s
		}
		if out != t.Text && !changed {
			changed = true
			b.Grow(len(text) + 8)
			for _, p := range toks[:i] {
				b.WriteString(p.Text)
			}
		}
		if changed {
			b.WriteString(out)
		}
	}
	if !changed {
		return text, nil
	}
	return b.String(), nil
}

// translateToken returns the reference token moved by dr rows and dc columns, refusing if it leaves the sheet.
func translateToken(t Token, dr, dc int) (string, error) {
	pre, body, ok := SplitQualified(t.Text)
	if !ok {
		return t.Text, nil
	}
	r, isErr, ok := ParseRefBody(body)
	if !ok || isErr {
		return t.Text, nil
	}
	move := func(c *Cell) bool {
		if r.Kind != Cols && !c.RowAbs {
			c.Row += dr
		}
		if r.Kind != Rows && !c.ColAbs {
			c.Col += dc
		}
		return (r.Kind == Cols || c.Row >= 1 && c.Row <= MaxRows) &&
			(r.Kind == Rows || c.Col >= 1 && c.Col <= MaxCols)
	}
	n := r
	if !move(&n.From) || !move(&n.To) {
		return "", refuse(t.Pos, "reference %q leaves the sheet when moved by %d rows and %d columns", t.Text, dr, dc)
	}
	if r.Single {
		n.To = n.From
	}
	if n == r {
		return t.Text, nil
	}
	return pre.Text + n.String(), nil
}

// Group is a shared formula group as a worksheet holds it.
type Group struct {
	// Ref is the master's ref attribute: the cells the group may cover.
	Ref Range
	// Master is the master cell and Text its formula.
	Master Cell
	Text   string
	// Members are the cells that carry the group's si, master included;
	// nil means every cell of Ref.
	Members []Cell
}

// DecisionKind is what a structural edit does to a shared group.
type DecisionKind uint8

// The decisions.
const (
	// Keep: the group stays one shared group; Parts holds it with its
	// new ref, master and master text.
	Keep DecisionKind = iota
	// Split: the group becomes the groups in Parts (the caller gives each
	// a fresh si after the first) and the plain formulas in Children.
	Split
	// Unshare: every surviving member becomes a plain formula, in
	// Children.
	Unshare
)

// Part is one shared group after an edit, in post-edit coordinates. The
// master is the first member in row-major order, as Excel writes it
// (corpus groups have masters to the right of their ref's left column).
type Part struct {
	Ref     Range
	Master  Cell
	Text    string
	Members []Cell
}

// Child is one member written out as a plain formula, at its post-edit
// cell.
type Child struct {
	Cell Cell
	Text string
}

// Decision is DecideShared's answer.
type Decision struct {
	Kind     DecisionKind
	Parts    []Part
	Children []Child
}

// maxEnumerate bounds the cells DecideShared enumerates when Members is
// nil (a whole sheet's worth of rows by one column).
const maxEnumerate = MaxRows

// DecideShared says what edit e does to shared group g, by position (W1
// of the design's library survey): a group whose members, after the
// edit, still read their own shifted formulas when translated from the
// shifted master is kept, with the master's text and ref shifted; else it
// is split where the edit cuts it (the members before and after the edit
// point, each part keeping a shared formula if it reads true); else it is
// unshared. Members a delete removes are dropped. The law, checked by
// construction: every surviving member reads Shift(Translate(master,
// member)) after the edit, whatever the decision. sh is the group's
// sheet (Own) and the workbook's order. Errors are *Refusal: a member
// whose formula Shift refuses refuses the group.
func DecideShared(g Group, sh Sheets, e Edit) (Decision, error) {
	if err := e.check(); err != nil {
		return Decision{}, err
	}
	members, err := groupMembers(g)
	if err != nil {
		return Decision{}, err
	}
	g.Master = Cell{Row: g.Master.Row, Col: g.Master.Col}
	onSheet := sh.Own != "" && strings.EqualFold(sh.Own, e.Sheet)
	moved := func(c Cell) (Cell, bool) {
		if !onSheet {
			return c, true
		}
		r, gone, err := shiftRange(Range{Kind: Cells, From: c, To: c, Single: true}, e)
		if gone || err != nil {
			return c, false
		}
		return r.From, true
	}
	want := func(c Cell) (string, error) {
		t, err := Translate(g.Text, g.Master, c)
		if err != nil {
			return "", err
		}
		return Shift(t, sh, e)
	}

	// Keep: the master survives and every member reads true from it.
	if m2, ok := moved(g.Master); ok {
		t2, err := Shift(g.Text, sh, e)
		if err != nil {
			return Decision{}, err
		}
		if part, ok, err := consistent(members, m2, t2, moved, want); err != nil {
			return Decision{}, err
		} else if ok {
			part.Ref = g.Ref
			if onSheet {
				if r, gone, err := shiftRange(g.Ref, e); err == nil && !gone {
					part.Ref = r
				} else {
					part.Ref = bounds(part.Members)
				}
			}
			return Decision{Kind: Keep, Parts: []Part{part}}, nil
		}
	}

	// Split: the members on each side of the edit point.
	if onSheet {
		var before, after []Cell
		for _, c := range members {
			m2, ok := moved(c)
			if !ok {
				continue
			}
			if m2 == c {
				before = append(before, c)
			} else {
				after = append(after, c)
			}
		}
		var d Decision
		d.Kind = Split
		split := true
		for _, side := range [][]Cell{before, after} {
			if len(side) == 0 {
				continue
			}
			master := side[0]
			t, err := want(master)
			if err != nil {
				return Decision{}, err
			}
			m2, _ := moved(master)
			part, ok, err := consistent(side, m2, t, moved, want)
			if err != nil {
				return Decision{}, err
			}
			if !ok {
				split = false
				break
			}
			if len(side) == 1 {
				d.Children = append(d.Children, Child{Cell: m2, Text: t})
				continue
			}
			part.Ref = bounds(part.Members)
			d.Parts = append(d.Parts, part)
		}
		if split {
			return d, nil
		}
	}

	// Unshare.
	d := Decision{Kind: Unshare}
	for _, c := range members {
		c2, ok := moved(c)
		if !ok {
			continue
		}
		t, err := want(c)
		if err != nil {
			return Decision{}, err
		}
		d.Children = append(d.Children, Child{Cell: c2, Text: t})
	}
	return d, nil
}

// consistent reports whether every surviving member of members reads its
// wanted text when translated from master m2 with text t2, and returns
// the part (Ref unset).
func consistent(members []Cell, m2 Cell, t2 string, moved func(Cell) (Cell, bool),
	want func(Cell) (string, error)) (Part, bool, error) {
	part := Part{Master: m2, Text: t2}
	for _, c := range members {
		c2, ok := moved(c)
		if !ok {
			continue
		}
		w, err := want(c)
		if err != nil {
			return Part{}, false, err
		}
		got, err := Translate(t2, m2, c2)
		if err != nil || got != w {
			return Part{}, false, nil
		}
		part.Members = append(part.Members, c2)
	}
	return part, true, nil
}

// groupMembers lists the group's members in row-major order, master
// included.
func groupMembers(g Group) ([]Cell, error) {
	var out []Cell
	if g.Members != nil {
		out = slices.Clone(g.Members)
	} else {
		r1, c1, r2, c2 := g.Ref.Bounds()
		if (r2-r1+1)*(c2-c1+1) > maxEnumerate {
			return nil, refuse(0, "shared group %s too large to enumerate; pass its members", g.Ref)
		}
		for r := r1; r <= r2; r++ {
			for c := c1; c <= c2; c++ {
				out = append(out, Cell{Row: r, Col: c})
			}
		}
	}
	if !slices.ContainsFunc(out, func(c Cell) bool { return c.Row == g.Master.Row && c.Col == g.Master.Col }) {
		out = append(out, Cell{Row: g.Master.Row, Col: g.Master.Col})
	}
	for i := range out {
		out[i].RowAbs, out[i].ColAbs = false, false
	}
	slices.SortFunc(out, func(a, b Cell) int {
		if a.Row != b.Row {
			return a.Row - b.Row
		}
		return a.Col - b.Col
	})
	return slices.Compact(out), nil
}

// bounds is the smallest area holding cells (non-empty).
func bounds(cells []Cell) Range {
	r1, c1, r2, c2 := cells[0].Row, cells[0].Col, cells[0].Row, cells[0].Col
	for _, c := range cells[1:] {
		r1, r2 = min(r1, c.Row), max(r2, c.Row)
		c1, c2 = min(c1, c.Col), max(c2, c.Col)
	}
	r := Range{Kind: Cells, From: Cell{Row: r1, Col: c1}, To: Cell{Row: r2, Col: c2}}
	r.Single = r.From == r.To
	return r
}
