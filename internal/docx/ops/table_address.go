package ops

import (
	"errors"
	"strconv"
	"strings"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Table addresses (docs/contract.md, section 6): "table:t", "table:t/row:r",
// "table:t/row:r/cell:c" and "table:t/column:n", 1-based. A positional
// address is counted in the document as the operation finds it (tables
// of the main body in document order, nested ones included; a table's
// rows; a row's cells), so an earlier row.insert shifts the rows after
// it. An address that starts with an alias ("$t/row:2") names what the
// op that defined the alias bound, through the registry.

// tablePart is one component of a table address.
type tablePart struct {
	kind string // table, row, cell or column
	n    int    // 1-based
}

// parseTableAddress splits at into its components; the first may be an
// alias, returned as alias with n 0. The rules have checked the form.
func parseTableAddress(at string) (alias string, parts []tablePart, ok bool) {
	for i, p := range strings.Split(at, "/") {
		if i == 0 && strings.HasPrefix(p, "$") {
			alias = p
			continue
		}
		kind, num, found := strings.Cut(p, ":")
		n, err := strconv.Atoi(num)
		if !found || err != nil || n < 1 {
			return "", nil, false
		}
		parts = append(parts, tablePart{kind, n})
	}
	return alias, parts, true
}

// nthTable returns the n-th (1-based) table of the body, walking no
// further than it.
func nthTable(body *tree.Node, n int) *tree.Node {
	var found *tree.Node
	i := 0
	body.Walk(func(c *tree.Node) bool {
		if found != nil {
			return false
		}
		if c != body && model.IsW(c, "tbl") {
			i++
			if i == n {
				found = c
				return false
			}
		}
		return true
	})
	return found
}

// tableRows returns a table's rows (its w:tr children).
func tableRows(t *tree.Node) []*tree.Node { return childrenW(t, "tr") }

// rowCells returns a row's cells (its w:tc children).
func rowCells(r *tree.Node) []*tree.Node { return childrenW(r, "tc") }

// childrenW returns n's WordprocessingML children named local.
func childrenW(n *tree.Node, local string) []*tree.Node {
	var out []*tree.Node
	for _, c := range n.Elements() {
		if model.IsW(c, local) {
			out = append(out, c)
		}
	}
	return out
}

// tableTarget is a resolved table address: the table, and the row and
// cell when the address names them.
type tableTarget struct {
	table, row, cell *tree.Node
	// column is the 1-based grid column of a column address.
	column int
	// at is the address as the plan gave it, for messages.
	at string
}

// resolveTable resolves a table, row, cell or column address against the
// main document as it stands. A component past the end, or an alias no
// earlier op bound, is not_found at at; a table, row or cell an earlier
// operation removed is not_found too.
func (env *Env) resolveTable(at string) (*tableTarget, error) {
	alias, parts, ok := parseTableAddress(at)
	if !ok {
		return nil, reject("invalid_operation", "at", "invalid table address %q", at)
	}
	t := &tableTarget{at: at}
	var cur *tree.Node
	if alias != "" {
		// An alias binds the table, or a row or cell under it by suffix:
		// take the longest bound prefix.
		reg := env.WS.Registry()
		n := len(parts)
		for ; n >= 0; n-- {
			key := alias
			for _, p := range parts[:n] {
				key += "/" + p.kind + ":" + strconv.Itoa(p.n)
			}
			node, _, err := reg.Table(key)
			if err == nil {
				cur = node
				break
			}
			if errors.Is(err, docxedit.ErrDeletedHandle) {
				return nil, reject("not_found", "at", "%s names a table, row or cell an earlier operation removed", key)
			}
			if n == 0 {
				return nil, reject("not_found", "at", "unknown plan-local alias %s", alias)
			}
		}
		parts = parts[n:]
		switch {
		case model.IsW(cur, "tbl"):
			t.table = cur
		case model.IsW(cur, "tr"):
			t.row, t.table = cur, cur.Parent()
		case model.IsW(cur, "tc"):
			t.cell, t.row = cur, cur.Parent()
			if t.row != nil {
				t.table = t.row.Parent()
			}
		}
	} else {
		_, x, err := env.Main()
		if err != nil {
			return nil, err
		}
		body := x.Body()
		if body == nil || len(parts) == 0 || parts[0].kind != "table" {
			return nil, reject("not_found", "at", "table address %q is out of range", at)
		}
		t.table = nthTable(body, parts[0].n)
		if t.table == nil {
			return nil, reject("not_found", "at", "table address %q is out of range: the document has fewer tables", at)
		}
		parts = parts[1:]
	}
	for _, p := range parts {
		switch {
		case p.kind == "row" && t.table != nil && t.row == nil:
			rows := tableRows(t.table)
			if p.n > len(rows) {
				return nil, reject("not_found", "at", "row address %q is out of range: the table has %d rows", at, len(rows))
			}
			t.row = rows[p.n-1]
		case p.kind == "cell" && t.row != nil && t.cell == nil:
			cells := rowCells(t.row)
			if p.n > len(cells) {
				return nil, reject("not_found", "at", "cell address %q is out of range: the row has %d cells", at, len(cells))
			}
			t.cell = cells[p.n-1]
		case p.kind == "column" && t.table != nil && t.row == nil:
			t.column = p.n
		default:
			return nil, reject("not_found", "at", "table address %q does not name a table, row or cell", at)
		}
	}
	if t.table == nil {
		return nil, reject("not_found", "at", "table address %q does not name a table", at)
	}
	return t, nil
}
