package edit

import (
	"slices"
	"sync"

	"github.com/jhnoel/documentdriver/internal/ooxml/schema"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Units is plan.Units for xlsx. No part is divided into units: a
// worksheet is verified as a spliced part (its declared spans), and the
// small parts the edit layer changes (workbook, styles, tables, content
// types, relationships) are declared as wholes through the edit core.
type Units struct{}

// HasUnits implements plan.Units.
func (Units) HasUnits(*tree.Document) bool { return false }

// IsUnit implements plan.Units.
func (Units) IsUnit(*tree.Node) bool { return false }

// Describe implements plan.Units.
func (Units) Describe(e *tree.Node) string { return e.Name() }

// worksheetOrder is the slot of each x:CT_Worksheet child, from the
// generated schema table.
var worksheetOrder = sync.OnceValue(func() map[string]int {
	order, _ := schema.ChildOrder("x:CT_Worksheet")
	out := map[string]int{}
	for k, names := range order {
		for _, n := range names {
			if i := len("x:"); len(n) > i && n[:i] == "x:" {
				out[n[i:]] = k
			}
		}
	}
	return out
})

// worksheetRank returns the schema slot of worksheet child local.
func worksheetRank(local string) (int, bool) {
	k, ok := worksheetOrder()[local]
	return k, ok
}

// cellOrder is x:CT_Cell's child order (f, v, is, extLst).
var cellOrder = sync.OnceValue(func() []string {
	order, _ := schema.ChildOrder("x:CT_Cell")
	var out []string
	for _, names := range order {
		for _, n := range names {
			out = append(out, n[len("x:"):])
		}
	}
	return out
})

// cellRank returns the slot of a <c> child, -1 for one the schema does
// not list.
func cellRank(local string) int { return slices.Index(cellOrder(), local) }
