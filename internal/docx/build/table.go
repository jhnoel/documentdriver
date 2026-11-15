package build

import (
	"errors"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// The table lowering (table_structure.rs insert): a new table is a grid
// of rows x columns cells, each holding one empty paragraph that the
// caller fills with Inline. Its properties are Word's for a new table
// without a style: an automatic preferred width and equal grid columns.

// DefaultColumnWidth is the width of a new table's grid columns, in
// twips (docxdriver's 2400, 1.67 inches).
const DefaultColumnWidth = 2400

// MaxTableSize bounds a new table's rows and columns (the plan rule).
const MaxTableSize = 100

// ErrTableSize is a table shape outside 1..MaxTableSize.
var ErrTableSize = errors.New("build: a new table has 1 to 100 rows and columns")

// NewTable returns a detached new table of rows x columns empty cells,
// parsed for placement under element at (a body or a cell), and its cell
// paragraphs in row-major order. The caller places it (Handle.PlaceAt)
// and fills the paragraphs.
func NewTable(h *core.Handle, at *tree.Node, rows, columns int) (*tree.Node, []*tree.Node, error) {
	if rows < 1 || rows > MaxTableSize || columns < 1 || columns > MaxTableSize {
		return nil, nil, ErrTableSize
	}
	var b strings.Builder
	b.WriteString(`<w:tbl><w:tblPr><w:tblW w:w="0" w:type="auto"/></w:tblPr><w:tblGrid>`)
	col := `<w:gridCol w:w="` + strconv.Itoa(DefaultColumnWidth) + `"/>`
	for range columns {
		b.WriteString(col)
	}
	b.WriteString(`</w:tblGrid>`)
	row := "<w:tr>" + strings.Repeat("<w:tc><w:p/></w:tc>", columns) + "</w:tr>"
	for range rows {
		b.WriteString(row)
	}
	b.WriteString(`</w:tbl>`)
	nodes, err := h.Workspace().Parse(at, []byte(b.String()), nsW)
	if err != nil {
		return nil, nil, err
	}
	if len(nodes) != 1 {
		return nil, nil, errors.New("build: a new table did not parse as one element")
	}
	t := nodes[0]
	var paras []*tree.Node
	for _, tr := range t.Elements() {
		if !model.IsW(tr, "tr") {
			continue
		}
		for _, tc := range tr.Elements() {
			if p := model.ChildW(tc, "p"); p != nil {
				paras = append(paras, p)
			}
		}
	}
	return t, paras, nil
}
