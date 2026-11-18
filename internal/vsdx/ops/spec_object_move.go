package ops

import (
	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// ObjectMoveSpec is object.move: translate an object by dx, dy (points, or the plan's units), as pptx's object.move.
// Designer-owned (the spec, its decoding and its rules); the op's chunk
// adds Run in run_object_move.go.
type ObjectMoveSpec struct {
	At string  `json:"at" doc:"A shape address as read shows it (page0/s1), or $name."`
	DX float64 `json:"dx" plan:"optional" default:"0" doc:"Translation in points, or the plan's units, as read's box moves."`
	DY float64 `json:"dy" plan:"optional" default:"0" doc:"Translation in points, or the plan's units, as read's box moves."`
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

// objectMoveRules are object.move's document-independent checks, in
// the order its check returns them. Chunk CC7.
var objectMoveRules = []string{
	"at must be a shape address or $name",
}

// objectMoveRefuses are object.move's document-dependent refusals,
// one per line. Chunk CC7.
var objectMoveRefuses = []string{
	"formula-bound cells: a cell bound to a formula vsdx does not evaluate",
	"containers/lists/callouts: a container, list or callout (User.msvStructureType) is not edited in v1",
	"a member of a container or list (Relationships DEPENDSON): its geometry and membership are not edited in v1",
	"scaled-page geometry: geometry is not edited on a page with a drawing scale",
	"masters and stencils: a master shape or a shape in a stencil cannot be edited",
	"both-ends-glued move: a connector glued at both ends cannot be moved (set from_glue or to_glue to none first)",
	"a connector whose ends coincide",
}

// init registers the help data of object.move (chunk CC7).
func init() {
	registerHelp("object.move", objectMoveRules, objectMoveRefuses)
}
