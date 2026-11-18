package ops

import (
	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// ObjectDeleteSpec is object.delete: remove an object (a group with its children) and every Connect row that names it.
// Designer-owned (the spec, its decoding and its rules); the op's chunk
// adds Run in run_object_delete.go.
type ObjectDeleteSpec struct {
	At string `json:"at" doc:"A shape address as read shows it (page0/s1), or $name."`
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

// objectDeleteRules are object.delete's document-independent checks,
// in the order its check returns them. Chunk CC7.
var objectDeleteRules = []string{
	"at must be a shape address or $name",
}

// objectDeleteRefuses are object.delete's document-dependent
// refusals, one per line. Chunk CC7.
var objectDeleteRefuses = []string{
	"containers/lists/callouts: a container, list or callout (User.msvStructureType) is not edited in v1",
	"a member of a container or list (Relationships DEPENDSON): its geometry and membership are not edited in v1",
	"masters and stencils: a master shape or a shape in a stencil cannot be edited",
	"a sub-shape a master instance inherits",
}

// init registers the help data of object.delete (chunk CC7).
func init() {
	registerHelp("object.delete", objectDeleteRules, objectDeleteRefuses)
}
