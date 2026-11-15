package prop

import (
	"bytes"
	"fmt"
	"math"
	"strconv"

	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/edit"
	"github.com/jhnoel/documentdriver/internal/pptx/show"
)

// CustomVal is the non-path part of a custom geometry as --detail full
// shows it ("custom"): adjust values, guides and handles as written,
// connection sites and the text rectangle with literal coordinates in
// EMU relative to the box (scaled like the path) and literal angles in
// degrees.
type CustomVal = derive.ShownCustom

// Custom returns the codec of a custom geometry's lists, shown in units
// u. Adjust values, guides and handles are formulas and references the
// tool does not evaluate for writing: they are read-only, writable back
// as shown (an empty list may be added or removed). Connection sites
// (a:cxnLst) and the text rectangle (a:rect) are writable: names are
// written as given (guide references), numbers become whole EMU of the
// shape's own coordinates and whole 60000ths of a degree.
func Custom(u Units) Codec[CustomVal] {
	return Codec[CustomVal]{
		Name:  "custom",
		Read:  readCustom,
		Write: writeCustom,
		Show:  func(v CustomVal) string { return string(show.Custom(&v, u.Length)) },
	}
}

// readCustom reads branch i's custom geometry lists as shown.
func readCustom(h *edit.Handle, i int) derive.Val[CustomVal] {
	f, err := readCustFrame(h, i)
	if err != nil {
		return derive.Missing[CustomVal](err.Error())
	}
	sc, err := derive.ShowCustom(f.cg, f.kx, f.ky)
	if err != nil {
		return derive.Missing[CustomVal](err.Error())
	}
	return derive.Got(sc, derive.FromSlide)
}

// formulas shows the read-only part of a custom geometry: adjust values,
// guides and handles, with their presence.
func formulas(c CustomVal) string {
	r := CustomVal{Av: c.Av, Gd: c.Gd, Ah: c.Ah, HasAv: c.HasAv, HasGd: c.HasGd, HasAh: c.HasAh}
	return string(show.Custom(&r, derive.FormatNum))
}

// emptyLists returns c with formula-list presence kept only for nonempty lists.
func emptyLists(c CustomVal) CustomVal {
	return CustomVal{HasAv: c.HasAv && len(c.Av) > 0, HasGd: c.HasGd && len(c.Gd) > 0, HasAh: c.HasAh && len(c.Ah) > 0, Av: c.Av, Gd: c.Gd, Ah: c.Ah}
}

// writeCustom writes the wanted connection sites and text rectangle, keeping formulas as shown.
func writeCustom(h *edit.Handle, want CustomVal) error {
	for i := range h.Branches() {
		if _, custom := ownGeom(h, i); !custom {
			return fmt.Errorf("%w: custom: the object has no custom geometry of its own; give path first", ErrNoProperty)
		}
	}
	cur, ok := readCustom(h, 0).Get()
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotDerived, readCustom(h, 0).Reason())
	}
	// Formulas: written back as shown; only empty lists may come or go.
	if formulas(emptyLists(cur)) != formulas(emptyLists(want)) {
		return fmt.Errorf("%w: custom av, gd and ah (adjust values, guides and handles) are formulas; write them back as shown", ErrReadOnly)
	}
	for _, l := range []struct {
		name      string
		have, set bool
	}{{"avLst", cur.HasAv, want.HasAv}, {"gdLst", cur.HasGd, want.HasGd}, {"ahLst", cur.HasAh, want.HasAh}} {
		if err := presence(h, l.name, l.have, l.set); err != nil {
			return err
		}
	}
	n := h.Branches()
	// Sites.
	if want.HasCxn {
		if err := h.Ensure(pathCust, edit.A("cxnLst")); err != nil {
			return err
		}
		frags := make([][]byte, n)
		for i := range n {
			f, err := readCustFrame(h, i)
			if err != nil {
				return err
			}
			pfx := h.FindIn(i, pathCxnLst).Prefix()
			name := func(local string) string {
				if pfx == "" {
					return local
				}
				return pfx + ":" + local
			}
			var b bytes.Buffer
			for k, s := range want.Cxn {
				ang, err := storedAngle(s[0])
				if err != nil {
					return fmt.Errorf("custom cxn %d: %w", k, err)
				}
				x, err := storedCoord(s[1], f.kx)
				if err != nil {
					return fmt.Errorf("custom cxn %d x: %w", k, err)
				}
				y, err := storedCoord(s[2], f.ky)
				if err != nil {
					return fmt.Errorf("custom cxn %d y: %w", k, err)
				}
				b.WriteString("<" + name("cxn") + ` ang="` + escAttr(ang) + `"><` + name("pos") + ` x="` + escAttr(x) + `" y="` + escAttr(y) + `"/></` + name("cxn") + ">")
			}
			var old bytes.Buffer
			for _, c := range h.FindIn(i, pathCxnLst).Children() {
				old.Write(c.Bytes())
			}
			if !bytes.Equal(old.Bytes(), b.Bytes()) {
				frags[i] = b.Bytes()
				if frags[i] == nil {
					frags[i] = []byte{}
				}
			}
		}
		if err := h.ReplaceChildren(pathCxnLst, frags); err != nil {
			return err
		}
	} else if err := h.Remove(pathCxnLst); err != nil {
		return err
	}
	// Text rectangle.
	if want.Rect == nil {
		return h.Remove(pathRect)
	}
	if err := h.Ensure(pathCust, edit.A("rect")); err != nil {
		return err
	}
	edits := make([][]edit.AttrEdit, n)
	for i := range n {
		f, err := readCustFrame(h, i)
		if err != nil {
			return err
		}
		r := h.FindIn(i, pathRect)
		for k, a := range []string{"l", "t", "r", "b"} {
			kk := f.kx
			if k%2 == 1 {
				kk = f.ky
			}
			v, err := storedCoord(want.Rect[k], kk)
			if err != nil {
				return fmt.Errorf("custom rect %s: %w", a, err)
			}
			if old, ok := r.Attr(a); !ok || old != v {
				edits[i] = append(edits[i], edit.AttrEdit{Path: pathRect, Name: a, Value: v})
			}
		}
	}
	return h.ApplyEach(edits)
}

// presence adds an empty list or removes an empty one.
func presence(h *edit.Handle, name string, have, want bool) error {
	p := append(append(edit.Path{}, pathCust...), edit.A(name))
	switch {
	case want && !have:
		return h.Ensure(pathCust, edit.A(name))
	case have && !want:
		return h.Remove(p)
	}
	return nil
}

// storedCoord turns a shown coordinate into its stored form: a guide name
// as given, or EMU relative to the box divided by the display scale and
// rounded to whole EMU.
func storedCoord(r derive.ShownRef, k float64) (string, error) {
	if r.Name != "" {
		if err := edit.CheckAttr("a:CT_GeomRect", "l", r.Name); err != nil {
			return "", err
		}
		return r.Name, nil
	}
	if k == 0 {
		return "", fmt.Errorf("%w: the shape's groups collapse an axis", ErrInexpressible)
	}
	v := strconv.FormatInt(int64(math.Round(r.V/k)), 10)
	if err := edit.CheckAttr("a:CT_GeomRect", "l", v); err != nil {
		return "", fmt.Errorf("%w: %w", ErrInexpressible, err)
	}
	return v, nil
}

// storedAngle turns a shown site angle into its stored form: a guide name
// as given, or degrees as whole 60000ths.
func storedAngle(r derive.ShownRef) (string, error) {
	v := r.Name
	if v == "" {
		v = strconv.FormatInt(int64(derive.RoundAngle(r.V)), 10)
	}
	if err := edit.CheckAttr("a:CT_ConnectionSite", "ang", v); err != nil {
		return "", err
	}
	return v, nil
}
