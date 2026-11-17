package doc

import (
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// MasterEntry is one Master of masters.xml in stored order.
type MasterEntry struct {
	ID       uint32
	Name     string
	NameU    string
	UniqueID string
	BaseID   string
	Hidden   bool
	Part     string
}

// readMasters parses masters.xml into entries, as readPages does.
// Chunk D1.
func (d *Document) readMasters() error {
	return nil
}

// MasterContents returns the master's parsed contents, parsing on first
// use and keeping the tree for later pages. Chunk D2.
func (d *Document) MasterContents(id uint32) (*tree.Document, error) {
	_ = id
	return nil, vsd.ErrNotImplemented
}
