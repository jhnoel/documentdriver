package ops

import (
	"strconv"
	"strings"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/view"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// sectionTarget is the section a section operation edits: its w:sectPr,
// or none yet when the address names the body's implicit final section
// (a body without a final w:sectPr), which ensure creates.
type sectionTarget struct {
	env  *Env
	part string
	body *tree.Node
	sect *tree.Node
	// n is the section's 1-based number, as read lists the sections.
	n int
}

// label is the section's name in summaries and errors.
func (s *sectionTarget) label() string { return "section " + strconv.Itoa(s.n) }

// section resolves a section address of the main document (layout.rs
// ensure_sect_pr, sections.rs select): no address is section 1;
// section:<paraId> the section that paragraph ends; section:N the Nth w:sectPr in body order, the body's implicit final
// section (one more than the w:sectPr there are, when the body has no
// final w:sectPr) included, as read lists it; section:final the body's
// own; section:$name the section an earlier operation named. docxdriver
// counts the implicit final section in set_section_layout only, and
// refuses it in set_page_margins and set_first_page_mode unless the
// document has no w:sectPr at all; here every section op counts it.
func (env *Env) section(at *SectionAddress) (*sectionTarget, error) {
	main, x, err := env.Main()
	if err != nil {
		return nil, err
	}
	body := x.Body()
	if body == nil {
		return nil, reject("unreadable_target", "at", "the document has no w:body")
	}
	nodes := view.SectPrNodes(x)
	final := model.ChildW(body, "sectPr")
	count := len(nodes)
	if final == nil {
		count++ // the implicit final section
	}
	t := &sectionTarget{env: env, part: main, body: body}
	pick := func(n int) {
		t.n = n
		if n <= len(nodes) {
			t.sect = nodes[n-1]
		}
	}
	if at == nil {
		pick(1)
		return t, nil
	}
	if name, ok := at.Alias(); ok {
		return t, env.sectionAlias(t, nodes, final, name)
	}
	if id, ok := at.ParaID(); ok {
		return t, env.sectionByParagraph(t, nodes, id)
	}
	n, isFinal, _ := at.Number()
	switch {
	case isFinal:
		pick(count)
	case int(n) > count:
		return nil, reject("not_found", "at", "section %d not found (document has %d sections)", n, count)
	default:
		pick(int(n))
	}
	return t, nil
}

// sectionByParagraph resolves "section:<paraId>": the section the
// paragraph with that id ends (its w:pPr carries the w:sectPr). A
// paragraph an earlier operation joined away is followed to the one it
// was joined into.
func (env *Env) sectionByParagraph(t *sectionTarget, nodes []*tree.Node, id string) error {
	reg := env.WS.Registry()
	p, ok := reg.Paragraph(id)
	for seen := 0; !ok && seen < 64; seen++ {
		next, joined := reg.Joined(id)
		if !joined {
			break
		}
		id = next
		p, ok = reg.Paragraph(id)
	}
	if !ok {
		return reject("not_found", "at", "section not found: no paragraph %s ends a section", id)
	}
	var sect *tree.Node
	if ppr := model.ChildW(p, "pPr"); ppr != nil {
		sect = model.ChildW(ppr, "sectPr")
	}
	for i, s := range nodes {
		if sect != nil && s == sect {
			t.n, t.sect = i+1, s
			return nil
		}
	}
	return reject("not_found", "at", "section not found: paragraph %s does not end a section", id)
}

// sectionAlias resolves a section an earlier operation named: the
// alias holds a section handle ("section:<paraId>" of the paragraph that
// ends the section, "section:final") or the paraId alone.
func (env *Env) sectionAlias(t *sectionTarget, nodes []*tree.Node, final *tree.Node, name string) error {
	v, err := env.WS.Registry().Alias(name)
	if err != nil {
		return reject("not_found", "at", "%s", err.Error())
	}
	handle := v
	if !strings.HasPrefix(handle, "section:") {
		handle = "section:" + strings.ToUpper(v)
	}
	if handle == "section:final" && final == nil {
		t.n = len(nodes) + 1
		return nil
	}
	sect, err := env.WS.Registry().Section(handle)
	if err != nil {
		return reject("not_found", "at", "section $%s: %s", name, err.Error())
	}
	for i, s := range nodes {
		if s == sect {
			t.n, t.sect = i+1, s
			return nil
		}
	}
	return reject("not_found", "at", "section $%s no longer identifies a section", name)
}

// ensure returns the section's w:sectPr, creating the body's final one
// where the schema puts it when the section is the implicit final
// section.
func (s *sectionTarget) ensure() (*tree.Node, error) {
	if s.sect != nil {
		return s.sect, nil
	}
	s.env.WS.Core().DeclareContainer(s.part, s.body)
	h := s.env.WS.Target(s.part, "body", s.body)
	sect, err := h.EnsureIn(s.body, docxedit.W("sectPr"))
	if err != nil {
		return nil, err
	}
	s.sect = sect
	return sect, nil
}

// handle returns an editor over the section's w:sectPr, creating it
// first when needed.
func (s *sectionTarget) handle() (*core.Handle, *tree.Node, error) {
	sect, err := s.ensure()
	if err != nil {
		return nil, nil, err
	}
	return s.env.WS.Target(s.part, s.label(), sect), sect, nil
}

// pendingChange returns the section's w:sectPrChange, or nil.
func (s *sectionTarget) pendingChange() *tree.Node {
	if s.sect == nil {
		return nil
	}
	return model.ChildW(s.sect, "sectPrChange")
}

// guardChange refuses a section with a pending property change the
// operation cannot fold into (layout.rs snapshot_section_properties and
// its callers): in track mode one by another author (the author's own
// keeps its snapshot, as Word folds one author's successive changes into
// one record); in direct mode, when anyChange is set, any (docxdriver's
// set_section_layout and set_first_page_mode refuse it; set_page_margins
// writes the margins beside it).
func (s *sectionTarget) guardChange(anyChange bool) error {
	c := s.pendingChange()
	if c == nil {
		return nil
	}
	if s.env.Mode == ModeDirect {
		if anyChange {
			return reject("operation_failed", "at", "blocked in %s: the section already has a pending property change (w:sectPrChange); accept or reject it first", s.label())
		}
		return nil
	}
	if model.AttrOr(c, "author") != s.env.Author {
		return reject("operation_failed", "at", "blocked in %s: the section already has a pending property change by another author; accept or reject it first", s.label())
	}
	return nil
}

// change runs mutate on the section's properties through the Reviser,
// which records a w:sectPrChange with the properties before the change
// in track mode (direct mode accepts it). It reports whether anything
// changed.
func (s *sectionTarget) change(mutate func(h *core.Handle, sect *tree.Node) error) (bool, error) {
	h, sect, err := s.handle()
	if err != nil {
		return false, err
	}
	return s.env.Rev.Property(h, sect, func() error { return mutate(h, sect) })
}

// setW sets WordprocessingML attribute local of element n to value,
// keeping the name the attribute already has (layout.rs set_attr, which
// replaces an unqualified one too).
func setW(h *core.Handle, n *tree.Node, local, value string) error {
	name := qname(n, local)
	for _, a := range n.Attrs() {
		if a.Name == local && a.Name != name {
			if err := h.RemoveNodeAttr(n, a.Name); err != nil {
				return err
			}
		}
	}
	if v, ok := n.Attr(name); ok && v == value {
		return nil
	}
	return h.SetNodeAttr(n, name, value)
}

// twipsInt formats a length in twips.
func twipsInt(v int64) string { return strconv.FormatInt(v, 10) }

// removeW removes WordprocessingML attribute local of element n, under
// either spelling.
func removeW(h *core.Handle, n *tree.Node, local string) error {
	for _, a := range n.Attrs() {
		if a.Name == local || a.Name == qname(n, local) {
			if err := h.RemoveNodeAttr(n, a.Name); err != nil {
				return err
			}
		}
	}
	return nil
}

// sectInt is the integer attribute local of sect's child tag, or def
// when sect (nil for a section without one yet), the child or the
// attribute is missing or not a number.
func sectInt(sect *tree.Node, tag, local string, def int64) int64 {
	if sect == nil {
		return def
	}
	if v := attrInt(model.ChildW(sect, tag), local); v != nil {
		return *v
	}
	return def
}
