package build

import (
	"strconv"

	"github.com/jhnoel/documentdriver/internal/docx/html"
)

// Block is one block of document HTML: a paragraph or a table (exactly
// one of the two is set).
type Block struct {
	Para  *Para
	Table *Table
}

// Table is an HTML table as written: its rows in order and the style
// read writes for it.
type Table struct {
	CSS  string
	Rows []Row
}

// Row is a table row: its cells in order and its style.
type Row struct {
	CSS   string
	Cells []Cell
}

// Cell is a table cell: its spans (1 when not written), its style and its
// content, paragraphs and nested tables.
type Cell struct {
	CSS              string
	Colspan, Rowspan int
	Blocks           []Block
}

// Blocks splits document HTML items into paragraphs and tables, with the
// paragraph rules of Paragraphs. Table structure must nest as HTML has
// it: rows in a table, cells in a row, content in a cell.
func Blocks(items []html.Item) ([]Block, error) {
	return collect(items, true)
}

// frame is an open table during collect: the blocks of the container it
// sits in and the cell, if any, now open.
type frame struct {
	table  *Table
	inCell bool
}

// collect is Blocks; without tables, table structure is refused as
// Paragraphs refuses it.
func collect(items []html.Item, tables bool) ([]Block, error) { // tables: document HTML
	var root []Block
	var stack []frame
	// container is where the next block goes: the open cell's blocks, or
	// the root.
	container := func() *[]Block {
		if len(stack) == 0 {
			return &root
		}
		f := stack[len(stack)-1]
		row := &f.table.Rows[len(f.table.Rows)-1]
		return &row.Cells[len(row.Cells)-1].Blocks
	}
	inContent := func() bool { return len(stack) == 0 || stack[len(stack)-1].inCell }
	var open *Para
	for _, it := range items {
		switch it.Kind {
		case html.ItemParaOpen:
			if !inContent() {
				return nil, contentErr("a paragraph inside a table goes in a cell")
			}
			a := *it.Para
			if err := checkPara(a); err != nil {
				return nil, err
			}
			c := container()
			*c = append(*c, Block{Para: &Para{Attrs: a}})
			open = (*c)[len(*c)-1].Para
		case html.ItemParaClose:
			open = nil
		case html.ItemStruct:
			if !tables {
				return nil, unsupported("table")
			}
			if err := structure(&stack, container, inContent, it); err != nil {
				return nil, err
			}
		case html.ItemAtom:
			if open == nil {
				return nil, contentErr("content outside a paragraph")
			}
			if err := checkAtom(it.Atom, tables); err != nil {
				return nil, err
			}
			open.Atoms = append(open.Atoms, it.Atom)
		}
	}
	if len(stack) > 0 {
		return nil, unsupported("table")
	}
	return root, nil
}

// structure applies one table token to the open tables.
func structure(stack *[]frame, container func() *[]Block, inContent func() bool, it html.Item) error {
	top := func() *frame { return &(*stack)[len(*stack)-1] }
	var a html.TableAttrs
	if it.Table != nil {
		a = *it.Table
	}
	css := ""
	if a.CSS.OK {
		css = a.CSS.V
	}
	switch it.Struct {
	case "table":
		if !inContent() {
			return contentErr("a table inside a table goes in a cell")
		}
		*stack = append(*stack, frame{table: &Table{CSS: css}})
	case "/table":
		if len(*stack) == 0 || top().inCell {
			return contentErr("</table> closes no table")
		}
		t := top().table
		if len(t.Rows) == 0 {
			return contentErr("a table needs at least one row")
		}
		*stack = (*stack)[:len(*stack)-1]
		c := container()
		*c = append(*c, Block{Table: t})
	case "tr":
		if len(*stack) == 0 || top().inCell {
			return contentErr("<tr> belongs in a table")
		}
		top().table.Rows = append(top().table.Rows, Row{CSS: css})
	case "/tr":
		if len(*stack) == 0 || top().inCell {
			return contentErr("</tr> closes no row")
		}
		if rows := top().table.Rows; len(rows[len(rows)-1].Cells) == 0 {
			return contentErr("a table row needs at least one cell")
		}
	case "/td":
		if len(*stack) == 0 || !top().inCell {
			return contentErr("</td> closes no cell")
		}
		top().inCell = false
	default: // "td …"
		if len(*stack) == 0 || top().inCell || len(top().table.Rows) == 0 {
			return contentErr("<td> belongs in a table row")
		}
		cs, err := span("colspan", a.Colspan)
		if err != nil {
			return err
		}
		rs, err := span("rowspan", a.Rowspan)
		if err != nil {
			return err
		}
		rows := top().table.Rows
		row := &rows[len(rows)-1]
		row.Cells = append(row.Cells, Cell{CSS: css, Colspan: cs, Rowspan: rs})
		top().inCell = true
	}
	return nil
}

// span reads a colspan or rowspan: 1 when absent, else a count from 1 to
// MaxGridColumns (a rowspan past the table's end stops at its last row).
func span(name string, v html.Opt[string]) (int, error) {
	if !v.OK {
		return 1, nil
	}
	limit := maxRowspan
	if name == "colspan" {
		limit = MaxGridColumns
	}
	n, err := strconv.Atoi(v.V)
	if err != nil || n < 1 || n > limit {
		return 0, contentErr("%s must be a whole number from 1 to %d", name, limit)
	}
	return n, nil
}

// maxRowspan bounds a rowspan as written (HTML's own limit).
const maxRowspan = 65534

// checkPara refuses a paragraph open tag a new paragraph cannot carry.
func checkPara(a html.ParaAttrs) error {
	if a.BreakIns.OK || a.BreakDel.OK {
		return contentErr("revision markup is not content: the engine records revisions")
	}
	if a.Num.OK {
		return contentErr("list markers are computed from the numbering part, never written")
	}
	if a.List.OK {
		switch {
		case a.List.V.Continuation:
			return contentErr("a list item of several paragraphs cannot be written: give each paragraph its own <li>")
		case a.List.V.Level > 8:
			return contentErr("DOCX lists support at most nine levels")
		}
	}
	// read writes a heading's own style as its data-style (the class
	// here), which is the tag's and changes nothing.
	if a.Tag != "p" && a.Class.OK && a.Class.V != StyleOf(html.ParaAttrs{Tag: a.Tag}) {
		return contentErr("<%s> takes its style from the tag; class is only for <p>", a.Tag)
	}
	return nil
}

// checkAtom refuses content a new paragraph cannot carry; images only in
// document HTML (create, which places them through Options.Image).
func checkAtom(a html.Atom, document bool) error {
	if a.Ins.OK || a.Del.OK || (a.Kind == html.AtomChar && a.FmtPending.OK) {
		return contentErr("revision markup is not content: the engine records revisions")
	}
	switch a.Kind {
	case html.AtomImage:
		if !document {
			return unsupported("img")
		}
	case html.AtomNote:
		if a.Endnote {
			return unsupported("endnote")
		}
		return unsupported("footnote")
	case html.AtomMilestone:
		return unsupported("comment-start")
	}
	return nil
}
