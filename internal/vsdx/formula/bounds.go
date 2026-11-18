package formula

import (
	"math"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ascii"
)

// CellBounds reports the bounds Visio applies to a value it computes
// for the cell cell: a lower bound and an upper bound in internal
// units, and ok false when no bound is evidenced for that cell.
// The table has one entry, the Character section's Size cell (the
// "Size" cell, spelled in a reference as Char.Size[1] and the like):
// lo is 1/72 in (1 pt), the minimum font size Visio shows, and hi
// is +Inf, as no upper bound is evidenced. The evidence is the four
// corpus Char.Size cells with F="GUARD(0)" that cache 1 pt
// (0.013888... in) while GUARD(0) is 0: libvisio testfile1 master
// master5/s7, testfile3 master5/s7, testfile4 master2/s7 and
// testfile6 master12/s7, each Char.Size[1]. Names match
// case-insensitively, as Visio reads them, and a bare Size also
// matches, as that is the cell's name in its section.
func CellBounds(cell string) (lo, hi float64, ok bool) {
	name := strings.TrimSpace(cell)
	prefix, base, hasDot := strings.Cut(name, ".")
	if !hasDot {
		base = name
	} else if idx := strings.LastIndex(name, "."); idx >= 0 {
		prefix, base = name[:idx], name[idx+1:]
	}
	if idx := strings.Index(base, "["); idx >= 0 {
		base = base[:idx]
	}
	if !ascii.EqualFold(base, "Size") {
		return 0, 0, false
	}
	if hasDot && !ascii.EqualFold(prefix, "Char") && !ascii.EqualFold(prefix, "Character") {
		return 0, 0, false
	}
	return 1.0 / 72.0, math.Inf(1), true
}

// Clamp returns v clamped to the bounds of CellBounds for the cell
// cell, or v unchanged when that cell has no bounds. It is the one
// helper every computed value for a cell passes through (the FL4
// corpus sweep and the dependent rewrites), so a value below a
// cell's minimum, such as a font size under 1 pt, is stored as the
// size Visio shows.
func Clamp(cell string, v float64) float64 {
	lo, hi, ok := CellBounds(cell)
	if !ok {
		return v
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
