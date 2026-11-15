package prop

import (
	"errors"
	"fmt"
	"slices"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/edit"
	"github.com/jhnoel/documentdriver/internal/pptx/show"
)

// ErrReadOnly is returned for properties shown but not yet writable.
var ErrReadOnly = errors.New("prop: not writable")

// GeomVal is an object's geometry as the views show it: a preset with its
// adjust values in stored order, or a custom geometry.
type GeomVal struct {
	Preset string
	Adj    []derive.Adjust
	Custom bool
}

var (
	pathSpPr  = edit.Path{edit.P("spPr")}
	pathPrst  = edit.Path{edit.P("spPr"), edit.A("prstGeom")}
	pathAvLst = edit.Path{edit.P("spPr"), edit.A("prstGeom"), edit.A("avLst")}
)

// Geometry returns the geometry codec. Its read half is derive.ReadGeom
// over the object's inheritance chain, as in the read model. Writes go to
// the object's own a:prstGeom: the preset name, then the adjust values
// exactly as given (guides not in the value are removed, existing ones
// keep their place, new ones are appended in the order given). A preset
// written over a custom geometry replaces it (the explicit switch from
// custGeom to prstGeom); custom geometry itself is written by Path.
func Geometry() Codec[GeomVal] {
	return Codec[GeomVal]{
		Name:  "geom",
		Read:  readGeom,
		Write: writeGeom,
		Show: func(g GeomVal) string {
			s := "geom=" + show.Geom(derive.Geom{Preset: g.Preset, Custom: g.Custom})
			if len(g.Adj) > 0 {
				s += " adj=" + string(show.Adj(g.Adj))
			}
			return s
		},
	}
}

// readGeom reads branch i's geometry through its inheritance chain.
func readGeom(h *edit.Handle, i int) derive.Val[GeomVal] {
	chain, err := h.Chain(i)
	if err != nil {
		return derive.Missing[GeomVal](err.Error())
	}
	g, origin, ok := derive.ReadGeom(chain)
	if !ok {
		return derive.Missing[GeomVal]("no prstGeom or custGeom on the object or its placeholders")
	}
	return derive.Got(GeomVal{Preset: g.Preset, Adj: g.Adj, Custom: g.Custom}, origin)
}

// own reports whether branch i has a geometry element of its own (not
// inherited from a placeholder), and whether it is custom.
func ownGeom(h *edit.Handle, i int) (has, custom bool) {
	if h.FindIn(i, pathPrst) != nil {
		return true, false
	}
	if h.FindIn(i, edit.Path{edit.P("spPr"), edit.A("custGeom")}) != nil {
		return true, true
	}
	return false, false
}

// writeGeom writes the wanted preset geometry and its adjust values onto the object.
func writeGeom(h *edit.Handle, want GeomVal) error {
	if want.Custom {
		return fmt.Errorf("%w: a custom geometry is written through its path (the path codec)", ErrReadOnly)
	}
	if want.Preset == "" {
		return errors.New("no preset name")
	}
	if err := edit.CheckAttr("a:CT_PresetGeometry2D", "prst", want.Preset); err != nil {
		return err
	}
	for i := range h.Branches() {
		if h.FindIn(i, pathSpPr) == nil {
			return fmt.Errorf("%s has no shape properties", h.Elem(i).Name())
		}
	}
	if err := h.Ensure(pathSpPr, edit.A("prstGeom")); err != nil {
		return err
	}
	if err := h.SetAttr(pathPrst, "prst", want.Preset); err != nil {
		return err
	}
	return writeAdj(h, want.Adj)
}

// writeAdj makes every branch's avLst hold exactly adj: guides not named
// in adj are removed, existing ones get their formula, new ones are
// appended in order. An object without guides and no avLst gets none.
func writeAdj(h *edit.Handle, adj []derive.Adjust) error {
	want := map[string]string{}
	for _, a := range adj {
		want[a.Name] = a.Fmla
	}
	if len(adj) == 0 {
		for i := range h.Branches() {
			if h.FindIn(i, pathAvLst) != nil {
				return h.RemoveChildren(pathAvLst, func(*tree.Node) bool { return true })
			}
		}
		return nil
	}
	if err := h.Ensure(pathPrst, edit.A("avLst")); err != nil {
		return err
	}
	if err := h.RemoveChildren(pathAvLst, func(n *tree.Node) bool {
		name, _ := n.Attr("name")
		_, keep := want[name]
		return !keep || !derive.Is(n, derive.DML, "gd")
	}); err != nil {
		return err
	}
	have := derive.ReadAdj(h.FindIn(0, pathAvLst))
	for _, a := range adj {
		if !slices.ContainsFunc(have, func(x derive.Adjust) bool { return x.Name == a.Name }) {
			if err := h.AppendEach(pathAvLst, edit.A("gd"), [][2]string{{"name", a.Name}, {"fmla", a.Fmla}}); err != nil {
				return err
			}
		}
	}
	// Existing guides: their formula, by name, in every branch.
	for _, a := range adj {
		if err := h.SetGuide(pathAvLst, a.Name, a.Fmla); err != nil {
			return err
		}
	}
	return nil
}

// Name returns the codec of the object's name (cNvPr name), shown as the
// views show it.
func Name() Codec[string] {
	return Codec[string]{
		Name: "name",
		Read: func(h *edit.Handle, i int) derive.Val[string] {
			if n := h.CNvPr(i); n != nil {
				v, _ := n.Attr("name")
				return derive.Got(v, derive.FromSlide)
			}
			return derive.Missing[string]("no cNvPr")
		},
		Write: func(h *edit.Handle, v string) error { return h.SetCNvPrAttr("name", v) },
		Show:  func(v string) string { return v },
	}
}
