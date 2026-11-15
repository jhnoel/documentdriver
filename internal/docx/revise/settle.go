// Package revise is the docx revision core: Settle, the one engine that
// accepts or rejects tracked changes (for the projection's settled views,
// revision_settle and direct mode), and the Reviser, which authors an
// operation's revisions and keeps the ledger direct mode accepts.
package revise

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Options say what Settle settles and how.
type Options struct {
	// Accept accepts the selected revisions; otherwise they are rejected.
	Accept bool
	// Select picks the revision elements to settle; nil selects all. It
	// is asked about content wrappers (w:ins, w:del, w:moveTo,
	// w:moveFrom), paragraph-mark and row markers, cell markers
	// (w:cellIns, w:cellDel) and property-change records.
	Select func(rev *tree.Node) bool
	// Join, when set, is called before a paragraph mark's join moves
	// absorbed's content into survivor: the workspace records the join
	// and moves the identity (docx edit's JoinParagraphs).
	Join func(h *edit.Handle, survivor, absorbed *tree.Node) error
	// Moved, when set, is called when a join moves a paragraph's identity
	// to the next paragraph (docxedit.IdentityMoves): for a private copy,
	// whose views key paragraphs by identity.
	Moved func(from, to *tree.Node)
}

// Scope is what Settle settles in a part: its w:body, or its root element
// (a header, footer or note part).
func Scope(x *model.XML) *tree.Node {
	if b := x.Body(); b != nil {
		return b
	}
	return x.Root()
}

// ErrSettle wraps a tree operation that failed while settling.
var ErrSettle = errors.New("settle")

// wrap wraps err with the settle error, preserving nil.
func wrap(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrSettle, err)
}

// settler is one Settle run over one part.
type settler struct {
	h      *edit.Handle
	accept bool
	sel    func(*tree.Node) bool
	moved  func(from, to *tree.Node)
	join   func(h *edit.Handle, survivor, absorbed *tree.Node) error
	count  int
	// moves are the ids of settled move revisions, whose range markers
	// go with them.
	moves map[string]bool
	// short are rows that lost a cell, padded at the end.
	short []*tree.Node
}

// picked reports whether this settler selects node n.
func (s *settler) picked(n *tree.Node) bool { return s.sel == nil || s.sel(n) }

// remove removes element n; whitespace text is left where it is.
func (s *settler) remove(n *tree.Node) error {
	if n.Parent() == nil || n.Kind() != tree.ElementNode {
		return nil
	}
	return wrap(s.h.RemoveNode(n))
}

// Settle accepts or rejects the selected revisions under scope (a part's
// w:body, or its root element for a header, footer or note part), the one
// engine behind the projection's final and original views, revision_settle
// and the direct mode of tracked operations (docxdriver 0.5.2's
// accept_reject_all, accept_reject_one and accept_new_revisions, which
// differ from each other; see docs/design.md). It returns how many
// revisions it settled, as docxdriver counts them.
//
// The order is docxdriver's: table rows (a row that goes takes its
// revisions with it), content wrappers first first, paragraph marks last
// first (a join consumes the next paragraph), cells, property records;
// then rows that lost a cell are padded with w:gridAfter and the range
// markers of settled moves go. Every change goes through h, a region (a
// story, a range, or a private copy) whose write set covers the scope.
func Settle(h *edit.Handle, scope *tree.Node, o Options) (int, error) {
	s := &settler{h: h, accept: o.Accept, sel: o.Select, moved: o.Moved, join: o.Join, moves: map[string]bool{}}
	if scope == nil {
		return 0, nil
	}
	for _, step := range []func(*tree.Node) error{s.rows, s.content, s.marks, s.cells, s.properties, s.pad, s.moveMarkers} {
		if err := step(scope); err != nil {
			return s.count, err
		}
	}
	return s.count, nil
}

// noteMove records a settled move revision's id.
func (s *settler) noteMove(rev *tree.Node) {
	if model.IsLocal(rev, "moveFrom") || model.IsLocal(rev, "moveTo") {
		if id, ok := model.Attr(rev, "id"); ok {
			s.moves[id] = true
		}
	}
}

// ancestor returns the nearest ancestor of n (excluding n) that is IsW
// local, or nil.
func ancestor(n *tree.Node, local string) *tree.Node {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if model.IsW(p, local) {
			return p
		}
	}
	return nil
}

// firstDescendant returns scope's first descendant satisfying keep, or nil.
func firstDescendant(scope *tree.Node, keep func(*tree.Node) bool) *tree.Node {
	var found *tree.Node
	scope.Walk(func(n *tree.Node) bool {
		if found != nil {
			return false
		}
		if n != scope && keep(n) {
			found = n
			return false
		}
		return true
	})
	return found
}

// rows settles table row insertions and deletions: the row goes on a
// rejected insertion or an accepted deletion (and its table with its last
// row), otherwise only the marker does.
func (s *settler) rows(scope *tree.Node) error {
	for {
		row := firstDescendant(scope, func(n *tree.Node) bool {
			m := model.RowRevision(n)
			return model.IsW(n, "tr") && m != nil && s.picked(m)
		})
		if row == nil {
			return nil
		}
		s.count++
		if err := s.resolveRow(row); err != nil {
			return err
		}
	}
}

// resolveRow settles one row revision, removing the row or its marker.
func (s *settler) resolveRow(row *tree.Node) error {
	marker := model.RowRevision(row)
	if model.RowGoes(row, s.accept) {
		table := ancestor(row, "tbl")
		if err := s.remove(row); err != nil {
			return err
		}
		if table != nil && model.ChildW(table, "tr") == nil {
			return s.remove(table)
		}
		return nil
	}
	trpr := marker.Parent()
	if err := s.remove(marker); err != nil {
		return err
	}
	if trpr != nil && len(trpr.Elements()) == 0 && len(trpr.Attrs()) == 0 {
		return s.remove(trpr)
	}
	return nil
}

// firstWrapper returns the first selected content wrapper (not a
// paragraph-mark marker) inside story paragraph p, or nil.
func (s *settler) firstWrapper(p *tree.Node) *tree.Node {
	return firstDescendant(p, func(n *tree.Node) bool {
		return (model.IsRevIns(n) || model.IsRevDel(n)) && !model.InsidePPr(n, p) && s.picked(n)
	})
}

// content settles the content wrappers, first first, re-scanning after
// each: an accepted insertion or rejected deletion is unwrapped, the
// others go with their content. Resolving a wrapper never adds one before
// it, so the scan resumes at the paragraph that held it.
func (s *settler) content(scope *tree.Node) error {
	paras := model.ParagraphsIn(scope)
	for i := 0; i < len(paras); {
		w := s.firstWrapper(paras[i].Node)
		if w == nil {
			i++
			continue
		}
		s.count++
		s.noteMove(w)
		if err := s.resolveContent(w); err != nil {
			return err
		}
	}
	return nil
}

// resolveContent settles one content wrapper under accept or reject.
func (s *settler) resolveContent(w *tree.Node) error {
	if model.IsRevIns(w) != s.accept {
		return s.removeContent(w)
	}
	if !s.accept {
		// A rejected deletion: its deleted text and field code are live
		// again.
		for _, c := range slices.Clone(w.Children()) {
			if err := s.undeleteLeaves(c); err != nil {
				return err
			}
		}
	}
	return wrap(s.h.Unwrap(w))
}

// marks settles the selected paragraph-mark revisions, the last
// paragraph's first: a joined mark moves the paragraph's content to the
// front of the next paragraph, whose mark and identity win; a rejected
// inserted paragraph with nothing to join goes.
func (s *settler) marks(scope *tree.Node) error {
	mark := func(p *tree.Node) *tree.Node {
		if m := model.ParagraphMarkDel(p); m != nil && s.picked(m) {
			return m
		}
		if m := model.ParagraphMarkIns(p); m != nil && s.picked(m) {
			return m
		}
		return nil
	}
	paras := model.ParagraphsIn(scope)
	for _, para := range slices.Backward(paras) {
		p := para.Node
		for p.Parent() != nil {
			m := mark(p)
			if m == nil {
				break
			}
			s.count++
			s.noteMove(m)
			if err := s.resolveMark(p, m); err != nil {
				return err
			}
		}
	}
	return nil
}

// onlyProperties reports whether paragraph p holds only paragraph properties.
func onlyProperties(p *tree.Node) bool {
	for _, c := range p.Children() {
		if model.IsElement(c) && !model.IsW(c, "pPr") {
			return false
		}
	}
	return true
}

// resolveMark settles one paragraph mark revision, joining paragraphs when needed.
func (s *settler) resolveMark(p, m *tree.Node) error {
	ins := model.IsLocal(m, "ins") || model.IsLocal(m, "moveTo")
	join := s.accept != ins
	vanishes := !s.accept && model.RejectedInsertionVanishes(p) && onlyProperties(p)
	if err := s.remove(m); err != nil {
		return err
	}
	if vanishes {
		return s.remove(p)
	}
	if !join {
		return nil
	}
	next := model.ParagraphJoinTarget(p)
	if next == nil {
		return nil
	}
	if s.join != nil {
		if err := s.join(s.h, next, p); err != nil {
			return wrap(err)
		}
	}
	if s.moved != nil && docxedit.IdentityMoves(p, next) {
		s.moved(p, next)
	}
	var anchor *tree.Node
	for _, c := range next.Children() {
		if !model.IsW(c, "pPr") && c.Kind() == tree.ElementNode {
			anchor = c
			break
		}
	}
	for _, c := range slices.Clone(p.Children()) {
		if model.IsW(c, "pPr") || c.Kind() != tree.ElementNode {
			continue
		}
		var err error
		if anchor != nil {
			err = s.h.PutBefore(c, anchor)
		} else {
			err = s.h.Append(next, c)
		}
		if err != nil {
			return wrap(err)
		}
	}
	return s.remove(p)
}

// cells settles the selected tracked cell insertions and deletions: the
// marker sits in the cell's w:tcPr, and the cell goes on a rejected
// insertion or an accepted deletion.
func (s *settler) cells(scope *tree.Node) error {
	for {
		m := firstDescendant(scope, func(n *tree.Node) bool {
			return (model.IsW(n, "cellIns") || model.IsW(n, "cellDel")) && n.Parent() != nil &&
				model.IsW(n.Parent(), "tcPr") && s.picked(n)
		})
		if m == nil {
			return nil
		}
		s.count++
		if s.accept != model.IsW(m, "cellDel") {
			if err := s.remove(m); err != nil {
				return err
			}
			continue
		}
		cell := m.Parent().Parent()
		if cell == nil {
			if err := s.remove(m); err != nil {
				return err
			}
			continue
		}
		if err := s.removeCell(cell); err != nil {
			return err
		}
	}
}

// rowCells returns row's cells, through content controls, but not those
// of tables nested in them.
func rowCells(row *tree.Node) []*tree.Node {
	return model.Descendants(row, func(n *tree.Node) bool { return model.IsW(n, "tc") && ancestor(n, "tr") == row })
}

// removeCell removes a cell (and a content control holding only it); a row
// left without cells goes, and a table left without rows becomes an empty
// paragraph. A row that remains is padded at the end.
func (s *settler) removeCell(cell *tree.Node) error {
	row := ancestor(cell, "tr")
	target := cell
	for p := target.Parent(); p != nil && model.IsW(p, "sdtContent") && len(p.Elements()) == 1; p = target.Parent() {
		if p.Parent() != nil {
			target = p.Parent()
		} else {
			target = p
			break
		}
	}
	if err := s.remove(target); err != nil {
		return err
	}
	if row == nil {
		return nil
	}
	if len(rowCells(row)) > 0 {
		s.short = append(s.short, row)
		return nil
	}
	table := ancestor(row, "tbl")
	if err := s.remove(row); err != nil {
		return err
	}
	if table == nil || table.Parent() == nil {
		return nil
	}
	if model.Descendants(table, func(n *tree.Node) bool { return model.IsW(n, "tr") && ancestor(n, "tbl") == table }) != nil {
		return nil
	}
	// The table becomes an empty paragraph in its place.
	_, err := s.h.ReplaceWithNew(table, docxedit.W("p"))
	return wrap(err)
}

// pad raises the w:gridAfter of each row that lost a cell so that its
// cells and padding cover the table grid, the way Word writes a short row
// (revisions.rs pad_short_row).
func (s *settler) pad(*tree.Node) error {
	for _, row := range s.short {
		if err := s.padRow(row); err != nil {
			return err
		}
	}
	return nil
}

// gridValue returns child name's value, or def when it cannot be read.
func gridValue(parent *tree.Node, name string, def int64) int64 {
	if parent == nil {
		return def
	}
	n := model.ChildW(parent, name)
	if n == nil {
		return def
	}
	v, ok := model.Attr(n, "val")
	if !ok {
		return def
	}
	i, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil {
		return def
	}
	return i
}

// attached reports whether node n is attached to a document.
func attached(n *tree.Node) bool {
	for p := n; p != nil; p = p.Parent() {
		if p.Kind() == tree.DocumentNode {
			return true
		}
	}
	return false
}

// padRow pads a short row's grid coverage with w:gridAfter.
func (s *settler) padRow(row *tree.Node) error {
	table := ancestor(row, "tbl")
	if table == nil || !attached(row) {
		return nil
	}
	var columns int64
	if grid := model.ChildW(table, "tblGrid"); grid != nil {
		for _, c := range grid.Children() {
			if model.IsW(c, "gridCol") {
				columns++
			}
		}
	}
	trpr := model.ChildW(row, "trPr")
	after := gridValue(trpr, "gridAfter", 0)
	position := gridValue(trpr, "gridBefore", 0) + after
	for _, cell := range rowCells(row) {
		position += gridValue(model.ChildW(cell, "tcPr"), "gridSpan", 1)
	}
	if columns == 0 || position >= columns {
		return nil
	}
	return wrap(s.setGridAfter(row, trpr, after+columns-position))
}

// setGridAfter gives row the w:gridAfter value v, first in its w:trPr as
// Word writes a short row. CT_TrPrBase is a repeated choice the schema
// cannot place in, so the row's w:trPr is rebuilt (same name, same
// place) with the padding first and its other properties as they were.
func (s *settler) setGridAfter(row, trpr *tree.Node, v int64) error {
	if trpr == nil {
		n, err := s.h.EnsureIn(row, docxedit.W("trPr"))
		if err != nil {
			return err
		}
		trpr = n
	}
	var frag strings.Builder
	frag.WriteString(`<w:trPr><w:gridAfter w:val="` + strconv.FormatInt(v, 10) + `"/>`)
	var kept []*tree.Node
	for _, c := range trpr.Children() {
		if !model.IsW(c, "gridAfter") {
			frag.Write(c.Bytes())
			if c.Kind() == tree.ElementNode {
				kept = append(kept, c)
			}
		}
	}
	frag.WriteString(`</w:trPr>`)
	nodes, err := s.h.Workspace().Parse(row, []byte(frag.String()), nil)
	if err != nil {
		return err
	}
	if len(nodes) != 1 {
		return errors.New("revise: the rebuilt row properties did not parse as one element")
	}
	// The kept properties are copies (their faults stay theirs); the
	// padding is new.
	ws := s.h.Workspace()
	ws.RecordCopy(nodes[0], trpr)
	if els := nodes[0].Elements(); len(els) == len(kept)+1 {
		for i, c := range kept {
			ws.RecordCopy(els[i+1], c)
		}
	}
	return s.h.ReplaceWith(trpr, nodes[0])
}

// moveRangeMarkers are the milestones of a move's source and destination.
var moveRangeMarkers = []string{"moveFromRangeStart", "moveFromRangeEnd", "moveToRangeStart", "moveToRangeEnd"}

// moveMarkers removes move range markers: all of them when everything is
// settled, else those of the settled moves.
func (s *settler) moveMarkers(scope *tree.Node) error {
	markers := model.Descendants(scope, func(n *tree.Node) bool {
		if !slices.ContainsFunc(moveRangeMarkers, func(l string) bool { return model.IsW(n, l) }) {
			return false
		}
		if s.sel == nil {
			return true
		}
		id, ok := model.Attr(n, "id")
		return ok && s.moves[id]
	})
	for _, m := range markers {
		if err := s.remove(m); err != nil {
			return err
		}
	}
	return nil
}

// wName is the qualified name of WordprocessingML attribute local on an
// element at n: with the prefix bound to the main namespace there, or
// unqualified when that namespace is the default one.
func wName(n *tree.Node, local string) string {
	if p, ok := n.LookupNamespace(model.NSW); ok && p != "" {
		return p + ":" + local
	}
	return "w:" + local
}
