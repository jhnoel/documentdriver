package ops

import (
	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// DeleteTable is table.delete (docxdriver's edit_table).
type DeleteTable struct {
	At string `json:"at"`
}

// opTableDelete is table.delete's entry in the operation table.
var opTableDelete = OpDef{
	Name: "table.delete", Oracle: "edit_table", New: func() Spec { return &DeleteTable{} },
	Fixed: map[string]string{"action.kind": `"delete"`}, Rules: deleteTableRules,
}

// Run deletes a table (table_structure.rs edit, delete). Tracked, every
// row is a tracked row deletion with its content deleted, as Word records
// a deleted table, and rows the author inserted go outright (the table
// with the last of them); direct, the table is removed. A cell or text
// box the table leaves without a closing paragraph gets an empty one. A
// table without rows is refused in both modes: it has nothing a tracked
// deletion could mark (docxdriver removes it untracked).
func (o *DeleteTable) Run(env *Env) error {
	t, err := env.resolveTable(o.At)
	if err != nil {
		return err
	}
	if t.row != nil || t.column != 0 {
		return reject("not_found", "at", "%s does not name a table", o.At)
	}
	if err := env.guardTable(t.table, o.At); err != nil {
		return err
	}
	rows := tableRows(t.table)
	if len(rows) == 0 {
		return reject("unreadable_target", "at", "%s has no rows (w:tr): there is nothing a deletion could mark", o.At)
	}
	h, _, err := env.tableRegion(t.table, o.At)
	if err != nil {
		return err
	}
	container := t.table.Parent()
	if env.Mode == ModeDirect {
		if err := h.RemoveNode(t.table); err != nil {
			return err
		}
	} else {
		// The story keeps a closing paragraph once the table goes: a tracked
		// inserted one, which rejecting removes with the deletion.
		if !model.IsW(container, "body") && lastBlock(container) == t.table {
			if err := closeStory(env, h, container, true); err != nil {
				return err
			}
		}
		for _, tr := range rows {
			if err := env.deleteRowTracked(h, tr); err != nil {
				return err
			}
		}
	}
	if t.table.Parent() == nil {
		if err := closeStory(env, h, container, false); err != nil {
			return err
		}
	}
	return env.refreshTables()
}

// closeStory gives a cell or text box that a removed table left without
// a closing paragraph (a story other than the body must end with one) an
// empty paragraph.
func closeStory(env *Env, h *core.Handle, c *tree.Node, tracked bool) error {
	if c == nil || model.IsW(c, "body") {
		return nil
	}
	if last := lastBlock(c); last != nil && model.IsW(last, "p") {
		return nil
	}
	p, err := emptyParagraph(env, c)
	if err != nil {
		return err
	}
	if _, ok := docxedit.Type(c); ok {
		err = h.PlaceAt(c, p, nil)
	} else {
		// A text box's content (w:txbxContent under a DrawingML or VML
		// shape) has no tabulated parent: the paragraph goes in at the end.
		main, _, merr := env.Main()
		if merr != nil {
			return merr
		}
		err = env.WS.Core().Insert(main, core.Into(c), p, nil)
	}
	if err != nil || !tracked {
		return err
	}
	_, err = env.Rev.MarkParagraph(h, p, "ins")
	return err
}

// lastBlock returns c's last paragraph or table child, or nil.
func lastBlock(c *tree.Node) *tree.Node {
	els := c.Elements()
	for i := len(els) - 1; i >= 0; i-- {
		if model.IsW(els[i], "p") || model.IsW(els[i], "tbl") {
			return els[i]
		}
	}
	return nil
}
