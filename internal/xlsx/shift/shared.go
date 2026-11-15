package shift

import (
	"strings"

	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
)

// SharedGroup is one shared-formula group of the edited sheet before the
// edit: its si, master cell, master text and ref, and its members
// (master included), in document order.
type SharedGroup struct {
	SI      int
	Master  formula.Cell
	Text    string
	Ref     formula.Range
	Members []formula.Cell
}

// SharedCell is what one member's <f> becomes after the edit, at its
// post-edit cell: Shared with SI and (for a master) Ref and Text, or a
// plain formula (Shared false) with Text.
type SharedCell struct {
	Shared, Master bool
	SI             int
	Ref            string
	Text           string
}

// PlanShared decides every group's fate under e (chunk K7b), pure:
// formula.DecideShared per group; Keep keeps the si and writes the
// shifted master text and ref; Split gives the first part the group's si
// and each later part nextSI, nextSI+1, ... in group order, its own
// master (the part's first member in row-major order) with the part's
// text and ref; Unshare and Split's Children become plain formulas.
// Members a delete removes are absent from the answer. Stats.Adjusted
// counts the texts written that differ from what the member read
// before (a master's text, a child written out); RefErrors the cells
// whose formula gained #REF!.
func PlanShared(groups []SharedGroup, nextSI int, sh formula.Sheets, e formula.Edit) (map[formula.Cell]SharedCell, Stats, error) {
	out := make(map[formula.Cell]SharedCell)
	var stats Stats
	for _, g := range groups {
		d, err := formula.DecideShared(formula.Group{Ref: g.Ref, Master: g.Master, Text: g.Text, Members: g.Members}, sh, e)
		if err != nil {
			return nil, Stats{}, err
		}
		// Map surviving positions back to their original members for statistics.
		original := make(map[formula.Cell]formula.Cell, len(g.Members))
		for _, member := range g.Members {
			c := formula.Cell{Row: member.Row, Col: member.Col}
			moved := c
			if sh.Own != "" && strings.EqualFold(sh.Own, e.Sheet) {
				v := &moved.Row
				if e.Kind == formula.InsertCols || e.Kind == formula.DeleteCols {
					v = &moved.Col
				}
				if e.Kind == formula.DeleteRows || e.Kind == formula.DeleteCols {
					if *v >= e.At && *v < e.At+e.Count {
						continue
					}
					if *v >= e.At+e.Count {
						*v -= e.Count
					}
				} else if *v >= e.At {
					*v += e.Count
				}
			}
			original[moved] = c
		}
		record := func(c formula.Cell, cell SharedCell) error {
			out[c] = cell
			before, ok := original[c]
			if !ok {
				before = c
				if sh.Own != "" && strings.EqualFold(sh.Own, e.Sheet) {
					v := &before.Row
					if e.Kind == formula.InsertCols || e.Kind == formula.DeleteCols {
						v = &before.Col
					}
					if e.Kind == formula.DeleteRows || e.Kind == formula.DeleteCols {
						if *v >= e.At {
							*v += e.Count
						}
					} else if *v >= e.At+e.Count {
						*v -= e.Count
					}
				}
			}
			old, err := formula.Translate(g.Text, g.Master, before)
			if err != nil {
				return err
			}
			writtenBefore := ""
			if before.Row == g.Master.Row && before.Col == g.Master.Col {
				writtenBefore = g.Text
			}
			if cell.Text != "" && cell.Text != writtenBefore {
				stats.Adjusted++
			}
			shifted, err := formula.Shift(old, sh, e)
			if err != nil {
				return err
			}
			if strings.Count(shifted, "#REF!") > strings.Count(old, "#REF!") {
				stats.RefErrors = append(stats.RefErrors, c.String())
			}
			return nil
		}
		for i, part := range d.Parts {
			si := g.SI
			if i > 0 {
				si = nextSI
				nextSI++
			}
			for _, c := range part.Members {
				cell := SharedCell{Shared: true, SI: si}
				if c == part.Master {
					cell.Master, cell.Ref, cell.Text = true, part.Ref.String(), part.Text
				}
				if err := record(c, cell); err != nil {
					return nil, Stats{}, err
				}
			}
		}
		for _, child := range d.Children {
			if err := record(child.Cell, SharedCell{Text: child.Text}); err != nil {
				return nil, Stats{}, err
			}
		}
	}
	return out, stats, nil
}
