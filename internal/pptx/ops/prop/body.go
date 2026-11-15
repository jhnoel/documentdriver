package prop

import (
	"errors"
	"fmt"

	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/edit"
	"github.com/jhnoel/documentdriver/internal/pptx/show"
)

// BodyVal is a text body's properties as the views show them (`text` in
// --detail full): the stored value of each attribute of derive.BodyAttrs
// ("" where not derived) and the autofit choice (mode, and for shrink the
// stored fontScale and lnSpcReduction).
type BodyVal = show.Body

var pathBodyPr = edit.Path{edit.P("txBody"), edit.A("bodyPr")}

// ErrNoTextBody is returned when body properties are written to a shape
// without a text body.
var ErrNoTextBody = errors.New("prop: the shape has no text body")

// Body returns the codec of a shape's text body properties, shown in
// units u. Its read half is derive.ReadBodyAttr and derive.ReadAutofit
// over the placeholder chain, as in the read model (a value found
// nowhere is the one the specification implies, origin spec), and it
// shows through show.BodyJSON, as the views do. A write compares
// property by property in the shown form and sets only those that
// differ, on the shape's own a:bodyPr: attributes, and the autofit choice
// through Ensure (which replaces the choice's other element). The rest of
// a:bodyPr keeps its bytes. An inherited value cannot be unset.
func Body(u Units) Codec[BodyVal] {
	num := func(emu float64) string { return u.Length(emu) }
	return Codec[BodyVal]{
		Name:  "text",
		Read:  readBody,
		Write: func(h *edit.Handle, b BodyVal) error { return writeBody(h, b, num) },
		Show:  func(b BodyVal) string { return string(show.BodyJSON(b, num)) },
	}
}

// ReadBodyChain reads a text body's properties through an object's
// placeholder chain, with each value's origin: the read half the read
// model shares.
func ReadBodyChain(chain []derive.Link) (BodyVal, map[string]derive.Origin) {
	b := BodyVal{Attrs: map[string]string{}}
	org := map[string]derive.Origin{}
	for _, a := range derive.BodyAttrs {
		if v, o, ok := derive.ReadBodyAttr(chain, a); ok {
			b.Attrs[a], org[a] = v, o
		}
	}
	b.Autofit = derive.ReadAutofit(chain)
	org["autofit"] = b.Autofit.Origin
	return b, org
}

// readBody reads branch i's text body properties through its placeholder chain.
func readBody(h *edit.Handle, i int) derive.Val[BodyVal] {
	if k := h.Elem(i).Local(); k != "sp" {
		return derive.Missing[BodyVal]("a " + k + " holds no text")
	}
	if h.FindIn(i, edit.Path{edit.P("txBody")}) == nil {
		return derive.Missing[BodyVal]("no text body")
	}
	chain, err := h.Chain(i)
	if err != nil {
		return derive.Missing[BodyVal](err.Error())
	}
	b, org := ReadBodyChain(chain)
	return derive.Got(b, org["anchor"])
}

// shownBody is one value as shown, "" when not shown.
func shownBody(key, v string, num func(float64) string) string {
	s, ok := show.BodyValue(key, v, num)
	if !ok {
		return ""
	}
	return string(s)
}

// writeBody writes the wanted text body properties and autofit choice onto the shape.
func writeBody(h *edit.Handle, want BodyVal, num func(float64) string) error {
	if h.Kind() != "sp" {
		return fmt.Errorf("%w: a %s holds no text", ErrNoProperty, h.Kind())
	}
	for i := range h.Branches() {
		if h.FindIn(i, pathBodyPr) == nil {
			return fmt.Errorf("%w (branch %d); give html to create one", ErrNoTextBody, i)
		}
	}
	var cur BodyVal
	if v, ok := readBody(h, 0).Get(); ok {
		cur = v
	}
	type set struct{ attr, v string }
	var sets []set
	for _, a := range derive.BodyAttrs {
		w, c := want.Attrs[a], cur.Attrs[a]
		switch {
		case shownBody(a, w, num) == shownBody(a, c, num):
		case w == "":
			return fmt.Errorf("text %s: cannot unset an inherited value", a)
		default:
			sets = append(sets, set{a, w})
		}
	}
	// Every value in its schema type before anything is written.
	for _, s := range sets {
		if err := edit.CheckAttr("a:CT_TextBodyProperties", s.attr, s.v); err != nil {
			return fmt.Errorf("text %s: %w", s.attr, err)
		}
	}
	wa, ca := want.Autofit, cur.Autofit
	elem, ok := derive.AutofitElems[wa.Mode]
	if !ok {
		return fmt.Errorf("text autofit %q: want none, shape or shrink", wa.Mode)
	}
	type scale struct{ attr, want, cur string }
	scales := []scale{{"fontScale", wa.FontScale, ca.FontScale}, {"lnSpcReduction", wa.LnSpcReduction, ca.LnSpcReduction}}
	if wa.Mode != "shrink" {
		for _, s := range scales {
			if s.want != "" {
				return fmt.Errorf("text %s: only with autofit shrink", s.attr)
			}
		}
	}
	for _, s := range scales {
		if s.want != "" {
			if err := edit.CheckAttr("a:CT_TextNormalAutofit", s.attr, s.want); err != nil {
				return fmt.Errorf("text %s: %w", s.attr, err)
			}
		}
	}
	for _, s := range sets {
		if err := h.SetAttr(pathBodyPr, s.attr, s.v); err != nil {
			return err
		}
	}
	modeChanged := wa.Mode != ca.Mode
	scaleChanged := false
	for _, s := range scales {
		if shownBody(s.attr, s.want, num) != shownBody(s.attr, s.cur, num) {
			scaleChanged = true
		}
	}
	if !modeChanged && !scaleChanged {
		return nil
	}
	// The choice on the shape's own a:bodyPr: Ensure keeps an element of
	// the mode wanted and replaces another of the choice with an empty
	// one, so a new a:normAutofit holds no scale unless one is given.
	if err := h.Ensure(pathBodyPr, edit.A(elem)); err != nil {
		return err
	}
	if wa.Mode != "shrink" {
		return nil
	}
	el := append(append(edit.Path{}, pathBodyPr...), edit.A(elem))
	for _, s := range scales {
		if s.want == "" {
			if err := h.RemoveAttr(el, s.attr); err != nil {
				return err
			}
			continue
		}
		if err := h.SetAttr(el, s.attr, s.want); err != nil {
			return err
		}
	}
	return nil
}
