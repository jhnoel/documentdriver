package ops

import (
	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// ObjectMoveSpec is object.move: translate an object by dx, dy (points, or the plan's units), as pptx's object.move.
// Designer-owned (the spec, its decoding and its rules); the op's chunk
// adds Run in run_object_move.go.
type ObjectMoveSpec struct {
	At string  `json:"at"`
	DX float64 `json:"dx" plan:"optional" default:"0"`
	DY float64 `json:"dy" plan:"optional" default:"0"`
}

// DecodePlan implements cli.PlanDecoder for object.move.
func (s *ObjectMoveSpec) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	decodeFields(v, path, d, []planField{
		{Name: "at", Required: true, Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.At) }},
		{Name: "dx", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.DX) }},
		{Name: "dy", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.DY) }},
	})
}

// check reports the first rule object.move breaks: its message and its field
// relative to the operation, or false.
func (s *ObjectMoveSpec) check() (string, string, bool) {
	return needAddress(s.At, "at", "a shape address", vsd.KindShape)
}
