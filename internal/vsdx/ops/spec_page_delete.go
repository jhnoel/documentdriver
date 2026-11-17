package ops

import (
	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// PageDeleteSpec is page.delete: delete a page: its pages.xml entry, its part, Override and relationship.
// Designer-owned (the spec, its decoding and its rules); the op's chunk
// adds Run in run_page_delete.go.
type PageDeleteSpec struct {
	At string `json:"at"`
}

// DecodePlan implements cli.PlanDecoder for page.delete.
func (s *PageDeleteSpec) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	decodeFields(v, path, d, []planField{
		{Name: "at", Required: true, Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.At) }},
	})
}

// check reports the first rule page.delete breaks: its message and its field
// relative to the operation, or false.
func (s *PageDeleteSpec) check() (string, string, bool) {
	return needAddress(s.At, "at", "a page address", vsd.KindPage)
}
