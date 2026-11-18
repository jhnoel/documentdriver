package ops

import (
	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// PageDeleteSpec is page.delete: delete a page: its pages.xml entry, its part, Override and relationship.
// Designer-owned (the spec, its decoding and its rules); the op's chunk
// adds Run in run_page_delete.go.
type PageDeleteSpec struct {
	At string `json:"at" doc:"A page address as read shows it (page0), or $name."`
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

// pageDeleteRules are page.delete's document-independent checks, in
// the order its check returns them. Chunk CC7.
var pageDeleteRules = []string{
	"at must be a page address or $name",
}

// pageDeleteRefuses are page.delete's document-dependent refusals,
// one per line. Chunk CC7.
var pageDeleteRefuses = []string{
	"deleting the last foreground page",
	"deleting a page another page uses as its background",
}

// init registers the help data of page.delete (chunk CC7).
func init() {
	registerHelp("page.delete", pageDeleteRules, pageDeleteRefuses)
}
