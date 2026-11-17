package doc

import "github.com/jhnoel/documentdriver/internal/ooxml/tree"

// PageEntry is one Page of pages.xml in stored order: its ID, names,
// background flags, its PageSheet node (in the pages.xml tree) and the
// part its Rel points at ("" with a note when the Rel does not resolve).
type PageEntry struct {
	ID         uint32
	Name       string
	NameU      string
	Background bool
	BackPage   uint32
	HasBack    bool
	PageSheet  *tree.Node
	Part       string
}

// readPages parses pages.xml (found through the document part's pages
// relationship) into entries; an entry whose ID does not parse or
// repeats an earlier one is left out with a note. Chunk D1.
func (d *Document) readPages() error {
	return nil
}
