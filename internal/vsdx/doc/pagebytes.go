package doc

import (
	"fmt"

	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// PageBytes returns the page part's original bytes as a string, and
// drops the part's cached bytes (Part.Forget as PageContents does) so
// a caller that visits pages one at a time holds one page's bytes at a
// time. The returned string stays valid after the part is forgotten.
// A page with no part, or a part with no original bytes, is an error
// the caller turns into unreadable_target. Chunk RM2.
func (d *Document) PageBytes(e *PageEntry) (string, error) {
	if d == nil || d.Pkg == nil || e == nil {
		return "", fmt.Errorf("vsdx: no page contents")
	}
	if e.Part == "" {
		return "", fmt.Errorf("vsdx: %s has no part", vsd.PageID(e.ID, 1))
	}
	pt, err := d.Pkg.Part(e.Part)
	if err != nil {
		return "", fmt.Errorf("vsdx: %s: %w", e.Part, err)
	}
	s, ok, err := pt.OriginalString()
	if err != nil {
		return "", fmt.Errorf("vsdx: %s: %w", e.Part, err)
	}
	if !ok {
		return "", fmt.Errorf("vsdx: %s has no original bytes", e.Part)
	}
	pt.Forget()
	return s, nil
}
