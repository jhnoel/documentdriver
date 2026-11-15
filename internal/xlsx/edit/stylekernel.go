package edit

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/schema"
	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
	"github.com/jhnoel/documentdriver/internal/xlsx/numfmt"
)

// The styles kernel (chunk K6): pure functions over a StyleTable, the
// stored bytes of the styles part's lists. They plan; Styles (styles.go,
// designer-owned) applies the plan. No tree, no state, no I/O.

// StyleTable is the styles part's lists as stored: custom number
// formats by id, and the bytes of each <font>, <fill> and cellXfs <xf>
// element in order (their index is their id).
type StyleTable struct {
	NumFmts           map[int]string
	Fonts, Fills, XFs []string
}

// StylePlan is what to append for one request: XF is the resulting
// cellXfs index (an existing entry, or len(XFs) when NewXF is set);
// NewNumFmt, NewFont, NewFill and NewXF are whole elements to append
// ("" for none), written unprefixed with attributes in schema order.
// A new font or fill gets the next index of its list, which NewXF uses.
type StylePlan struct {
	XF                                 int
	NewNumFmt, NewFont, NewFill, NewXF string
}

// NumFmtOf returns cellXfs entry xf's numFmtId and its code (custom from
// t.NumFmts, built-in from numfmt.BuiltIn); ok false when xf is out of
// range, the xf has no parseable numFmtId (0 is General), or the code is
// unknown.
func NumFmtOf(t StyleTable, xf int) (id int, code string, ok bool) {
	if xf < 0 || xf >= len(t.XFs) {
		return 0, "", false
	}
	e, err := parseStyle(t.XFs[xf])
	if err != nil {
		return 0, "", false
	}
	s, found := e.get("numFmtId")
	if !found {
		return 0, "", false
	}
	id, err = strconv.Atoi(s)
	if err != nil || id < 0 {
		return 0, "", false
	}
	if code, ok = t.NumFmts[id]; ok {
		return id, code, true
	}
	code, ok = numfmt.BuiltIn(id)
	return id, code, ok
}

// PlanNumFmt plans xf with numFmtId id (a built-in id: 14, 21, 22) and
// applyNumberFormat="1", everything else of the xf kept as it is. An
// existing xf that is the same element (attributes compared as a set,
// children byte for byte) is reused. xf out of range is an error.
func PlanNumFmt(t StyleTable, xf, id int) (StylePlan, error) {
	e, err := baseStyle(t, xf)
	if err != nil {
		return StylePlan{}, err
	}
	if id < 0 {
		return StylePlan{}, fmt.Errorf("invalid number format id %d", id)
	}
	e.set("numFmtId", strconv.Itoa(id))
	e.set("applyNumberFormat", "1")
	return finishStyle(t, e, StylePlan{})
}

// PlanFormat plans xf with f applied:
//   - NumberFormat: a code numfmt.BuiltIn has keeps its built-in id;
//     another reuses an existing custom numFmt with the same code or
//     appends one with id max(163, largest custom id)+1.
//   - Bold, Italic, Underline, Color: the xf's font (fontId) is copied
//     with <b/>, <i/>, <u/> (single), <color rgb="FFRRGGBB"/> set or
//     removed, children in x:CT_Font order; an identical existing font is
//     reused, else the copy is appended; applyFont="1".
//   - Fill: <fill><patternFill patternType="solid"><fgColor
//     rgb="FFRRGGBB"/><bgColor indexed="64"/></patternFill></fill>,
//     reused or appended; applyFill="1".
//   - Align, Valign, Wrap: the xf's <alignment> gains or changes
//     horizontal, vertical, wrapText; applyAlignment="1".
//   - Values are checked with schema.CheckAttr (ST_HorizontalAlignment,
//     ST_VerticalAlignment, ST_UnsignedIntHex for rgb) first; a bad one
//     is an error and the plan is empty.
//
// The resulting xf is reused when an identical one exists.
func PlanFormat(t StyleTable, xf int, f CellFormat) (StylePlan, error) {
	return planFormat(t, xf, f)
}

// styleElement retains a style element's source bytes while allowing
// attributes to be edited: its start tag is re-rendered with
// xmlscan.EditTag (an untouched attribute keeps its spelling) and its
// inner bytes are kept as written.
type styleElement struct {
	name  string
	src   string
	tok   xmlscan.Token
	edits []xmlscan.AttrEdit
	inner string
}

// parseStyle parses one whole style element, returning its editable form or errLex.
func parseStyle(s string) (styleElement, error) {
	tok, err := xmlscan.Next(s, 0)
	if err != nil {
		return styleElement{}, errLex
	}
	if !tok.IsElement() {
		return styleElement{}, errLex
	}
	contentEnd, end, err := xmlscan.ElementEnd(s, tok)
	if err != nil || end != len(s) {
		return styleElement{}, errLex
	}
	e := styleElement{name: tok.Name(s), src: s, tok: tok}
	if tok.Kind == xmlscan.StartTag {
		e.inner = s[tok.End:contentEnd]
	}
	return e, nil
}

// get returns the decoded value of attribute name, edits included; ok
// is false when it has none.
func (e styleElement) get(name string) (string, bool) {
	for i := len(e.edits) - 1; i >= 0; i-- {
		if e.edits[i].Name == name {
			return e.edits[i].Value, !e.edits[i].Remove
		}
	}
	a, ok := xmlscan.FindAttr(e.src, e.tok, name)
	if !ok {
		return "", false
	}
	return a.Value(e.src), true
}

// set edits attribute name to value, as xmlscan.EditTag applies it.
func (e *styleElement) set(name, value string) {
	e.edits = append(e.edits, xmlscan.AttrEdit{Name: name, Value: value})
}

// attrs returns the element's attributes with its edits applied, as
// decoded (name, value) pairs in order.
func (e styleElement) attrs() []AttrEdit {
	var out []AttrEdit
	for a := range xmlscan.Attrs(e.src, e.tok) {
		out = append(out, AttrEdit{Name: a.Name(e.src), Value: a.Value(e.src)})
	}
	edits := make([]AttrEdit, 0, len(e.edits))
	for _, ed := range e.edits {
		edits = append(edits, AttrEdit{Name: ed.Name, Value: ed.Value, Remove: ed.Remove})
	}
	return applyAttrEdits(out, edits)
}

// text renders the element with its attribute edits applied and returns its XML.
func (e styleElement) text() (string, error) {
	var tag string
	if e.src == "" {
		// A synthesized element (the alignment element): no source tag
		// to edit, so the attributes are written out afresh.
		var b strings.Builder
		b.WriteByte('<')
		b.WriteString(e.name)
		for i, ed := range e.edits {
			if slices.ContainsFunc(e.edits[i+1:], func(later xmlscan.AttrEdit) bool { return later.Name == ed.Name }) {
				continue
			}
			if ed.Remove {
				continue
			}
			b.WriteByte(' ')
			b.WriteString(ed.Name)
			b.WriteString(`="`)
			b.WriteString(xmlscan.EscapeAttr(ed.Value, '"'))
			b.WriteByte('"')
		}
		tag = b.String()
	} else {
		var err error
		if tag, err = xmlscan.EditTag(e.src, e.tok, e.edits...); err != nil {
			return "", err
		}
	}
	if e.src == "" && e.inner == "" {
		return tag + "/>", nil
	}
	if e.src == "" {
		return tag + ">" + e.inner + "</" + e.name + ">", nil
	}
	if e.tok.Kind == xmlscan.EmptyTag {
		if e.inner == "" {
			return tag, nil
		}
		tag = strings.TrimSuffix(tag, "/>") + ">"
	}
	return tag + e.inner + "</" + e.name + ">", nil
}

// equalStyle reports whether a and b are the same element: the same
// name and inner bytes and the same attributes as a set, their values
// compared decoded (attribute spelling does not affect equality).
func equalStyle(a, b styleElement) bool {
	if a.name != b.name || a.inner != b.inner {
		return false
	}
	x, y := a.attrs(), b.attrs()
	if len(x) != len(y) {
		return false
	}
	slices.SortFunc(x, func(i, j AttrEdit) int { return strings.Compare(i.Name, j.Name) })
	slices.SortFunc(y, func(i, j AttrEdit) int { return strings.Compare(i.Name, j.Name) })
	for i := range x {
		if x[i].Name != y[i].Name || x[i].Value != y[i].Value {
			return false
		}
	}
	return true
}

// baseStyle returns the editable xf at index xf, or an error when out of range.
func baseStyle(t StyleTable, xf int) (styleElement, error) {
	if xf < 0 || xf >= len(t.XFs) {
		return styleElement{}, fmt.Errorf("xf index %d out of range", xf)
	}
	return parseStyle(t.XFs[xf])
}

// finishStyle reuses an identical xf in t or plans a new one in p and returns p.
func finishStyle(t StyleTable, e styleElement, p StylePlan) (StylePlan, error) {
	for i, s := range t.XFs {
		other, err := parseStyle(s)
		if err != nil {
			continue
		}
		if equalStyle(e, other) {
			p.XF = i
			return p, nil
		}
	}
	p.XF = len(t.XFs)
	xf, err := e.text()
	if err != nil {
		return StylePlan{}, err
	}
	p.NewXF = xf
	return p, nil
}

// styleChildren splits direct children without interpreting their contents.
func styleChildren(s string) ([]string, error) {
	var out []string
	for i := 0; i < len(s); {
		t, err := xmlscan.Next(s, i)
		if err != nil {
			return nil, errLex
		}
		end := t.End
		switch {
		case t.IsElement():
			_, end, err = xmlscan.ElementEnd(s, t)
			if err != nil {
				return nil, errLex
			}
		case t.Kind == xmlscan.EndTag:
			return nil, errLex
		}
		out = append(out, s[i:end])
		i = end
	}
	return out, nil
}

// childName returns the local name of the element that begins s
// ("" when it does not).
func childName(s string) string {
	t, err := xmlscan.Next(s, 0)
	if err != nil || !t.IsElement() {
		return ""
	}
	return t.Local(s)
}

// internStyle returns the index of value's element in list, or len(list) when absent.
func internStyle(list []string, value string) int {
	a, err := parseStyle(value)
	if err != nil {
		return len(list)
	}
	for i, s := range list {
		b, err := parseStyle(s)
		if err == nil && equalStyle(a, b) {
			return i
		}
	}
	return len(list)
}

// planFormat plans xf with format f applied, returning the reuse or append plan.
func planFormat(t StyleTable, xf int, f CellFormat) (StylePlan, error) {
	e, err := baseStyle(t, xf)
	if err != nil {
		return StylePlan{}, err
	}
	for _, v := range []struct {
		typ, attr string
		val       *string
	}{{"x:CT_CellAlignment", "horizontal", f.Align}, {"x:CT_CellAlignment", "vertical", f.Valign}} {
		if v.val != nil {
			if err := schema.CheckAttr(v.typ, v.attr, *v.val); err != nil {
				return StylePlan{}, err
			}
		}
	}
	for _, v := range []*string{f.Color, f.Fill} {
		if v != nil {
			if len(*v) != 6 {
				return StylePlan{}, fmt.Errorf("color must have six hex digits")
			}
			if err := schema.CheckAttr("x:CT_Color", "rgb", "FF"+*v); err != nil {
				return StylePlan{}, err
			}
		}
	}
	p := StylePlan{}
	if f.NumberFormat != nil {
		id := -1
		for i := 0; i < 164; i++ {
			s, ok := numfmt.BuiltIn(i)
			if ok && s == *f.NumberFormat {
				id = i
				break
			}
		}
		if id < 0 {
			maxID := 163
			for n, s := range t.NumFmts {
				if n > maxID {
					maxID = n
				}
				if s == *f.NumberFormat && (id < 0 || n < id) {
					id = n
				}
			}
			if id < 0 {
				if maxID == int(^uint(0)>>1) {
					return StylePlan{}, fmt.Errorf("number format id overflow")
				}
				id = maxID + 1
				p.NewNumFmt = fmt.Sprintf(`<numFmt numFmtId="%d" formatCode="%s"/>`, id, xmlscan.EscapeAttrMinimal(*f.NumberFormat, '"'))
			}
		}
		e.set("numFmtId", strconv.Itoa(id))
		e.set("applyNumberFormat", "1")
	}
	if f.Bold != nil || f.Italic != nil || f.Underline != nil || f.Color != nil {
		if err := formatFont(t, &e, &p, f); err != nil {
			return StylePlan{}, err
		}
	}
	if f.Fill != nil {
		fill := `<fill><patternFill patternType="solid"><fgColor rgb="FF` + strings.ToUpper(*f.Fill) + `"/><bgColor indexed="64"/></patternFill></fill>`
		id := internStyle(t.Fills, fill)
		if id == len(t.Fills) {
			p.NewFill = fill
		}
		e.set("fillId", strconv.Itoa(id))
		e.set("applyFill", "1")
	}
	if f.Align != nil || f.Valign != nil || f.Wrap != nil {
		if err := formatAlignment(&e, f); err != nil {
			return StylePlan{}, err
		}
	}
	return finishStyle(t, e, p)
}

// formatFont plans xf's font with f's font fields and updates e and p.
func formatFont(t StyleTable, e *styleElement, p *StylePlan, f CellFormat) error {
	s, ok := e.get("fontId")
	id := 0
	var err error
	if ok {
		id, err = strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("invalid font id: %w", err)
		}
	}
	if id < 0 || id >= len(t.Fonts) {
		return fmt.Errorf("font index %d out of range", id)
	}
	font, err := parseStyle(t.Fonts[id])
	if err != nil {
		return err
	}
	children, err := styleChildren(font.inner)
	if err != nil {
		return err
	}
	changes := map[string]string{}
	for _, v := range []struct {
		name  string
		value *bool
	}{{"b", f.Bold}, {"i", f.Italic}, {"u", f.Underline}} {
		if v.value != nil {
			changes[v.name] = ""
			if *v.value {
				changes[v.name] = "<" + v.name + "/>"
			}
		}
	}
	if f.Color != nil {
		changes["color"] = `<color rgb="FF` + strings.ToUpper(*f.Color) + `"/>`
	}
	var rest strings.Builder
	for _, c := range children {
		if _, found := changes[childName(c)]; !found {
			rest.WriteString(c)
		}
	}
	var added strings.Builder
	for _, name := range []string{"b", "i", "u", "color"} {
		added.WriteString(changes[name])
	}
	font.inner = added.String() + rest.String()
	result, err := font.text()
	if err != nil {
		return err
	}
	id = internStyle(t.Fonts, result)
	if id == len(t.Fonts) {
		p.NewFont = result
	}
	e.set("fontId", strconv.Itoa(id))
	e.set("applyFont", "1")
	return nil
}

// formatAlignment plans xf's alignment with f's alignment fields and sets applyAlignment.
func formatAlignment(e *styleElement, f CellFormat) error {
	children, err := styleChildren(e.inner)
	if err != nil {
		return err
	}
	found := -1
	for i, c := range children {
		if childName(c) == "alignment" {
			found = i
			break
		}
	}
	a := styleElement{name: "alignment"}
	if found >= 0 {
		a, err = parseStyle(children[found])
		if err != nil {
			return err
		}
	}
	if f.Align != nil {
		a.set("horizontal", *f.Align)
	}
	if f.Valign != nil {
		a.set("vertical", *f.Valign)
	}
	if f.Wrap != nil {
		v := "0"
		if *f.Wrap {
			v = "1"
		}
		a.set("wrapText", v)
	}
	if found >= 0 {
		children[found], err = a.text()
		if err != nil {
			return err
		}
	} else {
		pos := len(children)
		for i, c := range children {
			if n := childName(c); n == "protection" || n == "extLst" {
				pos = i
				break
			}
		}
		children = append(children, "")
		copy(children[pos+1:], children[pos:])
		if children[pos], err = a.text(); err != nil {
			return err
		}
	}
	e.inner = strings.Join(children, "")
	e.set("applyAlignment", "1")
	return nil
}
