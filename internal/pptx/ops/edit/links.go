package edit

import (
	"fmt"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
)

// The hyperlink relationship type is ctypes.RelHyperlink, the transitional
// type PowerPoint writes, or its strict counterpart: the base is taken
// from the part's own relationship types, never assumed.

// PartRels is the text operations' access to the relationships of a
// handle's part (it implements text.Rels): reading hyperlink targets,
// finding or adding an external hyperlink relationship, and pruning
// relationships nothing refers to. Every change declares the
// relationships part, so the verifier accepts it.
type PartRels struct {
	w    *Workspace
	part string
}

// Rels returns the relationships of the handle's part.
func (h *Handle) Rels() *PartRels { return &PartRels{w: h.w, part: h.ref.part} }

// Color implements text.Ctx: the text operations compare colours as
// written, so no displayed value is derived here.
func (r *PartRels) Color(string, *tree.Node) (string, bool) { return "", true }

// Link returns the target of relationship rid and whether it is
// external.
func (r *PartRels) Link(rid string) (string, bool, bool) {
	rels, err := r.w.pkg.Rels(r.part)
	if err != nil {
		return "", false, false
	}
	rel, ok := rels.Get(rid)
	if !ok {
		return "", false, false
	}
	return rel.Target, rel.External(), true
}

// Hyperlink returns the id of an external hyperlink relationship of the
// part to target: an existing one with the same target (PowerPoint shares
// one relationship among the runs of a link), else a new one with the
// next free id and TargetMode External.
func (r *PartRels) Hyperlink(target string) (string, error) {
	rels, err := r.w.pkg.Rels(r.part)
	if err != nil {
		return "", err
	}
	base := ""
	for _, rel := range rels.All() {
		i := strings.LastIndexByte(rel.Type, '/')
		if i < 0 {
			continue
		}
		if ctypes.Is(rel.Type, ctypes.RelHyperlink) && rel.External() && rel.Target == target {
			return rel.ID, nil
		}
		if base == "" && strings.HasPrefix(rel.Type, "http") {
			base = rel.Type[:i+1]
		}
	}
	if base == "" {
		return "", fmt.Errorf("edit: %s has no relationship whose type gives the relationship namespace for a hyperlink", r.part)
	}
	r.w.DeclarePart(rels.Name())
	return rels.Add(base+ctypes.Last(ctypes.RelHyperlink), target, true)
}

// Prune removes each relationship in rids that no attribute in the
// relationships namespace anywhere in the part refers to any more.
func (r *PartRels) Prune(rids []string) error {
	doc, err := r.w.tree(r.part)
	if err != nil {
		return err
	}
	used := map[string]bool{}
	var walk func(n *tree.Node)
	walk = func(n *tree.Node) {
		for _, a := range n.Attrs() {
			if !strings.Contains(a.Name, ":") {
				continue
			}
			if v, ok := derive.AttrNS(n, derive.REL, a.Name[strings.IndexByte(a.Name, ':')+1:]); ok && v == a.Value {
				used[v] = true
			}
		}
		for _, c := range n.Elements() {
			walk(c)
		}
	}
	walk(doc.Root())
	rels, err := r.w.pkg.Rels(r.part)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, id := range rids {
		if seen[id] || used[id] {
			continue
		}
		seen[id] = true
		if _, ok := rels.Get(id); !ok {
			continue
		}
		r.w.DeclarePart(rels.Name())
		if !rels.Remove(id) {
			return fmt.Errorf("edit: could not remove relationship %s of %s", id, r.part)
		}
	}
	return nil
}
