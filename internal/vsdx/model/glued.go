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
	var fromID, toID uint32
	var fromOK bool
	fromCell, toCell := "", ""
	for name, value := range c.AttrsSeq() {
		switch name {
		case "FromSheet":
			if u, err := strconv.ParseUint(value, 10, 32); err == nil {
				fromID, fromOK = uint32(u), true
			}
		case "ToSheet":
			if u, err := strconv.ParseUint(value, 10, 32); err == nil {
				toID = uint32(u)
			}
		case "FromCell":
			fromCell = value
		case "ToCell":
			toCell = value
		}
	}
	if fromCell == "" || fromCell == vsd.FromCellBegin || fromCell == vsd.FromCellEnd {
		return false
	}
	fromPl, found := byID[fromID]
	if !fromOK || fromID == 0 || !found || fromPl == nil || fromPl.obj == nil {
		p.Notes = append(p.Notes, doc.Note{At: p.ID, Reason: "connect from sheet names no shape"})
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
