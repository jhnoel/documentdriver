package ops

import (
	"sync"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/ids"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
)

// OpDef is one entry of the op table: the contract name, a constructor
// of its spec and its summary for help. Whether the op is implemented is
// not stored here: it is whether its spec type has a Run method
// (Implemented), which the op's own run_<name>.go adds, so an op chunk
// never edits this file.
type OpDef struct {
	Name    string
	Summary string
	New     func() Spec
}

// Runner is what an implemented op's spec does: run against the edit
// workspace, given the plan's aliases so far, its units and the id
// source, and report the address it created (for as), the changed and
// deleted addresses, and a stale-values detail ("" when it left no value
// stale). apply calls it inside the transaction.
type Runner interface {
	Run(ws *edit.Workspace, aliases map[string]string, units string, ids ids.Source) (as string, changed []string, deleted []string, stale string, err error)
}

// Implemented reports whether the op has a Run (its spec is a Runner).
// apply refuses an op that is not with operation_failed, and help lists
// only the implemented ones.
func (d *OpDef) Implemented() bool {
	_, ok := d.New().(Runner)
	return ok
}

// Ops is the op table: every op of the contract draft (section 3), so
// that an op not implemented yet is operation_failed, never an unknown
// op (docs/contract.md, section 2). The specs of the later ops live in
// spec_<name>.go (designer-owned). An op is
// implemented when its run_<name>.go gives its spec a Run method; this
// table does not change when one lands.
var Ops = []OpDef{
	{Name: "object.add", Summary: "add a shape (from a master, or a rect or ellipse) or a straight connector", New: func() Spec { return &ObjectAddSpec{} }},
	{Name: "object.set", Summary: "set an object's box, rotation, flips, name, fill, line, ends or glue", New: func() Spec { return &ObjectSetSpec{} }},
	{Name: "text.set", Summary: "replace a shape's text with HTML", New: func() Spec { return &TextSetSpec{} }},
	{Name: "prop.add", Summary: "add a shape data row", New: func() Spec { return &PropAddSpec{} }},
	{Name: "prop.set", Summary: "set a shape data row's value", New: func() Spec { return &PropSetSpec{} }},
	{Name: "page.add", Summary: "add a page", New: func() Spec { return &PageAddSpec{} }},
	{Name: "page.rename", Summary: "rename a page", New: func() Spec { return &PageRenameSpec{} }},
	{Name: "page.set", Summary: "set a page's size or orientation", New: func() Spec { return &PageSetSpec{} }},
	{Name: "object.move", Summary: "translate an object by dx, dy", New: func() Spec { return &ObjectMoveSpec{} }},
	{Name: "object.delete", Summary: "delete an object and the Connect rows that name it", New: func() Spec { return &ObjectDeleteSpec{} }},
	{Name: "object.reorder", Summary: "move an object in paint order within its parent", New: func() Spec { return &ObjectReorderSpec{} }},
	{Name: "text.replace", Summary: "replace exact text in a shape or in every shape of a page", New: func() Spec { return &TextReplaceSpec{} }},
	{Name: "prop.delete", Summary: "delete a shape data row (hide a master's with Del)", New: func() Spec { return &PropDeleteSpec{} }},
	{Name: "page.delete", Summary: "delete a page", New: func() Spec { return &PageDeleteSpec{} }},
	{Name: "page.reorder", Summary: "move a page before or after another", New: func() Spec { return &PageReorderSpec{} }},
}

// index is the op table's index, built once.
var index = sync.OnceValue(func() *cli.OpIndex[OpDef] {
	return cli.NewOpIndex(Ops, func(d *OpDef) string { return d.Name }, func(d *OpDef) any { return d.New() })
})

// Lookup returns the op named name.
func Lookup(name string) (*OpDef, bool) { return index().Lookup(name) }
