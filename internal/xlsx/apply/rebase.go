package apply

import (
	"strings"

	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
)

// rebase moves the units earlier ops reported, and the aliases, through
// one op's structural edits and sheet renames, in order. A range on the
// edited sheet moves as the edit moves its rows or columns and shrinks
// where a delete cuts it; one a delete removes whole is dropped (its
// cells no longer exist). A rename renames the sheet in every range,
// sheet and sheet-scoped name unit and in every alias.
func rebase(res *Result, shifted []formula.Edit, renamed [][2]string) {
	for _, e := range shifted {
		out := res.Units[:0]
		for _, u := range res.Units {
			if u.Kind == ops.UnitRange && strings.EqualFold(u.Sheet, e.Sheet) {
				var ok bool
				if u, ok = shiftUnit(u, e); !ok {
					continue
				}
			}
			out = append(out, u)
		}
		res.Units = out
	}
	for _, rn := range renamed {
		old, name := rn[0], rn[1]
		for i, u := range res.Units {
			switch u.Kind {
			case ops.UnitRange:
				if strings.EqualFold(u.Sheet, old) {
					u.Sheet = name
					u.ID = model.Area(name, u.R1, u.C1, u.R2, u.C2)
				}
			case ops.UnitSheet:
				if strings.EqualFold(u.Sheet, old) {
					u.Sheet, u.ID = name, model.SheetID(name)
				}
			case ops.UnitName:
				u.ID = renameNameID(u.ID, old, name)
			}
			res.Units[i] = u
		}
		for k, v := range res.Aliases {
			if strings.EqualFold(v, model.SheetID(old)) {
				res.Aliases[k] = model.SheetID(name)
			}
		}
	}
}

// shiftUnit moves range unit u through e; false when e deletes it whole.
func shiftUnit(u ops.Unit, e formula.Edit) (ops.Unit, bool) {
	rows := e.Kind == formula.InsertRows || e.Kind == formula.DeleteRows
	lo, hi := u.C1, u.C2
	if rows {
		lo, hi = u.R1, u.R2
	}
	switch e.Kind {
	case formula.InsertRows, formula.InsertCols:
		if lo >= e.At {
			lo += e.Count
		}
		if hi >= e.At {
			hi += e.Count
		}
	default:
		end := e.At + e.Count - 1
		move := func(x int, first bool) int {
			switch {
			case x < e.At:
				return x
			case x > end:
				return x - e.Count
			case first:
				return e.At
			}
			return e.At - 1
		}
		lo, hi = move(lo, true), move(hi, false)
		if lo > hi {
			return u, false
		}
	}
	if rows {
		u.R1, u.R2 = lo, hi
	} else {
		u.C1, u.C2 = lo, hi
	}
	u.ID = model.Area(u.Sheet, u.R1, u.C1, u.R2, u.C2)
	return u, true
}

// renameNameID renames the scope sheet of a name unit's id
// ("name:Old!Local"). The id is parsed with model.ParseRef, which
// splits at the last "!" outside quotes, and rebuilt with
// model.NameID, which quotes the new sheet as a reference prefix
// writes it; an id ParseRef refuses, or one scoped to another sheet,
// is returned unchanged.
func renameNameID(id, old, name string) string {
	ref, err := model.ParseRef(id)
	if err != nil || ref.Kind != model.RefName || ref.Sheet == "" {
		return id
	}
	if !strings.EqualFold(ref.Sheet, old) {
		return id
	}
	return model.NameID(name, ref.Name)
}
