package ops

import (
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// fillCells returns the cells a fill that is not "none" stores
// (docs/contract.md §3, Pattern fills), in Visio's order: FillForegnd
// hex, then FillBkgnd hex with FillBkgndTrans 0 or, for a clear
// background, FillBkgndTrans 1, then FillPattern (1 for a solid fill,
// the pattern number otherwise). A solid fill writes no background.
func fillCells(f *PaintSpec, hex string) []edit.Cell {
	if f.Pattern == "" {
		return []edit.Cell{{N: vsd.CellFillForegnd, V: hex}, {N: vsd.CellFillPattern, V: "1"}}
	}
	cells := []edit.Cell{{N: vsd.CellFillForegnd, V: hex}}
	if f.BgNone {
		cells = append(cells, edit.Cell{N: vsd.CellFillBkgndTrans, V: "1"})
	} else {
		cells = append(cells, edit.Cell{N: vsd.CellFillBkgnd, V: f.BgHex}, edit.Cell{N: vsd.CellFillBkgndTrans, V: "0"})
	}
	return append(cells, edit.Cell{N: vsd.CellFillPattern, V: f.Pattern})
}

// styleFillMatches reports whether st already shows the fill f with
// colour hex, as read shows it: the same foreground and FillPattern
// (1 for solid), and for a pattern the same background (clear when
// the effective FillBkgndTrans is at least 1, else the effective
// FillBkgnd with no transparency).
func styleFillMatches(st *styleShape, f *PaintSpec, hex string) bool {
	cur, derived := styleFillHex(st)
	if !derived || cur != hex {
		return false
	}
	want := 1.0
	if f.Pattern != "" {
		n, _ := vsd.Number(f.Pattern)
		want = n
	}
	if got, ok := styleNumberCell(st, vsd.CellFillPattern); !ok || got != want {
		return false
	}
	if f.Pattern == "" {
		return true
	}
	trans, hasTrans := styleNumberCell(st, vsd.CellFillBkgndTrans)
	if !hasTrans {
		trans = 0
	}
	if f.BgNone {
		return trans >= 1
	}
	bg, ok := styleColorCell(st, vsd.CellFillBkgnd)
	return ok && trans == 0 && bg == f.BgHex
}
