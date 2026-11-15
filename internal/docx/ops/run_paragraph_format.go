package ops

import (
	"strconv"
	"strings"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// FormatParagraph is paragraph.format (docxdriver's format_paragraph).
type FormatParagraph struct {
	At              ParagraphAddress `json:"at"`
	Style           *string          `json:"style" default:"null"`
	Alignment       *string          `json:"alignment" default:"null" enum:"left,center,right,justify=both" schema:"w:ST_Jc"`
	IndentLeft      *SignedPoints    `json:"indent_left" default:"null"`
	IndentRight     *SignedPoints    `json:"indent_right" default:"null"`
	FirstLineIndent *SignedPoints    `json:"first_line_indent" doc:"Signed first-line offset: negative means hanging; zero explicitly disables it."`
	SpaceBefore     *Points          `json:"space_before" default:"null"`
	SpaceAfter      *Points          `json:"space_after" default:"null"`
	LineSpacing     *LineSpacing     `json:"line_spacing" default:"null"`
	Clear           []string         `json:"clear" default:"[]"`
}

// opParagraphFormat is paragraph.format's entry in the operation table.
var opParagraphFormat = OpDef{
	Name: "paragraph.format", Oracle: "format_paragraph", New: func() Spec { return &FormatParagraph{} },
	Rules: formatParagraphRules, Normalize: normalizeAlignment,
}

// paragraph resolves a paragraph address of the main document: an id as
// read shows it ("0A1B2C3D", "0A1B2C3D~2") or an alias. A paragraph an
// earlier operation joined away is not_found, naming its survivor.
func (env *Env) paragraph(at ParagraphAddress) (*tree.Node, string, error) {
	id := string(at)
	reg := env.WS.Registry()
	if at.IsAlias() {
		v, err := reg.Alias(strings.TrimPrefix(id, "$"))
		if err != nil {
			return nil, "", reject("not_found", "at", "%s", err.Error())
		}
		id = v
	}
	id = strings.ToUpper(id)
	if p, ok := reg.Paragraph(id); ok {
		return p, id, nil
	}
	if s, ok := reg.Joined(id); ok {
		return nil, "", reject("not_found", "at", "paragraph %s was joined into %s by an earlier operation", id, s)
	}
	return nil, "", reject("not_found", "at", "paragraph not found: %s", id)
}

// pprState is the modelled paragraph-property state (typed.rs PPrState).
type pprState struct {
	style, jc                       *string
	firstLineChars, hangingChars    *int64
	firstLine, hanging, left, right *int64
	spaceBefore, spaceAfter, line   *int64
	lineRule                        *string
}

// attrInt returns node n's attribute local as an integer pointer, or nil.
func attrInt(n *tree.Node, local string) *int64 {
	if n == nil {
		return nil
	}
	v, ok := model.Attr(n, local)
	if !ok {
		return nil
	}
	i, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return nil
	}
	return &i
}

// attrStr returns node n's attribute local as a string pointer, or nil.
func attrStr(n *tree.Node, local string) *string {
	if n == nil {
		return nil
	}
	if v, ok := model.Attr(n, local); ok {
		return &v
	}
	return nil
}

// readPPr returns the formatting state of paragraph p's properties.
func readPPr(p *tree.Node) pprState {
	var s pprState
	ppr := model.ChildW(p, "pPr")
	if ppr == nil {
		return s
	}
	s.style = attrStr(model.ChildW(ppr, "pStyle"), "val")
	s.jc = attrStr(model.ChildW(ppr, "jc"), "val")
	ind := model.ChildW(ppr, "ind")
	s.firstLineChars, s.hangingChars = attrInt(ind, "firstLineChars"), attrInt(ind, "hangingChars")
	s.firstLine, s.hanging = attrInt(ind, "firstLine"), attrInt(ind, "hanging")
	s.left, s.right = attrInt(ind, "left"), attrInt(ind, "right")
	sp := model.ChildW(ppr, "spacing")
	s.spaceBefore, s.spaceAfter, s.line = attrInt(sp, "before"), attrInt(sp, "after"), attrInt(sp, "line")
	s.lineRule = attrStr(sp, "lineRule")
	return s
}

// eqS reports whether two optional strings are equally present and equal.
func eqS(a, b *string) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }

// eqI reports whether two optional integers are equally present and equal.
func eqI(a, b *int64) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }

// equal reports whether two formatting states agree on all fields.
func (s pprState) equal(t pprState) bool {
	return eqS(s.style, t.style) && eqS(s.jc, t.jc) && eqI(s.firstLineChars, t.firstLineChars) &&
		eqI(s.hangingChars, t.hangingChars) && eqI(s.firstLine, t.firstLine) && eqI(s.hanging, t.hanging) &&
		eqI(s.left, t.left) && eqI(s.right, t.right) && eqI(s.spaceBefore, t.spaceBefore) &&
		eqI(s.spaceAfter, t.spaceAfter) && eqI(s.line, t.line) && eqS(s.lineRule, t.lineRule)
}

// ptr returns a pointer to a copy of value v.
func ptr[T any](v T) *T { return &v }

// target is para_format_target: the state after the request.
func (o *FormatParagraph) target(cur pprState) pprState {
	t := cur
	if o.Style != nil {
		t.style = ptr(*o.Style)
	}
	if o.Alignment != nil {
		a := *o.Alignment
		if a == "justify" {
			a = "both"
		}
		t.jc = &a
	}
	if o.IndentLeft != nil {
		t.left = ptr(int64(*o.IndentLeft))
	}
	if o.IndentRight != nil {
		t.right = ptr(int64(*o.IndentRight))
	}
	if o.FirstLineIndent != nil {
		v := int64(*o.FirstLineIndent)
		t.firstLineChars, t.hangingChars, t.firstLine, t.hanging = nil, nil, nil, nil
		if v >= 0 {
			t.firstLine = &v
		} else {
			t.hanging = ptr(-v)
		}
	}
	if o.SpaceBefore != nil {
		t.spaceBefore = ptr(int64(*o.SpaceBefore))
	}
	if o.SpaceAfter != nil {
		t.spaceAfter = ptr(int64(*o.SpaceAfter))
	}
	if l := o.LineSpacing; l != nil {
		switch l.Mode {
		case "multiple":
			t.line, t.lineRule = ptr(int64(roundHalfAway(l.Multiple*240))), ptr("auto")
		case "exact":
			t.line, t.lineRule = ptr(int64(l.Points)), ptr("exact")
		case "at_least":
			t.line, t.lineRule = ptr(int64(l.Points)), ptr("atLeast")
		}
	}
	for _, c := range o.Clear {
		switch c {
		case "style":
			t.style = nil
		case "alignment":
			t.jc = nil
		case "indent_left":
			t.left = nil
		case "indent_right":
			t.right = nil
		case "first_line_indent":
			t.firstLine, t.hanging, t.firstLineChars, t.hangingChars = nil, nil, nil, nil
		case "space_before":
			t.spaceBefore = nil
		case "space_after":
			t.spaceAfter = nil
		case "line_spacing":
			t.line, t.lineRule = nil, nil
		}
	}
	return t
}

// roundHalfAway rounds as Rust's f64::round does.
func roundHalfAway(f float64) float64 {
	if f < 0 {
		return -roundHalfAway(-f)
	}
	return float64(int64(f + 0.5))
}

// paragraphStyleExists reports whether the styles part declares a
// paragraph style with id (layout.rs paragraph_style_exists).
func (env *Env) paragraphStyleExists(snap *model.Package, id string) bool {
	x, ok := env.part(snap.StylesPartName())
	if !ok {
		return false
	}
	found := false
	x.Root().Walk(func(n *tree.Node) bool {
		if found {
			return false
		}
		if model.IsW(n, "style") && model.AttrOr(n, "styleId") == id {
			t, ok := model.Attr(n, "type")
			found = !ok || t == "paragraph"
		}
		return true
	})
	return found
}

// Run formats one paragraph (typed.rs format_paragraph_typed): its style,
// alignment, indentation and spacing, recorded as a w:pPrChange with the
// paragraph's base properties before the change; direct mode accepts it.
// A paragraph with a pending w:pPrChange is refused, as is a style the
// document does not declare.
func (o *FormatParagraph) Run(env *Env) error {
	main, _, err := env.Main()
	if err != nil {
		return err
	}
	if o.Style != nil {
		if !env.paragraphStyleExists(env.ReadModel(), *o.Style) {
			return reject("not_found", "style", "style %q is not a declared paragraph style — paragraph.format requires an existing paragraph style", *o.Style)
		}
	}
	p, id, err := env.paragraph(o.At)
	if err != nil {
		return err
	}
	if ppr := model.ChildW(p, "pPr"); ppr != nil && model.ChildW(ppr, "pPrChange") != nil {
		return reject("operation_failed", "at", "blocked in paragraph %s: the paragraph already has a pending format change (w:pPrChange) — incompatible with a new change; accept or reject it before formatting again", id)
	}
	cur := readPPr(p)
	want := o.target(cur)
	if want.equal(cur) {
		return nil
	}
	h, err := env.WS.Core().Range(main, "paragraph "+id, p, p)
	if err != nil {
		return err
	}
	ppr, err := h.EnsureIn(p, docxedit.W("pPr"))
	if err != nil {
		return err
	}
	if _, err := env.Rev.Property(h, ppr, func() error { return applyPPr(h, ppr, cur, want) }); err != nil {
		return err
	}
	return nil
}

// applyPPr rewrites the modelled children of ppr from cur to want
// (typed.rs apply_para_format); new children go where the schema puts
// them.
func applyPPr(h *core.Handle, ppr *tree.Node, cur, want pprState) error {
	val := func(local string, v *string) error {
		if v == nil {
			if c := model.ChildW(ppr, local); c != nil {
				return h.RemoveNode(c)
			}
			return nil
		}
		c, err := h.EnsureIn(ppr, docxedit.W(local))
		if err != nil {
			return err
		}
		return h.SetNodeAttr(c, qname(ppr, "val"), *v)
	}
	if !eqS(cur.style, want.style) {
		if err := val("pStyle", want.style); err != nil {
			return err
		}
	}
	if !eqS(cur.jc, want.jc) {
		if err := val("jc", want.jc); err != nil {
			return err
		}
	}
	type attr struct {
		local string
		v     *int64
		s     *string
	}
	set := func(local string, attrs []attr) error {
		c, err := h.EnsureIn(ppr, docxedit.W(local))
		if err != nil {
			return err
		}
		for _, a := range attrs {
			name := qname(ppr, a.local)
			switch {
			case a.v != nil:
				err = h.SetNodeAttr(c, name, strconv.FormatInt(*a.v, 10))
			case a.s != nil:
				err = h.SetNodeAttr(c, name, *a.s)
			default:
				for _, x := range c.Attrs() {
					if x.Name == a.local || strings.HasSuffix(x.Name, ":"+a.local) {
						err = h.RemoveNodeAttr(c, x.Name)
					}
				}
			}
			if err != nil {
				return err
			}
		}
		if len(c.Attrs()) == 0 {
			return h.RemoveNode(c)
		}
		return nil
	}
	if !eqI(cur.firstLine, want.firstLine) || !eqI(cur.hanging, want.hanging) ||
		!eqI(cur.firstLineChars, want.firstLineChars) || !eqI(cur.hangingChars, want.hangingChars) {
		if err := set("ind", []attr{{"firstLineChars", want.firstLineChars, nil}, {"hangingChars", want.hangingChars, nil},
			{"firstLine", want.firstLine, nil}, {"hanging", want.hanging, nil}}); err != nil {
			return err
		}
	}
	if !eqI(cur.left, want.left) || !eqI(cur.right, want.right) {
		if err := set("ind", []attr{{"left", want.left, nil}, {"right", want.right, nil}}); err != nil {
			return err
		}
	}
	if !eqI(cur.spaceBefore, want.spaceBefore) || !eqI(cur.spaceAfter, want.spaceAfter) ||
		!eqI(cur.line, want.line) || !eqS(cur.lineRule, want.lineRule) {
		if err := set("spacing", []attr{{"before", want.spaceBefore, nil}, {"after", want.spaceAfter, nil},
			{"line", want.line, nil}, {"lineRule", nil, want.lineRule}}); err != nil {
			return err
		}
	}
	return nil
}

// qname is the qualified name of WordprocessingML attribute local at n.
func qname(n *tree.Node, local string) string {
	if p, ok := n.LookupNamespace(model.NSW); ok && p != "" {
		return p + ":" + local
	}
	return "w:" + local
}
