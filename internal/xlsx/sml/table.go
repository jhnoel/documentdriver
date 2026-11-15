package sml

import (
	"fmt"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
)

// Table is a table part's facts (a ListObject).
type Table struct {
	// Name is table@displayName, the name formulas and table: addresses
	// use; table@name when displayName is absent.
	Name string
	// Ref is table@ref, normalised: From top-left, To bottom-right, no
	// "$" flags, Single for one cell (header and totals rows included).
	Ref formula.Range
	// HeaderRows is table@headerRowCount (default 1); TotalsRows
	// table@totalsRowCount (default 0). A value that is not ParseIndex
	// keeps the default. totalsRowShown does not count.
	HeaderRows, TotalsRows int
	// Columns are tableColumns/tableColumn@name in order (never nil).
	Columns []string
}

// ParseTable reads the root of a parsed table part. It fails when root
// is not <table> in a main namespace or its ref is not an A1 cell or
// area.
func ParseTable(root *tree.Node) (Table, error) {
	if root == nil || !IsMain(root.Space()) || root.Local() != "table" {
		return Table{}, fmt.Errorf("table: the part has no table root")
	}
	t := Table{Columns: []string{}, HeaderRows: 1}
	if v, ok := root.Attr("displayName"); ok {
		t.Name = v
	} else if v, ok := root.Attr("name"); ok {
		t.Name = v
	}
	ref, _ := root.Attr("ref")
	r, ok := formula.ParseArea(ref)
	if !ok {
		return Table{}, fmt.Errorf("table: ref %q does not parse", ref)
	}
	t.Ref = r
	if v, ok := root.Attr("headerRowCount"); ok {
		if n, ok := ParseIndex(v); ok {
			t.HeaderRows = n
		}
	}
	if v, ok := root.Attr("totalsRowCount"); ok {
		if n, ok := ParseIndex(v); ok {
			t.TotalsRows = n
		}
	}
	ns := root.Space()
	if cols := root.Child(ns, "tableColumns"); cols != nil {
		for e := range cols.ElementsSeq() {
			if !e.Is(ns, "tableColumn") {
				continue
			}
			if name, ok := e.Attr("name"); ok {
				t.Columns = append(t.Columns, name)
			}
		}
	}
	return t, nil
}
