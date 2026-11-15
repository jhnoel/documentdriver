package ops

import (
	"strings"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// FormatTable is table.format (docxdriver's format_table).
type FormatTable struct {
	At     string      `json:"at"`
	Format TableFormat `json:"format"`
}

// opTableFormat is table.format's entry in the operation table.
var opTableFormat = OpDef{
	Name: "table.format", Oracle: "format_table", New: func() Spec { return &FormatTable{} },
	Rules: formatTableRules,
}

// tableStyleExists reports whether the styles part beside the main
// document declares a table style with id.
func (env *Env) tableStyleExists(id string) (found, hasStyles bool) {
	x, ok := env.part(env.siblingPart("styles.xml"))
	if !ok {
		return false, false
	}
	x.Root().Walk(func(n *tree.Node) bool {
		if found {
			return false
		}
		if model.IsW(n, "style") && model.AttrOr(n, "type") == "table" && model.AttrOr(n, "styleId") == id {
			found = true
		}
		return true
	})
	return found, true
}

// siblingPart is the part named name in the main document part's folder
// ("word/styles.xml" beside "word/document.xml").
func (env *Env) siblingPart(name string) string {
	main := strings.TrimPrefix(env.WS.DocumentPart(), "/")
	if i := strings.LastIndexByte(main, '/'); i >= 0 {
		return main[:i+1] + name
	}
	return name
}

// tableClears are the table properties clear removes, by field.
var tableClears = map[string]string{
	"width": "tblW", "layout": "tblLayout", "alignment": "jc", "borders": "tblBorders",
	"padding": "tblCellMar", "cell_spacing": "tblCellSpacing", "style": "tblStyle",
}

// sets reports whether the format sets any table property (rather than
// only clearing).
func (f *TableFormat) sets() bool {
	return f.Width != nil || f.Layout != nil || f.Alignment != nil || f.Borders != nil ||
		f.CellSpacing != nil || f.Padding != nil || f.Style != nil
}

// Run formats a table (tables.rs format_table): its width, layout,
// alignment, borders, cell spacing, default cell margins and style in
// w:tblPr, and its column widths in w:tblGrid, each recorded as a
// property change (w:tblPrChange, w:tblGridChange); direct mode accepts
// them. A container with a pending change is refused, as are column
// widths that do not match the grid and a style the document does not
// declare.
func (o *FormatTable) Run(env *Env) error {
	main, _, err := env.Main()
	if err != nil {
		return err
	}
	f := &o.Format
	if f.Style != nil {
		found, hasStyles := env.tableStyleExists(*f.Style)
		if !hasStyles {
			return reject("not_found", "format.style", "table style %q not found: the document has no styles part", *f.Style)
		}
		if !found {
			return reject("not_found", "format.style", "table style %q not found: the document declares no table style with that id", *f.Style)
		}
	}
	t, err := env.resolveTable(o.At)
	if err != nil {
		return err
	}
	if t.row != nil || t.cell != nil {
		return reject("not_found", "at", "%s does not name a table", o.At)
	}
	tbl := t.table
	grid := model.ChildW(tbl, "tblGrid")
	var cols []*tree.Node
	if f.ColumnWidths != nil {
		if grid == nil {
			return reject("operation_failed", "at", "%s has no grid (w:tblGrid): its column widths cannot be set", o.At)
		}
		cols = childrenW(grid, "gridCol")
		if len(cols) != len(*f.ColumnWidths) {
			return reject("operation_failed", "format.column_widths", "column_widths has %d values; the table grid has %d columns", len(*f.ColumnWidths), len(cols))
		}
		if pendingRecord(grid, "tblGridChange") {
			return reject("operation_failed", "at", "the grid of %s has a pending change (w:tblGridChange); accept or reject it first", o.At)
		}
	}
	tblPr := model.ChildW(tbl, "tblPr")
	if pendingRecord(tblPr, "tblPrChange") {
		return reject("operation_failed", "at", "%s has a pending property change (w:tblPrChange); accept or reject it first", o.At)
	}
	h := env.WS.Target(main, o.At, tbl)
	if f.ColumnWidths != nil {
		if _, err := env.Rev.Property(h, grid, func() error {
			w := propWriter{h, grid}
			for i, c := range cols {
				if err := w.set(c, "w", twips((*f.ColumnWidths)[i])); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
		if err := gridRecordMarkup(h, grid); err != nil {
			return err
		}
	}
	created := false
	if tblPr == nil {
		if !f.sets() {
			return nil
		}
		if tblPr, err = insertTblPr(env, main, tbl); err != nil {
			return err
		}
		created = true
	}
	if _, err := env.Rev.Property(h, tblPr, func() error { return f.apply(propWriter{h, tblPr}) }); err != nil {
		return err
	}
	if err := dropIfEmpty(h, tblPr, created); err != nil {
		return err
	}
	return nil
}

// apply writes the table format into w:tblPr.
func (f *TableFormat) apply(w propWriter) error {
	if f.Width != nil {
		if err := w.width(w.bag, "tblW", *f.Width); err != nil {
			return err
		}
	}
	if f.Layout != nil {
		n, err := w.ensure(w.bag, "tblLayout")
		if err != nil {
			return err
		}
		if err := w.set(n, "type", *f.Layout); err != nil {
			return err
		}
	}
	if f.Alignment != nil {
		if err := w.val("jc", *f.Alignment); err != nil {
			return err
		}
	}
	if f.Style != nil {
		if err := w.val("tblStyle", *f.Style); err != nil {
			return err
		}
	}
	if f.Padding != nil {
		if err := w.padding("tblCellMar", *f.Padding); err != nil {
			return err
		}
	}
	if f.Borders != nil {
		if err := w.borders("tblBorders", *f.Borders); err != nil {
			return err
		}
	}
	if f.CellSpacing != nil {
		if err := w.width(w.bag, "tblCellSpacing", TableWidth{Unit: "points", Points: *f.CellSpacing}); err != nil {
			return err
		}
	}
	return w.clears(f.Clear, tableClears)
}

// gridRecordMarkup trims a new w:tblGridChange to its schema: CT_Markup
// carries an id only, no author or date (the Reviser stamps every record
// with all three).
func gridRecordMarkup(h *core.Handle, grid *tree.Node) error {
	rec := model.ChildW(grid, "tblGridChange")
	if rec == nil || !h.Created(rec) {
		return nil
	}
	w := propWriter{h, grid}
	for _, a := range []string{"author", "date"} {
		if err := w.unset(rec, a); err != nil {
			return err
		}
	}
	return nil
}

// insertTblPr gives a table without one an empty w:tblPr, first after
// any range markup before its grid and rows (CT_Tbl).
func insertTblPr(env *Env, part string, tbl *tree.Node) (*tree.Node, error) {
	prefix := strings.TrimSuffix(tbl.Name(), "tbl")
	nodes, err := env.WS.Core().Parse(tbl, []byte("<"+prefix+"tblPr/>"), nil)
	if err != nil || len(nodes) != 1 {
		return nil, reject("unreadable_target", "at", "the table's properties cannot be written")
	}
	n := nodes[0]
	var before *tree.Node
	for _, c := range tbl.Elements() {
		if model.IsW(c, "tblGrid") || model.IsW(c, "tr") || model.IsW(c, "sdt") || model.IsW(c, "customXml") {
			before = c
			break
		}
	}
	pos := core.Into(tbl)
	if before != nil {
		pos = core.Before(before)
	}
	if err := env.WS.Core().Insert(part, pos, n, nil); err != nil {
		return nil, err
	}
	return n, nil
}
