package model

import (
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
)

// CellKey spells a sheet key as --detail full shows it, in ShapeSheet
// reference syntax where Visio has one: "PinX"; named rows
// "<prefix>.<row>.<cell>" with Visio's section prefixes (Prop, User,
// Hyperlink, Actions, Controls, ...): "Prop.Cost.Value",
// "Controls.Row_1.X"; Geometry "Geometry1.X2" (section IX+1, row IX);
// Connection "Connections.X1" (row IX 0); indexed text rows with a
// 1-based index: "Char.Size[1]", "Para.HorzAlign[1]", "Tabs.Position[1]",
// "Fields.Value[1]"; layers, which formulas cannot name,
// "Layer[1].Name". The table of prefixes is designer-owned (chunk M7).
func CellKey(k sheet.Key) string {
	_ = k
	return ""
}

// rawCells returns the shape's own stored cells in stored order with
// their keys. Chunk M7.
func rawCells(s *sheet.Sheet) []RawCell {
	_ = s
	return nil
}

// boxOrigin is the origin of a property shown from several cells: the
// most inherited of theirs, in the order local < inherited < master <
// style. Chunk M7.
func boxOrigin(origins ...inherit.Origin) inherit.Origin {
	_ = origins
	return ""
}
