package doc

import (
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// PageContents parses the page's part and returns its tree; it is not
// kept, and the part's cached bytes are dropped (Part.Forget) once
// parsed. A missing or malformed part is an error the caller turns into
// unreadable_target. Chunk D2.
func (d *Document) PageContents(e *PageEntry) (*tree.Document, error) {
	_ = e
	return nil, vsd.ErrNotImplemented
}

// MasterContents returns the master's parsed contents, parsing on first
// use and keeping the tree for later pages. Chunk D2.
func (d *Document) MasterContents(id uint32) (*tree.Document, error) {
	_ = id
	_ = d.masters
	return nil, vsd.ErrNotImplemented
}
