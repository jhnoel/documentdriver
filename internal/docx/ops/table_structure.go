package ops

import (
	"bytes"
	"fmt"
	"slices"
	"strconv"
	"strings"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Shared machinery of the structural table operations (docxdriver's
// table_structure.rs): the guard that keeps them to tables they can edit
// safely, new rows and cells built from the ones beside them, and the
// merge properties of a cell.

// structuralLimit is the most rows or grid columns an edit may leave a
// table with.
const structuralLimit = 1000

// wrappedBy are the containers a table must not sit in: a structural edit
// inside a revision, a content control, custom XML or an mc branch would
// edit one copy of content that has several.
var wrappedBy = []string{"ins", "del", "moveFrom", "moveTo", "sdt", "sdtContent", "customXml", "AlternateContent"}

// heldMarkup are the elements a table must not hold for a structural edit:
// ranges, fields and controls whose ends a moved or removed cell would
// separate, and pending property, move and cell revisions whose snapshot
// no longer matches what the edit leaves.
var heldMarkup = []string{
	"moveFrom", "moveTo", "bookmarkStart", "bookmarkEnd", "commentRangeStart", "commentRangeEnd",
	"commentReference", "fldChar", "fldSimple", "sdt", "customXml", "tblPrChange", "tblGridChange",
	"trPrChange", "tcPrChange", "pPrChange", "rPrChange", "cellIns", "cellDel", "cellMerge", "AlternateContent",
}

// guardTable refuses a structural edit of table t, the same way in both
// change modes: a table inside a revision or control wrapper, one that
// holds range markup, fields, controls or pending property, move or cell
// revisions, and one that holds another author's pending insertion or
// deletion. The author's own pending insertions and deletions are allowed
// (a plan may insert rows and then edit them). docxdriver refuses every
// pending insertion or deletion in direct mode and only other authors' in
// track mode.
func (env *Env) guardTable(t *tree.Node, at string) error {
	for a := t.Parent(); a != nil; a = a.Parent() {
		if slices.ContainsFunc(wrappedBy, func(l string) bool { return model.IsLocal(a, l) }) {
			return reject("operation_failed", "at", "%s is inside a w:%s: structural table edits inside revision or content-control wrappers are not supported", at, a.Local())
		}
	}
	var held, foreign *tree.Node
	t.Walk(func(n *tree.Node) bool {
		if held != nil || n == t || !model.IsElement(n) {
			return held == nil
		}
		if slices.ContainsFunc(heldMarkup, func(l string) bool { return model.IsLocal(n, l) }) {
			held = n
			return false
		}
		if foreign == nil && (model.IsW(n, "ins") || model.IsW(n, "del")) && model.AttrOr(n, "author") != env.Author {
			foreign = n
		}
		return true
	})
	if held != nil {
		return reject("operation_failed", "at", "%s holds a %s: structural table edits need settled revisions and no bookmarks, comments, fields or content controls in the table", at, held.Name())
	}
	if foreign != nil {
		return reject("operation_failed", "at", "%s holds another author's pending %s: accept or reject it before a structural edit", at, foreign.Name())
	}
	return nil
}

// regularTable lays t on its grid and requires a plain rectangle (no
// merged cells, no skipped grid columns, the same cells in every row).
func regularTable(t *tree.Node, at string) (*tableGrid, int, error) {
	g := readGrid(t)
	width, ok := g.regular()
	if !ok {
		return nil, 0, reject("operation_failed", "at", "%s is not a plain grid: this edit needs a rectangular table without merged cells or omitted cells (split merged cells first)", at)
	}
	return g, width, nil
}

// prefixOf is the prefix element n is written with, with its colon ("w:"),
// or "".
func prefixOf(n *tree.Node) string {
	if i := strings.IndexByte(n.Name(), ':'); i >= 0 {
		return n.Name()[:i+1]
	}
	return ""
}

// fragment parses markup to place under or beside element at (prefixes
// resolve as at has them).
func fragment(env *Env, at *tree.Node, markup string) (*tree.Node, error) {
	nodes, err := env.WS.Core().Parse(at, []byte(markup), nil)
	if err != nil {
		return nil, fmt.Errorf("docx: new table markup does not parse: %w", err)
	}
	if len(nodes) != 1 {
		return nil, fmt.Errorf("docx: new table markup parsed as %d elements", len(nodes))
	}
	return nodes[0], nil
}

// stripped returns the children of property container c to copy into a
// new row or cell: all but the revision marks and property changes
// (leave), and their bytes.
func stripped(c *tree.Node, leave ...string) ([]*tree.Node, []byte) {
	if c == nil {
		return nil, nil
	}
	var kept []*tree.Node
	var b bytes.Buffer
	for _, k := range c.Elements() {
		if slices.ContainsFunc(leave, func(l string) bool { return model.IsW(k, l) }) {
			continue
		}
		kept = append(kept, k)
		b.Write(k.Bytes())
	}
	return kept, b.Bytes()
}

// container writes property container c (whose kept children are given)
// with its own start tag, or nothing when it keeps none.
func containerMarkup(c *tree.Node, kept []*tree.Node, inner []byte) string {
	if c == nil || len(kept) == 0 {
		return ""
	}
	return "<" + c.Name() + ">" + string(inner) + "</" + c.Name() + ">"
}

// recordCopied marks the kept children of a copied property container as
// copies of their originals, so that a fault one already had stays the
// input's (the integrity gate).
func recordCopied(env *Env, copied *tree.Node, kept []*tree.Node) {
	if copied != nil && len(kept) > 0 {
		env.WS.Core().RecordCopies(copied, kept)
	}
}

// blankCell builds a new empty cell like cell src (docxdriver's
// blank_like): src's properties, without their pending change and cell
// revisions, and one empty paragraph. It is detached.
func blankCell(env *Env, src *tree.Node) (*tree.Node, error) {
	p := prefixOf(src)
	tcPr := model.ChildW(src, "tcPr")
	kept, inner := stripped(tcPr, "tcPrChange", "cellIns", "cellDel", "cellMerge")
	c, err := fragment(env, src, "<"+p+"tc>"+containerMarkup(tcPr, kept, inner)+"<"+p+"p/></"+p+"tc>")
	if err != nil {
		return nil, err
	}
	recordCopied(env, model.ChildW(c, "tcPr"), kept)
	return c, nil
}

// blankRow builds a new row like row src: its properties, without its
// revision marks and pending change, and a blank cell like each of its
// first width cells. It is detached; cells are its new cells.
func blankRow(env *Env, src *tree.Node, width int) (row *tree.Node, cells []*tree.Node, err error) {
	p := prefixOf(src)
	trPr := model.ChildW(src, "trPr")
	keptRow, innerRow := stripped(trPr, "ins", "del", "trPrChange")
	var b strings.Builder
	b.WriteString("<" + p + "tr>" + containerMarkup(trPr, keptRow, innerRow))
	type copied struct{ kept []*tree.Node }
	var cellKept []copied
	for i, tc := range rowCells(src) {
		if i >= width {
			break
		}
		tcPr := model.ChildW(tc, "tcPr")
		kept, inner := stripped(tcPr, "tcPrChange", "cellIns", "cellDel", "cellMerge")
		cellKept = append(cellKept, copied{kept})
		b.WriteString("<" + p + "tc>" + containerMarkup(tcPr, kept, inner) + "<" + p + "p/></" + p + "tc>")
	}
	b.WriteString("</" + p + "tr>")
	row, err = fragment(env, src, b.String())
	if err != nil {
		return nil, nil, err
	}
	recordCopied(env, model.ChildW(row, "trPr"), keptRow)
	cells = rowCells(row)
	for i, c := range cells {
		if i < len(cellKept) {
			recordCopied(env, model.ChildW(c, "tcPr"), cellKept[i].kept)
		}
	}
	return row, cells, nil
}

// emptyParagraph builds a new empty paragraph for cell tc.
func emptyParagraph(env *Env, tc *tree.Node) (*tree.Node, error) {
	return fragment(env, tc, "<"+prefixOf(tc)+"p/>")
}

// setMerge gives cell tc the merge properties of one cell of a merged
// block (table_structure.rs merge_props): w:gridSpan for a block wider
// than one column, w:vMerge (restart for the top row, continue below) for
// one taller than one row, neither for a single cell; and a preferred
// width that no longer holds becomes auto.
func setMerge(h *core.Handle, tc *tree.Node, span int, vertical *bool) error {
	tcPr, err := h.EnsureIn(tc, docxedit.W("tcPr"))
	if err != nil {
		return err
	}
	for _, l := range []string{"gridSpan", "vMerge", "hMerge"} {
		for _, c := range childrenW(tcPr, l) {
			if err := h.RemoveNode(c); err != nil {
				return err
			}
		}
	}
	w := propWriter{h, tcPr}
	if span > 1 {
		if err := w.val("gridSpan", strconv.Itoa(span)); err != nil {
			return err
		}
	}
	if vertical != nil {
		v := "continue"
		if *vertical {
			v = "restart"
		}
		if err := w.val("vMerge", v); err != nil {
			return err
		}
	}
	if tcW := model.ChildW(tcPr, "tcW"); tcW != nil {
		if err := w.set(tcW, "w", "0"); err != nil {
			return err
		}
		if err := w.set(tcW, "type", "auto"); err != nil {
			return err
		}
	}
	if len(tcPr.Elements()) == 0 && len(tcPr.Attrs()) == 0 {
		return h.RemoveNode(tcPr)
	}
	return nil
}

// tableRegion opens a region over table t: its rows, cells and
// properties may change, and t itself may go.
// A table in a story that is not itself a unit (a text box inside a
// paragraph) is part of that paragraph's own content, so the region is
// the nearest enclosing unit.
func (env *Env) tableRegion(t *tree.Node, label string) (*core.Handle, string, error) {
	main, _, err := env.Main()
	if err != nil {
		return nil, "", err
	}
	u := t
	if c := t.Parent(); c != nil && !docxedit.IsUnit(c) {
		for a := c; a != nil; a = a.Parent() {
			if docxedit.IsUnit(a) {
				u = a
				break
			}
		}
	}
	h, err := env.WS.Core().Range(main, label, u, u)
	if err != nil {
		return nil, "", reject("unreadable_target", "at", "%s cannot be edited as a block: %v", label, err)
	}
	return h, main, nil
}

// refreshTables recomputes every table address after a structural edit
// (the registry's positions, which the plan's aliases report).
func (env *Env) refreshTables() error { return env.WS.Registry().Refresh() }

// bindInserted binds the alias name of a row or column insertion: "$name"
// is claimed, and "$name/<suffix>" names each inserted row or cell.
func (env *Env) bindInserted(name *string, nodes map[string]*tree.Node) error {
	if name == nil {
		return nil
	}
	reg := env.WS.Registry()
	if err := reg.DefineAlias(*name, "$"+*name); err != nil {
		return reject("invalid_operation", "as", "%s", err.Error())
	}
	reg.BindTableSuffixes(*name, nodes)
	return nil
}

// deleteRowTracked records the deletion of row tr: the live content of
// each of its paragraphs goes into w:del and the row is marked deleted
// (w:trPr/w:del). A row the author inserted and has not settled is
// removed instead (Word's rule; the Reviser's MarkRow).
func (env *Env) deleteRowTracked(h *core.Handle, tr *tree.Node) error {
	if m := model.RowRevision(tr); m != nil && model.IsW(m, "ins") && model.AttrOr(m, "author") == env.Author {
		_, err := env.Rev.MarkRow(h, tr, "del")
		return err
	}
	for _, p := range model.Descendants(tr, func(n *tree.Node) bool { return model.IsW(n, "p") }) {
		if err := env.Rev.DeleteContent(h, p); err != nil {
			return reject("operation_failed", "at", "a paragraph of the row cannot be deleted as a tracked change: %v", err)
		}
	}
	_, err := env.Rev.MarkRow(h, tr, "del")
	return err
}

// appendParagraph ends cell tc with a new empty paragraph.
func appendParagraph(env *Env, h *core.Handle, tc *tree.Node) error {
	p, err := emptyParagraph(env, tc)
	if err != nil {
		return err
	}
	return h.PlaceAt(tc, p, nil)
}
