package model

import (
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
)

// pageFacts fills the page's size (PageWidth, PageHeight), Orientation
// (from the size), PrintOrientation (PrintPageOrientation of the page
// sheet through its style chain), the drawing-scale note and Layers
// (Layer rows: Name, IX, Visible, Print, Lock) from its PageSheet.
// Chunk M6.
func pageFacts(p *Page, d *doc.Document, r *inherit.Resolver, e *doc.PageEntry) {
	_, _, _, _ = p, d, r, e
}

// layerMembers returns the layer addresses of a LayerMember value
// ("0;2"); an index with no layer row is left out with a note. Chunk M6.
func layerMembers(p *Page, v string) []string {
	_, _ = p, v
	return nil
}

// PageSheet returns a Page holding only the page-sheet facts of e (ID,
// names, background, size, orientations, layers, notes), without its
// shapes: the summary's page entries (read.Document, chunk R1). Chunk M6.
func PageSheet(d *doc.Document, r *inherit.Resolver, e *doc.PageEntry) *Page {
	p := &Page{}
	pageFacts(p, d, r, e)
	return p
}
