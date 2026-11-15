package edit

import (
	"fmt"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
	"github.com/jhnoel/documentdriver/internal/xlsx/sml"
)

// The small parts, parsed as trees. This is the one file of the package
// that parses a part (internal/archtest: xlsxTreeFiles): the workbook and
// table parts. smallTree refuses a worksheet or the shared strings.

// smallTree returns the tree of a small part of the working copy (the
// workbook, styles, a table). It refuses the parts the streaming rule
// keeps out of trees: worksheets and the shared strings.
func (w *Workspace) smallTree(part string) (*tree.Document, error) {
	if w.big[opc.PartKey(part)] {
		return nil, fmt.Errorf("edit: %s is never parsed into a tree", part)
	}
	return w.core.Tree(part)
}

// readBook reads the workbook tree: its sheets in tab order (with their
// relationship's part and kind), workbookPr@date1904 and
// calcPr@fullCalcOnLoad. A sheet whose relationship or part is missing
// is listed with Part "" (an op on it is unreadable_target).
func (w *Workspace) readBook() error {
	doc, err := w.smallTree(w.wb)
	if err != nil {
		return err
	}
	root := doc.Root()
	if root == nil || !sml.IsMain(root.Space()) || root.Local() != "workbook" {
		return fmt.Errorf("edit: %s has no workbook root", w.wb)
	}
	rels, err := w.pkg.Rels(w.wb)
	if err != nil {
		return err
	}
	// One pass over the relationships: opc.Rels.Get walks them all, so a
	// Get per sheet is quadratic in the sheets, hence RelsByID, built once.
	byID := sml.RelsByID(rels)
	w.sheets = w.sheets[:0]
	w.date1904 = false
	for e := range root.ElementsSeq() {
		if !sml.IsMain(e.Space()) {
			continue
		}
		switch e.Local() {
		case "workbookPr":
			v, _ := e.Attr("date1904")
			w.date1904 = sml.IsTrue(v)
		case "calcPr":
			v, _ := e.Attr("fullCalcOnLoad")
			w.fullCalc = sml.IsTrue(v)
		case "sheets":
			for _, ent := range sml.SheetEntries(e) {
				part, kind, _ := sml.ResolveSheet(ent, rels, byID, w.pkg)
				ref := SheetRef{Index: len(w.sheets), Name: ent.Name, State: ent.State, ID: ent.SheetID, RID: ent.RID, Part: part}
				switch kind {
				case sml.KindWorksheet:
					ref.Kind = KindWorksheet
				case sml.KindChartsheet:
					ref.Kind = KindChartsheet
				case sml.KindMissing:
					ref.Kind = KindMissing
				default:
					ref.Kind = KindOther
				}
				w.sheets = append(w.sheets, ref)
			}
		}
	}
	for _, s := range w.sheets {
		if s.Part != "" && s.Kind == KindWorksheet {
			w.big[opc.PartKey(s.Part)] = true
		}
	}
	for _, r := range rels.All() {
		if ctypes.Is(r.Type, ctypes.RelSharedStrings) && !r.External() {
			if p, err := rels.TargetPart(r); err == nil {
				w.sst = p
				w.big[opc.PartKey(p)] = true
			}
		}
	}
	return nil
}

// TableRef is one table (a ListObject) as its part stores it.
type TableRef struct {
	// Name is table@displayName (the name formulas and `table:` use);
	// Part the table part; Sheet the name of the sheet it is on.
	Name, Part, Sheet string
	// Ref is table@ref, normalised (header and totals rows included).
	Ref formula.Range
	// HeaderRows is headerRowCount (default 1); TotalsRows totalsRowCount
	// (default 0).
	HeaderRows, TotalsRows int
	// Columns are the tableColumn names, in order.
	Columns []string
}

// Tables returns the tables of sheet (by name, ignoring case), in the
// order of the sheet's relationships. A table part that cannot be read
// is left out: an op that needs it refuses on its own target.
func (w *Workspace) Tables(sheetName string) ([]TableRef, error) {
	ref, ok := w.SheetRef(sheetName)
	if !ok {
		return nil, fmt.Errorf("%w: sheet %s", ErrNotFound, sheetName)
	}
	if ref.Part == "" {
		return nil, nil
	}
	rels, err := w.pkg.Rels(ref.Part)
	if err != nil {
		return nil, &Unreadable{Target: model.SheetID(ref.Name), Detail: "its relationships cannot be read: " + err.Error()}
	}
	var out []TableRef
	for _, r := range rels.ByType(ctypes.RelTable) {
		if r.External() {
			continue
		}
		p, err := rels.TargetPart(r)
		if err != nil || !w.pkg.Has(p) {
			continue
		}
		doc, err := w.smallTree(p)
		if err != nil {
			continue
		}
		tbl, err := sml.ParseTable(doc.Root())
		if err != nil {
			continue
		}
		out = append(out, TableRef{Name: tbl.Name, Part: p, Sheet: ref.Name, Ref: tbl.Ref, HeaderRows: tbl.HeaderRows, TotalsRows: tbl.TotalsRows, Columns: tbl.Columns})
	}
	return out, nil
}

// WorkbookTree returns the workbook part's tree for reading. Changes go
// through WorkbookHandle, which declares them.
func (w *Workspace) WorkbookTree() (*tree.Document, error) { return w.smallTree(w.wb) }

// SmallTree returns the tree of a small part (styles, a table, a
// drawing) for reading; a worksheet or the shared strings is refused.
// Changes go through the edit core (Core().Part), which declares them.
func (w *Workspace) SmallTree(part string) (*tree.Document, error) { return w.smallTree(part) }

// insertSmall inserts frag, one element named local and its content,
// all written unprefixed (the main namespace is bound as the default
// while it parses), as the last member of its slot in container c
// (complex type typ) of small part part: the edit core's Parse, SlotEnd
// and Insert, so the element lands where the schema orders it, in the
// part's own main namespace (Transitional or Strict).
func (w *Workspace) insertSmall(part string, c *tree.Node, typ, local, frag string) error {
	ns := sml.NSMain
	if c.Space() == sml.NSStrict {
		ns = sml.NSStrict
	}
	nodes, err := w.core.Parse(c, []byte(frag), map[string]string{"": ns})
	if err != nil {
		return err
	}
	if len(nodes) != 1 {
		return fmt.Errorf("edit: %s: the fragment is not one element", local)
	}
	pos, err := w.core.SlotEnd(c, typ, SML.Name(local), true, nil, nil)
	if err != nil {
		return err
	}
	return w.core.Insert(part, pos, nodes[0], nil)
}
