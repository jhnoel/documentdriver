package ops

import (
	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// SetFirstPageMode is section.set_first_page (docxdriver's set_first_page_mode).
type SetFirstPageMode struct {
	At      *SectionAddress `json:"at" default:"null" doc:"The section: section:<paraId> as read shows it, section:final, section:N (positional) or section:$name; section 1 when omitted."`
	Enabled bool            `json:"enabled" doc:"Whether the section's first page has its own header and footer (w:titlePg)."`
}

// opSectionSetFirstPage is section.set_first_page's entry in the operation table.
var opSectionSetFirstPage = OpDef{
	Name: "section.set_first_page", Oracle: "set_first_page_mode", New: func() Spec { return &SetFirstPageMode{} },
	Moved:        "`at` ← `section`",
	Rules:        sectionRules[SetFirstPageMode](`{"op":"section.set_first_page","at":"section:0","enabled":true}`),
	TrackGuards:  []string{"a pending section property change by another author"},
	DirectGuards: []string{"a pending section property change"},
}

// Run turns the section's distinct first page (w:titlePg) on or off
// (layout.rs set_first_page_mode): w:val 1 or 0 on its w:titlePg,
// recorded in a w:sectPrChange in track mode. A section already so
// writes nothing; one with a pending property change is refused as
// section.set_layout refuses it.
func (o *SetFirstPageMode) Run(env *Env) error {
	s, err := env.section(o.At)
	if err != nil {
		return err
	}
	if err := s.guardChange(true); err != nil {
		return err
	}
	current := false
	if s.sect != nil {
		if t := model.ChildW(s.sect, "titlePg"); t != nil {
			v, _ := model.Attr(t, "val")
			current = v != "0" && v != "false" && v != "off"
		}
	}
	if current == o.Enabled {
		return nil
	}
	val := "0"
	if o.Enabled {
		val = "1"
	}
	if _, err := s.change(func(h *core.Handle, sect *tree.Node) error {
		t, err := h.EnsureIn(sect, docxedit.W("titlePg"))
		if err != nil {
			return err
		}
		return setW(h, t, "val", val)
	}); err != nil {
		return err
	}
	return nil
}
