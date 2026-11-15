package ops

import (
	"strconv"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// SetSectionLayout is section.set_layout (docxdriver's set_section_layout).
type SetSectionLayout struct {
	At                 *SectionAddress `json:"at" default:"null" doc:"The section: section:<paraId> as read shows it, section:final, section:N (positional) or section:$name; section 1 when omitted."`
	PageSize           *string         `json:"page_size" default:"null" enum:"letter,legal,a4,a3,a5"`
	Width              *Points         `json:"width" default:"null"`
	Height             *Points         `json:"height" default:"null"`
	Orientation        *string         `json:"orientation" default:"null" enum:"portrait,landscape" schema:"w:ST_PageOrientation"`
	Columns            *uint32         `json:"columns" default:"null"`
	ColumnSpacing      *Points         `json:"column_spacing" default:"null"`
	ColumnWidths       *[]Points       `json:"column_widths" default:"null"`
	ColumnSeparator    *bool           `json:"column_separator" default:"null"`
	BreakType          *string         `json:"break_type" default:"null" enum:"nextPage,continuous,oddPage,evenPage,nextColumn" schema:"w:ST_SectionMark"`
	PageNumberFormat   *string         `json:"page_number_format" default:"null" enum:"decimal,upperRoman,lowerRoman,upperLetter,lowerLetter" schema:"w:ST_NumberFormat"`
	PageNumberStart    *uint32         `json:"page_number_start" default:"null"`
	PageNumberContinue *bool           `json:"page_number_continue" default:"null"`
}

// opSectionSetLayout is section.set_layout's entry in the operation table.
var opSectionSetLayout = OpDef{
	Name: "section.set_layout", Oracle: "set_section_layout", New: func() Spec { return &SetSectionLayout{} },
	Moved:        "`at` ← `section` or `section_handle`",
	Rules:        setSectionLayoutRules,
	TrackGuards:  []string{"a pending section property change by another author"},
	DirectGuards: []string{"a pending section property change"},
}

// pageSizes are the named page sizes in twips (width, height, portrait).
var pageSizes = map[string][2]int64{
	"letter": {12240, 15840}, "legal": {12240, 20160}, "a4": {11906, 16838}, "a3": {16838, 23811}, "a5": {8391, 11906},
}

// satAdd returns the saturating sum of a and b.
func satAdd(a, b int64) int64 {
	if s := a + b; (b > 0 && s < a) || (b < 0 && s > a) {
		if b > 0 {
			return 1<<63 - 1
		}
		return -1 << 63
	}
	return a + b
}

// satMul returns the saturating product of a and b.
func satMul(a, b int64) int64 {
	if a == 0 || b == 0 {
		return 0
	}
	p := a * b
	if p/b != a {
		if (a > 0) == (b > 0) {
			return 1<<63 - 1
		}
		return -1 << 63
	}
	return p
}

// layoutPlan is what the operation writes, worked out before any change.
type layoutPlan struct {
	w, h          int64
	orient        string
	count, gap    int64
	changeSize    bool
	changeColumns bool
}

// plan works out the page size and the columns (layout.rs
// set_section_layout) and refuses columns that do not fit the text area.
func (o *SetSectionLayout) plan(sect *tree.Node, label string) (layoutPlan, error) {
	var p layoutPlan
	p.w, p.h = sectInt(sect, "pgSz", "w", 12240), sectInt(sect, "pgSz", "h", 15840)
	if o.PageSize != nil {
		s := pageSizes[*o.PageSize]
		p.w, p.h = s[0], s[1]
	}
	if o.Width != nil && o.Height != nil {
		p.w, p.h = int64(*o.Width), int64(*o.Height)
	}
	p.orient = "portrait"
	if sect != nil {
		if sz := model.ChildW(sect, "pgSz"); sz != nil {
			if v, ok := model.Attr(sz, "orient"); ok {
				p.orient = v
			}
		}
	}
	if o.Orientation != nil {
		p.orient = *o.Orientation
	}
	p.changeSize = o.PageSize != nil || o.Width != nil || o.Orientation != nil
	if p.changeSize && (p.orient == "landscape" && p.w < p.h || p.orient == "portrait" && p.w > p.h) {
		p.w, p.h = p.h, p.w
	}
	area := p.w - sectInt(sect, "pgMar", "left", 1440) - sectInt(sect, "pgMar", "right", 1440) - sectInt(sect, "pgMar", "gutter", 0)
	var cols []*tree.Node
	if sect != nil {
		if c := model.ChildW(sect, "cols"); c != nil {
			for _, n := range c.Elements() {
				if model.IsW(n, "col") {
					cols = append(cols, n)
				}
			}
		}
	}
	var oldWidths []int64
	for _, c := range cols {
		if v := attrInt(c, "w"); v != nil {
			oldWidths = append(oldWidths, *v)
		}
	}
	switch {
	case o.Columns != nil:
		p.count = int64(*o.Columns)
	case o.ColumnWidths != nil:
		p.count = int64(len(*o.ColumnWidths))
	default:
		def := int64(1)
		if len(oldWidths) > 0 {
			def = int64(len(oldWidths))
		}
		p.count = sectInt(sect, "cols", "num", def)
	}
	p.gap = sectInt(sect, "cols", "space", 720)
	if o.ColumnSpacing != nil {
		p.gap = int64(*o.ColumnSpacing)
	}
	var gapTotal int64
	if o.ColumnSpacing == nil && o.ColumnWidths == nil && o.Columns == nil && len(oldWidths) > 0 {
		for i, c := range cols {
			if i == len(cols)-1 {
				break
			}
			g := p.gap
			if v := attrInt(c, "space"); v != nil {
				g = *v
			}
			gapTotal = satAdd(gapTotal, g)
		}
	} else {
		gapTotal = satMul(p.gap, max(p.count-1, 0))
	}
	unequal := oldWidths
	if o.ColumnWidths != nil {
		unequal = nil
		for _, w := range *o.ColumnWidths {
			unequal = append(unequal, int64(w))
		}
	}
	p.changeColumns = o.Columns != nil || o.ColumnSpacing != nil || o.ColumnWidths != nil || o.ColumnSeparator != nil
	if p.changeColumns || p.changeSize {
		if area <= 0 || gapTotal >= area {
			return p, reject("operation_failed", "", "blocked in %s: columns do not fit the section text area", label)
		}
		sum := gapTotal
		for _, w := range unequal {
			sum = satAdd(sum, w)
		}
		if len(unequal) > 0 && (o.Columns == nil || o.ColumnWidths != nil) && sum > area {
			return p, reject("operation_failed", "column_widths", "blocked in %s: column widths and spacing exceed the section text area", label)
		}
	}
	return p, nil
}

// Run sets the section's page size and orientation, columns, break type
// and page numbering (layout.rs set_section_layout). A section with a
// pending property change is refused in direct mode, and in track mode
// when another author made it; track mode records the properties before
// the change in a w:sectPrChange, direct mode accepts it.
func (o *SetSectionLayout) Run(env *Env) error {
	s, err := env.section(o.At)
	if err != nil {
		return err
	}
	if err := s.guardChange(true); err != nil {
		return err
	}
	p, err := o.plan(s.sect, s.label())
	if err != nil {
		return err
	}
	changed, err := s.change(func(h *core.Handle, sect *tree.Node) error { return o.apply(h, sect, p) })
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	return nil
}

// apply writes the layout into sect.
func (o *SetSectionLayout) apply(h *core.Handle, sect *tree.Node, p layoutPlan) error {
	child := func(local string) (*tree.Node, error) { return h.EnsureIn(sect, docxedit.W(local)) }
	if p.changeSize {
		n, err := child("pgSz")
		if err != nil {
			return err
		}
		// A page already of this size and orientation is left as it is
		// (an absent w:orient is portrait); otherwise all three are
		// written, as docxdriver writes them.
		orient, ok := model.Attr(n, "orient")
		if !ok {
			orient = "portrait"
		}
		same := orient == p.orient && sectInt(sect, "pgSz", "w", -1) == p.w && sectInt(sect, "pgSz", "h", -1) == p.h
		for _, a := range [][2]string{{"w", twipsInt(p.w)}, {"h", twipsInt(p.h)}, {"orient", p.orient}} {
			if same {
				break
			}
			if err := setW(h, n, a[0], a[1]); err != nil {
				return err
			}
		}
	}
	if p.changeColumns {
		n, err := child("cols")
		if err != nil {
			return err
		}
		if o.Columns != nil || o.ColumnWidths != nil {
			if err := setW(h, n, "num", strconv.FormatInt(p.count, 10)); err != nil {
				return err
			}
		}
		if o.ColumnSpacing != nil {
			if err := setW(h, n, "space", twipsInt(int64(*o.ColumnSpacing))); err != nil {
				return err
			}
		}
		if o.ColumnSeparator != nil {
			v := "0"
			if *o.ColumnSeparator {
				v = "1"
			}
			if err := setW(h, n, "sep", v); err != nil {
				return err
			}
		}
		if o.ColumnWidths != nil || o.Columns != nil {
			for _, c := range n.Elements() {
				if model.IsW(c, "col") {
					if err := h.RemoveNode(c); err != nil {
						return err
					}
				}
			}
			v := "1"
			if o.ColumnWidths != nil {
				v = "0"
			}
			if err := setW(h, n, "equalWidth", v); err != nil {
				return err
			}
		}
		if o.ColumnSpacing != nil && o.ColumnWidths == nil && o.Columns == nil {
			var cols []*tree.Node
			for _, c := range n.Elements() {
				if model.IsW(c, "col") {
					cols = append(cols, c)
				}
			}
			for i, c := range cols {
				g := p.gap
				if i == len(cols)-1 {
					g = 0
				}
				if err := setW(h, c, "space", twipsInt(g)); err != nil {
					return err
				}
			}
		}
		if o.ColumnWidths != nil {
			ws := *o.ColumnWidths
			for i, w := range ws {
				g := p.gap
				if i == len(ws)-1 {
					g = 0
				}
				frag := `<w:col w:w="` + twipsInt(int64(w)) + `" w:space="` + twipsInt(g) + `"/>`
				nodes, err := h.Workspace().Parse(n, []byte(frag), map[string]string{"w": model.NSW})
				if err != nil {
					return err
				}
				if err := h.PlaceAt(n, nodes[0], nil); err != nil {
					return err
				}
			}
		}
	}
	if o.BreakType != nil {
		n, err := child("type")
		if err != nil {
			return err
		}
		if err := setW(h, n, "val", *o.BreakType); err != nil {
			return err
		}
	}
	if o.PageNumberFormat != nil || o.PageNumberStart != nil || isTrue(o.PageNumberContinue) {
		n, err := child("pgNumType")
		if err != nil {
			return err
		}
		if o.PageNumberFormat != nil {
			if err := setW(h, n, "fmt", *o.PageNumberFormat); err != nil {
				return err
			}
		}
		if o.PageNumberStart != nil {
			if err := setW(h, n, "start", strconv.FormatUint(uint64(*o.PageNumberStart), 10)); err != nil {
				return err
			}
		}
		if isTrue(o.PageNumberContinue) {
			if err := removeW(h, n, "start"); err != nil {
				return err
			}
		}
	}
	return nil
}
