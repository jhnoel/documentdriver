package ops

import (
	"strconv"

	"github.com/jhnoel/documentdriver/internal/docx/build"
	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/html"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// InsertTable is table.insert (docxdriver's insert_table).
type InsertTable struct {
	At       ParagraphAddress `json:"at"`
	Position InsertPosition   `json:"position"`
	Rows     uint32           `json:"rows"`
	Columns  uint32           `json:"columns"`
	Cells    *[][]string      `json:"cells" default:"null"`
	Alias    *string          `json:"as" default:"null"`
}

// opTableInsert is table.insert's entry in the operation table.
var opTableInsert = OpDef{
	Name: "table.insert", Oracle: "insert_table", New: func() Spec { return &InsertTable{} },
	Moved: "`cells` hold `html`",
	Rules: insertTableRules, TrackGuards: []string{"inserting after the last block of a table cell"},
}

// Run inserts a rows x columns table before or after a paragraph of the
// body or of a table cell (table_structure.rs insert), its cells filled
// row by row with the inline content of cells (the build lowering,
// build.NewTable and build.Inline). Every cell paragraph gets its id at
// once, so later operations address it; with `as`, "$as" names the table
// (and "$as/row:N/cell:M" its cells) and "$as_rNcM" the paragraph of row N
// column M. Tracked, every row is a row insertion and every cell's
// content an insertion, as Word writes an inserted table. A table that
// would end a cell needs a paragraph after it: direct mode adds one;
// track mode refuses, since rejecting could not take it away. A
// section-break paragraph is refused as the anchor.
func (o *InsertTable) Run(env *Env) error {
	part, _, err := env.Main()
	if err != nil {
		return err
	}
	p, id, err := env.paragraph(o.At)
	if err != nil {
		return err
	}
	if ppr := model.ChildW(p, "pPr"); ppr != nil && model.ChildW(ppr, "sectPr") != nil {
		return reject("operation_failed", "at", "paragraph %s ends a section: a table cannot be inserted at a section-boundary paragraph", id)
	}
	c := p.Parent()
	if c == nil || !model.IsW(c, "body") && !model.IsW(c, "tc") {
		return reject("operation_failed", "at", "paragraph %s is not a paragraph of the body or of a table cell", id)
	}
	rows, cols := int(o.Rows), int(o.Columns)
	contents := make([]paragraphHTML, rows*cols)
	if o.Cells != nil {
		for r, row := range *o.Cells {
			for k, s := range row {
				if s == "" {
					continue
				}
				h, err := parseParagraphHTML(s)
				if err != nil {
					return reject("invalid_operation", cellField(r, k), "%s", err.Error())
				}
				for i, a := range h.Atoms {
					if a.Kind == html.AtomEquation && a.AtomObject != nil && a.Ref != "" {
						obj := *a.AtomObject
						obj.Ref = ""
						h.Atoms[i].AtomObject = &obj
					}
				}
				contents[r*cols+k] = h
			}
		}
	}
	reg := env.WS.Registry()
	before := o.Position == "before"
	ref := p
	if !before {
		ref = model.NextElement(reg.InsertAfter(p))
	}
	endsCell := model.IsW(c, "tc") && ref == nil
	if endsCell && env.Mode == ModeTrack {
		return reject("refused_in_mode", "", "table.insert cannot be tracked as the last block of a table cell: the cell needs a new paragraph after it, which rejecting cannot take away; insert before the paragraph or use a change_mode direct plan")
	}
	if o.Alias != nil {
		for r := range rows {
			for k := range cols {
				if name := *o.Alias + "_r" + strconv.Itoa(r+1) + "c" + strconv.Itoa(k+1); reg.Taken(name) {
					return reject("invalid_operation", "as", "cell alias %q of table alias %q is already defined", name, *o.Alias)
				}
			}
		}
	}
	anchor, err := env.WS.Core().Range(part, "table.insert at "+id, p, p)
	if err != nil {
		return err
	}
	tbl, paras, err := build.NewTable(anchor, c, rows, cols)
	if err != nil {
		return err
	}
	if err := anchor.PlaceAt(c, tbl, ref); err != nil {
		return err
	}
	if !before {
		reg.NoteInsertedAfter(p, tbl)
	}
	h, err := env.WS.Core().Range(part, "table.insert at "+id, tbl, tbl)
	if err != nil {
		return err
	}
	if endsCell {
		if err := appendParagraph(env, anchor, c); err != nil {
			return err
		}
	}
	if err := env.WS.Core().EnsureRootNamespace(part, docxedit.W14.Prefix, docxedit.W14.URIs[0], true); err != nil {
		return err
	}
	ids, err := env.WS.ParaIDs(part, env.Index, 1, len(paras))
	if err != nil {
		return err
	}
	opt := build.Options{Link: env.hyperlink}
	var groups [][]*tree.Node
	for i, cp := range paras {
		if err := h.SetNodeAttr(cp, docxedit.W14.Prefix+":paraId", ids[i]); err != nil {
			return err
		}
		reg.BindParagraph(ids[i], cp)
		ct := contents[i]
		if ct.Attrs != nil {
			if err := build.Properties(h, cp, *ct.Attrs); err != nil {
				return buildRejection(err, cellField(i/cols, i%cols))
			}
		}
		if len(ct.Atoms) == 0 && ct.Attrs == nil {
			continue
		}
		content, err := build.Inline(h, cp, nil, ct.Atoms, opt)
		if err != nil {
			return buildRejection(err, cellField(i/cols, i%cols))
		}
		groups = append(groups, content.Groups...)
	}
	if env.Mode == ModeTrack {
		for _, tr := range tableRows(tbl) {
			if _, err := env.Rev.MarkRow(h, tr, "ins"); err != nil {
				return err
			}
		}
		for _, g := range groups {
			if _, err := env.Rev.Insert(h, g); err != nil {
				return err
			}
		}
	}
	if err := env.refreshTables(); err != nil {
		return err
	}
	if o.Alias != nil {
		if err := reg.BindTable(*o.Alias, tbl); err != nil {
			return reject("invalid_operation", "as", "%s", err.Error())
		}
		for i, cid := range ids {
			name := *o.Alias + "_r" + strconv.Itoa(i/cols+1) + "c" + strconv.Itoa(i%cols+1)
			if err := reg.DefineAlias(name, cid); err != nil {
				return reject("invalid_operation", "as", "%s", err.Error())
			}
		}
	}
	return nil
}

// cellField is the plan path of one cell's html.
func cellField(r, c int) string { return "cells[" + strconv.Itoa(r) + "][" + strconv.Itoa(c) + "]" }
