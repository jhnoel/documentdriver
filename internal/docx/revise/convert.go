package revise

import (
	"slices"
	"unicode"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Converted is what Convert did, by revision kind.
type Converted struct {
	// IDs are the w:id of every revision converted, in the order
	// converted.
	IDs                                        []string
	Inserted, Deleted, Marks, Rows, Formatting int
}

// Count is how many revisions were converted.
func (c Converted) Count() int { return len(c.IDs) }

// convEntry is one revision Convert may convert (revisions.rs
// RevisionEntry).
type convEntry struct {
	node, para   *tree.Node
	mark, row    bool
	content, ins bool // a content wrapper; an insertion (w:ins, w:moveTo)
}

// ConvertOptions say what Convert converts.
type ConvertOptions struct {
	// Select picks the revisions (nil: all).
	Select func(rev *tree.Node) bool
	// Paragraphs, when set, limits Convert to the revisions of these
	// paragraphs (and of the rows holding one); records outside
	// paragraphs (table, row, cell and section properties) are then left.
	Paragraphs map[*tree.Node]bool
}

// Convert settles the selected revisions under scope into amendment
// formatting (revisions.rs convert_in, 37 CFR 1.121): inserted text
// stays, underlined; deleted text comes back, struck through; text
// deleted inside a converted insertion goes (it was never in the
// source); paragraph marks and table rows stay, unmarked; property
// changes are accepted. It writes no revision. Every change goes through
// h, a region over scope.
func Convert(h *edit.Handle, scope *tree.Node, o ConvertOptions) (Converted, error) {
	var done Converted
	if scope == nil {
		return done, nil
	}
	picked := func(n *tree.Node) bool { return o.Select == nil || o.Select(n) }
	inScope := func(e convEntry) bool {
		switch {
		case o.Paragraphs == nil:
			return true
		case e.row:
			return model.Descendants(e.para, func(n *tree.Node) bool { return o.Paragraphs[n] }) != nil
		}
		return o.Paragraphs[e.para]
	}
	var entries []convEntry
	add := func(e convEntry) {
		if picked(e.node) && inScope(e) {
			entries = append(entries, e)
		}
	}
	for _, p := range model.ParagraphsIn(scope) {
		for _, w := range model.Descendants(p.Node, func(n *tree.Node) bool {
			return (model.IsRevIns(n) || model.IsRevDel(n)) && !model.InsidePPr(n, p.Node)
		}) {
			add(convEntry{node: w, para: p.Node, content: true, ins: model.IsRevIns(w)})
		}
		for _, m := range []*tree.Node{model.ParagraphMarkIns(p.Node), model.ParagraphMarkDel(p.Node)} {
			if m != nil {
				add(convEntry{node: m, para: p.Node, mark: true})
			}
		}
		for _, r := range model.Descendants(p.Node, isRecord) {
			add(convEntry{node: r, para: p.Node})
		}
	}
	for _, row := range model.Descendants(scope, func(n *tree.Node) bool { return model.IsW(n, "tr") }) {
		if m := model.RowRevision(row); m != nil {
			add(convEntry{node: m, para: row, row: true})
		}
	}
	chosen := map[*tree.Node]bool{}
	for _, e := range entries {
		chosen[e.node] = true
	}
	// Deletions inside an insertion converted here.
	doublyNew := map[*tree.Node]bool{}
	for _, e := range entries {
		if !e.content || e.ins {
			continue
		}
		for a := e.node.Parent(); a != nil; a = a.Parent() {
			if model.IsRevIns(a) && chosen[a] {
				doublyNew[e.node] = true
				break
			}
		}
	}
	gaps := replacementGaps(entries, doublyNew)
	moves := map[string]bool{}
	note := func(n *tree.Node) {
		if id, ok := model.Attr(n, "id"); ok {
			done.IDs = append(done.IDs, id)
		}
	}
	noteMove := func(n *tree.Node) {
		if model.IsLocal(n, "moveTo") || model.IsLocal(n, "moveFrom") {
			moves[model.AttrOr(n, "id")] = true
		}
	}
	s := &settler{h: h, accept: true, moves: map[string]bool{}}
	for _, e := range entries {
		if !e.content || !e.ins {
			continue
		}
		note(e.node)
		noteMove(e.node)
		if err := formatRuns(h, e.node, "u"); err != nil {
			return done, err
		}
		if err := h.Unwrap(e.node); err != nil {
			return done, wrap(err)
		}
		done.Inserted++
	}
	for _, e := range entries {
		if !e.content || e.ins {
			continue
		}
		note(e.node)
		noteMove(e.node)
		if doublyNew[e.node] {
			if err := s.removeContent(e.node); err != nil {
				return done, err
			}
		} else {
			for _, c := range slices.Clone(e.node.Children()) {
				if c.Kind() != tree.ElementNode {
					continue
				}
				if err := markLeaves(h, c, false); err != nil {
					return done, wrap(err)
				}
			}
			if err := formatRuns(h, e.node, "strike"); err != nil {
				return done, err
			}
			if err := h.Unwrap(e.node); err != nil {
				return done, wrap(err)
			}
		}
		done.Deleted++
	}
	for _, e := range entries {
		if !e.mark {
			continue
		}
		note(e.node)
		if err := s.remove(e.node); err != nil {
			return done, err
		}
		done.Marks++
	}
	for _, e := range entries {
		if !e.row {
			continue
		}
		note(e.node)
		trpr := e.node.Parent()
		if err := s.remove(e.node); err != nil {
			return done, err
		}
		if trpr != nil && len(trpr.Elements()) == 0 && len(trpr.Attrs()) == 0 {
			if err := s.remove(trpr); err != nil {
				return done, err
			}
		}
		done.Rows++
	}
	for _, e := range entries {
		if e.content || e.mark || e.row || rootOf(e.node) == nil {
			continue
		}
		note(e.node)
		if err := s.remove(e.node); err != nil {
			return done, err
		}
		done.Formatting++
	}
	// Table, row, cell and section records are not paragraph-anchored.
	if o.Paragraphs == nil {
		// One scan: accepting a record removes it with its snapshot,
		// which never brings another into view.
		records := model.Descendants(scope, func(n *tree.Node) bool {
			cell := (model.IsW(n, "cellIns") || model.IsW(n, "cellDel") || model.IsW(n, "cellMerge")) &&
				n.Parent() != nil && model.IsW(n.Parent(), "tcPr")
			return (isRecord(n) || cell) && picked(n)
		})
		for _, r := range records {
			if rootOf(r) == nil {
				continue // inside a record removed before it
			}
			note(r)
			// A cell insertion or deletion keeps its cell, like a row.
			if err := s.remove(r); err != nil {
				return done, err
			}
			done.Formatting++
		}
	}
	for _, g := range gaps {
		if err := plainSpace(h, g[0], g[1]); err != nil {
			return done, err
		}
	}
	if len(moves) > 0 {
		s.moves = moves
		s.sel = func(*tree.Node) bool { return true }
		if err := s.moveMarkers(scope); err != nil {
			return done, err
		}
	}
	return done, nil
}

// wrapperText is the text of n's live and deleted text leaves.
func wrapperText(n *tree.Node) []rune {
	var out []rune
	n.Walk(func(d *tree.Node) bool {
		if model.IsW(d, "t") || model.IsW(d, "delText") {
			out = append(out, []rune(d.Text())...)
		}
		return true
	})
	return out
}

// replacementGaps are the places where a converted deletion and a
// converted insertion meet with a word character on both sides
// (revisions.rs replacement_gaps): the redline of a replaced word shares
// one surrounding space between them, so converted they would run
// together. Each is the last run of the first and the first run of the
// second; a plain space goes between them.
func replacementGaps(entries []convEntry, removed map[*tree.Node]bool) [][2]*tree.Node {
	content := map[*tree.Node]bool{}
	var order []*tree.Node
	for _, e := range entries {
		if e.content && !removed[e.node] {
			content[e.node] = true
			order = append(order, e.node)
		}
	}
	word := func(r []rune, last bool) bool {
		if len(r) == 0 {
			return false
		}
		c := r[0]
		if last {
			c = r[len(r)-1]
		}
		return unicode.IsLetter(c) || unicode.IsNumber(c)
	}
	var gaps [][2]*tree.Node
	for _, left := range order {
		right := model.NextElement(left)
		if right == nil || !content[right] || model.IsRevIns(left) == model.IsRevIns(right) {
			continue
		}
		if !word(wrapperText(left), true) || !word(wrapperText(right), false) {
			continue
		}
		isRun := func(n *tree.Node) bool { return model.IsW(n, "r") }
		lr, rr := model.Descendants(left, isRun), model.Descendants(right, isRun)
		if len(lr) > 0 && len(rr) > 0 {
			gaps = append(gaps, [2]*tree.Node{lr[len(lr)-1], rr[0]})
		}
	}
	return gaps
}

// plainSpace puts one plain space run before right: left's run
// properties without the underline or strike conversion gave it
// (revisions.rs insert_plain_space).
func plainSpace(h *edit.Handle, left, right *tree.Node) error {
	frag := []byte(`<w:r>`)
	if rpr := model.ChildW(left, "rPr"); rpr != nil {
		var kept []byte
		for _, c := range rpr.Elements() {
			if !model.IsW(c, "u") && !model.IsW(c, "strike") && !model.IsW(c, "dstrike") && !model.IsW(c, "rPrChange") {
				kept = append(kept, c.Bytes()...)
			}
		}
		if len(kept) > 0 {
			frag = append(append(append(frag, `<w:rPr>`...), kept...), `</w:rPr>`...)
		}
	}
	frag = append(frag, `<w:t xml:space="preserve"> </w:t></w:r>`...)
	nodes, err := h.Workspace().Parse(right.Parent(), frag, map[string]string{"w": model.NSW})
	if err != nil {
		return wrap(err)
	}
	return wrap(h.PlaceAt(right.Parent(), nodes[0], right))
}

// formatRuns underlines (property "u") or strikes ("strike") every run
// in wrapper whose nearest revision wrapper it is, keeping an underline
// or strike the run already has (revisions.rs format_runs).
func formatRuns(h *edit.Handle, wrapper *tree.Node, property string) error {
	runs := model.Descendants(wrapper, func(n *tree.Node) bool {
		if !model.IsW(n, "r") {
			return false
		}
		for a := n.Parent(); a != nil; a = a.Parent() {
			if a == wrapper || model.IsRevIns(a) || model.IsRevDel(a) {
				return a == wrapper
			}
		}
		return false
	})
	on := func(n *tree.Node) bool {
		if n == nil {
			return false
		}
		v, _ := model.Attr(n, "val")
		return v != "0" && v != "false" && v != "off" && v != "none"
	}
	for _, run := range runs {
		rpr, err := h.EnsureIn(run, docxedit.W("rPr"))
		if err != nil {
			return wrap(err)
		}
		if property == "strike" {
			if on(model.ChildW(rpr, "strike")) || on(model.ChildW(rpr, "dstrike")) {
				continue
			}
			n, err := h.EnsureIn(rpr, docxedit.W("strike"))
			if err != nil {
				return wrap(err)
			}
			for _, a := range n.Attrs() {
				if a.Name == "val" || a.Name == wName(n, "val") {
					if err := h.RemoveNodeAttr(n, a.Name); err != nil {
						return wrap(err)
					}
				}
			}
			continue
		}
		if on(model.ChildW(rpr, "u")) {
			continue
		}
		n, err := h.EnsureIn(rpr, docxedit.W("u"))
		if err != nil {
			return wrap(err)
		}
		if err := h.SetNodeAttr(n, wName(n, "val"), "single"); err != nil {
			return wrap(err)
		}
	}
	return nil
}
