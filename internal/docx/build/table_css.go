package build

import (
	"math"
	"slices"
	"strconv"
	"strings"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/html"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// The style of a table, row or cell, as read writes it (view's tableCSS),
// stored as the properties it describes: the inverse of that rendering,
// property by property. Anything else in the style, or a property the
// element cannot carry, is refused (an AttributeError for style).

// scope is the element a style belongs to.
type scope uint8

const (
	scopeTable scope = iota
	scopeRow
	scopeCell
)

// tableCSS writes css into bag, the element's w:tblPr, w:trPr or w:tcPr.
func tableCSS(h *core.Handle, bag *tree.Node, sc scope, css string) error {
	if css == "" {
		return nil
	}
	decls, err := html.Declarations(css)
	if err != nil {
		return styleErr("%s", err.Error())
	}
	m := map[string]string{}
	for _, d := range decls {
		m[d.Name] = d.Value
	}
	w := cssWriter{h: h, bag: bag, sc: sc}
	// Declarations sorts by name, so the properties are written, and
	// errors found, in the same order for the same style.
	for _, d := range decls {
		if err := w.decl(d.Name, d.Value, m); err != nil {
			return err
		}
	}
	return nil
}

// cssWriter writes one element's style.
type cssWriter struct {
	h   *core.Handle
	bag *tree.Node
	sc  scope
}

// in refuses name unless the element is one of scopes.
func (w cssWriter) in(name string, scopes ...scope) error {
	if !slices.Contains(scopes, w.sc) {
		return styleErr("%s is not a property of a table %s", name, [...]string{"", "row", "cell"}[w.sc])
	}
	return nil
}

// decl applies one CSS declaration to the current table, row or cell scope.
func (w cssWriter) decl(name, v string, all map[string]string) error {
	switch name {
	case "width":
		if err := w.in(name, scopeTable, scopeCell); err != nil {
			return err
		}
		tag := "tblW"
		if w.sc == scopeCell {
			tag = "tcW"
		}
		return w.width(tag, v)
	case "table-layout":
		if err := w.in(name, scopeTable); err != nil {
			return err
		}
		t, ok := map[string]string{"fixed": "fixed", "auto": "autofit"}[v]
		if !ok {
			return styleErr("table-layout must be fixed or auto")
		}
		return w.attrs("tblLayout", "type", t)
	case "margin-left", "margin-right":
		if err := w.in(name, scopeTable, scopeRow); err != nil {
			return err
		}
		if name == "margin-right" {
			return nil // written with margin-left
		}
		jc, ok := map[[2]string]string{{"auto", "auto"}: "center", {"auto", "0"}: "right", {"0", "auto"}: "left"}[[2]string{zero(v), zero(all["margin-right"])}]
		if !ok {
			return styleErr("a table's margin-left and margin-right are its alignment: auto auto (center), auto 0 (right) or 0 auto (left)")
		}
		return w.attrs("jc", "val", jc)
	case "background-color":
		if err := w.in(name, scopeTable, scopeCell); err != nil {
			return err
		}
		fill, ok := hexColor(v)
		if !ok {
			return styleErr("background-color must be #RRGGBB")
		}
		return w.attrs("shd", "val", "clear", "color", "auto", "fill", fill)
	case "vertical-align":
		if err := w.in(name, scopeCell); err != nil {
			return err
		}
		a, ok := map[string]string{"top": "top", "middle": "center", "bottom": "bottom"}[v]
		if !ok {
			return styleErr("vertical-align must be top, middle or bottom")
		}
		return w.attrs("vAlign", "val", a)
	case "height":
		if err := w.in(name, scopeRow); err != nil {
			return err
		}
		tw, err := points(name, v)
		if err != nil {
			return err
		}
		return w.attrs("trHeight", "val", tw)
	case "break-inside":
		if err := w.in(name, scopeRow); err != nil {
			return err
		}
		if v != "avoid" {
			return styleErr("break-inside must be avoid")
		}
		_, err := w.ensure("cantSplit")
		return err
	case "border-collapse":
		if err := w.in(name, scopeTable, scopeRow); err != nil {
			return err
		}
		if v != "separate" || all["border-spacing"] == "" {
			return styleErr("border-collapse is written only as separate, with border-spacing")
		}
		return nil
	case "border-spacing":
		if err := w.in(name, scopeTable, scopeRow); err != nil {
			return err
		}
		if all["border-collapse"] != "separate" {
			return styleErr("border-spacing needs border-collapse:separate")
		}
		tw, err := points(name, v)
		if err != nil {
			return err
		}
		return w.attrs("tblCellSpacing", "w", tw, "type", "dxa")
	case "padding-top", "padding-left", "padding-bottom", "padding-right":
		if err := w.in(name, scopeTable, scopeCell); err != nil {
			return err
		}
		tw, err := points(name, v)
		if err != nil {
			return err
		}
		return w.side("tblCellMar", "tcMar", strings.TrimPrefix(name, "padding-"), "w", tw, "type", "dxa")
	case "border-top", "border-left", "border-bottom", "border-right":
		if err := w.in(name, scopeTable, scopeCell); err != nil {
			return err
		}
		kv, err := border(name, v)
		if err != nil {
			return err
		}
		return w.side("tblBorders", "tcBorders", strings.TrimPrefix(name, "border-"), kv...)
	}
	return styleErr("%s is not a table property docx stores (docx help html)", name)
}

// width writes a preferred width: auto, a percentage or points.
func (w cssWriter) width(tag, v string) error {
	switch {
	case v == "auto":
		return w.attrs(tag, "w", "0", "type", "auto")
	case strings.HasSuffix(v, "%"):
		f, ok := number(strings.TrimSuffix(v, "%"))
		if !ok || f <= 0 || f > 100 {
			return styleErr("a percentage width must be more than 0 and at most 100")
		}
		return w.attrs(tag, "w", strconv.Itoa(int(math.Round(f*50))), "type", "pct")
	}
	tw, err := points("width", v)
	if err != nil {
		return err
	}
	return w.attrs(tag, "w", tw, "type", "dxa")
}

// side writes one side's attributes in the table's or the cell's
// container of sides.
func (w cssWriter) side(tableTag, cellTag, side string, kv ...string) error {
	tag := tableTag
	if w.sc == scopeCell {
		tag = cellTag
	}
	c, err := w.ensure(tag)
	if err != nil {
		return err
	}
	e, err := w.h.EnsureIn(c, docxedit.W(side))
	if err != nil {
		return err
	}
	return setAttrs(w.h, e, kv)
}

// attrs ensures the property tag and sets its attributes.
func (w cssWriter) attrs(tag string, kv ...string) error {
	e, err := w.ensure(tag)
	if err != nil {
		return err
	}
	return setAttrs(w.h, e, kv)
}

// ensure returns the property tag, created where it belongs.
func (w cssWriter) ensure(tag string) (*tree.Node, error) {
	if w.sc == scopeRow {
		return ensureRow(w.h, w.bag, tag)
	}
	return w.h.EnsureIn(w.bag, docxedit.W(tag))
}

// setAttrs sets attribute pairs kv on element e, returning the first error.
func setAttrs(h *core.Handle, e *tree.Node, kv []string) error {
	for i := 0; i+1 < len(kv); i += 2 {
		if err := h.SetNodeAttr(e, wq(e, kv[i]), kv[i+1]); err != nil {
			return err
		}
	}
	return nil
}

// RowPropertyOrder is the order Word writes row properties in: CT_TrPrBase
// is a repeated choice, which fixes no order (the table operations write
// them in the same order).
var RowPropertyOrder = []string{
	"cnfStyle", "divId", "gridBefore", "gridAfter", "wBefore", "wAfter", "cantSplit", "trHeight",
	"tblHeader", "tblCellSpacing", "jc", "hidden", "ins", "del", "trPrChange",
}

// ensureRow returns w:trPr's child w:tag, placing a new one before the
// first child RowPropertyOrder puts after it.
func ensureRow(h *core.Handle, trPr *tree.Node, tag string) (*tree.Node, error) {
	rank := slices.Index(RowPropertyOrder, tag)
	var before *tree.Node
	for _, c := range trPr.Elements() {
		if c.Local() == tag {
			return c, nil
		}
		if before == nil && slices.Index(RowPropertyOrder, c.Local()) > rank {
			before = c
		}
	}
	e, err := parseOne(h, trPr, `<w:`+tag+`/>`)
	if err != nil {
		return nil, err
	}
	return e, h.PlaceAt(trPr, e, before)
}

// zero spells a zero margin one way ("0" or "0pt").
func zero(v string) string {
	if v == "0pt" {
		return "0"
	}
	return v
}

// number parses a finite, non-negative decimal.
func number(s string) (float64, bool) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f < 0 {
		return 0, false
	}
	return f, true
}

// points converts a length in points to whole twips.
func points(name, v string) (string, error) {
	f, ok := number(strings.TrimSuffix(v, "pt"))
	if !ok || !strings.HasSuffix(v, "pt") || f*20 > math.MaxInt32 {
		return "", styleErr("%s must be a length in points (for example 12pt)", name)
	}
	return strconv.Itoa(int(math.Round(f * 20))), nil
}

// hexColor reads #RRGGBB as RRGGBB.
func hexColor(v string) (string, bool) {
	if len(v) != 7 || v[0] != '#' {
		return "", false
	}
	for _, r := range v[1:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return "", false
		}
	}
	return strings.ToUpper(v[1:]), true
}

// borderStyles maps the border styles read writes to w:val.
var borderStyles = map[string]string{"none": "nil", "solid": "single", "double": "double", "dotted": "dotted", "dashed": "dashed"}

// border reads "<width>pt <style> <color>" (color #RRGGBB or
// currentColor) as w:val, w:sz (eighths of a point) and w:color.
func border(name, v string) ([]string, error) {
	f := strings.Fields(v)
	if len(f) != 3 {
		return nil, styleErr("%s must be <width>pt <style> <color>, as read writes it", name)
	}
	pt, ok := number(strings.TrimSuffix(f[0], "pt"))
	if !ok || !strings.HasSuffix(f[0], "pt") || pt > 1000 {
		return nil, styleErr("%s width must be a length in points (for example 0.5pt)", name)
	}
	val, ok := borderStyles[f[1]]
	if !ok {
		return nil, styleErr("%s style must be none, solid, double, dotted or dashed", name)
	}
	color := "auto"
	if f[2] != "currentColor" {
		if color, ok = hexColor(f[2]); !ok {
			return nil, styleErr("%s color must be #RRGGBB or currentColor", name)
		}
	}
	sz := strconv.Itoa(int(math.Round(pt * 8))) // w:sz is in eighths of a point
	return []string{"val", val, "sz", sz, "space", "0", "color", color}, nil
}
