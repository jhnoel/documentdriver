package revise

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Mode is a plan's change mode.
type Mode uint8

const (
	// Track records every change as a revision.
	Track Mode = iota
	// Direct makes the change: the operation is authored as in Track and
	// the Reviser then accepts exactly the revisions it authored.
	Direct
)

// DateLayout is how a revision's w:date is written: UTC, whole seconds,
// as Word writes it.
const DateLayout = "2006-01-02T15:04:05Z"

// Reviser authors the revisions of one operation and keeps a ledger of
// them: the revision elements it created, nothing else. Finish gives them
// ids (Track) or accepts exactly them (Direct). Revisions already in the
// document, of any author, are never in the ledger, so direct mode never
// settles them (docxdriver's accept_new_revisions picked "new" revisions
// by author and node age, and settled split copies of existing ones).
//
// It authors three kinds of revision, the forms Word writes:
//
//   - content wrappers: Insert (w:ins around placed content) and Delete
//     (w:del, its text becoming deleted text);
//   - marks: MarkParagraph (a paragraph mark inserted or deleted, in
//     w:pPr/w:rPr) and MarkRow (a table row, in w:trPr);
//   - property records: Property (w:pPrChange, w:rPrChange, w:tblPrChange,
//     w:trPrChange, w:tcPrChange, w:sectPrChange, w:tblGridChange).
//
// The author's own pending insertion is edited, not revised (Word's
// rule): deleting content inside the author's own w:ins removes it, and
// deleting a row the author inserted removes the row.
type Reviser struct {
	author string
	date   string
	mode   Mode
	ledger []authored
	// join is called before a join the reviser makes moves content
	// (SetJoin).
	join func(h *edit.Handle, survivor, absorbed *tree.Node) error
	// shells are the property containers the reviser created to hold a
	// mark (w:pPr, its w:rPr, w:trPr), innermost first: direct mode
	// removes those the accepted mark leaves empty. Containers that were
	// already there belong to the source and stay, empty or not.
	shells []shell
	// visited counts the nodes Finish's numbering walks visited: one
	// walk of each part per Finish, which the tests hold it to.
	visited int
}

// shell is a property container the reviser created, and its editor.
type shell struct {
	h *edit.Handle
	n *tree.Node
}

// authored is one ledger entry: a revision element and the editor of its
// part.
type authored struct {
	h *edit.Handle
	n *tree.Node
}

// New returns a Reviser for author, stamping now, in mode.
func New(author string, now time.Time, mode Mode) *Reviser {
	return &Reviser{author: author, date: now.UTC().Format(DateLayout), mode: mode}
}

// Mode returns the reviser's change mode.
func (r *Reviser) Mode() Mode { return r.mode }

// Ledger returns the revision elements authored so far, in order.
func (r *Reviser) Ledger() []*tree.Node {
	out := make([]*tree.Node, len(r.ledger))
	for i, a := range r.ledger {
		out[i] = a.n
	}
	return out
}

// ErrRevise wraps a failed authoring step.
var ErrRevise = errors.New("revise")

// attrs are the revision attributes of a new revision element at n (w:id
// first, as Word writes them; Finish gives the id).
func (r *Reviser) attrs(n *tree.Node) [][2]string {
	return [][2]string{{wName(n, "id"), "0"}, {wName(n, "author"), r.author}, {wName(n, "date"), r.date}}
}

// note records an authored revision element in the ledger.
func (r *Reviser) note(h *edit.Handle, n *tree.Node) { r.ledger = append(r.ledger, authored{h, n}) }

// mark places revision element w:kind under container c where the schema
// puts it, stamps it and records it. c must not have one already.
func (r *Reviser) mark(h *edit.Handle, c *tree.Node, kind string) (*tree.Node, error) {
	if model.ChildW(c, kind) != nil {
		return nil, fmt.Errorf("%w: %s already has a w:%s", ErrRevise, c.Name(), kind)
	}
	m, err := h.EnsureIn(c, docxedit.W(kind))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRevise, err)
	}
	for _, a := range r.attrs(c) {
		if err := h.SetNodeAttr(m, a[0], a[1]); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrRevise, err)
		}
	}
	r.note(h, m)
	return m, nil
}

// ownInsertion returns the nearest w:ins (or w:moveTo) at or above n,
// below stop, that the author wrote, or nil.
func (r *Reviser) ownInsertion(n, stop *tree.Node) *tree.Node {
	for a := n; a != nil && a != stop; a = a.Parent() {
		if model.IsRevIns(a) && model.AttrOr(a, "author") == r.author {
			return a
		}
		if model.IsRevDel(a) || model.IsW(a, "p") {
			return nil
		}
	}
	return nil
}

// Insert wraps nodes, consecutive siblings already placed in a paragraph
// (runs, hyperlinks, fields), in one w:ins and returns it.
func (r *Reviser) Insert(h *edit.Handle, nodes []*tree.Node) (*tree.Node, error) {
	if err := contiguous(nodes); err != nil {
		return nil, err
	}
	return r.wrap(h, "ins", nodes)
}

// wrap wraps nodes in a new revision element w:kind and records it.
func (r *Reviser) wrap(h *edit.Handle, kind string, nodes []*tree.Node) (*tree.Node, error) {
	w, err := h.Wrap(nodes[0], nodes[len(nodes)-1], docxedit.W(kind), r.attrs(nodes[0]))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRevise, err)
	}
	r.note(h, w)
	return w, nil
}

// contiguous checks that nodes are consecutive siblings (whitespace text
// between them allowed).
func contiguous(nodes []*tree.Node) error {
	if len(nodes) == 0 {
		return fmt.Errorf("%w: nothing to wrap", ErrRevise)
	}
	p := nodes[0].Parent()
	if p == nil {
		return fmt.Errorf("%w: content is not placed", ErrRevise)
	}
	kids := p.Children()
	i := slices.Index(kids, nodes[0])
	for _, n := range nodes[1:] {
		j := slices.Index(kids, n)
		if j <= i {
			return fmt.Errorf("%w: content is not contiguous", ErrRevise)
		}
		for _, between := range kids[i+1 : j] {
			if model.IsElement(between) {
				return fmt.Errorf("%w: content is not contiguous", ErrRevise)
			}
		}
		i = j
	}
	return nil
}

// Delete records the deletion of nodes, consecutive siblings in a
// paragraph: they go into one w:del (several when the author's own
// insertions split them), and their text and field code become deleted
// text. Content inside the author's own pending insertion is removed
// instead, and an insertion it empties goes too.
func (r *Reviser) Delete(h *edit.Handle, nodes []*tree.Node) error {
	if err := contiguous(nodes); err != nil {
		return err
	}
	var group []*tree.Node
	flush := func() error {
		if len(group) == 0 {
			return nil
		}
		w, err := r.wrap(h, "del", group)
		if err != nil {
			return err
		}
		group = nil
		return markLeaves(h, w, true)
	}
	for _, n := range nodes {
		own := r.ownInsertion(n, nil)
		if own == nil {
			group = append(group, n)
			continue
		}
		if err := flush(); err != nil {
			return err
		}
		if err := h.RemoveNode(n); err != nil {
			return fmt.Errorf("%w: %w", ErrRevise, err)
		}
		if own != n && own.Parent() != nil && len(own.Elements()) == 0 {
			if err := h.RemoveNode(own); err != nil {
				return fmt.Errorf("%w: %w", ErrRevise, err)
			}
		}
	}
	return flush()
}

// container returns e's property container w:local, creating it where
// the schema puts it when e has none and remembering that it did.
func (r *Reviser) container(h *edit.Handle, e *tree.Node, local string) (*tree.Node, error) {
	if c := model.ChildW(e, local); c != nil {
		return c, nil
	}
	c, err := h.EnsureIn(e, docxedit.W(local))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRevise, err)
	}
	// Innermost first: a w:rPr is created after its w:pPr.
	r.shells = append([]shell{{h, c}}, r.shells...)
	return c, nil
}

// dropEmptyShells removes the containers the reviser created that are
// still in the document with no attributes and nothing but whitespace
// in them.
func (r *Reviser) dropEmptyShells() error {
	for _, s := range r.shells {
		if rootOf(s.n) == nil || len(s.n.Attrs()) > 0 || len(s.n.Elements()) > 0 {
			continue
		}
		if err := s.h.RemoveNode(s.n); err != nil {
			return fmt.Errorf("%w: %w", ErrRevise, err)
		}
	}
	return nil
}

// MarkParagraph records that paragraph p's mark was inserted (kind "ins")
// or deleted ("del"), in p's w:pPr/w:rPr, creating them as needed. A
// deleted mark the author inserted is not recorded: the paragraphs join
// at once.
func (r *Reviser) MarkParagraph(h *edit.Handle, p *tree.Node, kind string) (*tree.Node, error) {
	if kind != "ins" && kind != "del" {
		return nil, fmt.Errorf("%w: paragraph mark kind %q", ErrRevise, kind)
	}
	if own := model.ParagraphMarkIns(p); kind == "del" && own != nil && model.AttrOr(own, "author") == r.author {
		_, err := Settle(h, p.Parent(), Options{Select: func(n *tree.Node) bool { return n == own }, Join: r.join})
		return nil, err
	}
	ppr, err := r.container(h, p, "pPr")
	if err != nil {
		return nil, err
	}
	rpr, err := r.container(h, ppr, "rPr")
	if err != nil {
		return nil, err
	}
	return r.mark(h, rpr, kind)
}

// MarkRow records that table row tr was inserted ("ins") or deleted
// ("del"), in its w:trPr. Deleting a row the author inserted removes it
// (and its table with its last row).
func (r *Reviser) MarkRow(h *edit.Handle, tr *tree.Node, kind string) (*tree.Node, error) {
	if kind != "ins" && kind != "del" {
		return nil, fmt.Errorf("%w: row mark kind %q", ErrRevise, kind)
	}
	if m := model.RowRevision(tr); kind == "del" && m != nil && model.IsW(m, "ins") && model.AttrOr(m, "author") == r.author {
		_, err := Settle(h, tr.Parent(), Options{Select: func(n *tree.Node) bool { return n == m }})
		return nil, err
	}
	trpr, err := r.container(h, tr, "trPr")
	if err != nil {
		return nil, err
	}
	return r.mark(h, trpr, kind)
}

// recordOf is the property-change record of each property container, and
// the container's children a snapshot leaves out (they have their own
// revisions or are not base properties).
var recordOf = map[string]struct {
	record string
	keep   []string
}{
	"pPr":     {"pPrChange", []string{"rPr", "sectPr", "pPrChange"}},
	"rPr":     {"rPrChange", []string{"rPrChange", "ins", "del", "moveFrom", "moveTo"}},
	"tblPr":   {"tblPrChange", []string{"tblPrChange"}},
	"trPr":    {"trPrChange", []string{"trPrChange", "ins", "del"}},
	"tcPr":    {"tcPrChange", []string{"tcPrChange", "cellIns", "cellDel", "cellMerge"}},
	"sectPr":  {"sectPrChange", []string{"headerReference", "footerReference", "sectPrChange"}},
	"tblGrid": {"tblGridChange", []string{"tblGridChange"}},
}

// base returns the children of property container bag that a snapshot
// holds.
func base(bag *tree.Node, keep []string) []*tree.Node {
	var out []*tree.Node
	for _, c := range bag.Elements() {
		if !slices.ContainsFunc(keep, func(l string) bool { return model.IsW(c, l) }) {
			out = append(out, c)
		}
	}
	return out
}

// baseBytes returns the base property nodes' bytes excluding keep.
func baseBytes(bag *tree.Node, keep []string) []byte {
	var b bytes.Buffer
	for _, c := range base(bag, keep) {
		b.Write(c.Bytes())
	}
	return b.Bytes()
}

// Property runs mutate, which changes the properties in container bag (a
// w:pPr, w:rPr, w:tblPr, w:trPr, w:tcPr, w:sectPr or w:tblGrid), and
// records the change: a snapshot of the base properties before it, in the
// container's property-change record. Nothing is recorded when mutate
// leaves the base properties as they were, or when the container already
// has a record (its snapshot already holds the properties before any
// pending change). It reports whether the properties changed.
func (r *Reviser) Property(h *edit.Handle, bag *tree.Node, mutate func() error) (bool, error) {
	spec, ok := recordOf[bag.Local()]
	if !ok || !model.IsW(bag, bag.Local()) {
		return false, fmt.Errorf("%w: %s is not a property container", ErrRevise, bag.Name())
	}
	before := baseBytes(bag, spec.keep)
	// The base elements as they stand: the snapshot is their copy (a
	// fault they already had is the source's, never one this change adds).
	originals := base(bag, spec.keep)
	if err := mutate(); err != nil {
		return false, err
	}
	if bytes.Equal(before, baseBytes(bag, spec.keep)) {
		return false, nil
	}
	if model.ChildW(bag, spec.record) != nil {
		return true, nil
	}
	// The record is parsed whole, its snapshot the base properties'
	// bytes before the change, and placed where the schema puts it.
	var frag bytes.Buffer
	frag.WriteString("<w:" + spec.record)
	for _, a := range recordAttrs(bag, spec.record, r.author, r.date) {
		frag.WriteString(" " + a[0] + `="` + string(tree.EscapeText(a[1])) + `"`)
	}
	frag.WriteString("><w:" + bag.Local() + ">")
	frag.Write(before)
	frag.WriteString("</w:" + bag.Local() + "></w:" + spec.record + ">")
	nodes, err := h.Workspace().Parse(bag, frag.Bytes(), nil)
	if err != nil || len(nodes) != 1 {
		return true, fmt.Errorf("%w: the property snapshot does not parse: %w", ErrRevise, err)
	}
	h.Workspace().RecordCopies(model.ChildW(nodes[0], bag.Local()), originals)
	if err := h.PlaceNew(bag, nodes[0]); err != nil {
		return true, fmt.Errorf("%w: %w", ErrRevise, err)
	}
	r.note(h, nodes[0])
	return true, nil
}

// Finish ends the operation. In Track mode it gives the authored
// revisions still in the document their ids: each part's highest revision
// id plus one, then upwards, in document order. In Direct mode it accepts
// exactly the authored revisions. It returns how many revisions it
// numbered or accepted.
func (r *Reviser) Finish() (int, error) {
	byPart := map[*tree.Node][]authored{}
	var roots []*tree.Node
	for _, a := range r.ledger {
		root := rootOf(a.n)
		if root == nil {
			continue // removed since
		}
		if _, ok := byPart[root]; !ok {
			roots = append(roots, root)
		}
		byPart[root] = append(byPart[root], a)
	}
	total := 0
	for _, root := range roots {
		entries := byPart[root]
		if r.mode == Direct {
			// Each revision is accepted through the handle it was
			// authored with: an operation may author through several
			// regions, and one region's handle refuses another's
			// content. One handle's revisions settle together, in the
			// order the handles were first used.
			var handles []*edit.Handle
			mine := map[*edit.Handle]map[*tree.Node]bool{}
			for _, a := range entries {
				if mine[a.h] == nil {
					mine[a.h] = map[*tree.Node]bool{}
					handles = append(handles, a.h)
				}
				mine[a.h][a.n] = true
			}
			scope := root
			if b := model.ChildW(root, "body"); b != nil {
				scope = b
			}
			for _, h := range handles {
				sel := mine[h]
				n, err := Settle(h, scope, Options{Accept: true, Select: func(n *tree.Node) bool { return sel[n] }, Join: r.join})
				if err != nil {
					return total, err
				}
				total += n
			}
			continue
		}
		// Each revision is numbered through the handle it was authored
		// with, for the same reason.
		handle := map[*tree.Node]*edit.Handle{}
		mine := map[*tree.Node]bool{}
		for _, a := range entries {
			handle[a.n], mine[a.n] = a.h, true
		}
		top, ordered, seen := revisionIDs(root, mine)
		r.visited += seen
		next := top + 1
		for _, n := range ordered {
			if err := handle[n].SetNodeAttr(n, wName(n, "id"), strconv.FormatUint(next, 10)); err != nil {
				return total, fmt.Errorf("%w: %w", ErrRevise, err)
			}
			next++
			total++
		}
	}
	if r.mode == Direct {
		if err := r.dropEmptyShells(); err != nil {
			return total, err
		}
	}
	r.ledger, r.shells = nil, nil
	return total, nil
}

// rootOf returns the root element of the document n is attached to, or
// nil when n is detached.
func rootOf(n *tree.Node) *tree.Node {
	for p := n; p != nil; p = p.Parent() {
		if pp := p.Parent(); pp != nil && pp.Kind() == tree.DocumentNode {
			return p
		}
	}
	return nil
}

// revisionIDTags are the revision elements whose w:id the revision ids
// share (move range markers pair under their own ids), by local name.
var revisionIDTags = map[string]bool{
	"ins": true, "del": true, "cellIns": true, "cellDel": true, "cellMerge": true, "moveFrom": true, "moveTo": true, "rPrChange": true,
	"pPrChange": true, "tblPrChange": true, "trPrChange": true, "tcPrChange": true, "sectPrChange": true, "tblGridChange": true,
	"numberingChange": true, "tblPrExChange": true,
}

// isRevisionIDTag reports whether n is one of the revisionIDTags (as
// model.IsW: by local name, then in no namespace or WordprocessingML).
func isRevisionIDTag(n *tree.Node) bool {
	return model.IsElement(n) && revisionIDTags[n.Local()] && (n.Space() == "" || model.WordML.Has(n))
}

// revisionIDs walks root once: it returns the highest decimal revision id
// under root, leaving out the elements in mine (0 when there is none),
// the elements in mine in document order, and the nodes it visited.
func revisionIDs(root *tree.Node, mine map[*tree.Node]bool) (uint64, []*tree.Node, int) {
	var top uint64
	var ordered []*tree.Node
	seen := 0
	root.Walk(func(n *tree.Node) bool {
		seen++
		if mine[n] {
			ordered = append(ordered, n)
			return true
		}
		if !isRevisionIDTag(n) {
			return true
		}
		if v, ok := model.ParseU64(model.AttrOr(n, "id")); ok && v > top {
			top = v
		}
		return true
	})
	return top, ordered, seen
}
