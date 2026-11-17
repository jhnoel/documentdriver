package model

import (
	"strconv"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// gluedOf handles one Connect row whose FromCell is not BeginX or EndX
// (a control handle such as Controls.Row_1, 48 in the corpus): it adds
// a Glued entry to the from-shape's object (read only) and a
// dangling-glue hazard when ToSheet names no shape. It reports false for
// an end Connect (BeginX, EndX), which linkConnects handles. Chunk M4b.
func gluedOf(p *Page, c *tree.Node, byID map[uint32]*placed) bool {
	if p == nil || c == nil {
		return false
	}
	cr := readConnect(c)
	fromID, toID, fromCell, toCell := cr.fromID, cr.toID, cr.fromCell, cr.toCell
	if fromCell == "" || fromCell == vsd.FromCellBegin || fromCell == vsd.FromCellEnd {
		return false
	}
	fromPl, found := byID[fromID]
	if !cr.fromOK || fromID == 0 || !found || fromPl == nil || fromPl.obj == nil {
		p.Notes = append(p.Notes, noShapeNote(p))
		return true
	}
	toPl, found := byID[toID]
	resolved := found && toPl != nil && toPl.obj != nil
	fromPl.obj.Glued = append(fromPl.obj.Glued, Glued{Cell: fromCell, To: vsd.Glue(toID, toCell, resolved)})
	if !resolved {
		p.Hazards = append(p.Hazards, Hazard{Kind: "dangling-glue", ID: fromPl.obj.ID,
			Detail: fromCell + " names shape " + strconv.FormatUint(uint64(toID), 10) + ", which is not on the page"})
	}
	return true
}

// connectRow is the attributes of one Connect element that the glue
// passes read: FromSheet (fromOK when it parses), ToSheet, FromCell and
// ToCell.
type connectRow struct {
	fromID, toID     uint32
	fromOK           bool
	fromCell, toCell string
}

// readConnect reads the attributes of Connect element c.
func readConnect(c *tree.Node) connectRow {
	var cr connectRow
	for name, value := range c.AttrsSeq() {
		switch name {
		case "FromSheet":
			if u, err := strconv.ParseUint(value, 10, 32); err == nil {
				cr.fromID, cr.fromOK = uint32(u), true
			}
		case "ToSheet":
			if u, err := strconv.ParseUint(value, 10, 32); err == nil {
				cr.toID = uint32(u)
			}
		case "FromCell":
			cr.fromCell = value
		case "ToCell":
			cr.toCell = value
		}
	}
	return cr
}

// noShapeNote is the note of a Connect whose FromSheet names no shape
// on the page.
func noShapeNote(p *Page) doc.Note {
	return doc.Note{At: p.ID, Reason: "connect from sheet names no shape"}
}

// connectNotes adds to p, in document order, the notes linkConnects
// and gluedOf give for the Connect rows of connects (nil when none)
// without building any glue: a row with a FromCell whose FromSheet does
// not parse, is 0, or names a shape for which has reports false gets
// "connect from sheet names no shape". It is the notes pass of a walk
// that keeps no placed map (VisitPage, chunk RM2); has reports whether
// the walk built an object for a stored shape ID.
func connectNotes(p *Page, connects *tree.Node, has func(id uint32) bool) {
	if p == nil || connects == nil {
		return
	}
	for c := range connects.ElementsSeq() {
		if c.Local() != vsd.ElemConnect {
			continue
		}
		cr := readConnect(c)
		if cr.fromCell == "" {
			continue
		}
		if !cr.fromOK || cr.fromID == 0 || !has(cr.fromID) {
			p.Notes = append(p.Notes, noShapeNote(p))
		}
	}
}
