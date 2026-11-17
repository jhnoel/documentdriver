package read

import "github.com/jhnoel/documentdriver/internal/vsdx/vsd"

// Page reads one page (or master, for an address master<N>) as JSON in
// the layout pptx read --slide uses: base, page, name, units, size,
// orientation, background, background_page, layers, objects,
// connections, hazards, notes. Chunk R2.
func Page(input []byte, base, address string, o Options) ([]byte, error) {
	_, _, _, _ = input, base, address, o
	return nil, vsd.ErrNotImplemented
}
