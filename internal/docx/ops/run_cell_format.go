package ops

import (
	"strings"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// FormatCell is cell.format (docxdriver's format_cell).
type FormatCell struct {
	At     string     `json:"at"`
	Format CellFormat `json:"format"`
}

// opCellFormat is cell.format's entry in the operation table.
var opCellFormat = OpDef{
	Name: "cell.format", Oracle: "format_cell", New: func() Spec { return &FormatCell{} },
	Rules: formatCellRules,
}

// cellClears are the cell properties clear removes, by field.
var cellClears = map[string]string{
	"width": "tcW", "borders": "tcBorders", "fill": "shd", "padding": "tcMar", "vertical_alignment": "vAlign",
}

// sets reports whether the format sets any cell property.
func (f *CellFormat) sets() bool {
	return f.Width != nil || f.Borders != nil || f.Fill != nil || f.Padding != nil || f.VerticalAlignment != nil
}

// logicalCell resolves a cell address to the cells that make up the
// logical cell: the cell itself, or a vertical merge's restart and its
// continuations. A continuation or a legacy w:hMerge cell is refused: the
// logical cell is addressed by its restart, and hMerge is not modelled.
func (env *Env) logicalCell(at string) (*tableTarget, []*tree.Node, error) {
	t, err := env.resolveTable(at)
	if err != nil {
		return nil, nil, err
	}
	if t.cell == nil {
		return nil, nil, reject("not_found", "at", "%s does not name a cell", at)
	}
	g := readGrid(t.table)
	ri := g.rowIndex(t.row)
	ci := -1
	if ri >= 0 {
		ci = g.rows[ri].index(t.cell)
	}
	if ci < 0 {
		return nil, nil, reject("unreadable_target", "at", "%s is not a cell of a table row", at)
	}
	c := g.rows[ri].cells[ci]
	if c.merge == vmContinue {
		return nil, nil, reject("operation_failed", "at", "%s is a vertical-merge continuation; address the merged cell by its first cell", at)
	}
	if c.hmerge {
		return nil, nil, reject("operation_failed", "at", "%s uses legacy w:hMerge; it cannot be edited", at)
	}
	cells, _, ok := g.verticalRun(ri, ci)
	if !ok {
		return nil, nil, reject("unreadable_target", "at", "%s is a vertical merge whose continuations span other grid columns", at)
	}
	return t, cells, nil
}

// Run formats a logical cell (tables.rs format_cell): its width, borders,
// margins, fill and vertical alignment, on every physical cell of a
// vertical merge, each recorded as a w:tcPrChange; direct mode accepts
// them. A cell with a pending property change is refused.
func (o *FormatCell) Run(env *Env) error {
	_, cells, err := env.logicalCell(o.At)
	if err != nil {
		return err
	}
	for _, c := range cells {
		if pendingRecord(model.ChildW(c, "tcPr"), "tcPrChange") {
			return reject("operation_failed", "at", "%s has a pending property change (w:tcPrChange); accept or reject it first", o.At)
		}
	}
	f := &o.Format
	main, _, err := env.Main()
	if err != nil {
		return err
	}
	h := env.WS.Core().Target(main, o.At, cells)
	for _, c := range cells {
		if model.ChildW(c, "tcPr") == nil && !f.sets() {
			continue
		}
		tcPr, created, err := ensureBag(h, c, "tcPr")
		if err != nil {
			return err
		}
		w := propWriter{h, tcPr}
		if _, err := env.Rev.Property(h, tcPr, func() error { return f.apply(w) }); err != nil {
			return err
		}
		if err := dropIfEmpty(h, tcPr, created); err != nil {
			return err
		}
	}
	return nil
}

// apply writes the cell format into one w:tcPr.
func (f *CellFormat) apply(w propWriter) error {
	if f.Width != nil {
		if err := w.width(w.bag, "tcW", *f.Width); err != nil {
			return err
		}
	}
	if f.Borders != nil {
		if err := w.borders("tcBorders", *f.Borders); err != nil {
			return err
		}
	}
	if f.Padding != nil {
		if err := w.padding("tcMar", *f.Padding); err != nil {
			return err
		}
	}
	if f.Fill != nil {
		n, err := w.ensure(w.bag, "shd")
		if err != nil {
			return err
		}
		if _, ok := model.Attr(n, "val"); !ok {
			if err := w.set(n, "val", "clear"); err != nil {
				return err
			}
		}
		if err := w.set(n, "fill", strings.TrimPrefix(*f.Fill, "#")); err != nil {
			return err
		}
		for _, a := range []string{"themeFill", "themeFillTint", "themeFillShade"} {
			if err := w.unset(n, a); err != nil {
				return err
			}
		}
	}
	if f.VerticalAlignment != nil {
		if err := w.val("vAlign", *f.VerticalAlignment); err != nil {
			return err
		}
	}
	return w.clears(f.Clear, cellClears)
}
