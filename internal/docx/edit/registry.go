package edit

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Registry is the workspace's map from a plan's addresses to nodes,
// bound when the workspace opens so that an earlier operation never
// shifts what a later one addresses (docxdriver's source-bound handles):
//
//   - tables, rows and cells by their positional address at open
//     ("table:2/row:1/cell:3"), and by alias once an operation binds one
//     ("$t", "$t/row:2/cell:1"); Refresh recomputes every node's current
//     positional address after a structural operation;
//   - paragraphs of the main document by w14:paraId (the id resolved at
//     open, see Workspace.ParaID, or one bound for a created paragraph),
//     and paragraph aliases and alias groups by name;
//   - sections by handle: "section:<paraId>" of the paragraph that ends
//     it, "section:final" for the body's own;
//   - the visible text every main-document paragraph had at open, for the
//     diagnostic that a selector matched the paragraph only before an
//     earlier operation changed it.
type Registry struct {
	w       *Workspace
	tables  map[string]*tree.Node
	pos     map[*tree.Node]string
	paras   map[string]*tree.Node
	aliases map[string]string
	groups  map[string][]string
	base    map[*tree.Node]string
	joined  map[string]string // absorbed paraId -> surviving paraId
	after   map[*tree.Node]*tree.Node
}

// Errors from the registry.
var (
	ErrUnknownHandle = errors.New("unknown source-bound table handle")
	ErrDeletedHandle = errors.New("handle targets a deleted object")
	ErrDuplicateName = errors.New("alias is already defined")
	ErrUnknownAlias  = errors.New("unresolved alias")
	ErrNoSection     = errors.New("section_handle no longer identifies a section boundary")
)

// newRegistry binds the registry over the workspace as it opened.
func newRegistry(w *Workspace) (*Registry, error) {
	r := &Registry{w: w, tables: map[string]*tree.Node{}, paras: map[string]*tree.Node{},
		aliases: map[string]string{}, groups: map[string][]string{}, base: map[*tree.Node]string{}, joined: map[string]string{}}
	if err := r.Refresh(); err != nil {
		return nil, err
	}
	for n, a := range r.pos {
		r.tables[a] = n
	}
	doc, err := w.Tree(w.doc)
	if err != nil {
		return nil, err
	}
	for _, p := range (&model.XML{Doc: doc}).AllParagraphs() {
		if id, ok := w.ids[p]; ok {
			r.paras[id.ID] = p
		}
		r.base[p] = model.ParagraphText(p, model.Current)
	}
	return r, nil
}

// Registry returns the workspace's address registry.
func (w *Workspace) Registry() *Registry { return w.reg }

// Refresh recomputes the current positional address of every table, row
// and cell of the main document body (docxdriver's table_positions):
// tables in document order, nested ones included, rows and cells as
// children. Call it after an operation that inserts, deletes or moves
// tables, rows, cells or paragraphs.
func (r *Registry) Refresh() error {
	doc, err := r.w.Tree(r.w.doc)
	if err != nil {
		return err
	}
	r.pos = map[*tree.Node]string{}
	body := (&model.XML{Doc: doc}).Body()
	if body == nil {
		return nil
	}
	ti := 0
	body.Walk(func(t *tree.Node) bool {
		if t == body || !model.IsW(t, "tbl") {
			return true
		}
		ti++
		at := "table:" + strconv.Itoa(ti)
		r.pos[t] = at
		ri := 0
		for _, row := range t.Elements() {
			if !model.IsW(row, "tr") {
				continue
			}
			ri++
			ra := at + "/row:" + strconv.Itoa(ri)
			r.pos[row] = ra
			ci := 0
			for _, c := range row.Elements() {
				if model.IsW(c, "tc") {
					ci++
					r.pos[c] = ra + "/cell:" + strconv.Itoa(ci)
				}
			}
		}
		return true
	})
	return nil
}

// attached reports whether n is still in the main document's tree.
func (r *Registry) attached(n *tree.Node) bool {
	doc, err := r.w.Tree(r.w.doc)
	if err != nil {
		return false
	}
	p := n
	for p.Parent() != nil {
		p = p.Parent()
	}
	return p == doc.Node()
}

// Table resolves a table, row or cell handle (a positional address as
// the document was opened, or an alias bound since) to its node and its
// current positional address.
func (r *Registry) Table(handle string) (*tree.Node, string, error) {
	n, ok := r.tables[handle]
	if !ok {
		return nil, "", fmt.Errorf("%w %q", ErrUnknownHandle, handle)
	}
	at, ok := r.pos[n]
	if !ok || !r.attached(n) {
		return nil, "", fmt.Errorf("table handle %q: %w", handle, ErrDeletedHandle)
	}
	return n, at, nil
}

// BindTable binds alias ("$name" in plans) to table t and every row and
// cell under it by its suffix ("$name/row:1/cell:2"), as they stand now
// (call Refresh first). It fails when the name is taken.
func (r *Registry) BindTable(alias string, t *tree.Node) error {
	at, ok := r.pos[t]
	if !ok {
		return fmt.Errorf("edit: table for alias %q is not in the body", alias)
	}
	if err := r.claim(alias); err != nil {
		return err
	}
	r.aliases[alias] = "$" + alias
	prefix := at + "/"
	for n, a := range r.pos {
		switch {
		case a == at:
			r.tables["$"+alias] = n
		case strings.HasPrefix(a, prefix):
			r.tables["$"+alias+"/"+strings.TrimPrefix(a, prefix)] = n
		}
	}
	return nil
}

// BindTableSuffixes binds "$alias/<suffix>" to each node (the rows or
// columns' cells an edit_table insertion made), without claiming alias
// itself.
func (r *Registry) BindTableSuffixes(alias string, nodes map[string]*tree.Node) {
	for s, n := range nodes {
		r.tables["$"+alias+"/"+s] = n
	}
}

// claim reserves name for an alias or group.
func (r *Registry) claim(name string) error {
	if _, ok := r.aliases[name]; ok {
		return fmt.Errorf("%w: %q", ErrDuplicateName, name)
	}
	if _, ok := r.groups[name]; ok {
		return fmt.Errorf("%w: %q", ErrDuplicateName, name)
	}
	return nil
}

// BindParagraph records created paragraph p under id (its new
// w14:paraId), so later operations address it.
func (r *Registry) BindParagraph(id string, p *tree.Node) { r.paras[strings.ToUpper(id)] = p }

// Paragraph returns the main-document paragraph with paraId id (any
// case): the one bound at open or by BindParagraph, else a created
// paragraph that carries the id. It reports false when there is none or
// it was deleted.
func (r *Registry) Paragraph(id string) (*tree.Node, bool) {
	id = strings.ToUpper(id)
	if _, gone := r.joined[id]; gone {
		return nil, false
	}
	if p, ok := r.paras[id]; ok {
		return p, r.attached(p)
	}
	doc, err := r.w.Tree(r.w.doc)
	if err != nil {
		return nil, false
	}
	for _, p := range (&model.XML{Doc: doc}).AllParagraphs() {
		if v, ok := model.Attr(p, "paraId"); ok && strings.EqualFold(v, id) && r.w.core.Created(p) {
			r.paras[id] = p
			return p, true
		}
	}
	return nil, false
}

// DefineAlias binds a paragraph alias to paraId id. It fails when the
// name is taken by an alias or a group.
func (r *Registry) DefineAlias(name, id string) error {
	if err := r.claim(name); err != nil {
		return err
	}
	r.aliases[name] = id
	return nil
}

// DefineGroup binds a group alias (a list insert_paragraph's as) to its
// paragraphs' ids, in order.
func (r *Registry) DefineGroup(name string, ids []string) error {
	if err := r.claim(name); err != nil {
		return err
	}
	r.groups[name] = slices.Clone(ids)
	return nil
}

// Alias returns the paraId (or, for a table alias, "$name") an alias
// names.
func (r *Registry) Alias(name string) (string, error) {
	v, ok := r.aliases[name]
	if !ok {
		return "", fmt.Errorf("%w $%s", ErrUnknownAlias, name)
	}
	return v, nil
}

// Group returns the paraIds of a group alias.
func (r *Registry) Group(name string) ([]string, bool) {
	ids, ok := r.groups[name]
	return slices.Clone(ids), ok
}

// Taken reports whether name is already taken: an alias or a group.
func (r *Registry) Taken(name string) bool {
	if _, err := r.Alias(name); err == nil {
		return true
	}
	_, ok := r.Group(name)
	return ok
}

// Aliases returns every alias and its value, and every group, for a plan
// report.
func (r *Registry) Aliases() (map[string]string, map[string][]string) {
	a := make(map[string]string, len(r.aliases))
	for k, v := range r.aliases {
		a[k] = v
	}
	g := make(map[string][]string, len(r.groups))
	for k, v := range r.groups {
		g[k] = slices.Clone(v)
	}
	return a, g
}

// BaseText returns the visible text (insertions shown, deletions hidden)
// paragraph p had when the workspace opened, and false for a paragraph
// created since.
func (r *Registry) BaseText(p *tree.Node) (string, bool) {
	s, ok := r.base[p]
	return s, ok
}

// Section resolves a section handle to its w:sectPr: "section:final" is
// the body's own, "section:<paraId>" the one in the properties of the
// paragraph that ends the section.
func (r *Registry) Section(handle string) (*tree.Node, error) {
	id, ok := strings.CutPrefix(handle, "section:")
	if !ok {
		return nil, errors.New("invalid section_handle")
	}
	if id == "final" {
		doc, err := r.w.Tree(r.w.doc)
		if err != nil {
			return nil, err
		}
		if b := (&model.XML{Doc: doc}).Body(); b != nil {
			if s := model.ChildW(b, "sectPr"); s != nil {
				return s, nil
			}
		}
		return nil, ErrNoSection
	}
	p, ok := r.Paragraph(id)
	if !ok {
		return nil, ErrNoSection
	}
	if ppr := model.ChildW(p, "pPr"); ppr != nil {
		if s := model.ChildW(ppr, "sectPr"); s != nil {
			return s, nil
		}
	}
	return nil, ErrNoSection
}

// join records that paragraph id gone was joined into the paragraph now
// identified as keep (node survivor).
func (r *Registry) join(gone, keep string, survivor *tree.Node) {
	gone, keep = strings.ToUpper(gone), strings.ToUpper(keep)
	r.joined[gone] = keep
	r.paras[keep] = survivor
	delete(r.paras, gone)
}

// Joined reports the id a paragraph joined away by an earlier operation
// of the plan now lives on: an address naming id is refused, and the
// refusal can name the survivor.
func (r *Registry) Joined(id string) (string, bool) {
	s, ok := r.joined[strings.ToUpper(id)]
	return s, ok
}
