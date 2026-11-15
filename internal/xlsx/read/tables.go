package read

import (
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
	"github.com/jhnoel/documentdriver/internal/xlsx/sml"
)

// Table is one table part (xl/tables/tableN.xml) of a worksheet.
type Table struct {
	// Name is table@displayName, the name formulas and table: addresses
	// use (table@name when displayName is absent).
	Name string
	// Sheet is the name of the sheet holding it.
	Sheet string
	// Ref is table@ref, normalised (formula.ParseArea rules); a table
	// whose ref does not parse is left out with a NotePart note.
	Ref formula.Range
	// HeaderRows is table@headerRowCount (default 1); TotalsRows is
	// table@totalsRowCount (default 0). totalsRowShown does not count.
	HeaderRows, TotalsRows int
	// Columns are tableColumns/tableColumn@name in order.
	Columns []string
}

// parseTable reads a parsed table part of sheet. It fails when the
// root is not <table> in a main namespace or its ref does not parse.
func parseTable(doc *tree.Document, sheet string) (Table, error) {
	t, err := sml.ParseTable(doc.Root())
	if err != nil {
		return Table{}, err
	}
	return Table{
		Name:       t.Name,
		Sheet:      sheet,
		Ref:        t.Ref,
		HeaderRows: t.HeaderRows,
		TotalsRows: t.TotalsRows,
		Columns:    t.Columns,
	}, nil
}

// ColumnRange is the plain range of column col (matched ignoring case)
// for table:T/column:C: the column's cells in Ref without the header and
// totals rows. found is false when the table has no such column (the
// caller's not_found); hasData is false when no row is left (the caller
// refuses with invalid_operation: "table Sales has no data rows").
func (t Table) ColumnRange(col string) (r formula.Range, found, hasData bool) {
	idx := -1
	for i, c := range t.Columns {
		if strings.EqualFold(c, col) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return formula.Range{}, false, false
	}
	r1, c1, r2, _ := t.Ref.Bounds()
	from := r1 + t.HeaderRows
	to := r2 - t.TotalsRows
	if from > to {
		return formula.Range{}, true, false
	}
	cn := c1 + idx
	return formula.Range{
		Kind:   formula.Cells,
		From:   formula.Cell{Row: from, Col: cn},
		To:     formula.Cell{Row: to, Col: cn},
		Single: from == to,
	}, true, true
}

// Tables returns the tables of sheet sh, in the order of its
// relationships of type ctypes.RelTable, each parsed on first use and
// cached per sheet part. A table part that is missing or unreadable is
// left out with a NotePart note. It does not read the worksheet itself.
func (b *Book) Tables(sh *SheetInfo) []Table {
	if b.tables == nil {
		b.tables = map[string][]Table{}
	}
	if sh == nil || sh.Part == "" {
		return nil
	}
	if ts, ok := b.tables[sh.Part]; ok {
		return ts
	}
	rels, err := b.pkg.Rels(sh.Part)
	if err != nil {
		b.Notes.Add(model.NotePart, sh.Part, "the sheet's relationships cannot be read")
		b.tables[sh.Part] = nil
		return nil
	}
	var out []Table
	for _, r := range rels.All() {
		if !ctypes.Is(r.Type, ctypes.RelTable) || r.External() {
			continue
		}
		tp, err := rels.TargetPart(r)
		if err != nil {
			continue
		}
		if !b.pkg.Has(tp) {
			b.Notes.Add(model.NotePart, tp, "the table part is missing")
			continue
		}
		pt, err := b.pkg.Part(tp)
		if err != nil {
			continue
		}
		doc, err := pt.Tree()
		if err != nil {
			b.recordLimit(err)
			b.Notes.Add(model.NotePart, tp, "the table part cannot be read")
			continue
		}
		t, err := parseTable(doc, sh.Name)
		if err != nil {
			b.Notes.Add(model.NotePart, tp, "the table part cannot be read")
			continue
		}
		out = append(out, t)
	}
	b.tables[sh.Part] = out
	return out
}

// Table finds a table by name on any worksheet, ignoring case; ok is
// false when there is none.
func (b *Book) Table(name string) (Table, bool) {
	for i := range b.Sheets {
		for _, t := range b.Tables(&b.Sheets[i]) {
			if strings.EqualFold(t.Name, name) {
				return t, true
			}
		}
	}
	return Table{}, false
}
