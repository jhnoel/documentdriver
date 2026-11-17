// Package doc opens a Visio package: it finds the document part through
// the package relationships, lists the pages (pages.xml) and masters
// (masters.xml) in stored order, reads the style sheets and font names
// of document.xml, and hands out a page's or master's parsed contents.
// Page contents are parsed with tree.ParseString on the part's original
// string, never Part.Tree, so a reader that visits pages one at a time
// holds one page tree at a time (docs/vsdx-design.md, Performance).
package doc

import (
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// Kind is the package kind, from the document part's content type.
type Kind uint8

// Package kinds. Stencils are read (masters only) and never edited.
const (
	Drawing  Kind = iota + 1 // .vsdx, .vsdm
	Template                 // .vstx, .vstm
	Stencil                  // .vssx, .vssm
)

// Document is an open Visio package: the parts lists, the style
// sheets and the notes about what could not be read. Masters' trees
// are parsed on first use and kept (they are small and shared by every
// page); page trees are not kept.
type Document struct {
	Pkg       *opc.Package
	Kind      Kind
	Macro     bool
	Part      string // the document part, /visio/document.xml
	Pages     []PageEntry
	Masters   []MasterEntry
	Styles    map[uint32]*StyleSheet
	FaceNames map[string]string // Font cell value -> face name
	// Recalc is true when docProps/custom.xml holds RecalcDocument true
	// (read shows values_stale).
	Recalc  bool
	Notes   []Note
	masters map[uint32]*tree.Document
}

// Note is something the reader left out: the nearest address or part
// name, and why (contract section 4).
type Note struct {
	At, Reason string
}

// Open reads the package structure of p: the document part (refused as
// an error, which the caller reports as invalid_document, when the
// package has no Visio document relationship or its part is
// malformed), then pages.xml, masters.xml, document.xml's StyleSheets
// and FaceNames and the custom properties. A malformed pages or masters
// part is a note, not an error (contract section 10). Uses ctypes
// constants generated in chunk S0. Chunk D1.
func Open(p *opc.Package) (*Document, error) {
	d := &Document{Pkg: p, masters: map[uint32]*tree.Document{}}
	if err := d.readPages(); err != nil {
		return nil, err
	}
	if err := d.readMasters(); err != nil {
		return nil, err
	}
	d.readStyles(nil)
	d.readCustom()
	return nil, vsd.ErrNotImplemented
}

// Page returns the page entry with stored ID id, and false when there is
// none.
func (d *Document) Page(id uint32) (*PageEntry, bool) {
	for i := range d.Pages {
		if d.Pages[i].ID == id {
			return &d.Pages[i], true
		}
	}
	return nil, false
}

// Master returns the master entry with stored ID id, and false when
// there is none.
func (d *Document) Master(id uint32) (*MasterEntry, bool) {
	for i := range d.Masters {
		if d.Masters[i].ID == id {
			return &d.Masters[i], true
		}
	}
	return nil, false
}
