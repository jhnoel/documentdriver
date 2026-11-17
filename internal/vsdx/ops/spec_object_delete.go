package ops

import (
	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// ObjectDeleteSpec is object.delete: remove an object (a group with its children) and every Connect row that names it.
// Designer-owned (the spec, its decoding and its rules); the op's chunk
// adds Run in run_object_delete.go.
type ObjectDeleteSpec struct {
	At string `json:"at"`
}

// DecodePlan implements cli.PlanDecoder for object.delete.
func (s *ObjectDeleteSpec) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	decodeFields(v, path, d, []planField{
		{Name: "at", Required: true, Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.At) }},
	})
}

// check reports the first rule object.delete breaks: its message and its field
// relative to the operation, or false.
func (s *ObjectDeleteSpec) check() (string, string, bool) {
	return needAddress(s.At, "at", "a shape address", vsd.KindShape)
}
