package main

import (
	"regexp"
	"strconv"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Cell addresses and grid columns. The contract's cell.merge and
// cell.split take table:t/row:r/cell:c, c the cell's position in its row
// (the td read shows); docxdriver's merge_cells and split_cell take the
// row and a grid column, the cell being the one that starts there
// (table_structure.rs at_column: the sum of the gridSpans of the cells
// before it, from 1, with w:gridBefore not counted). Without the document
// the two cannot be told apart (ops.FromOracle carries the number across),
// so the oracle's plan is translated with the source document in hand:
// gridColumns rewrites each cell.merge and cell.split address to the grid
// column its cell starts at before downTranslate moves it into the
// action. A cell the source does not have is left as it is, for the
// oracle to refuse.
//
// The grid is the source document's: a plan whose earlier operations
// change that row's spans (a merge, then a split of a later cell of the
// same row) is translated against the row as it was. The recorded plans
// split only the first cell of a row after such a merge, whose grid
// column a merge before it does not change.

var cellAt = regexp.MustCompile(`^(table:(\d+)/row:(\d+)/cell:)(\d+)$`)

// gridColumns rewrites the cell addresses of a contract plan's cell.merge
// and cell.split operations to grid columns of the source document src.
func gridColumns(plan string, src []byte) (string, error) {
	var x *model.XML
	return mapOps(plan, func(op *jv) (*jv, error) {
		name := op.get("op")
		at := op.get("at")
		if name == nil || name.kind != 's' || (name.s != "cell.merge" && name.s != "cell.split") || at == nil || at.kind != 's' {
			return op, nil
		}
		m := cellAt.FindStringSubmatch(at.s)
		if m == nil {
			return op, nil
		}
		if x == nil {
			d, err := model.OpenDocument(src)
			if err != nil {
				return op, nil // the oracle refuses the source itself
			}
			x = d.XML
		}
		t, _ := strconv.Atoi(m[2])
		r, _ := strconv.Atoi(m[3])
		c, _ := strconv.Atoi(m[4])
		if col, ok := gridColumnOf(x, t, r, c); ok {
			op = clone(op)
			op.put("at", str(m[1]+strconv.Itoa(col)))
		}
		return op, nil
	})
}

// gridColumnOf is the grid column (docxdriver's, from 1) that cell c of
// row r of body table t starts at.
func gridColumnOf(x *model.XML, t, r, c int) (int, bool) {
	body := x.Body()
	if body == nil || t < 1 || r < 1 || c < 1 {
		return 0, false
	}
	var tbl *tree.Node
	i := 0
	body.Walk(func(n *tree.Node) bool {
		if tbl != nil {
			return false
		}
		if n != body && model.IsW(n, "tbl") {
			if i++; i == t {
				tbl = n
				return false
			}
		}
		return true
	})
	if tbl == nil {
		return 0, false
	}
	row := nthChildW(tbl, "tr", r)
	if row == nil {
		return 0, false
	}
	col := 1
	for k, tc := range childrenOf(row, "tc") {
		if k == c-1 {
			return col, true
		}
		col += spanOf(tc)
	}
	return 0, false
}

// childrenOf returns n's child elements in the w namespace with the given local name.
func childrenOf(n *tree.Node, local string) []*tree.Node {
	var out []*tree.Node
	for _, c := range n.Elements() {
		if model.IsW(c, local) {
			out = append(out, c)
		}
	}
	return out
}

// nthChildW returns the k-th child of n with local name, or nil if fewer.
func nthChildW(n *tree.Node, local string, k int) *tree.Node {
	cs := childrenOf(n, local)
	if k > len(cs) {
		return nil
	}
	return cs[k-1]
}

// spanOf is a cell's w:gridSpan as docxdriver reads it (1 when absent or
// unreadable).
func spanOf(tc *tree.Node) int {
	pr := model.ChildW(tc, "tcPr")
	if pr == nil {
		return 1
	}
	gs := model.ChildW(pr, "gridSpan")
	if gs == nil {
		return 1
	}
	v, err := strconv.ParseUint(model.AttrOr(gs, "val"), 10, 32)
	if err != nil {
		return 1 // an unreadable span counts as 1, a zero as 0 (Rust's usize parse)
	}
	return int(v)
}
