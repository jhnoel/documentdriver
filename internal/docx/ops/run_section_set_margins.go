package ops

import (
	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// SetPageMargins is section.set_margins (docxdriver's set_page_margins).
type SetPageMargins struct {
	At     *SectionAddress `json:"at" default:"null" doc:"The section: section:<paraId> as read shows it, section:final, section:N (positional) or section:$name; section 1 when omitted."`
	Top    *Points         `json:"top" default:"null"`
	Right  *Points         `json:"right" default:"null"`
	Bottom *Points         `json:"bottom" default:"null"`
	Left   *Points         `json:"left" default:"null"`
	Header *Points         `json:"header" default:"null"`
	Footer *Points         `json:"footer" default:"null"`
	Gutter *Points         `json:"gutter" default:"null"`
}

// opSectionSetMargins is section.set_margins's entry in the operation table.
var opSectionSetMargins = OpDef{
	Name: "section.set_margins", Oracle: "set_page_margins", New: func() Spec { return &SetPageMargins{} },
	Moved:       "`at` `section:n` ← `section`",
	Rules:       setPageMarginsRules,
	TrackGuards: []string{"a pending section property change by another author"},
}

// pageMarginDefaults are the margins a new w:pgMar starts from: CT_PageMar
// requires all seven attributes, and these are Word's (layout.rs
// ensure_pg_mar).
var pageMarginDefaults = [7]struct {
	local string
	v     int64
}{{"top", 1440}, {"right", 1440}, {"bottom", 1440}, {"left", 1440}, {"header", 720}, {"footer", 720}, {"gutter", 0}}

// Run sets the section's page margins (layout.rs set_page_margins_typed):
// the given attributes of its w:pgMar, created with Word's defaults when
// the section has none. Track mode records the properties before the
// change in a w:sectPrChange (its author's own pending one keeps its
// snapshot; another author's is refused); direct mode accepts it.
// Margins already as given write nothing.
func (o *SetPageMargins) Run(env *Env) error {
	s, err := env.section(o.At)
	if err != nil {
		return err
	}
	if err := s.guardChange(false); err != nil {
		return err
	}
	want := [7]*Points{o.Top, o.Right, o.Bottom, o.Left, o.Header, o.Footer, o.Gutter}
	changed, err := s.change(func(h *core.Handle, sect *tree.Node) error {
		mar := model.ChildW(sect, "pgMar")
		if mar == nil {
			var err error
			if mar, err = h.EnsureIn(sect, docxedit.W("pgMar")); err != nil {
				return err
			}
			for _, d := range pageMarginDefaults {
				if err := setW(h, mar, d.local, twipsInt(d.v)); err != nil {
					return err
				}
			}
		}
		for i, v := range want {
			if v == nil {
				continue
			}
			if err := setW(h, mar, pageMarginDefaults[i].local, twipsInt(int64(*v))); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	return nil
}
