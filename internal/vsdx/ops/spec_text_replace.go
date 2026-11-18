package ops

import (
	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// TextReplaceSpec is text.replace: replace exact text in a shape's text (at a shape) or in every shape of a page (at a page): select, with, and occurrence or all, as pptx's text.replace.
// Designer-owned (the spec, its decoding and its rules); the op's chunk
// adds Run in run_text_replace.go.
type TextReplaceSpec struct {
	At         string `json:"at" doc:"A shape or page address as read shows it (page0/s1 or page0), or $name."`
	Select     string `json:"select" doc:"Exact text to find, as read shows text."`
	With       string `json:"with" doc:"The replacement text."`
	Occurrence int    `json:"occurrence" plan:"optional" doc:"1-based: which match, counting in paint order; not with all."`
	All        bool   `json:"all" plan:"optional" doc:"Replace every match; not with occurrence."`
}

// DecodePlan implements cli.PlanDecoder for text.replace.
func (s *TextReplaceSpec) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	decodeFields(v, path, d, []planField{
		{Name: "at", Required: true, Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.At) }},
		{Name: "select", Required: true, Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.Select) }},
		{Name: "with", Required: true, Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.With) }},
		{Name: "occurrence", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.Occurrence) }},
		{Name: "all", Decode: func(mv *cli.Value, fp string, dd *cli.Decoder) { dd.Value(mv, fp, &s.All) }},
	})
}

// check reports the first rule text.replace breaks: its message and its field
// relative to the operation, or false.
func (s *TextReplaceSpec) check() (string, string, bool) {
	if !addressOK(s.At, vsd.KindShape, vsd.KindPage) {
		return "at must be a shape or page address or $name", "at", true
	}
	if s.Select == "" {
		return "select must not be empty", "select", true
	}
	if s.Occurrence < 0 {
		return "occurrence is 1 or more", "occurrence", true
	}
	if s.Occurrence > 0 && s.All {
		return "occurrence and all exclude each other", "all", true
	}
	return "", "", false
}

// textReplaceRules are text.replace's document-independent checks,
// in the order its check returns them. Chunk CC7.
var textReplaceRules = []string{
	"at must be a shape or page address or $name",
	"select must not be empty",
	"occurrence is 1 or more",
	"occurrence and all exclude each other",
}

// textReplaceRefuses are text.replace's document-dependent refusals,
// one per line. Chunk CC7.
var textReplaceRefuses = []string{
	"containers, lists and callouts are skipped by a page-wide replace",
	"containers/lists/callouts: a container, list or callout named directly at at (User.msvStructureType) is not edited in v1",
	"masters and stencils: a master shape (masters cannot be edited) or a shape in a stencil (stencils cannot be edited)",
	"text on a picture or other read-only kind (Foreign or guide shape)",
	"a match crossing a paragraph or line break or touching a field",
}

// init registers the help data of text.replace (chunk CC7).
func init() {
	registerHelp("text.replace", textReplaceRules, textReplaceRefuses)
}
