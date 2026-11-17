package edit

import "github.com/jhnoel/documentdriver/internal/vsdx/vsd"

// AddPage adds a page: its entry (emit.PageXML) in pages.xml after the
// page after (appended when ""), its part (emit.PageContentsXML) at the
// next free visio/pages/page<n>.xml with one Override and one
// relationship. It returns the page's address. Chunk E2.
func (w *Workspace) AddPage(entry func(relID string) ([]byte, error), after string) (string, error) {
	_, _ = entry, after
	return "", vsd.ErrNotImplemented
}

// RenamePage sets a page's Name and NameU; a name another page has
// (ignoring case) is refused. Chunk E2.
func (w *Workspace) RenamePage(page, name string) error {
	_, _ = page, name
	return vsd.ErrNotImplemented
}
