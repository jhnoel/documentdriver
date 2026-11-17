package model

import (
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/place"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// Options select what the builder derives beyond the default view:
// Sites adds connection sites; Full adds origins, raw cells and
// omissions.
type Options struct {
	Sites, Full bool
}

// BuildPage builds the model of page e of d. The page's contents are
// parsed once and walked iteratively (a stack of group frames, so the
// depth is the tokenizer's cap, never the Go stack); each shape is
// indexed once and resolved through r. An object the builder cannot
// model is kept with Kind other and an omission; it never fails the
// page. Chunk M1 (walk, kinds, addresses, z, the placed map); the
// per-object values are M2a to M2c, M3, M5 to M7; glue is M4.
func BuildPage(d *doc.Document, r *inherit.Resolver, e *doc.PageEntry, o Options) (*Page, error) {
	p := &Page{}
	pageFacts(p, d, r, e)
	o1 := &Object{}
	fillObject(o1, r, &inherit.Shape{}, place.Page, 0, o)
	o1.Layers = layerMembers(p, "")
	if o.Full {
		o1.Cells = rawCells(nil)
		o1.Origins = map[string]inherit.Origin{"box": boxOrigin()}
	}
	t, _, ok := transformOf(r, &inherit.Shape{})
	linkConnects(p, nil, map[uint32]*placed{1: {obj: o1, frame: place.Page, t: t, hasT: ok, sites: o1.Sites}}, 0)
	return nil, vsd.ErrNotImplemented
}

// BuildMaster builds the model of master m, read only, as BuildPage.
// Chunk M1.
func BuildMaster(d *doc.Document, r *inherit.Resolver, m *doc.MasterEntry, o Options) (*Page, error) {
	_, _, _, _ = d, r, m, o
	return nil, vsd.ErrNotImplemented
}
