// Package prop is the property codec table: for each writable property,
// how to read it (the same read half the read model uses), how to write
// it through an edit.Handle, and how to show it (the same form the views
// show). Set uses all three to keep the round-trip rule: writing the value
// shown is a no-op that touches no XML, and any other write must read
// back as the value requested or fail.
//
// The codec machinery (Codec, Set) is the edit core's
// (internal/ooxml/edit), shared with the docx side; the table of pptx
// codecs is here. It imports derive and the edit layer, never the read
// model.
package prop

import (
	"errors"

	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/edit"
)

// ErrInexpressible is returned when a value cannot be stored so that it
// reads back as requested: DrawingML cannot express it under the object's
// groups, or rounding to stored units changes what is shown.
var ErrInexpressible = derive.ErrInexpressible

// ErrNotDerived is returned when a property's current value cannot be
// read, so a write relative to it cannot be made.
var ErrNotDerived = errors.New("prop: current value not derived")

// Codec reads, writes and shows one property of type T of a slide
// object: the edit core's codec (core.Codec, with Name, Read, Write and
// Show) over pptx handles, read as derive values with their origin.
type Codec[T any] = core.Codec[*edit.Handle, T, derive.Val[T]]

// Set writes want unless the object already shows it (core.Set). When the
// current value shows as want, it returns without touching any XML.
// Otherwise it writes, re-reads every branch and fails with
// ErrInexpressible unless each shows want.
func Set[T any](h *edit.Handle, c Codec[T], want T) error {
	return core.Set(h, c, want, ErrInexpressible)
}

// check fails with ErrInexpressible unless every branch shows want
// (core.Check).
func check[T any](h *edit.Handle, c Codec[T], want T) error {
	return core.Check(h, c, want, ErrInexpressible)
}
