package ops

import (
	"errors"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/view"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// SetSectionBoundary is section.set_boundary (docxdriver's set_section_boundary).
type SetSectionBoundary struct {
	At        ParagraphAddress `json:"at"`
	Remove    bool             `json:"remove" default:"false"`
	BreakType *string          `json:"break_type" default:"null" enum:"nextPage,continuous,oddPage,evenPage,nextColumn" schema:"w:ST_SectionMark"`
}

// opSectionSetBoundary is section.set_boundary's entry in the operation table.
var opSectionSetBoundary = OpDef{
	Name: "section.set_boundary", Oracle: "set_section_boundary", New: func() Spec { return &SetSectionBoundary{} },
	Rules: setSectionBoundaryRules, Track: DirectOnly, Direct: OnlyDirect, TrackGuards: refusedInTrack,
}

// Run makes a body paragraph end a section, or stops it ending one
// (sections.rs boundary). Word records no revision for a section break,
// so track mode refuses. Adding a boundary at paragraph X splits the
// section holding X: the new section, section:X, ends at X and starts as
// a copy of the properties of the section it was split from (its
// headers and footers included), with break_type as its w:type. Removing
// X's boundary merges its section into the one after it, which keeps
// its own properties and takes the removed section's header and footer
// references of the kinds it has none of, so its stories do not change;
// section:X then names nothing (not_found for later operations).
// Paragraphs with a pending property change, and sections with one, are
// refused: settle them first.
func (o *SetSectionBoundary) Run(env *Env) error {
	if env.Mode == ModeTrack {
		return reject("refused_in_mode", "", "section.set_boundary cannot be tracked: Word records no revision for adding or removing a section break; use a direct plan")
	}
	part, x, err := env.Main()
	if err != nil {
		return err
	}
	p, id, err := env.paragraph(o.At)
	if err != nil {
		return err
	}
	body := x.Body()
	if body == nil || p.Parent() != body {
		return reject("operation_failed", "at", "paragraph %s is not a body-level paragraph: section boundaries are body paragraphs", id)
	}
	ppr := model.ChildW(p, "pPr")
	if ppr != nil && model.ChildW(ppr, "pPrChange") != nil {
		return reject("operation_failed", "at", "paragraph %s has a pending paragraph property change; settle it before editing a section boundary", id)
	}
	var existing *tree.Node
	if ppr != nil {
		existing = model.ChildW(ppr, "sectPr")
	}
	h := env.Story(part, body)
	if model.ChildW(body, "sectPr") == nil {
		if _, err := h.EnsureIn(body, docxedit.W("sectPr")); err != nil {
			return err
		}
	}
	nodes := view.SectPrNodes(x)
	order := map[*tree.Node]int{}
	for i, c := range body.Elements() {
		order[c] = i
	}
	// The section holding p: the first boundary at or after it.
	var following *tree.Node
	next := -1
	for i, s := range nodes {
		owner := s.Parent()
		if model.IsW(owner, "pPr") {
			owner = owner.Parent()
		}
		if owner == body || order[owner] >= order[p] {
			following = s
			next = i
			break
		}
	}
	if following == nil {
		return errors.New("ops: the document has no final section properties")
	}
	if model.ChildW(following, "sectPrChange") != nil {
		return reject("operation_failed", "at", "the section holding %s has a pending section property change; settle it first", id)
	}
	if o.Remove {
		if existing == nil {
			return reject("not_found", "at", "paragraph %s ends no section: there is no boundary to remove", id)
		}
		if next+1 >= len(nodes) {
			return errors.New("ops: a paragraph's section boundary has no section after it")
		}
		after := nodes[next+1]
		if model.ChildW(after, "sectPrChange") != nil {
			return reject("operation_failed", "at", "the section after %s has a pending section property change; settle it before merging", id)
		}
		if err := carryStories(h, existing, after); err != nil {
			return err
		}
		if err := h.RemoveNode(existing); err != nil {
			return err
		}
		env.Deleted = append(env.Deleted, "section:"+id)
		return nil
	}
	if existing != nil {
		return reject("operation_failed", "at", "paragraph %s already ends a section (section:%s)", id, id)
	}
	clone, err := h.Workspace().Copy(following)
	if err != nil {
		return err
	}
	h.Workspace().RecordCopy(clone, following)
	pp, err := h.EnsureIn(p, docxedit.W("pPr"))
	if err != nil {
		return err
	}
	if err := h.PlaceNew(pp, clone); err != nil {
		return err
	}
	if o.BreakType != nil {
		if err := setVal(h, clone, "type", *o.BreakType); err != nil {
			return err
		}
	}
	return nil
}

// carryStories gives section properties next a copy of each header and
// footer reference of old whose kind next has none of, after next's own
// references (sections.rs: the merged section keeps the stories it
// showed).
func carryStories(h *core.Handle, old, next *tree.Node) error {
	has := func(name, kind string) bool {
		for _, c := range next.Elements() {
			if model.IsW(c, name) && refKind(c) == kind {
				return true
			}
		}
		return false
	}
	for _, c := range old.Elements() {
		if !model.IsW(c, "headerReference") && !model.IsW(c, "footerReference") || has(c.Local(), refKind(c)) {
			continue
		}
		cp, err := h.Workspace().Copy(c)
		if err != nil {
			return err
		}
		h.Workspace().RecordCopy(cp, c)
		var at *tree.Node
		for _, n := range next.Elements() {
			if !model.IsW(n, "headerReference") && !model.IsW(n, "footerReference") {
				at = n
				break
			}
		}
		if err := h.PlaceAt(next, cp, at); err != nil {
			return err
		}
	}
	return nil
}

// refKind is a story reference's w:type, default when absent.
func refKind(ref *tree.Node) string {
	if t, ok := model.Attr(ref, "type"); ok {
		return t
	}
	return "default"
}
