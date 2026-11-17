package ops

import (
	"sync"

	"github.com/jhnoel/documentdriver/internal/cli"
)

// OpDef is one entry of the op table: the contract name, a constructor
// of its spec, its summary for help, and Pending while it is in the
// grammar but not implemented (apply refuses it with operation_failed).
type OpDef struct {
	Name    string
	Summary string
	New     func() Spec
	Pending bool
}

// Ops is the op table: the ops create needs first (docs/vsdx-design.md,
// Create), then the rest of P2 as they are designed. Chunk O0 fills the
// rules; each op's run chunk clears Pending.
var Ops = []OpDef{
	{Name: "object.add", Summary: "add a shape (from a master, or a rect or ellipse) or a straight connector", New: func() Spec { return &ObjectAddSpec{} }, Pending: true},
	{Name: "object.set", Summary: "set an object's box, rotation, flips, name, fill, line, ends or glue", New: func() Spec { return &ObjectSetSpec{} }, Pending: true},
	{Name: "text.set", Summary: "replace a shape's text with HTML", New: func() Spec { return &TextSetSpec{} }, Pending: true},
	{Name: "prop.add", Summary: "add a shape data row", New: func() Spec { return &PropAddSpec{} }, Pending: true},
	{Name: "prop.set", Summary: "set a shape data row's value", New: func() Spec { return &PropSetSpec{} }, Pending: true},
	{Name: "page.add", Summary: "add a page", New: func() Spec { return &PageAddSpec{} }, Pending: true},
	{Name: "page.rename", Summary: "rename a page", New: func() Spec { return &PageRenameSpec{} }, Pending: true},
	{Name: "page.set", Summary: "set a page's size or orientation", New: func() Spec { return &PageSetSpec{} }, Pending: true},
}

// index is the op table's index, built once.
var index = sync.OnceValue(func() *cli.OpIndex[OpDef] {
	return cli.NewOpIndex(Ops, func(d *OpDef) string { return d.Name }, func(d *OpDef) any { return d.New() })
})

// Lookup returns the op named name.
func Lookup(name string) (*OpDef, bool) { return index().Lookup(name) }
