package create

import (
	"time"

	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// Template returns the blank drawing's bytes, authored in code like
// xlsx's template, member for member equal to the golden members in
// testdata/vsdx/create/blank (designer-authored), with the content and
// relationship types from ctypes (S0) and the core properties' times
// from now (created and modified). Members are deflated with the fixed
// zip time, in a fixed order, so equal inputs give equal bytes.
// Chunk CT1.
func Template(now time.Time) ([]byte, error) {
	_ = now
	return nil, vsd.ErrNotImplemented
}
