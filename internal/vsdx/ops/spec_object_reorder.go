package ops

import (
	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// ObjectReorderSpec is object.reorder: move an object in paint order within its parent: to "front", "back", or before or after another object of the same parent (to and position), as pptx's object.reorder.
// Designer-owned (the spec, its decoding and its rules); the op's chunk
// adds Run in run_object_reorder.go.
type ObjectReorderSpec struct {
	At       string `json:"at" doc:"A shape address as read shows it (page0/s1), or $name."`
	To       string `json:"to" doc:"front, back, or a shape address of the same parent, as read shows paint order."`
	Position string `json:"position" plan:"optional" enum:"before,after" doc:"Before or after the shape in to; with front or back it is refused."`
}

// DecodePlan implements cli.PlanDecoder for object.reorder.
func (s *ObjectReorderSpec) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	decodeFields(v, path, d, []planField{
		{Name: "at", Required: true, Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.At) }},
		{Name: "to", Required: true, Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.To) }},
		{Name: "position", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) {
			cli.DecodeEnum(dd, mv, fp, []string{"before", "after"}, &s.Position)
		}},
	})
}

// check reports the first rule object.reorder breaks: its message and its field
// relative to the operation, or false.
func (s *ObjectReorderSpec) check() (string, string, bool) {
	if !addressOK(s.At, vsd.KindShape) {
		return "at must be a shape address or $name", "at", true
	}
	if s.To != "front" && s.To != "back" && !addressOK(s.To, vsd.KindShape) {
		return "to must be front, back or a shape address", "to", true
	}
	if (s.To == "front" || s.To == "back") && s.Position != "" {
		return "position goes with a shape in to, not front or back", "position", true
	}
	if s.To != "front" && s.To != "back" && s.Position == "" {
		return "position (before or after) is needed with a shape in to", "position", true
	}
	return "", "", false
}

// objectReorderRules are object.reorder's document-independent
// checks, in the order its check returns them. Chunk CC7.
var objectReorderRules = []string{
	"at must be a shape address or $name",
	"to must be front, back or a shape address",
	"position goes with a shape in to, not front or back",
	"position (before or after) is needed with a shape in to",
}

// objectReorderRefuses are object.reorder's document-dependent
// refusals, one per line. Chunk CC7.
var objectReorderRefuses = []string{
	"containers/lists/callouts: a container, list or callout (User.msvStructureType) is not edited in v1",
	"masters and stencils: a master shape (masters cannot be edited) or a shape in a stencil cannot be edited",
	"reorder relative to an object of another parent",
	"reorder relative to itself",
}

// init registers the help data of object.reorder (chunk CC7).
func init() {
	registerHelp("object.reorder", objectReorderRules, objectReorderRefuses)
}
