package ops

import (
	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// PropDeleteSpec is prop.delete: delete a shape data row; a row the master defines is hidden with Del="1" (the shape stops inheriting it).
// Designer-owned (the spec, its decoding and its rules); the op's chunk
// adds Run in run_prop_delete.go.
type PropDeleteSpec struct {
	At string `json:"at"`
}

// DecodePlan implements cli.PlanDecoder for prop.delete.
func (s *PropDeleteSpec) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	decodeFields(v, path, d, []planField{
		{Name: "at", Required: true, Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.At) }},
	})
}

// check reports the first rule prop.delete breaks: its message and its field
// relative to the operation, or false.
func (s *PropDeleteSpec) check() (string, string, bool) {
	return needAddress(s.At, "at", "a shape data row address", vsd.KindProp)
}
