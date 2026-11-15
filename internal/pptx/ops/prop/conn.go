package prop

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/edit"
)

// GlueEnd is one glued end of a connector: the glue id as written, the
// site index and, when the id names exactly one object on the slide, its
// short id as the views show it.
type GlueEnd struct {
	RawID string
	Site  int
	Short string
}

// ShowGlue shows a glue end as the views do: "s12:3", "#17:3" when the
// id does not name exactly one object, or "none".
func ShowGlue(g *GlueEnd) string {
	if g == nil {
		return "none"
	}
	id := g.Short
	if id == "" {
		id = "#" + g.RawID
	}
	return id + ":" + strconv.Itoa(g.Site)
}

var pathCNvCxnSpPr = edit.Path{edit.P("nvCxnSpPr"), edit.P("cNvCxnSpPr")}

// Glue returns the codec of a connector's start ("stCxn") or end
// ("endCxn") glue. It reads the element as the read model does and
// resolves the id with Workspace.ResolveGlue; a write sets the id and
// site, or removes the glue for nil. The connector's geometry is not
// touched: the agent supplies both glue and endpoints.
func Glue(el string) Codec[*GlueEnd] {
	p := append(append(edit.Path{}, pathCNvCxnSpPr...), edit.A(el))
	name := map[string]string{"stCxn": "from_glue", "endCxn": "to_glue"}[el]
	return Codec[*GlueEnd]{
		Name: name,
		Read: func(h *edit.Handle, i int) derive.Val[*GlueEnd] {
			if h.Elem(i).Local() != "cxnSp" {
				return derive.Missing[*GlueEnd]("not a connector")
			}
			n := h.FindIn(i, p)
			if n == nil {
				return derive.Got[*GlueEnd](nil, derive.FromSlide)
			}
			raw, _ := n.Attr("id")
			idx, _ := n.Attr("idx")
			site, err := strconv.Atoi(idx)
			if err != nil {
				return derive.Missing[*GlueEnd]("glue site index " + strconv.Quote(idx) + " is not an integer")
			}
			g := &GlueEnd{RawID: raw, Site: site}
			if r, err := h.ResolveGlue(raw); err == nil {
				g.Short = r.Short()
			}
			return derive.Got(g, derive.FromSlide)
		},
		Write: func(h *edit.Handle, g *GlueEnd) error {
			if h.Kind() != "cxnSp" {
				return fmt.Errorf("%w: only connectors (p:cxnSp) are glued", ErrNoProperty)
			}
			if g == nil {
				return h.Remove(p)
			}
			if err := edit.CheckAttr("a:CT_Connection", "idx", strconv.Itoa(g.Site)); err != nil {
				return fmt.Errorf("glue site: %w", err)
			}
			if err := edit.CheckAttr("a:CT_Connection", "id", g.RawID); err != nil {
				return fmt.Errorf("glue id: %w", err)
			}
			if err := h.Ensure(pathCNvCxnSpPr, edit.A(el)); err != nil {
				return err
			}
			if err := h.SetAttr(p, "id", g.RawID); err != nil {
				return err
			}
			return h.SetAttr(p, "idx", strconv.Itoa(g.Site))
		},
		Show: ShowGlue,
	}
}

// Ends are a connector's endpoints in slide space (EMU) and, for elbow
// connectors, the direction of the first segment.
type Ends struct {
	From, To derive.Pt
	First    string
}

// Endpoints returns the endpoint codec of connectors with a tabulated
// connector preset, shown in units u. Its read half is derive.
// ConnectorGeom over the transform the read model reads. A write inverts
// it with derive.ConnectorXfrm under each branch's parent map, keeping the
// stored rotation and flips when they still express the endpoints (and
// First), else rotation 0, or 90 degrees for First "v", and stores the
// result in whole EMU. Adjust values (elbow and curve positions, as
// fractions of the box) are kept.
func Endpoints(u Units) Codec[Ends] {
	return Codec[Ends]{
		Name:  "endpoints",
		Read:  readEnds,
		Write: writeEnds,
		Show: func(e Ends) string {
			s := "from=[" + u.Length(e.From.X) + " " + u.Length(e.From.Y) + "] to=[" + u.Length(e.To.X) + " " + u.Length(e.To.Y) + "]"
			if e.First != "" {
				s += " first=" + e.First
			}
			return s
		},
	}
}

// readEnds derives branch i's connector endpoints from its geometry and transform.
func readEnds(h *edit.Handle, i int) derive.Val[Ends] {
	g, ok := readGeom(h, i).Get()
	switch {
	case !ok:
		return derive.Missing[Ends]("no geometry")
	case g.Custom:
		return derive.Missing[Ends]("custom geometry connector: endpoints not derived")
	case !derive.IsConnectorPreset(g.Preset):
		return derive.Missing[Ends]("preset " + g.Preset + " is not a connector preset")
	}
	tr := transformRead(h, i)
	if tr.StoredErr != nil || tr.PlaceErr != nil {
		return derive.Missing[Ends]("no slide-space transform")
	}
	s := tr.Stored.Float()
	c, err := derive.ConnectorGeom(g.Preset, g.Adj, s.W, s.H, tr.Placed.M)
	if err != nil {
		return derive.Missing[Ends](err.Error())
	}
	return derive.Got(Ends{From: c.From, To: c.To, First: c.First}, tr.Origin)
}

// LocalFrame returns the projected branch's local-to-slide map and its
// stored extent, as the read model derives them.
func LocalFrame(h *edit.Handle) (derive.Affine, float64, float64, error) {
	tr := transformRead(h, 0)
	switch {
	case tr.StoredErr != nil:
		return derive.Affine{}, 0, 0, tr.StoredErr
	case tr.PlaceErr != nil:
		return derive.Affine{}, 0, 0, tr.PlaceErr
	}
	s := tr.Stored.Float()
	return tr.Placed.M, s.W, s.H, nil
}

// writeEnds stores the wanted connector endpoints by inverting the connector geometry.
func writeEnds(h *edit.Handle, want Ends) error {
	g, ok := readGeom(h, 0).Get()
	if !ok || g.Custom || !derive.IsConnectorPreset(g.Preset) {
		return fmt.Errorf("%w: endpoints need a connector preset geometry", ErrNoProperty)
	}
	first := ""
	if strings.HasPrefix(g.Preset, "bentConnector") {
		first = want.First
	}
	stored := make([]derive.StoredXfrm, h.Branches())
	for i := range h.Branches() {
		pm, err := h.ParentMap(i)
		if err != nil {
			return err
		}
		var cur *derive.Xfrm
		if tr := transformRead(h, i); tr.StoredErr == nil {
			x := tr.Stored.Float()
			cur = &x
		}
		x, err := derive.ConnectorXfrm(want.From, want.To, first, cur, pm)
		if err != nil {
			return err
		}
		stored[i] = round(x)
	}
	return writeStored(h, stored)
}
