package ops

import (
	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// TextReplaceSpec is text.replace: replace exact text in a shape's text (at a shape) or in every shape of a page (at a page): select, with, and occurrence or all, as pptx's text.replace.
// Designer-owned (the spec, its decoding and its rules); the op's chunk
// adds Run in run_text_replace.go.
type TextReplaceSpec struct {
	At         string `json:"at"`
	Select     string `json:"select"`
	With       string `json:"with"`
	Occurrence int    `json:"occurrence" plan:"optional"`
	All        bool   `json:"all" plan:"optional"`
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
