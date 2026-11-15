package apply

import (
	"encoding/json"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
	"github.com/jhnoel/documentdriver/internal/xlsx/read"
)

// finishUnits fills res.Changed from res.Units, read back from data (the
// package as the plan leaves it): a range as {"id", "cells"} with cells
// as read --range shows them in the cells view (at most read.DefaultLimit
// cells; a cut range adds read's "truncated"); a sheet as {"id",
// "formulas_adjusted"}; a name as {"id"} and read --kind names' entry
// fields, with res.ChangedIDs beside res.Changed as each unit's id.
// A unit read cannot show (a range on a sheet the plan later
// deleted, a name a later op deleted) is left out. The workbook is read
// once for names and once per range (each read.Cells opens it from the
// bytes and scans the range's sheet up to the range's last row:
// CellsOnly, since the gate has scanned the output in full).
func finishUnits(res *Result, data []byte) error {
	res.Changed = make([]json.RawMessage, 0, len(res.Units))
	res.ChangedIDs = make([]string, 0, len(res.Units))
	var names []read.NameEntry
	namesRead := false
	for _, u := range res.Units {
		var v any
		switch u.Kind {
		case ops.UnitRange:
			c, err := read.Cells(data, read.CellsRequest{Range: u.ID, Limit: read.DefaultLimit, CellsOnly: true}, read.Options{})
			if err != nil {
				continue
			}
			v = rangeUnit{ID: u.ID, Cells: c.Cells, Truncated: c.Truncated}
		case ops.UnitSheet:
			v = sheetUnit{ID: u.ID, FormulasAdjusted: u.FormulasAdjusted}
		case ops.UnitName:
			if !namesRead {
				namesRead = true
				wb, err := read.Workbook(data, read.Options{})
				if err != nil {
					return err
				}
				names = wb.Names
			}
			e, ok := findName(names, u.ID)
			if !ok {
				continue
			}
			v = nameUnit{ID: u.ID, NameEntry: e}
		}
		b, err := cli.Marshal(v)
		if err != nil {
			return err
		}
		res.Changed = append(res.Changed, b)
		res.ChangedIDs = append(res.ChangedIDs, u.ID)
	}
	return nil
}

type rangeUnit struct {
	ID        string          `json:"id"`
	Cells     *read.CellMap   `json:"cells"`
	Truncated *read.Truncated `json:"truncated,omitempty"`
}

type sheetUnit struct {
	ID               string `json:"id"`
	FormulasAdjusted int    `json:"formulas_adjusted"`
}

type nameUnit struct {
	ID string `json:"id"`
	read.NameEntry
}

// findName finds the entry of name address id ("name:Tax" for workbook
// scope, "name:Budget!Local" for sheet scope; names match ignoring
// case). The id is parsed with model.ParseRef, which splits at the
// last "!" outside quotes, so a sheet name holding a quoted "!" is
// read back as written.
func findName(names []read.NameEntry, id string) (read.NameEntry, bool) {
	ref, err := model.ParseRef(id)
	if err != nil || ref.Kind != model.RefName {
		return read.NameEntry{}, false
	}
	scope := "workbook"
	if ref.Sheet != "" {
		scope = model.SheetID(ref.Sheet)
	}
	for _, n := range names {
		if strings.EqualFold(n.Name, ref.Name) && strings.EqualFold(n.Scope, scope) {
			return n, true
		}
	}
	return read.NameEntry{}, false
}
