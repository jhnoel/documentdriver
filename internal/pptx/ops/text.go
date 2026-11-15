package ops

import (
	"fmt"

	"github.com/jhnoel/documentdriver/internal/pptx/ops/edit"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/text"
)

// The text operations work on Handles.
var _ text.Object = (*edit.Handle)(nil)

// TextSet is text.set: the object's text body is made to show HTML, in
// the grammar the read view emits; a:bodyPr and a:lstStyle are kept, and
// paragraphs and runs the HTML leaves unchanged keep their bytes (see
// internal/pptx/ops/text). A shape without a text body gets one; connectors
// and pictures cannot hold text.
type TextSet struct {
	Slide, Target string
	HTML          string
}

// Name implements Op.
func (*TextSet) Name() string { return "text.set" }

// apply makes the target object's text body show the operation's HTML.
func (o *TextSet) apply(t *Tx) error {
	h, err := t.target(o.Slide, o.Target)
	if err != nil {
		return err
	}
	if err := text.SetWith(h, o.HTML, h.Rels()); err != nil {
		return fmt.Errorf("%s: %w", h.Ref(), err)
	}
	return nil
}

// TextReplace is text.replace: Find replaced by With in the object's text
// body, in the run where each match starts, keeping its formatting. More
// than one match is an error unless All is set.
type TextReplace struct {
	Slide, Target string
	Find, With    string
	All           bool
	// Occurrence picks one match, counting from 1; 0 means the only one.
	Occurrence int
}

// Name implements Op.
func (*TextReplace) Name() string { return "text.replace" }

// apply replaces the selected text in the target object's text body.
func (o *TextReplace) apply(t *Tx) error {
	h, err := t.target(o.Slide, o.Target)
	if err != nil {
		return err
	}
	if _, err := text.ReplaceText(h, o.Find, o.With, o.All, o.Occurrence); err != nil {
		return fmt.Errorf("%s: %w", h.Ref(), err)
	}
	return nil
}
