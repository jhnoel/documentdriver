// Package edit is the vsdx edit layer (P2, needed by create first): the
// workspace of a plan transaction over a drawing, resolving addresses
// to handles on pages, shapes, cells, rows, text and Connect rows
// through the edit core (internal/ooxml/edit), declaring the write set,
// allocating shape and page IDs (max+1, across nested shapes) and
// adding parts (a page, docProps/custom.xml). Child order comes from the
// corpus-derived table (chunk S3), never a hand-written list.
package edit

import (
	"github.com/jhnoel/documentdriver/internal/plan"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// Workspace is a plan transaction's view of the drawing.
type Workspace struct {
	tx *plan.Tx
}

// Begin opens the drawing at path as a plan transaction. Chunk E1.
func Begin(path string, opts ...plan.Option) (*Workspace, error) {
	_, _ = path, opts
	return nil, vsd.ErrNotImplemented
}

// Verify checks the working copy against the declared write set (the
// plan.Workspace interface). Chunk E1.
func (w *Workspace) Verify() error {
	_ = w.tx
	return vsd.ErrNotImplemented
}

// NextShapeID returns the next free shape ID of a page: one more than
// the largest ID among all its shapes, nested ones included (notes §9:
// duplicate IDs make Visio repair the file). Chunk E1.
func (w *Workspace) NextShapeID(page string) (uint32, error) {
	_ = page
	return 0, vsd.ErrNotImplemented
}

// InsertShape inserts a Shape element (emit's bytes) as the last child
// of the page's Shapes, or of the group's Shapes when parent is a group
// address, creating the Shapes element through the core when absent,
// and declares the page part changed. It returns the new address.
// Chunk E1.
func (w *Workspace) InsertShape(page, parent string, frag []byte) (string, error) {
	_, _, _ = page, parent, frag
	return "", vsd.ErrNotImplemented
}

// InsertConnects appends Connect rows to the page's Connects, creating
// it after Shapes when absent. Chunk E1.
func (w *Workspace) InsertConnects(page string, frags [][]byte) error {
	_, _ = page, frags
	return vsd.ErrNotImplemented
}

// SetText replaces a shape's own Text and its Character and Paragraph
// sections with emit.TextXML's bytes (sections placed by the
// corpus-derived child order). Chunk E1.
func (w *Workspace) SetText(shape string, sections, text []byte) error {
	_, _, _ = shape, sections, text
	return vsd.ErrNotImplemented
}

// SetCells sets shape-level or page-sheet cells (name, V, U, F; an
// empty F removes the formula) on the addressed shape or page sheet,
// creating Cell elements in child order. Chunk E1.
func (w *Workspace) SetCells(at string, cells []Cell) error {
	_, _ = at, cells
	return vsd.ErrNotImplemented
}

// Cell is one cell a write sets.
type Cell struct {
	N, V, U, F string
}
