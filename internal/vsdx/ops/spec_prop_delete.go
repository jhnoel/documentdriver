package ops

import (
	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// PropDeleteSpec is prop.delete: delete a shape data row; a row the master defines is hidden with Del="1" (the shape stops inheriting it).
// Designer-owned (the spec, its decoding and its rules); the op's chunk
// adds Run in run_prop_delete.go.
type PropDeleteSpec struct {
	At string `json:"at" doc:"A shape data row address ({prop-address}) or $name."`
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

// propDeleteRules are prop.delete's document-independent checks, in
// the order its check returns them. Chunk CC7.
var propDeleteRules = []string{
	"at must be a shape data row address or $name",
}

// propDeleteRefuses are prop.delete's document-dependent refusals,
// one per line. Chunk CC7.
var propDeleteRefuses = []string{
	"containers/lists/callouts: a container, list or callout (User.msvStructureType) is not edited in v1",
	"masters and stencils: at must name a page shape data row, not a master shape",
	"a data-linked shape data row",
}

// init registers the help data of prop.delete (chunk CC7).
func init() {
	registerHelp("prop.delete", propDeleteRules, propDeleteRefuses)
}
