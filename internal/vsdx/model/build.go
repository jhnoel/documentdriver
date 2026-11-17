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
// page. Chunk M1 (walk, kinds, addresses, z), M2 (per-object values).
func BuildPage(d *doc.Document, r *inherit.Resolver, e *doc.PageEntry, o Options) (*Page, error) {
	_, _ = d, e
	p := &Page{}
	o1 := &Object{}
	fillObject(o1, r, &inherit.Shape{}, place.Page, 0, o)
	o1.Props, o1.Omitted = readProps(o1.ID, r, &inherit.Shape{})
	linkConnects(p, map[uint32]*Object{})
	return nil, vsd.ErrNotImplemented
}

// BuildMaster builds the model of master m, read only, as BuildPage.
// Chunk M1.
func BuildMaster(d *doc.Document, r *inherit.Resolver, m *doc.MasterEntry, o Options) (*Page, error) {
	_, _, _, _ = d, r, m, o
	return nil, vsd.ErrNotImplemented
}
