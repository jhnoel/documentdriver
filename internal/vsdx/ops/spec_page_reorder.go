package ops

import (
	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// PageReorderSpec is page.reorder: move a page before or after another page in pages.xml order.
// Designer-owned (the spec, its decoding and its rules); the op's chunk
// adds Run in run_page_reorder.go.
type PageReorderSpec struct {
	At       string `json:"at"`
	To       string `json:"to"`
	Position string `json:"position" enum:"before,after"`
}

// DecodePlan implements cli.PlanDecoder for page.reorder.
func (s *PageReorderSpec) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	decodeFields(v, path, d, []planField{
		{Name: "at", Required: true, Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.At) }},
		{Name: "to", Required: true, Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.To) }},
		{Name: "position", Required: true, Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) {
			cli.DecodeEnum(dd, mv, fp, []string{"before", "after"}, &s.Position)
		}},
	})
}

// check reports the first rule page.reorder breaks: its message and its field
// relative to the operation, or false.
func (s *PageReorderSpec) check() (string, string, bool) {
	if !addressOK(s.At, vsd.KindPage) {
		return "at must be a page address or $name", "at", true
	}
	if !addressOK(s.To, vsd.KindPage) {
		return "to must be a page address or $name", "to", true
	}
	return "", "", false
}
