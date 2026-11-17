package doc

import (
	"fmt"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// PageContents parses the page's part and returns its tree; it is not
// kept, and the part's cached bytes are dropped (Part.Forget) once
// parsed. A missing or malformed part is an error the caller turns into
// unreadable_target. Chunk D2.
func (d *Document) PageContents(e *PageEntry) (*tree.Document, error) {
	if d == nil || d.Pkg == nil || e == nil {
		return nil, fmt.Errorf("vsdx: no page contents")
	}
	if e.Part == "" {
		return nil, fmt.Errorf("vsdx: %s has no part", vsd.PageID(e.ID, 1))
	}
	doc, err := d.loadContents(e.Part, vsd.ElemPageContents)
	if err != nil {
		return nil, err
	}
	if pt, err := d.Pkg.Part(e.Part); err == nil {
		pt.Forget()
	}
	return doc, nil
}

// MasterContents returns the master's parsed contents, parsing on first
// use and keeping the tree for later pages. Chunk D2.
func (d *Document) MasterContents(id uint32) (*tree.Document, error) {
	if d == nil || d.Pkg == nil {
		return nil, fmt.Errorf("vsdx: %s: no such master", vsd.MasterID(id))
	}
	if doc, ok := d.masters[id]; ok && doc != nil {
		return doc, nil
	}
	entry, ok := d.Master(id)
	if !ok {
		return nil, fmt.Errorf("vsdx: %s: no such master", vsd.MasterID(id))
	}
	if entry.Part == "" {
		return nil, fmt.Errorf("vsdx: %s has no part", vsd.MasterID(id))
	}
	doc, err := d.loadContents(entry.Part, vsd.ElemMasterContents)
	if err != nil {
		return nil, err
	}
	if d.masters == nil {
		d.masters = map[uint32]*tree.Document{}
	}
	d.masters[id] = doc
	return doc, nil
}

// loadContents parses the part at part with partTree and reports an
// error when its root is not local in a Visio main namespace.
func (d *Document) loadContents(part, local string) (*tree.Document, error) {
	doc, err := partTree(d.Pkg, part)
	if err != nil {
		return nil, fmt.Errorf("vsdx: %s: %w", part, err)
	}
	if !isMain(doc.Root(), local) {
		return nil, fmt.Errorf("vsdx: %s is not a %s part", part, local)
	}
	return doc, nil
}
